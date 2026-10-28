package runs

import (
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/fiber/pkg/runtime"

	"github.com/urmzd/humagen/pkg/call"
)

// A run's channel is believed where the server wrote it: the API, and Slack
// and GitHub ingress, which writes it from the client's binding. Its message
// and thread only from the API, and nothing from any other origin.
func TestAChannelAndAMessageAreBelievedOnlyWhereTheServerWroteThem(t *testing.T) {
	x := NewIndex()
	placed := map[string]string{MetaThread: "th-1", MetaChannel: "renewals", MetaMessage: "m-3"}
	record(t, x, "acme", "api", "lead", OriginAPI, 0, placed)
	record(t, x, "acme", "slack", "lead", OriginSlack, 0, placed)
	record(t, x, "acme", "jira", "lead", "jira", 0, placed)
	if r, _ := x.Get("acme", "api"); r.Channel != "renewals" || r.Message != "m-3" {
		t.Fatalf("an API run = %+v", r)
	}
	if r, _ := x.Get("acme", "slack"); r.Channel != "renewals" || r.Message != "" || r.Thread == "th-1" {
		t.Fatalf("a Slack run = %+v", r)
	}
	if r, _ := x.Get("acme", "jira"); r.Channel != "" || r.Message != "" {
		t.Fatalf("a Jira tag placed its own run: %+v", r)
	}
}

func TestACalledRunIsInItsRootsChannelAndAnswersNoMessage(t *testing.T) {
	x := NewIndex()
	root := wire.NewEvent(wire.Raise{ID: "root", Tenant: "acme", Agent: "lead", Origin: OriginAPI, Text: "go", At: t0, Context: map[string]string{MetaChannel: "renewals", MetaMessage: "m-1", MetaThread: "th-1"}})
	if err := x.Record(runtime.RunID("acme", "root"), root, 1); err != nil {
		t.Fatal(err)
	}
	child, err := call.New(root, call.Request{Key: "k", Agent: "helper", Text: "go", Mode: call.Subagent}, call.State{}, call.Limits{MaxDepth: 3, MaxFanOut: 3}, t0)
	if err != nil {
		t.Fatal(err)
	}
	id := runtime.RunID("acme", child.ID)
	if err := x.Record(id, child, 1); err != nil {
		t.Fatal(err)
	}
	if r, _ := x.Get("acme", id); r.Channel != "renewals" || r.Message != "" || r.Thread != "th-1" {
		t.Fatalf("a called run = %+v", r)
	}
}

func TestAConversationWithNoChannelIsInGeneral(t *testing.T) {
	x := NewIndex()
	record(t, x, "acme", "old", "lead", OriginAPI, 0, nil)
	record(t, x, "acme", "new", "lead", OriginAPI, time.Second, map[string]string{MetaChannel: "renewals"})
	all := x.Threads("acme", 0)
	if len(all) != 2 || all[0].Channel != "renewals" || all[1].Channel != DefaultChannel {
		t.Fatalf("threads = %+v", all)
	}
	for _, c := range []struct {
		channel string
		want    string
	}{{DefaultChannel, "old"}, {"renewals", "new"}} {
		page, _, err := x.ThreadPage("acme", ThreadQuery{Channel: c.channel})
		if err != nil || len(page) != 1 || page[0].ID != c.want {
			t.Fatalf("threads in #%s = %+v, %v", c.channel, page, err)
		}
	}
	if page, _, _ := x.ThreadPage("acme", ThreadQuery{Channel: "press"}); len(page) != 0 {
		t.Fatalf("threads in an empty channel = %+v", page)
	}
}

func TestRunsAnsweringOneMessageAreSideBySideNotBranches(t *testing.T) {
	x := NewIndex()
	answer := map[string]string{MetaThread: "th-1", MetaPrev: "m-1", MetaMessage: "m-1"}
	record(t, x, "acme", "a", "ignite", OriginAPI, 0, answer)
	record(t, x, "acme", "b", "scribe", OriginAPI, time.Second, answer)
	// Two answers to the same run are a branch, as they always were.
	branch := map[string]string{MetaThread: "th-1", MetaPrev: "a"}
	record(t, x, "acme", "c", "ignite", OriginAPI, 2*time.Second, branch)
	record(t, x, "acme", "d", "ignite", OriginAPI, 3*time.Second, branch)
	got := x.Threads("acme", 0)
	if len(got) != 1 || got[0].Branches != 1 || got[0].Turns != 4 {
		t.Fatalf("thread = %+v; want one branch, at a", got)
	}
}
