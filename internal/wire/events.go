package wire

import (
	"fmt"

	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/envelope"
)

// Kind names what an event reports.
type Kind = envelope.Kind

// The kinds Humagen reads.
const (
	KindText        = envelope.KindText
	KindThinking    = envelope.KindThinking
	KindToolCall    = envelope.KindToolCall
	KindToolDone    = envelope.KindToolDone
	KindCitation    = envelope.KindCitation
	KindAccepted    = envelope.KindAccepted
	KindStarted     = envelope.KindStarted
	KindParked      = envelope.KindParked
	KindResumed     = envelope.KindResumed
	KindCompleted   = envelope.KindCompleted
	KindFailed      = envelope.KindFailed
	KindChildParked = envelope.KindChildParked
	KindStopped     = envelope.KindStopped
	KindActionTaken = envelope.KindActionTaken
	KindUsageCall   = envelope.KindUsageCall
	KindUsageChild  = envelope.KindUsageChild
)

// IsTerminal reports whether a kind reports an attempt ending: completed,
// failed or stopped. A failure may still be retried; the ledger decides.
func IsTerminal(k Kind) bool {
	return k == KindCompleted || k == KindFailed || k == KindStopped
}

// IsParked reports whether a kind reports a run, or a run it called, waiting
// on a person.
func IsParked(k Kind) bool { return k == KindParked || k == KindChildParked }

// IsActive reports whether a kind reports a run accepted, started or resumed.
func IsActive(k Kind) bool { return k == KindAccepted || k == KindStarted || k == KindResumed }

// LifecycleOf decodes a lifecycle event's body.
func LifecycleOf(e envelope.Event) (payload.Lifecycle, error) {
	return payload.Decode[payload.Lifecycle](e)
}

// StoppedOf decodes a lifecycle.stopped body.
func StoppedOf(e envelope.Event) (payload.Stopped, error) {
	return payload.Decode[payload.Stopped](e)
}

// ChildQuestionOf decodes a lifecycle.child.parked body.
func ChildQuestionOf(e envelope.Event) (payload.ChildQuestion, error) {
	return payload.Decode[payload.ChildQuestion](e)
}

// UsageOf decodes a usage event. Exactly one of call and child is set when err
// is nil; any other kind is an error.
func UsageOf(e envelope.Event) (call *payload.UsageCall, child *payload.UsageChild, err error) {
	switch e.Kind {
	case KindUsageCall:
		c, err := payload.Decode[payload.UsageCall](e)
		if err != nil {
			return nil, nil, err
		}
		return &c, nil, nil
	case KindUsageChild:
		c, err := payload.Decode[payload.UsageChild](e)
		if err != nil {
			return nil, nil, err
		}
		return nil, &c, nil
	}
	return nil, nil, fmt.Errorf("wire: %s is not a usage event", e.Kind)
}

// CitationOf decodes a delta.citation body.
func CitationOf(e envelope.Event) (payload.Citation, error) {
	return payload.Decode[payload.Citation](e)
}

// ActionOf decodes an action.taken body.
func ActionOf(e envelope.Event) (payload.Action, error) {
	return payload.Decode[payload.Action](e)
}

// TextDeltaOf decodes a delta.text body.
func TextDeltaOf(e envelope.Event) (payload.Text, error) {
	return payload.Decode[payload.Text](e)
}

// ThinkingOf decodes a delta.thinking body.
func ThinkingOf(e envelope.Event) (payload.Thinking, error) {
	return payload.Decode[payload.Thinking](e)
}

// ToolCallOf decodes a delta.tool.call body.
func ToolCallOf(e envelope.Event) (payload.ToolCall, error) {
	return payload.Decode[payload.ToolCall](e)
}

// ToolDoneOf decodes a delta.tool.done body.
func ToolDoneOf(e envelope.Event) (payload.ToolDone, error) {
	return payload.Decode[payload.ToolDone](e)
}
