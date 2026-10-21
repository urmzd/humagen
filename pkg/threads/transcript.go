package threads

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/urmzd/fiber/pkg/runtime"

	"github.com/urmzd/warp/pkg/runs"
)

// Turn is one turn of a thread: a person's message, or an agent's run.
// Exactly one is set.
type Turn struct {
	Message *Message  `json:"message,omitempty"`
	Run     *runs.Run `json:"run,omitempty"`
}

// MessageTurn and RunTurn make a turn of each kind.
func MessageTurn(m Message) Turn { return Turn{Message: &m} }

// RunTurn is a turn an agent took.
func RunTurn(r runs.Run) Turn { return Turn{Run: &r} }

// reply is what a run said, as the next agent is told it.
func reply(r runs.Run) string {
	switch {
	case r.Status == runs.Failed:
		return "(this reply failed)"
	case r.Status == runs.Stopped && strings.TrimSpace(r.Answer) == "":
		return runtime.StopMarker
	case r.Status == runs.Stopped:
		// What the people read, and that it is not the whole answer.
		return strings.TrimRight(r.Answer, " \t\r\n") + "\n" + runtime.StopMarker
	case !r.Status.Terminal():
		return "(still working)"
	}
	return r.Answer
}

// line renders one turn: a message as "Dana: text", a run answering one as
// "ignite: answer". A run that answers no message, of a conversation from
// before threads, renders as its text and its answer, as it always has.
func line(t Turn) string {
	switch {
	case t.Message != nil:
		return fmt.Sprintf("%s: %s", cmp.Or(t.Message.Name, t.Message.By), t.Message.Text)
	case t.Run != nil && t.Run.Message != "":
		return fmt.Sprintf("%s: %s", t.Run.Agent, reply(*t.Run))
	case t.Run != nil:
		return fmt.Sprintf("User: %s\n%s: %s", t.Run.Text, t.Run.Agent, reply(*t.Run))
	}
	return ""
}

// Transcript renders a thread's turns, oldest first, for the agent answering
// next: the room as it stands, people by name and agents by theirs. The
// newest turns that fit in limit bytes are kept, at least the newest one,
// and it says how many earlier turns were left out. Nothing is summarized.
func Transcript(turns []Turn, limit int) string {
	var kept []string
	size := 0
	for i := len(turns) - 1; i >= 0; i-- {
		l := line(turns[i])
		if l == "" {
			continue
		}
		if size+len(l) > limit && len(kept) > 0 {
			break
		}
		size += len(l)
		kept = append(kept, l)
	}
	var b strings.Builder
	b.WriteString("Conversation so far:\n")
	if omitted := countTurns(turns) - len(kept); omitted > 0 {
		fmt.Fprintf(&b, "(%d earlier turns omitted)\n", omitted)
	}
	for i := len(kept) - 1; i >= 0; i-- {
		b.WriteString("\n")
		b.WriteString(kept[i])
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func countTurns(turns []Turn) int {
	n := 0
	for _, t := range turns {
		if t.Message != nil || t.Run != nil {
			n++
		}
	}
	return n
}
