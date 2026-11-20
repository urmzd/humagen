// Package egressguard keeps a refused write to a surface from becoming a
// storm.
//
// A client's connector writes a run's message to its surface (a GitHub
// comment, a Slack message) and, when the write fails, leaves the message
// pending so a later flush writes it. The connector knows nothing of why a
// write failed, so a write the surface will never accept was repeated on every
// flush, for as long as the row was pending. This package decides what a
// failure means and what follows from it:
//
//   - a permanent refusal stops delivery of that one message, once, and is
//     recorded where a restart finds it;
//   - a rate limit pauses every write of the client until the time the
//     provider gives;
//   - a transient failure backs off, per message and per client, with jitter
//     and a cap;
//   - enough consecutive refused writes open a circuit for the client.
//
// It owns no rendering and calls no provider. A provider says what a response
// means through a Classifier; the guard is the HTTP transport those calls go
// through, and the delivery-state store the connector keeps its pending
// messages in.
package egressguard

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Class is what a failed call means for the calls after it.
type Class string

const (
	// Permanent is a refusal no retry changes: a missing permission, a
	// resource that is gone, a body the provider will not take.
	Permanent Class = "permanent"
	// RateLimited is the provider asking for a pause.
	RateLimited Class = "rate_limited"
	// Transient is the provider or the network having a bad moment.
	Transient Class = "transient"
	// Exhausted is a transient failure repeated until the guard gave up.
	Exhausted Class = "exhausted"
)

// Refusal is one failed call, reduced to what is safe to log and to keep. It
// never holds a response body: a provider's classifier fills it from named
// fields and headers only.
type Refusal struct {
	Class    Class  `json:"class"`
	Provider string `json:"provider"`
	Method   string `json:"method"`
	// Path is the request path, without its query.
	Path   string `json:"path"`
	Status int    `json:"status,omitempty"`
	// Message and DocumentationURL are the provider's own words for the
	// refusal, length-capped.
	Message          string `json:"message,omitempty"`
	DocumentationURL string `json:"documentation_url,omitempty"`
	RequestID        string `json:"request_id,omitempty"`
	// The rate-limit headers, as sent.
	RetryAfter         string `json:"retry_after,omitempty"`
	RateLimitLimit     string `json:"ratelimit_limit,omitempty"`
	RateLimitRemaining string `json:"ratelimit_remaining,omitempty"`
	RateLimitReset     string `json:"ratelimit_reset,omitempty"`
	RateLimitResource  string `json:"ratelimit_resource,omitempty"`
	// Until is when the provider says a retry may succeed. Zero when it gave
	// no time.
	Until time.Time `json:"until,omitzero"`
	At    time.Time `json:"at,omitzero"`
	// Attempts is how many times the message was tried, on an Exhausted one.
	Attempts int `json:"attempts,omitempty"`
}

// Error describes the refusal in one line.
func (r *Refusal) Error() string { return r.String() }

// String is the one-line form used in logs and in what people are shown.
func (r Refusal) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s %s: ", r.Provider, r.Method, r.Path)
	if r.Status != 0 {
		fmt.Fprintf(&b, "%d %s", r.Status, http.StatusText(r.Status))
	} else {
		b.WriteString("no response")
	}
	if r.Message != "" {
		b.WriteString(": " + r.Message)
	}
	if r.Class == Exhausted {
		fmt.Fprintf(&b, " (gave up after %d attempts)", r.Attempts)
	}
	if r.RequestID != "" {
		b.WriteString(" (request " + r.RequestID + ")")
	}
	return b.String()
}

// Attrs is the refusal as log attributes.
func (r Refusal) Attrs() []any {
	out := []any{"class", string(r.Class), "method", r.Method, "path", r.Path, "status", r.Status}
	for _, kv := range [][2]string{
		{"message", r.Message}, {"documentation_url", r.DocumentationURL}, {"request_id", r.RequestID},
		{"retry_after", r.RetryAfter}, {"ratelimit_limit", r.RateLimitLimit}, {"ratelimit_remaining", r.RateLimitRemaining},
		{"ratelimit_reset", r.RateLimitReset}, {"ratelimit_resource", r.RateLimitResource},
	} {
		if kv[1] != "" {
			out = append(out, kv[0], kv[1])
		}
	}
	return out
}

// Blocked is a call the guard did not send.
type Blocked struct {
	Provider string
	// Reason says which rule held the call.
	Reason string
	// Until is when the rule lets a call through again. Zero for a message
	// whose delivery has stopped.
	Until time.Time
	// Last is the refusal that set the rule, when there was one.
	Last *Refusal
}

func (b *Blocked) Error() string {
	s := b.Provider + ": not sent: " + b.Reason
	if !b.Until.IsZero() {
		s += " until " + b.Until.UTC().Format(time.RFC3339)
	}
	if b.Last != nil {
		s += " (last refusal: " + b.Last.String() + ")"
	}
	return s
}

// Classifier says what a provider's response means.
type Classifier interface {
	// Provider names the surface in logs: "github", "slack".
	Provider() string
	// Write reports whether a request changes something on the surface.
	Write(*http.Request) bool
	// Classify returns nil for a call that succeeded, and otherwise the
	// refusal. It may replace the response's body, and must leave one the
	// caller can still read.
	Classify(req *http.Request, resp *http.Response, now time.Time) *Refusal
}

// Health is what a client's guard would tell an operator.
type Health struct {
	// Healthy is false while writes are held: by a rate limit or an open
	// circuit.
	Healthy bool `json:"healthy"`
	// State is "ok", "rate_limited" or "circuit_open".
	State string `json:"state"`
	// Until is when writes resume.
	Until time.Time `json:"until,omitzero"`
	// Consecutive is the number of refused writes in a row.
	Consecutive int `json:"consecutive,omitempty"`
	// Last is the refusal that set the state.
	Last *Refusal `json:"last,omitempty"`
}

// Message is the health as one sentence for a person.
func (h Health) Message() string {
	until := h.Until.UTC().Format(time.RFC3339)
	last := ""
	if h.Last != nil {
		last = fmt.Sprintf(" The last refusal was %d", h.Last.Status)
		if h.Last.Message != "" {
			last += ": " + h.Last.Message
		}
		last += "."
	}
	switch h.State {
	case "rate_limited":
		return "The provider rate limited this connection. Writes are paused until " + until + "." + last
	case "circuit_open":
		return fmt.Sprintf("The provider refused %d writes in a row. Writes are paused until %s.%s Check the connection's permissions.", h.Consecutive, until, last)
	}
	return "Writes are being accepted."
}
