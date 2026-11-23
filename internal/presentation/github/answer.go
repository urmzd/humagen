package github

import (
	"encoding/json"
	"net/url"
	"strings"

	transport "github.com/urmzd/saige/flow/connectors/github"
)

// RenderComment renders a run snapshot as the comment Humagen posts on an
// issue or pull request. A structured review renders as RenderReview does.
// Anything else is the answer alone: no tool calls, no reasoning and no
// "changed:" lines for the helpers the agent called, which are the
// session's trace and stay in the console. While the run works, a one-line
// status stands in for the answer; a failure, a stop, and a question a
// person must answer are still said. session is the console's address of
// the run's session, linked in a small footer; empty leaves the footer out.
func RenderComment(v transport.View, session string) string {
	if isReview(v) {
		return RenderReview(v)
	}
	clean := transport.View{
		Agent: v.Agent, RunID: v.RunID, Version: v.Version, Channel: v.Channel, Tenant: v.Tenant,
		Status: v.Status, Error: v.Error, Reason: v.Reason,
		Question: v.Question, Answered: v.Answered, Waiting: v.Waiting,
		Text: v.Text, Citations: v.Citations,
		Seq: v.Seq, At: v.At,
	}
	if q := v.Question; q != nil && q.Kind == "delegation" {
		// Waiting on a helper is part of the trace: the reader sees that
		// the agent is working, not whom it asked.
		clean.Question, clean.Status = nil, transport.StatusRunning
	}
	if v.Status == transport.StatusDone && strings.TrimSpace(v.Text) != "" {
		// A finished answer is the answer alone.
		clean.Agent = ""
	}
	footer := sessionFooter(session)
	return clamp(transport.RenderDefault(clean), MaxBodyChars-len(footer)) + footer
}

// isReview reports whether v is, or is becoming, a structured review.
func isReview(v transport.View) bool {
	text := ReviewJSON(v.Text)
	var report ReviewReport
	structured := json.Unmarshal([]byte(text), &report) == nil && IsReviewFormat(report.Format)
	pending := strings.HasPrefix(text, "{") && NamesReviewFormat(text)
	return structured || pending || (strings.HasPrefix(text, "{") && !v.Terminal())
}

// sessionFooter is the small link under an answer to its session in the
// console, where the trace is. Empty for an address that is not a plain
// http(s) URL.
func sessionFooter(session string) string {
	u, err := url.Parse(session)
	if session == "" || err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return ""
	}
	dest := strings.NewReplacer("(", "%28", ")", "%29", "<", "%3C", ">", "%3E", "|", "%7C", " ", "%20", "\n", "%0A", "\r", "%0D").Replace(u.String())
	return "\n\n<sub>[View the session in Humagen ↗](" + dest + ")</sub>"
}
