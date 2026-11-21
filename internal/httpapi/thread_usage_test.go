package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/flow/event"
)

// GET /api/threads/{id}/usage folds the conversation's runs: the total, each
// agent's own calls dearest first, the latest context fill and how many runs
// are still working. Another tenant's conversation is a 404 and one with no
// runs answers zeros.
func TestThreadUsageFoldsTheConversationsRuns(t *testing.T) {
	f := newFixture(t)
	acme := f.machineAs(t, "ana", "acme", everything...)
	globex := f.machineAs(t, "gil", "globex", everything...)
	at := time.Now().UTC()
	mk := func(id string) threads.Thread {
		th, err := f.p.Threads.Create("acme", threads.Thread{ID: id, Channel: "general", Visibility: threads.Private, CreatedBy: "ana"}, at)
		if err != nil {
			t.Fatal(err)
		}
		return th
	}
	th, empty := mk("th-1234567890abcdef"), mk("th-fedcba0987654321")
	run := func(id, agent string, own event.Usage, ctx *event.ContextUsage, done bool) {
		tag := wire.NewEvent(wire.Raise{ID: id, Origin: runs.OriginAPI, Tenant: "acme", Agent: agent, At: at, Context: map[string]string{runs.MetaThread: th.ID}})
		if err := f.p.Runs.Record(id, tag, 1); err != nil {
			t.Fatal(err)
		}
		if done {
			f.p.Runs.Observe(event.Envelope{Tenant: "acme", Run: id, Type: event.TypeCompleted, Time: at.Add(time.Second), Data: event.Completed{Text: "ok", Usage: &event.RunUsage{Own: own, Total: own, Context: ctx}}})
		}
	}
	usd := func(m int64, in, out int) event.Usage {
		return event.Usage{ModelCalls: 1, PromptTokens: in, CompletionTokens: out, Cost: &event.Cost{Micros: m, Currency: "USD"}}
	}
	run("r1", "writer", usd(300, 100, 10), &event.ContextUsage{Model: "m1", Window: 1000, Tokens: 100}, true)
	run("r2", "scout", usd(500, 200, 20), &event.ContextUsage{Model: "m2", Window: 2000, Tokens: 400}, true)
	run("r3", "scout", usd(0, 0, 0), nil, false)

	res, got := f.do(t, nil, "GET", "/api/threads/"+th.ID+"/usage", acme, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("usage = %d %v", res.StatusCode, got)
	}
	if got["runs"] != float64(3) || got["working"] != float64(1) || got["truncated"] != false {
		t.Fatalf("counts = %v", got)
	}
	total := got["usage"].(map[string]any)
	if total["model_calls"] != float64(2) || total["prompt_tokens"] != float64(300) || total["cost"].(map[string]any)["micros"] != float64(800) {
		t.Fatalf("total = %v", total)
	}
	agents := got["agents"].([]any)
	if len(agents) != 2 || agents[0].(map[string]any)["agent"] != "scout" || agents[0].(map[string]any)["runs"] != float64(1) || agents[1].(map[string]any)["agent"] != "writer" {
		t.Fatalf("agents = %v", agents)
	}
	if c := got["context"].(map[string]any); c["run"] == "" || c["agent"] != "scout" || c["model"] != "m2" || c["window"] != float64(2000) || c["tokens"] != float64(400) {
		t.Fatalf("context = %v", c)
	}

	if res, _ := f.do(t, nil, "GET", "/api/threads/"+th.ID+"/usage", globex, ""); res.StatusCode != http.StatusNotFound {
		t.Fatalf("another tenant = %d, want 404", res.StatusCode)
	}
	res, got = f.do(t, nil, "GET", "/api/threads/"+empty.ID+"/usage", acme, "")
	if res.StatusCode != http.StatusOK || got["runs"] != float64(0) || got["working"] != float64(0) || got["truncated"] != false || len(got["agents"].([]any)) != 0 || got["context"] != nil {
		t.Fatalf("empty = %d %v", res.StatusCode, got)
	}
}
