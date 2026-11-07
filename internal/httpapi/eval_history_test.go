package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/event"
)

func TestEvalHistoryRoutesDoNotDeclassifyRemovedPrivateReplay(t *testing.T) {
	f := newFixture(t)
	owner := f.machineAs(t, "ana", "acme", everything...)
	stranger := f.machineAs(t, "bob", "acme", "humagen:evals:read", "humagen:agents:read", "humagen:runs:read", "humagen:projects:read", "humagen:knowledge:read")
	want(t, f, owner, "POST", "/api/agents", helperSpec, http.StatusCreated)
	thread, err := f.p.Threads.Create("acme", threads.Thread{ID: "th-1234567890abcdef", Channel: "general", Visibility: threads.Private, CreatedBy: "ana"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tag := wire.NewEvent(wire.Raise{ID: "original", Origin: runs.OriginAPI, Tenant: "acme", Agent: "helper", Actor: event.Actor{ID: "ana"}, At: time.Now(), Context: map[string]string{runs.MetaThread: thread.ID}})
	if err := f.p.Runs.Record("original", tag, 1); err != nil {
		t.Fatal(err)
	}
	suite := evals.Suite{Name: "history", Target: evals.Agent, Subject: "helper", Cases: []evals.Case{{ID: "c", Input: "query", Checks: []evals.Check{{Kind: evals.Contains, Value: "answer"}}, Trace: &evals.Trace{Run: "original"}}}}
	if err := f.p.Evals.Put("acme", suite, false); err != nil {
		t.Fatal(err)
	}
	run, err := f.p.Evals.Begin("acme", suite, 1, "ana", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	run.Finish([]evals.Result{{CaseID: "c", Output: "PRIVATE REPLAY ANSWER", Passed: true}}, nil, time.Now())
	if err := f.p.Evals.Save("acme", run); err != nil {
		t.Fatal(err)
	}
	suite.Cases[0].Trace = nil
	body, err := json.Marshal(suite)
	if err != nil {
		t.Fatal(err)
	}
	want(t, f, owner, "PUT", "/api/evals/history", string(body), http.StatusOK)
	for _, path := range []string{"/api/evals", "/api/evals/history/runs"} {
		out := want(t, f, stranger, "GET", path, "", http.StatusOK)
		raw, _ := json.Marshal(out)
		if strings.Contains(string(raw), "PRIVATE REPLAY ANSWER") || strings.Contains(string(raw), run.ID) {
			t.Fatalf("%s exposed removed private history: %s", path, raw)
		}
		out = want(t, f, owner, "GET", path, "", http.StatusOK)
		raw, _ = json.Marshal(out)
		if !strings.Contains(string(raw), "PRIVATE REPLAY ANSWER") {
			t.Fatalf("%s hid owner's readable history: %s", path, raw)
		}
	}
	other := f.machineAs(t, "ana", "other", everything...)
	want(t, f, other, "GET", "/api/evals/history/runs", "", http.StatusNotFound)
}
