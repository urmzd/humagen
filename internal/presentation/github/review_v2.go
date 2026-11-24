package github

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/urmzd/saige/flow/address"
	transport "github.com/urmzd/saige/flow/connectors/github"
)

// ReviewScope pins the review to the exact source revision it inspected.
// The host fills it from the pinned head (ReviewReport.Pin); a model's own
// value is replaced.
type ReviewScope struct {
	Commit string `json:"commit"`
}

// ReviewLocation identifies the changed line or range a finding concerns.
type ReviewLocation struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Side      string `json:"side,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
}

var fullCommit = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// foundBy is empty or the agent that found a finding, with its session label:
// security-reviewer or security-reviewer#a.
var foundBy = regexp.MustCompile(`^([a-z0-9][a-z0-9._-]{0,63}(#[A-Za-z0-9._-]{1,32})?)?$`)

// The bounds on a v2 report's free text, in characters. The text itself is
// free: it is escaped when rendered, so it cannot change the layout.
const (
	maxSummary     = 1500
	maxComments    = 10
	maxComment     = 500
	maxTitle       = 150
	maxWhy         = 600
	maxSuggestion  = 600
	maxPrompt      = 1000
	maxEvidence    = 8
	maxObservation = 600
	maxSource      = 500
	maxPath        = 400
	maxChange      = 8000
	maxRelated     = 10
)

func validateV2(r ReviewReport) error {
	if strings.TrimSpace(r.Title) == "" || len(r.Title) > 200 {
		return fmt.Errorf("the review needs a title of at most 200 characters")
	}
	switch r.Verdict {
	case "approve", "comment", "request_changes":
	default:
		return fmt.Errorf("the verdict must be approve, comment or request_changes")
	}
	if r.Scope == nil || !fullCommit.MatchString(r.Scope.Commit) {
		return fmt.Errorf("the review is not pinned to the reviewed commit")
	}
	if len(r.Findings) > 100 {
		return fmt.Errorf("the review has more than 100 findings")
	}
	if err := bounded("the summary", r.Summary, maxSummary); err != nil {
		return err
	}
	if len(r.Comments) > maxComments {
		return fmt.Errorf("the review has more than %d comments", maxComments)
	}
	for _, c := range r.Comments {
		if err := bounded("a comment", c, maxComment); err != nil {
			return err
		}
	}
	if len(r.Related) > maxRelated {
		return fmt.Errorf("the review has more than %d related links", maxRelated)
	}
	for _, l := range r.Related {
		if err := bounded("a related link's title", l.Title, maxTitle); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for i, f := range r.Findings {
		n := i + 1
		if strings.TrimSpace(f.ID) == "" || len(f.ID) > 100 || seen[f.ID] {
			return fmt.Errorf("finding %d needs an id no other finding has", n)
		}
		seen[f.ID] = true
		if !foundBy.MatchString(f.FoundBy) {
			return fmt.Errorf("finding %d: found_by names an agent, with an optional #label", n)
		}
		for _, field := range []struct {
			name, text string
			max        int
		}{{"title", f.Title, maxTitle}, {"why", f.Why, maxWhy}, {"suggestion", f.Suggestion, maxSuggestion}, {"prompt", f.Prompt, maxPrompt}} {
			if err := bounded(fmt.Sprintf("finding %d's %s", n, field.name), field.text, field.max); err != nil {
				return err
			}
		}
		if len(f.Evidence) > maxEvidence {
			return fmt.Errorf("finding %d has more than %d pieces of evidence", n, maxEvidence)
		}
		for _, e := range f.Evidence {
			if err := bounded(fmt.Sprintf("finding %d's evidence", n), e.Observation, maxObservation); err != nil {
				return err
			}
			if err := bounded(fmt.Sprintf("finding %d's evidence source", n), e.Source, maxSource); err != nil {
				return err
			}
		}
		if f.Change != nil && utf8.RuneCountInString(*f.Change) > maxChange {
			return fmt.Errorf("finding %d's change is longer than %d characters", n, maxChange)
		}
		if l := f.Location; l != nil {
			if err := validLocation(l); err != nil {
				return fmt.Errorf("finding %d: %w", n, err)
			}
		}
	}
	return nil
}

// bounded refuses text longer than max characters.
func bounded(what, text string, max int) error {
	if utf8.RuneCountInString(text) > max {
		return fmt.Errorf("%s is longer than %d characters", what, max)
	}
	return nil
}

// validLocation checks a location is a repository path and a line in it.
// A path is shown in code and in a link, so it may not hold a backtick, a
// control character or a step out of the repository.
func validLocation(l *ReviewLocation) error {
	if l.Path == "" || len(l.Path) > maxPath || strings.HasPrefix(l.Path, "/") || strings.ContainsAny(l.Path, "`\\") || !utf8.ValidString(l.Path) {
		return fmt.Errorf("the location's path is not a repository path")
	}
	for _, segment := range strings.Split(l.Path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("the location's path is not a repository path")
		}
	}
	for _, r := range l.Path {
		if unicode.IsControl(r) {
			return fmt.Errorf("the location's path is not a repository path")
		}
	}
	if l.Line < 1 || l.StartLine < 0 || l.StartLine > l.Line || l.Side != "" && l.Side != "LEFT" && l.Side != "RIGHT" {
		return fmt.Errorf("the location's lines are not a range in the file")
	}
	return nil
}

// NativeReview prepares a real pull request review at t, with the event
// ReviewEvent chooses from the report, policy and facts. A finding whose
// location is a line of the diff gets its own line comment; any other is in
// the body alone, with its change. The transport checks every anchor against
// the actual diff again. An APPROVE is returned prepared as a comment would
// be: only the transport's review sink posts it, after the host's MayApprove
// hook agrees, and a no posts it as a comment.
func NativeReview(report ReviewReport, files []transport.ContextFile, t Target, policy ReviewPolicy, facts ReviewFacts) (transport.Review, error) {
	r, err := nativeReview(report, files, t)
	if err != nil {
		return transport.Review{}, err
	}
	event := ReviewEvent(report, policy, facts)
	if event == "APPROVE" {
		r.Event = "COMMENT"
	} else {
		r.Event = event
	}
	r, err = transport.PrepareReview(r, files)
	if err != nil {
		return transport.Review{}, err
	}
	r.Event = event
	return r, nil
}

// RecoveredReview reproduces a review already posted, so a retried delivery
// matches it instead of posting another. disposition is the posted event: a
// comment or a change request is reproduced as posted, and an approval only
// for a report whose verdict approves. The transport prepares it and never
// submits a new one.
func RecoveredReview(report ReviewReport, files []transport.ContextFile, t Target, disposition string) (transport.Review, error) {
	r, err := nativeReview(report, files, t)
	if err != nil {
		return transport.Review{}, err
	}
	switch {
	case disposition == "APPROVE" && report.Verdict == "approve":
		r.Event = disposition
		return r, nil
	case disposition == "COMMENT", disposition == "REQUEST_CHANGES":
		r.Event = disposition
	}
	return transport.PrepareReview(r, files)
}

func nativeReview(report ReviewReport, files []transport.ContextFile, t Target) (transport.Review, error) {
	if report.Format != ReviewFormatV2 {
		return transport.Review{}, fmt.Errorf("github: native reviews require %s", ReviewFormatV2)
	}
	if err := ValidateReview(report); err != nil {
		return transport.Review{}, err
	}
	t.Head = report.Scope.Commit
	r := transport.Review{CommitID: report.Scope.Commit, Event: ReviewEvent(report, ReviewPolicy{}, ReviewFacts{})}
	anchored := map[string]bool{}
	for _, f := range ordered(report.Findings) {
		if f.Location == nil {
			continue
		}
		l := f.Location
		side := l.Side
		if side == "" {
			side = "RIGHT"
		}
		comment := transport.ReviewComment{FindingID: f.ID, Path: l.Path, Body: lineComment(f, t.Repo), Line: l.Line, Side: side, StartLine: l.StartLine, StartSide: side, Change: f.Change}
		if !onDiff(comment, r.CommitID, files) {
			continue
		}
		anchored[f.ID] = true
		r.Comments = append(r.Comments, comment)
	}
	r.Body = renderReview(report, t, anchored)
	if len(r.Body) > MaxBodyChars-2048 {
		return transport.Review{}, fmt.Errorf("the review is too long for one GitHub review; shorten the findings and keep their evidence")
	}
	return r, nil
}

// ApproveWhenClean is the one review policy that may approve. Any other
// value, "never" and "" among them, approves nothing.
const ApproveWhenClean = "when_clean"

// DefaultBlockOn is the severity a policy that names none blocks on: a
// finding that must be fixed requests changes.
const DefaultBlockOn = "error"

// ReviewPolicy is how an agent's reviews decide, as the host narrowed it:
// the agent's own policy, then the repository's settings, which only make it
// stricter. The zero value never approves and blocks on DefaultBlockOn.
type ReviewPolicy struct {
	// Approve is ApproveWhenClean, or anything else for never.
	Approve string
	// BlockOn is the least severity that requests changes: error, warning
	// or info. "" is DefaultBlockOn, and an unknown one blocks on every
	// finding.
	BlockOn string
}

// ReviewFacts are what the host read for itself about a review, never what
// the model wrote. The zero value approves nothing.
type ReviewFacts struct {
	// ChecksRead says the host read the reviewed commit's checks, all of
	// them, into Checks.
	ChecksRead bool
	Checks     []transport.CommitCheck
	// Own reports the connector's own check for the run under review,
	// still in progress while the review is decided, which is skipped.
	// Nil skips nothing.
	Own func(transport.CommitCheck) bool
	// Author is the pull request's author and App the app's own login. Both
	// must be known and differ: the app never approves its own pull request.
	// Draft is a pull request still in draft, which is never approved.
	Author, App string
	Draft       bool
	// Trusted says the run was an automatic review, a review request, or
	// started by a person the client maps.
	Trusted bool
}

// checksPassed reports whether the host read the commit's checks and
// saige's AllPassed accepts them: at least one check that Own does not
// skip, every such check passed, and fewer than the read's bound.
func (f ReviewFacts) checksPassed() bool {
	return f.ChecksRead && transport.AllPassed(f.Checks, f.Own)
}

// authorNotApp reports whether the pull request's author is known and is
// not the app.
func (f ReviewFacts) authorNotApp() bool {
	login := func(s string) string { return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "[bot]")) }
	return login(f.Author) != "" && login(f.App) != "" && login(f.Author) != login(f.App)
}

// blocks reports whether a finding is at or above the policy's severity.
func (p ReviewPolicy) blocks(f ReviewFinding) bool {
	on := p.BlockOn
	if on == "" {
		on = DefaultBlockOn
	}
	limit, ok := severities[on]
	if !ok {
		limit = len(severities)
	}
	rank, ok := severities[f.Severity]
	return !ok || rank <= limit
}

// ReviewEvent is the GitHub event a review is posted with. It reads the
// report's verdict and findings, the host's policy and the facts the host
// read, and nothing else: no text the model wrote chooses the event.
//
// APPROVE needs every guard at once: the policy is ApproveWhenClean, the
// verdict approves, no finding is at or above the policy's severity, the
// host read at least one check and all passed, the author is not the app,
// the pull request is not a draft, and the run was trusted to approve. REQUEST_CHANGES is a verdict that
// requests changes, or any finding at or above the severity. Anything else
// is COMMENT.
func ReviewEvent(report ReviewReport, policy ReviewPolicy, facts ReviewFacts) string {
	blocked := report.Verdict == "request_changes"
	for _, f := range report.Findings {
		blocked = blocked || policy.blocks(f)
	}
	switch {
	case blocked:
		return "REQUEST_CHANGES"
	case policy.Approve == ApproveWhenClean && report.Verdict == "approve" && facts.checksPassed() && facts.authorNotApp() && !facts.Draft && facts.Trusted:
		return "APPROVE"
	}
	return "COMMENT"
}

// onDiff reports whether the transport would post comment on a line of the
// diff, by asking it to prepare a review of that comment alone. The
// transport owns how a diff is read.
func onDiff(comment transport.ReviewComment, commit string, files []transport.ContextFile) bool {
	probe, err := transport.PrepareReview(transport.Review{CommitID: commit, Event: "COMMENT", Comments: []transport.ReviewComment{comment}}, files)
	return err == nil && len(probe.Comments) == 1
}

// lineComment is a finding as a colleague leaves it on a line: what is
// wrong, why it matters and what to do, then its severity and who found
// it, small. The transport adds the committable suggestion under it.
func lineComment(f ReviewFinding, repo string) string {
	label := severityLabel[f.Severity]
	if who := foundByName(f.FoundBy); who != "" {
		label += " · found by " + who
	}
	return fmt.Sprintf("**%s**\n%s\n\n<sub>%s</sub>", prose(f.Title), findingText(f, repo), label)
}

// parseAddress reads a run's channel.
func parseAddress(s string) (address.Address, error) { return address.Parse(s) }
