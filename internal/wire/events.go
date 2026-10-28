package wire

import (
	"encoding/json"
	"fmt"

	"github.com/urmzd/humagen/fiber/pkg/event"
)

// IsTerminal reports whether a type reports an attempt ending: completed,
// failed or stopped. A failure may still be retried; the ledger decides.
func IsTerminal(t string) bool {
	return t == event.TypeCompleted || t == event.TypeFailed || t == event.TypeStopped
}

// IsParked reports whether a type reports a run, or a run it called, waiting
// on a person.
func IsParked(t string) bool { return t == event.TypeParked || t == event.TypeChildParked }

// IsActive reports whether a type reports a run accepted, started or resumed.
func IsActive(t string) bool {
	return t == event.TypeAccepted || t == event.TypeStarted || t == event.TypeResumed
}

// IsLifecycle reports whether a type is one of a run's lifecycle types.
func IsLifecycle(t string) bool {
	switch t {
	case event.TypeAccepted, event.TypeStarted, event.TypeParked, event.TypeResumed,
		event.TypeCompleted, event.TypeFailed, event.TypeStopped, event.TypeChildParked:
		return true
	}
	return false
}

// Lifecycle is every lifecycle body read as one: the fields a body of its
// type does not carry are empty.
type Lifecycle struct {
	Attempt  int
	Error    string
	Terminal bool
	Reason   string
	By       string
	Text     string
	Usage    *event.RunUsage
	Question *event.Question
	Answer   *event.Answer
}

// LifecycleOf reads a lifecycle event's body. Any other type is an error.
func LifecycleOf(e event.Envelope) (Lifecycle, error) {
	switch b := e.Data.(type) {
	case event.Accepted:
		return Lifecycle{Attempt: b.Attempt}, nil
	case event.Started:
		return Lifecycle{Attempt: b.Attempt}, nil
	case event.Parked:
		return Lifecycle{Attempt: b.Attempt, Reason: b.Reason, Question: b.Question}, nil
	case event.Resumed:
		return Lifecycle{Attempt: b.Attempt, Answer: b.Answer}, nil
	case event.Completed:
		return Lifecycle{Attempt: b.Attempt, Text: b.Text, Usage: b.Usage}, nil
	case event.Failed:
		return Lifecycle{Attempt: b.Attempt, Error: b.Error, Terminal: b.Terminal}, nil
	case event.Stopped:
		return Lifecycle{Attempt: b.Attempt, Reason: b.Reason, By: b.By, Text: b.Text, Usage: b.Usage}, nil
	case event.ChildParked:
		q := b.Question
		return Lifecycle{Question: &q}, nil
	}
	return Lifecycle{}, fmt.Errorf("wire: %s is not a lifecycle event", e.Type)
}

// UsageOf reads a usage event's body: the call it reports, or the child run
// whose spending it adds. Any other type is an error.
func UsageOf(e event.Envelope) (*event.UsageCall, *event.UsageChild, error) {
	switch b := e.Data.(type) {
	case event.UsageCall:
		return &b, nil, nil
	case event.UsageChild:
		return nil, &b, nil
	}
	return nil, nil, fmt.Errorf("wire: %s is not a usage event", e.Type)
}

// CitationOf reads a citation event's body.
func CitationOf(e event.Envelope) (event.Citation, error) {
	b, ok := e.Data.(event.Citation)
	if !ok {
		return event.Citation{}, fmt.Errorf("wire: %s is not a citation", e.Type)
	}
	return b, nil
}

// ChildQuestionOf reads the body of a run.child.parked.v1 event.
func ChildQuestionOf(e event.Envelope) (event.ChildParked, error) {
	b, ok := e.Data.(event.ChildParked)
	if !ok {
		return event.ChildParked{}, fmt.Errorf("wire: %s is not a child question", e.Type)
	}
	return b, nil
}

// BodyOf decodes data as the body of type typ. Data that does not decode is
// kept as Unknown, so whoever reads it sees it is not the body it claims.
func BodyOf(typ string, data []byte) event.Body {
	b, err := event.Decode(typ, data)
	if err != nil {
		return event.Unknown{TypeName: typ, Raw: append([]byte(nil), data...)}
	}
	return b
}

// JSONOf is an event's body as JSON, "" for an event with none.
func JSONOf(e event.Envelope) string {
	if e.Data == nil {
		return ""
	}
	b, err := json.Marshal(e.Data)
	if err != nil {
		return ""
	}
	return string(b)
}

// LifecycleBody is the body of lifecycle type typ holding l: the inverse of
// LifecycleOf. The fields a body of typ does not carry are dropped.
func LifecycleBody(typ string, l Lifecycle) (event.Body, error) {
	switch typ {
	case event.TypeAccepted:
		return event.Accepted{Attempt: l.Attempt}, nil
	case event.TypeStarted:
		return event.Started{Attempt: l.Attempt}, nil
	case event.TypeParked:
		return event.Parked{Attempt: l.Attempt, Reason: l.Reason, Question: l.Question}, nil
	case event.TypeResumed:
		return event.Resumed{Attempt: l.Attempt, Answer: l.Answer}, nil
	case event.TypeCompleted:
		return event.Completed{Attempt: l.Attempt, Text: l.Text, Usage: l.Usage}, nil
	case event.TypeFailed:
		return event.Failed{Attempt: l.Attempt, Error: l.Error, Terminal: l.Terminal}, nil
	case event.TypeStopped:
		return event.Stopped{Attempt: l.Attempt, Reason: l.Reason, By: l.By, Text: l.Text, Usage: l.Usage}, nil
	case event.TypeChildParked:
		if l.Question == nil {
			return nil, fmt.Errorf("wire: %s needs a question", typ)
		}
		return event.ChildParked{Question: *l.Question}, nil
	}
	return nil, fmt.Errorf("wire: %s is not a lifecycle type", typ)
}
