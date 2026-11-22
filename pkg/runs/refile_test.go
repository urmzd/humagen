package runs

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/urmzd/saige/flow/runtime"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
)

// A conversation refiled into another is one with it: its runs, the calls
// they made among them, say they are in the other, its turns are the
// other's in the order they were asked, the other's summary counts them,
// and it is no longer listed. Refiling it again changes nothing, and a move
// between projects is refused.
func TestRefileMovesAConversation(t *testing.T) {
	x := NewIndex()
	// A conversation of its own, from before: a turn and a call it made.
	root := wire.NewEvent(wire.Raise{ID: "past", Tenant: "acme", Agent: "docs", Origin: "github", Text: "what changed?", At: t0})
	child, err := call.New(root, call.Request{Key: "k", Agent: "ops", Text: "look", Mode: call.Delegate}, call.State{}, call.Limits{MaxDepth: 2, MaxFanOut: 2}, t0)
	if err != nil {
		t.Fatal(err)
	}
	past, called := runtime.RunID("acme", "past"), runtime.RunID("acme", child.ID)
	if err := x.Record(past, root, 1); err != nil {
		t.Fatal(err)
	}
	if err := x.Record(called, child, 1); err != nil {
		t.Fatal(err)
	}
	x.End("acme", called, Completed, "looked", "", t0.Add(time.Minute))
	x.End("acme", past, Completed, "Nothing.", "", t0.Add(2*time.Minute))
	// The session's conversation, holding a later turn already.
	record(t, x, "acme", "later", "docs", OriginAPI, 2*time.Hour, map[string]string{MetaThread: "s_session"})
	opened := x.ProjectCounts("acme")[""].Threads

	moved, err := x.Refile("acme", past, "s_session")
	if err != nil || moved != 2 {
		t.Fatalf("refile moved %d, %v", moved, err)
	}
	for _, id := range []string{past, called} {
		if r, _ := x.Get("acme", id); r.Thread != "s_session" {
			t.Fatalf("run %s is in %q", id, r.Thread)
		}
	}
	turns, err := x.Conversation("acme", "s_session")
	if err != nil || len(turns) != 2 || turns[0].ID != past || turns[1].ID != "later" {
		t.Fatalf("the session's turns = %v, %v", turns, err)
	}
	sum, err := x.ConversationSummary("acme", "s_session")
	if err != nil || sum.Turns != 2 || sum.Title != "What changed?" || !sum.CreatedAt.Equal(t0) || !slices.Equal(sum.Agents, []string{"docs"}) {
		t.Fatalf("the session's summary = %+v, %v", sum, err)
	}
	if _, err := x.Conversation("acme", past); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the old conversation still has turns: %v", err)
	}
	listed, err := x.ListThreads("acme", 10)
	if err != nil || slices.ContainsFunc(listed, func(s ThreadSummary) bool { return s.ID == past }) || !slices.ContainsFunc(listed, func(s ThreadSummary) bool { return s.ID == "s_session" }) {
		t.Fatalf("the listed conversations = %v, %v", listed, err)
	}
	if got := x.ProjectCounts("acme")[""].Threads; got != opened-1 {
		t.Fatalf("the project holds %d conversations, want %d", got, opened-1)
	}

	if moved, err := x.Refile("acme", past, "s_session"); err != nil || moved != 0 {
		t.Fatalf("a second refile moved %d, %v", moved, err)
	}
	record(t, x, "acme", "elsewhere", "docs", OriginAPI, 3*time.Hour, map[string]string{MetaProject: "Launch"})
	if _, err := x.Refile("acme", "elsewhere", "s_session"); !errors.Is(err, ErrRefile) {
		t.Fatalf("a move between projects = %v", err)
	}
}
