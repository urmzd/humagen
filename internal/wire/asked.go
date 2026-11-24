package wire

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/thread"
)

// RunRequest is a run's request, read from its input as the run ledger keeps
// it.
type RunRequest struct {
	// Thread is the thread the run's exchange is journaled in.
	Thread thread.Key
	// Asked is the request as it was asked, as the thread journal records it
	// from saige's "journal each request as it was asked" on: the trigger as
	// the catalog renders it (its trigger context, then its request text),
	// and, for a trigger the catalog renders empty, its request text and
	// context as a turn is sent them before retrieval.
	Asked string
	// text and context are what a turn is sent before retrieval.
	text    string
	context map[string]string
}

// RunRequestOf reads the request of the run whose ledger input is input.
func RunRequestOf(input []byte) (RunRequest, error) {
	var in runtime.Input
	if err := json.Unmarshal(input, &in); err != nil {
		return RunRequest{}, fmt.Errorf("wire: run input: %w", err)
	}
	r := RunRequest{Thread: thread.KeyOf(in.Trigger, in.Subscriber)}
	if in.Session != nil {
		r.Thread = in.Session.ThreadKey()
	}
	r.text, r.context, _ = event.Prompt(in.Trigger.Data)
	r.Asked = AskedOf(in.Trigger)
	return r, nil
}

// AskedOf is the request as it was asked of a run whose trigger is tag (see
// RunRequest.Asked).
func AskedOf(tag event.Envelope) string {
	if s, err := event.Render(tag); err == nil && strings.TrimSpace(s) != "" {
		return s
	}
	text, context, _ := event.Prompt(tag.Data)
	return event.RequestText(text, context)
}

// WithPassages reports whether journaled is this request as a turn with
// retrieved passages sent it: the trigger context, then the passages, then
// the request text (event.RequestText(passages+"\n\n"+text, context)), with
// passages that are not blank. Such an entry is the request as it was asked
// once the passages are taken out.
func (r RunRequest) WithPassages(journaled string) bool {
	head := event.RequestText("", r.context)
	if strings.TrimSpace(head) == "" {
		head = ""
	}
	tail := "\n\n" + r.text
	if !strings.HasPrefix(journaled, head) || !strings.HasSuffix(journaled, tail) || len(journaled) < len(head)+len(tail) {
		return false
	}
	passages := journaled[len(head) : len(journaled)-len(tail)]
	return strings.TrimSpace(passages) != ""
}
