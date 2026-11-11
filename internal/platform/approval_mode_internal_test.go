package platform

import (
	"testing"

	"github.com/urmzd/saige/flow/event"

	"github.com/urmzd/humagen/pkg/operations"
)

// Only an approval of a tool action the policy asks about is approved by a
// conversation's approval mode; everything else goes to a person.
func TestAutoApprovableIsOnlyAPolicyAskAboutATool(t *testing.T) {
	tool := func(name string) event.Question {
		return event.Question{ID: "q", Kind: event.QuestionApproval, Tool: &event.QuestionTool{Name: name}}
	}
	for _, c := range []struct {
		name   string
		q      event.Question
		always []string
		forced bool
		level  operations.Level
		want   bool
	}{
		{"a policy ask about a tool", tool("file_write"), nil, false, operations.Ask, true},
		{"a budget request", event.Question{ID: "q", Kind: event.QuestionHold, Hold: &event.Hold{Topic: "budget"}}, nil, false, operations.Ask, false},
		{"a clarification", event.Question{ID: "q", Kind: event.QuestionReply}, nil, false, operations.Ask, false},
		{"an approval naming no tool", event.Question{ID: "q", Kind: event.QuestionApproval}, nil, false, operations.Ask, false},
		{"the agent always asks", tool("file_write"), []string{"file_write"}, false, operations.Ask, false},
		{"a run forced to ask", tool("file_write"), nil, true, operations.Ask, false},
		{"the policy allows without asking", tool("file_write"), nil, false, operations.Allow, false},
		{"the policy never allows", tool("file_write"), nil, false, operations.Never, false},
	} {
		if got, why := autoApprovable(c.q, c.always, c.forced, c.level); got != c.want {
			t.Errorf("%s: got %v (%s), want %v", c.name, got, why, c.want)
		}
	}
}
