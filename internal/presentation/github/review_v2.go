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
// security-reviewer, security-reviewer#a, or skill-runner#<skill> for a
// loaded skill, whose name is up to 64 characters.
var foundBy = regexp.MustCompile(`^([a-z0-9][a-z0-9._-]{0,63}(#[A-Za-z0-9._-]{1,64})?)?$`)

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

// agentName is an agent's name as a review's mark carries it.
var agentName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// agentMarkOpen and agentMarkClose enclose the agent's name in its mark.
const (
	agentMarkOpen  = "<!-- humagen.review.agent "
	agentMarkClose = " -->"
)

// AgentMark is the hidden line a native review carries to name the agent
// that wrote it, so the host dismisses only that agent's stale reviews. It is
// "" for a name that is not an agent's.
func AgentMark(agent string) string {
	if !agentName.MatchString(agent) {
		return ""
	}
	return agentMarkOpen + agent + agentMarkClose
}

// WithAgentMark is a review's body with the agent's mark on a line of its
// own at its end. Anything in body that would read as a mark, such as a
// code change quoted in a finding, is written so that it does not: only
// the host's line names the agent. A name that is not an agent's adds no
// mark.
func WithAgentMark(body, agent string) string {
	mark := AgentMark(agent)
	if mark == "" {
		return body
	}
	return strings.ReplaceAll(body, agentMarkOpen, "&lt;"+agentMarkOpen[1:]) + "\n\n" + mark
}

// postedTail is what may follow the host's mark in a posted review: the
// session footer, then the two markers the transport adds when it posts a
// review (its run marker and its payload digest).
var postedTail = regexp.MustCompile("^(" + regexp.QuoteMeta(sessionFooterOpen) + "[^\n]*" + regexp.QuoteMeta(sessionFooterClose) + ")?" +
	"(\n\n<!-- humagen-review:[0-9a-f]{64} -->\n\n<!-- humagen-review-payload:[0-9a-f]{64} -->)?$")

// ReviewAgent is the agent named by the mark WithAgentMark put at the end of
// a review's body, followed by nothing but the session footer and the
// transport's markers. A mark anywhere else names nobody, and a body
// without one is "".
func ReviewAgent(body string) string {
	i := strings.LastIndex(body, agentMarkOpen)
	if i < 2 || body[i-2:i] != "\n\n" {
		return ""
	}
	rest := body[i+len(agentMarkOpen):]
	end := strings.Index(rest, agentMarkClose)
	if end < 0 {
		return ""
	}
	name, tail := rest[:end], rest[end+len(agentMarkClose):]
	if !agentName.MatchString(name) || !postedTail.MatchString(tail) {
		return ""
	}
	return name
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
	// Author is the pull request's author and App the app's own login. Both
	// must be known and differ: the app never approves its own pull request.
	// Draft is a pull request still in draft, which is never approved.
	Author, App string
	Draft       bool
	// AuthorID is the pull request author's GitHub user ID. People are
	// told apart by ID, never by login, which its owner may change: 0 is
	// unknown and approves nothing.
	AuthorID int64
	// Trusted says the run was an automatic review, or asked by a person
	// the client maps. Asker is that person's login, "" for an automatic
	// review no person resumed, and AskerID their GitHub user ID.
	Trusted bool
	Asker   string
	AskerID int64
	// CommitsRead says the host read the pull request's commits, every one
	// linked to a GitHub account, and AskerCommitted that the asker
	// authored or committed one of them.
	CommitsRead    bool
	AskerCommitted bool
	// CoAuthorsRead says the host read the Co-authored-by trailers of
	// those commits, and AskerCoAuthored that one names the asker.
	CoAuthorsRead   bool
	AskerCoAuthored bool
}

// checksPassed reports whether the host read the commit's checks and they
// show CI that passed. The app's own check runs, from this run or an
// earlier one and whatever their name, are left out: they say only that a
// review ran. Of the rest, none may be pending or failed (only success,
// neutral and skipped pass), at least one must have concluded success, and
// the read must be below saige's bound.
func (f ReviewFacts) checksPassed() bool {
	app := loginOf(f.App)
	if !f.ChecksRead || app == "" || len(f.Checks) >= transport.MaxCommitChecks {
		return false
	}
	success := false
	for _, c := range f.Checks {
		if loginOf(c.App) == app {
			continue
		}
		if c.Status != "completed" {
			return false
		}
		switch c.Conclusion {
		case "success":
			success = true
		case "neutral", "skipped":
		default:
			return false
		}
	}
	return success
}

// loginOf is a login or app slug in one form: "Humagen[bot]" and "humagen"
// are both "humagen".
func loginOf(s string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "[bot]"))
}

// peopleKnown reports whether the people an approval depends on are told
// apart by GitHub user ID, and the asker, if any, is a second pair of eyes:
// not the pull request's author, under any login, and the author, committer
// or co-author of none of its commits, as the host read them.
func (f ReviewFacts) peopleKnown() bool {
	if f.AuthorID <= 0 {
		return false
	}
	if f.Asker == "" && f.AskerID == 0 {
		return true
	}
	return f.AskerID > 0 && f.AskerID != f.AuthorID && loginOf(f.Asker) != loginOf(f.Author) &&
		f.CommitsRead && !f.AskerCommitted && f.CoAuthorsRead && !f.AskerCoAuthored
}

// authorNotApp reports whether the pull request's author is known and is
// not the app.
func (f ReviewFacts) authorNotApp() bool {
	return loginOf(f.Author) != "" && loginOf(f.App) != "" && loginOf(f.Author) != loginOf(f.App)
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
// host read checks from CI and all passed, at least one with success, the
// author is not the app, the pull request is not a draft, and the run was
// trusted to approve, with the author known by ID and any asker neither the
// author nor a writer of the pull request's commits. REQUEST_CHANGES is a verdict that
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
	case policy.Approve == ApproveWhenClean && report.Verdict == "approve" && facts.checksPassed() && facts.authorNotApp() && !facts.Draft && facts.Trusted && facts.peopleKnown():
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
