package platform

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/session"
)

// Reviewing an alert.
//
// An alert sits on what raised it: a health alert on the answer that failed
// its checks. Review is a separate step a person takes. It opens a session
// under the alert's session, with the workspace's entry agent, whose first
// turn recaps the alert. The agent is sent a brief (alertBrief) with that
// first turn: the alert, the checks that failed and the run's trace. The
// session's agent keeps its own transcript, so a follow-up question still
// has the brief.

// Bounds on what one brief carries from a run.
const (
	briefText       = 2000 // runes of the question, and of the answer
	briefError      = 300  // runes of one tool error
	briefErrorsEach = 3    // distinct errors quoted for one tool
	briefToolsMax   = 10   // tools listed with their errors
)

// reviewLabel names the review session of one alert under its source.
func reviewLabel(id string) string { return "review-" + id }

// ReviewAlert opens the session where who reviews an alert and starts its
// recap. A second call opens the same session and starts nothing: started
// says which it was.
func (p *Platform) ReviewAlert(ctx context.Context, who auth.Principal, id string) (view SessionView, started bool, err error) {
	source, err := p.sourceAlertSession(ctx, who, id)
	if err != nil {
		return SessionView{}, false, err
	}
	a, err := p.Alerts.Alert(who.Tenant, id)
	if err != nil {
		return SessionView{}, false, threads.ErrNotFound
	}
	if err = p.EnsureBuiltins(ctx, who.Tenant); err != nil {
		return SessionView{}, false, err
	}
	agent := p.cfg.EntryAgent
	rec, err := p.Sessions.Open(ctx, who.Tenant, session.Open{Parent: source.ID, Agent: agent, Label: reviewLabel(a.ID), Channel: wire.Channel(who.Tenant)})
	if err != nil {
		return SessionView{}, false, err
	}
	if th, err := p.Threads.Get(who.Tenant, rec.ID); err == nil && th.Session != nil && th.Session.SourceAlert == a.ID {
		if head, err := p.Threads.Summary(who.Tenant, th.ID); err == nil && head.Messages > 0 {
			view, err = p.ReadSession(ctx, who, th.ID, "", "", false)
			return view, false, err
		}
	}
	view, err = p.StartSession(ctx, who, SessionInput{SourceAlert: a.ID, Parent: source.ID, Agent: agent, Label: reviewLabel(a.ID), Text: fmt.Sprintf("Recap the alert %q.", firstWords(a.Title, 200))})
	return view, err == nil, err
}

// alertBrief is what an agent is sent about the alert a session was opened
// from, for the person who asked: the alert, the checks that failed, and the
// run that raised it, with the tool errors in its trace. It is "" when that
// person may no longer read the alert. The run's own words are quoted, never
// followed.
func (p *Platform) alertBrief(ctx context.Context, tenant, subject, id string) string {
	if subject == "" || id == "" {
		return ""
	}
	who := p.invitationReader(tenant, subject)
	if current, ok := ctx.Value(sessionReaderKey{}).(auth.Principal); ok && current.Tenant == tenant && current.Subject == subject {
		who = current
	}
	a, err := p.Alerts.Alert(tenant, id)
	if err != nil || !who.Can(auth.AlertsRead) || !p.Seer(who).Alert(a) {
		return ""
	}
	var b strings.Builder
	b.WriteString("<alert>\n")
	b.WriteString("The person opened this session to review the alert below. Start with a short recap: which run, which check failed, and the evidence. For failed tools, say which tools, how many times, and the errors. Then say what to check next. Use only these facts; say when something is unknown.\n\n")
	fmt.Fprintf(&b, "Title: %s\n", a.Title)
	if a.Body != "" {
		fmt.Fprintf(&b, "Details: %s\n", clipRunes(a.Body, briefText))
	}
	fmt.Fprintf(&b, "Kind: %s\nRaised: %s\n", a.Kind, a.At.UTC().Format(time.RFC3339))
	if a.RunID != "" {
		if run, err := p.Runs.Get(tenant, a.RunID); err == nil && p.Seer(who).Run(run) {
			p.briefRun(&b, tenant, a, run)
		}
	}
	b.WriteString("</alert>")
	return b.String()
}

// briefRun writes what the brief says of the run that raised an alert.
func (p *Platform) briefRun(b *strings.Builder, tenant string, a alerts.Alert, run runs.Run) {
	fmt.Fprintf(b, "Run: %s by @%s, %s, in session %s\n", run.ID, run.Agent, run.Status, run.Thread)
	if link := p.answerLink(run); link != "" {
		fmt.Fprintf(b, "Link to the answer: %s\nEnd the recap with this link, as [the answer](%s).\n", link, link)
	}
	if online, err := p.Evals.Online(tenant, run.ID); err == nil {
		var failed []string
		for _, v := range online.Verdicts {
			if !v.Passed {
				failed = append(failed, fmt.Sprintf("- %s: %s", v.Check, v.Reason))
			}
		}
		if len(failed) > 0 {
			fmt.Fprintf(b, "Failed checks:\n%s\n", strings.Join(failed, "\n"))
		}
	}
	if events, err := p.Journal.Events(tenant, []string{run.ID}); err == nil {
		briefTools(b, events)
	}
	fmt.Fprintf(b, "<question quoted=\"true\">\n%s\n</question>\n", clipRunes(run.Text, briefText))
	if run.Answer != "" {
		fmt.Fprintf(b, "<answer quoted=\"true\">\n%s\n</answer>\n", clipRunes(run.Answer, briefText))
	}
	if run.Error != "" {
		fmt.Fprintf(b, "Run error: %s\n", clipRunes(run.Error, briefError))
	}
}

// briefTools writes the tool calls in a run's trace that failed: each tool,
// how many times, and its distinct errors.
func briefTools(b *strings.Builder, events []event.Envelope) {
	type failures struct {
		name   string
		count  int
		errors []string
	}
	var order []*failures
	byName := map[string]*failures{}
	calls, failed := 0, 0
	seen := map[string]bool{}
	for _, e := range events {
		done, ok := e.Data.(event.ToolDone)
		if !ok || (e.ID != "" && seen[e.ID]) {
			continue
		}
		seen[e.ID] = true
		calls++
		if done.Error == "" {
			continue
		}
		failed++
		name := cmp.Or(done.Name, "unnamed tool")
		f := byName[name]
		if f == nil {
			f = &failures{name: name}
			byName[name] = f
			order = append(order, f)
		}
		f.count++
		msg := clipRunes(strings.Join(strings.Fields(done.Error), " "), briefError)
		if len(f.errors) < briefErrorsEach && !slices.Contains(f.errors, msg) {
			f.errors = append(f.errors, msg)
		}
	}
	if calls == 0 {
		return
	}
	fmt.Fprintf(b, "Tool calls in the run's trace: %d, %d failed\n", calls, failed)
	for i, f := range order {
		if i == briefToolsMax {
			fmt.Fprintf(b, "- and %d more tools\n", len(order)-i)
			break
		}
		times := "times"
		if f.count == 1 {
			times = "time"
		}
		quoted := make([]string, len(f.errors))
		for j, e := range f.errors {
			quoted[j] = fmt.Sprintf("%q", e)
		}
		fmt.Fprintf(b, "- %s failed %d %s: %s\n", f.name, f.count, times, strings.Join(quoted, "; "))
	}
}

// answerLink is the console address of a run's answer, in its session.
func (p *Platform) answerLink(run runs.Run) string {
	if !strings.HasPrefix(run.Thread, "s_") {
		return ""
	}
	path := "/messages/" + url.PathEscape(run.Thread) + "?" + url.Values{"message": {run.ID}}.Encode()
	if base := strings.TrimRight(p.cfg.PublicURL, "/"); base != "" {
		return base + path
	}
	return path
}
