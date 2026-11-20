package platform

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/flow/event"
)

// Every tool call a run makes has one id in the API, htu_<run>_<seq>: the
// run, and the call's number among the run's calls (1-based, in the order
// the run asked for them, get_context and agent calls included). Provider
// call ids never leave the platform. A message's trace, its stream and its
// answers all name a call by this id.

// toolUseID is the API's id for a run's seq-th tool call.
func toolUseID(run string, seq int) string { return fmt.Sprintf("htu_%s_%d", run, seq) }

// parseToolUseID reads a tool call's API id back into its run and number.
func parseToolUseID(id string) (run string, seq int, ok bool) {
	rest, found := strings.CutPrefix(id, "htu_")
	i := strings.LastIndexByte(rest, '_')
	if !found || i <= 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(rest[i+1:])
	if err != nil || n < 1 || strconv.Itoa(n) != rest[i+1:] {
		return "", 0, false
	}
	return rest[:i], n, true
}

// callSeqs numbers a run's tool calls by the provider's call id, in the
// order its events first name them. Every call counts, whoever reads.
func callSeqs(held []event.Envelope) map[string]int {
	out := map[string]int{}
	for _, e := range held {
		if id, _ := toolCallOf(e); id != "" {
			if _, ok := out[id]; !ok {
				out[id] = len(out) + 1
			}
		}
	}
	return out
}

// toolCallOf is the provider's call id and the tool's name an event about a
// tool call carries, or "".
func toolCallOf(e event.Envelope) (id, name string) {
	switch b := e.Data.(type) {
	case event.ToolCall:
		return b.ID, b.Name
	case *event.ToolCall:
		return b.ID, b.Name
	case event.ToolDone:
		return b.ID, b.Name
	case *event.ToolDone:
		return b.ID, b.Name
	}
	return "", ""
}

// questionsAt are the questions a run asked a person at each of its tool
// calls, by the provider's call id, oldest first. An approval names its
// call. A question ask_person asked does not: it is the oldest ask_person
// call before it that has no question yet, so a batch of ask_person calls
// that parks the run once puts each question at its own call, in call order,
// or failing that the latest ask_person call.
func questionsAt(held []event.Envelope) map[string][]string {
	out := map[string][]string{}
	asking := ""
	var unasked []string
	seen := map[string]bool{}
	for _, e := range held {
		if id, name := toolCallOf(e); id != "" && name == toolAskPerson && !seen[id] {
			seen[id] = true
			unasked = append(unasked, id)
		}
		var qs []event.Question
		switch b := e.Data.(type) {
		case event.Parked:
			qs = wire.QuestionsOf(b)
		case *event.Parked:
			qs = wire.QuestionsOf(*b)
		}
		// A park on several questions holds one for each waiting call of
		// the batch.
		for _, q := range qs {
			if q.ID == "" {
				continue
			}
			if q.Tool != nil && q.Tool.CallID != "" {
				out[q.Tool.CallID] = append(out[q.Tool.CallID], q.ID)
				continue
			}
			if len(unasked) > 0 {
				asking, unasked = unasked[0], unasked[1:]
			}
			if asking != "" {
				out[asking] = append(out[asking], q.ID)
			}
		}
	}
	return out
}

// AnswerToolUse answers what waits on a person at the tool call toolUseID
// names, in the conversation of message id (a message or a session id): an
// approval of that call, or a question the call asked. It is the same as
// answering the question on its run (AnswerQuestion), who may answer and
// all. A call outside the conversation, or one the caller may not read, is
// threads.ErrNotFound, and so is a call nothing waited on.
func (p *Platform) AnswerToolUse(ctx context.Context, who auth.Principal, id, toolUseID string, a QuestionAnswer) (inbox.Message, error) {
	runID, seq, ok := parseToolUseID(toolUseID)
	if !ok {
		return inbox.Message{}, fmt.Errorf("%w: tool_use_id is htu_<run>_<n>", threads.ErrInvalid)
	}
	session, _ := SplitMessageID(id)
	if !p.SeesSession(who, session) {
		return inbox.Message{}, threads.ErrNotFound
	}
	run, err := p.Runs.Get(who.Tenant, runID)
	if err != nil || run.Thread == "" || !p.Seer(who).Run(run) {
		return inbox.Message{}, threads.ErrNotFound
	}
	root, _, err := p.sessionOrigin(who.Tenant, session)
	if err != nil {
		return inbox.Message{}, threads.ErrNotFound
	}
	if runRoot, _, err := p.sessionOrigin(who.Tenant, run.Thread); err != nil || runRoot != root {
		return inbox.Message{}, threads.ErrNotFound
	}
	held, err := p.Journal.Events(who.Tenant, []string{run.ID})
	if err != nil {
		return inbox.Message{}, err
	}
	call := ""
	for provider, n := range callSeqs(held) {
		if n == seq {
			call = provider
		}
	}
	// The latest question asked at that call: still open, or answered, in
	// which case answering again shows what was recorded.
	question := ""
	if asked := questionsAt(held)[call]; call != "" && len(asked) > 0 {
		question = asked[len(asked)-1]
	}
	if question == "" {
		return inbox.Message{}, fmt.Errorf("%w: nothing waits on a person at %s", threads.ErrNotFound, toolUseID)
	}
	return p.AnswerQuestion(ctx, who, run.ID, question, a)
}
