package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	presentation "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/saige/flow/address"
	transport "github.com/urmzd/saige/flow/connectors/github"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/session"
)

const reviewNeedsCorrection = "Review needs correction: the commit it reviewed is not recorded."
const reviewEvidenceUnavailable = "Review evidence is unavailable to this audience. An authorized summary is required."

// reviewText projects claims into references the host can substantiate. Model
// output never supplies a trusted flag. Clearing an unsupported reference makes
// every existing renderer label the evidence unverified.
func (p *Platform) reviewText(ctx context.Context, run runs.Run, text string, who *auth.Principal, cutoff time.Time) string {
	var report presentation.ReviewReport
	if json.Unmarshal([]byte(presentation.ReviewJSON(text)), &report) != nil || report.Format != presentation.ReviewFormatV2 {
		return text
	}
	if len(text) > 128<<10 || len(report.Findings) > 100 {
		return reviewNeedsCorrection
	}
	events, err := p.Journal.Events(run.Tenant, []string{run.ID})
	if err != nil {
		events = nil
	}
	// Historical reads must not acquire evidence created after this answer.
	// The completion timestamp is authoritative; UpdatedAt can change later.
	cutoff, cutoffSeq := reviewCutoff(events, cutoff)
	head := pinnedHead(run.Tag())
	if head == "" {
		head = readHead(events, cutoff, cutoffSeq)
	}
	// The host pins the reviewed commit; the model never supplies it, and
	// a value it wrote anyway is replaced.
	if head == "" {
		return reviewNeedsCorrection
	}
	report.Pin(head)
	tree, treeErr := p.Runs.Tree(run.Tenant, run.ID)
	byID := map[string]runs.Run{run.ID: run}
	if treeErr == nil {
		for _, child := range tree {
			byID[child.ID] = child
		}
	}
	// Continuations and explicitly shared sessions must have been read by this
	// run. A current grant alone cannot promote a model's invented reference.
	readRuns := map[string]bool{}
	deniedSessions := map[string]bool{}
	loadedSessions := map[string][]string{}
	for _, e := range events {
		done, ok := e.Data.(event.ToolDone)
		if !ok || done.Name != "session_read" || done.Error != "" || e.Time.IsZero() || cutoff.IsZero() || e.Time.After(cutoff) || cutoffSeq != 0 && e.Seq > cutoffSeq {
			continue
		}
		var args struct {
			Session string `json:"session"`
		}
		if json.Unmarshal(done.Arguments, &args) != nil || args.Session == "" {
			continue
		}
		if !p.reviewSessionAccess(ctx, run.Tenant, run.Thread, args.Session) {
			deniedSessions[args.Session] = true
			continue
		}
		ids, loaded := loadedSessions[args.Session]
		if !loaded {
			if len(loadedSessions) >= 32 {
				continue
			}
			ids, err = p.Sessions.Runs(ctx, run.Tenant, args.Session, 200)
			loadedSessions[args.Session] = ids
			if err != nil {
				continue
			}
		}
		read := reviewReadRunIDs(done.Result)
		for _, id := range ids {
			if !read[id] {
				continue
			}
			candidate, err := p.Runs.Get(run.Tenant, id)
			if err != nil || candidate.Thread != args.Session {
				continue
			}
			byID[id], readRuns[id] = candidate, true
		}
	}
	cache := map[string][]event.Envelope{run.ID: events}
	destination := p.RunPlace(run.Tenant, run).Project
	checkAuthority := func(candidate runs.Run) bool {
		if who != nil {
			if who.Tenant != run.Tenant || !p.Seer(*who).Run(candidate) {
				return false
			}
			if th, err := p.Threads.Get(run.Tenant, candidate.Thread); err == nil && th.Session != nil && !p.SeesSession(*who, candidate.Thread) {
				return false
			}
			proofs, err := p.SourceEvidence.ForRun(run.Tenant, candidate.ID)
			if err != nil {
				return false
			}
			for _, proof := range proofs {
				if !p.citationEvidenceVisible(*who, proof) {
					return false
				}
			}
			return true
		}
		if p.RunPlace(run.Tenant, candidate).Project != destination || p.publishesPrivately(candidate) {
			return false
		}
		if candidate.ID == run.ID {
			return !p.RunRequiresReviewedSummary(ctx, candidate, destination)
		}
		return !p.runNeedsSummary(ctx, candidate, destination)
	}
	authority := map[string]bool{}
	authorized := func(candidate runs.Run) bool {
		if allowed, ok := authority[candidate.ID]; ok {
			return allowed
		}
		allowed := checkAuthority(candidate)
		authority[candidate.ID] = allowed
		return allowed
	}
	if !authorized(run) {
		return reviewEvidenceUnavailable
	}
	descends := func(candidate runs.Run) bool {
		for depth := 0; depth <= 64; depth++ {
			if candidate.ID == run.ID {
				return true
			}
			var ok bool
			candidate, ok = byID[candidate.Parent]
			if !ok {
				return false
			}
		}
		return false
	}
	getEvents := func(candidate runs.Run) []event.Envelope {
		if found, ok := cache[candidate.ID]; ok {
			return found
		}
		found, err := p.Journal.Events(run.Tenant, []string{candidate.ID})
		if err != nil {
			found = nil
		}
		cache[candidate.ID] = found
		return found
	}
	count := 0
	for i := range report.Findings {
		for j := range report.Findings[i].Evidence {
			claim := &report.Findings[i].Evidence[j]
			if deniedSessions[claim.Session] {
				return reviewEvidenceUnavailable
			}
			count++
			verified := false
			if count <= 256 && !cutoff.IsZero() && authorized(run) {
				for _, candidate := range byID {
					if claim.Session == "" && candidate.ID != run.ID || claim.Session != "" && candidate.Thread != claim.Session || !descends(candidate) && !readRuns[candidate.ID] {
						continue
					}
					if !authorized(candidate) {
						return reviewEvidenceUnavailable
					}
					if candidate.ID != run.ID && !p.sessionDescendantOf(run.Tenant, candidate.Thread, run.Thread) && !readRuns[candidate.ID] {
						continue
					}
					for _, e := range getEvents(candidate) {
						if e.Time.IsZero() || e.Time.After(cutoff) || candidate.ID == run.ID && cutoffSeq != 0 && e.Seq > cutoffSeq {
							continue
						}
						if done, ok := e.Data.(event.ToolDone); ok && claim.Step != "" && (done.ID == claim.Step || e.ID == claim.Step) && (claim.Source == "" || strings.Contains(done.Result, claim.Source) || strings.Contains(string(done.Arguments), claim.Source)) {
							verified = true
						}
						if done, ok := e.Data.(event.Completed); ok && claim.Step == "" && claim.Session != "" && done.Text != "" && (claim.Source == "" || strings.Contains(done.Text, claim.Source)) {
							verified = true
						}
					}
				}
			}
			if !verified {
				if claim.Step != "" || claim.Session != "" {
					claim.Source = "Unverified evidence"
					claim.Observation = "The cited evidence could not be verified for this audience."
				}
				claim.Step, claim.Session = "", ""
				if claim.Source == "" {
					claim.Source = "Unverified evidence"
				}
			}
		}
	}
	data, err := json.Marshal(report)
	if err != nil {
		return reviewNeedsCorrection
	}
	return string(data)
}

func reviewReadRunIDs(result string) map[string]bool {
	out := map[string]bool{}
	var entries []struct {
		Run string `json:"run_id"`
	}
	if json.Unmarshal([]byte(result), &entries) == nil {
		for _, entry := range entries {
			if entry.Run != "" {
				out[entry.Run] = true
			}
		}
		return out
	}
	// session_read's summary mode prefixes each entry with its sequence and
	// run identity. References appearing inside model text do not count.
	for _, line := range strings.Split(result, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || !strings.HasPrefix(fields[0], "[") || !strings.HasSuffix(fields[0], "]") || !strings.HasPrefix(fields[1], "run=") {
			continue
		}
		if _, err := strconv.ParseUint(strings.Trim(fields[0], "[]"), 10, 64); err != nil {
			continue
		}
		id := strings.TrimPrefix(fields[1], "run=")
		if id != "" {
			out[id] = true
		}
	}
	return out
}

func (p *Platform) reviewEvent(ctx context.Context, e event.Envelope, who *auth.Principal) event.Envelope {
	var text string
	switch body := e.Data.(type) {
	case event.Completed:
		text = body.Text
	case event.Stopped:
		text = body.Text
	default:
		return e
	}
	if !strings.Contains(text, presentation.ReviewFormatV2) {
		return e
	}
	run, err := p.Runs.Get(e.Tenant, e.Run)
	if err != nil {
		text = reviewNeedsCorrection
	} else {
		text = p.reviewText(ctx, run, text, who, e.Time)
	}
	switch body := e.Data.(type) {
	case event.Completed:
		body.Text = text
		e.Data = body
	case event.Stopped:
		body.Text = text
		e.Data = body
	}
	return e
}

// reviewCutoff is when a run's answer was final: its first completion or
// stop, no later than cutoff when cutoff is set, and that event's sequence.
func reviewCutoff(events []event.Envelope, cutoff time.Time) (time.Time, uint64) {
	var cutoffSeq uint64
	for _, e := range events {
		if (e.Type == event.TypeCompleted || e.Type == event.TypeStopped) && !e.Time.IsZero() && (cutoff.IsZero() || e.Time.Before(cutoff)) {
			cutoff = e.Time
		}
		if (e.Type == event.TypeCompleted || e.Type == event.TypeStopped) && (cutoffSeq == 0 || e.Seq < cutoffSeq) {
			cutoffSeq = e.Seq
		}
	}
	return cutoff, cutoffSeq
}

// readHead is the pull request head a run read with github_read_context
// before its answer was final, for a run whose tag pinned none.
func readHead(events []event.Envelope, cutoff time.Time, cutoffSeq uint64) string {
	head := ""
	for _, e := range events {
		done, ok := e.Data.(event.ToolDone)
		if !ok || done.Name != "github_read_context" || done.Error != "" || cutoff.IsZero() || e.Time.After(cutoff) || cutoffSeq != 0 && e.Seq > cutoffSeq {
			continue
		}
		var result struct {
			Subject struct {
				Head struct {
					SHA string `json:"sha"`
				} `json:"head"`
			} `json:"subject"`
		}
		if json.Unmarshal([]byte(done.Result), &result) == nil && result.Subject.Head.SHA != "" {
			head = result.Subject.Head.SHA
		}
	}
	return head
}

// reviewedHead is the commit a run's review is about: the head the host
// pinned when it started, else the one it read before it answered.
func (p *Platform) reviewedHead(run runs.Run) string {
	if head := pinnedHead(run.Tag()); head != "" {
		return head
	}
	events, err := p.Journal.Events(run.Tenant, []string{run.ID})
	if err != nil {
		return ""
	}
	cutoff, seq := reviewCutoff(events, time.Time{})
	return readHead(events, cutoff, seq)
}

// pinnedHead is the pull request head the host pinned when a run on GitHub
// started: the event's head, else its channel's, "" when it has none.
func pinnedHead(tag event.Envelope) string {
	if head := wire.SurfaceOf(tag).GitHubHeadSHA; transport.ValidSHA(head) {
		return head
	}
	if a, err := address.Parse(tag.Channel); err == nil {
		if head, _ := a.Param("head_sha"); transport.ValidSHA(head) {
			return head
		}
	}
	return ""
}

// asksReview reports whether the run v answers for may post a native
// review: one the host saved as asked for a review, one that called
// start_review, or one from before the host saved a shape.
func (p *Platform) asksReview(c clients.Client, v transport.View) (bool, error) {
	run, err := p.Runs.Get(c.Tenant, v.RunID)
	if err != nil {
		return false, errors.New("review context is not available")
	}
	if wire.ShapeOf(run.Tag()) != wire.ShapeReply {
		return true, nil
	}
	// A reply becomes a review when the run called start_review, which
	// its journal holds once its answer is recorded.
	if err := p.reviewRecorded(run.Tenant, run.ID, 0); err != nil {
		return false, err
	}
	shape, err := p.runShape(run)
	return shape == wire.ShapeReview, err
}

func (p *Platform) reviewView(c clients.Client, v transport.View) (string, error) {
	if !strings.Contains(v.Text, presentation.ReviewFormatV2) {
		return v.Text, nil
	}
	run, err := p.Runs.Get(c.Tenant, v.RunID)
	current, enabled := p.currentInstallation(c)
	if err != nil || v.Tenant != c.Tenant || !enabled || !current.Routes(run.Agent) {
		return "", errors.New("review context is not available")
	}
	if err := p.reviewRecorded(run.Tenant, run.ID, 0); err != nil {
		return "", err
	}
	return p.reviewText(context.Background(), run, v.Text, nil, time.Time{}), nil
}

// Observer and egress are independent consumers. Retrying until completion is
// recorded prevents a valid review from being permanently downgraded by lag.
func (p *Platform) reviewRecorded(tenant, id string, seq uint64) error {
	events, err := p.Journal.Events(tenant, []string{id})
	if err != nil {
		return errors.New("review evidence is temporarily unavailable")
	}
	for _, e := range events {
		if (e.Type == event.TypeCompleted || e.Type == event.TypeStopped) && (seq == 0 || e.Seq == seq) {
			return nil
		}
	}
	return errors.New("review evidence is not recorded yet")
}

func (p *Platform) reviewPublication(ctx context.Context, e event.Envelope) (event.Envelope, error) {
	var text string
	switch body := e.Data.(type) {
	case event.Completed:
		text = body.Text
	case event.Stopped:
		text = body.Text
	}
	if strings.Contains(text, presentation.ReviewFormatV2) {
		if err := p.reviewRecorded(e.Tenant, e.Run, e.Seq); err != nil {
			return e, err
		}
		if _, err := p.Runs.Get(e.Tenant, e.Run); err != nil {
			return e, errors.New("review context is not available")
		}
	}
	return p.reviewEvent(ctx, e, nil), nil
}

func (p *Platform) reviewSessionAccess(ctx context.Context, tenant, reader, target string) bool {
	root := func(id string) string {
		for depth := 0; depth <= 64; depth++ {
			r, err := p.Sessions.Get(ctx, tenant, id)
			if err != nil {
				return ""
			}
			if !strings.HasPrefix(r.Key.Parent, "s_") {
				return r.ID
			}
			id = r.Key.Parent
		}
		return ""
	}
	a, b := root(reader), root(target)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	grants, ok := p.Sessions.(session.AccessStore)
	if !ok {
		return false
	}
	allowed, err := grants.MayRead(ctx, tenant, target, reader)
	return err == nil && allowed
}
