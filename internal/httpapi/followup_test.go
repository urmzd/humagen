package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/urmzd/orchagent/flow/turn/native"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
)

// held answers once release is closed, so a run stays working until then.
type held chan struct{}

func (h held) Stream(ctx context.Context, _ saigetypes.Request) (<-chan saigetypes.Delta, error) {
	ch := make(chan saigetypes.Delta, 1)
	go func() {
		defer close(ch)
		select {
		case <-h:
			ch <- saigetypes.PartDelta{Text: "Done."}
		case <-ctx.Done():
		}
	}()
	return ch, nil
}

// A message posted while a run works says what it does to that work: the
// rule is followup, it starts nothing yet, and the message carries the
// decision, in what posting answers and in the thread. With no model to
// classify it, it queues, and starts once the run has ended.
func TestAMessagePostedWhileARunWorksCarriesItsFollowup(t *testing.T) {
	release := make(held)
	f := newFixtureWith(t, platform.Config{Ext: platform.Extensions{Providers: map[string]native.ProviderFunc{
		"held": func(native.Spec) (saigetypes.Provider, error) { return release, nil },
	}}})
	released := false
	t.Cleanup(func() {
		if !released {
			close(release)
		}
	})
	if _, err := f.p.Agents.Create(t.Context(), "acme", wire.AgentSpec{Name: "slow", Provider: "held", Model: "held", SystemPrompt: "Take your time."}, "test"); err != nil {
		t.Fatal(err)
	}
	ana := f.person(t, "acme", "00u-ana", "Ana", "humagen-admins")
	res, started := f.do(t, nil, "POST", "/api/threads", ana, `{"visibility":"shared","text":"@slow build the report"}`)
	if res.StatusCode != http.StatusCreated || len(started["runs"].([]any)) != 1 {
		t.Fatalf("starting = %d %v", res.StatusCode, started)
	}
	thread := started["thread"].(map[string]any)["id"].(string)
	first := started["runs"].([]any)[0].(map[string]any)["id"].(string)

	res, posted := f.do(t, nil, "POST", "/api/threads/"+thread+"/messages", ana, `{"text":"@slow and the appendix"}`)
	if res.StatusCode != http.StatusCreated || posted["rule"] != "followup" || len(posted["runs"].([]any)) != 0 {
		t.Fatalf("posting = %d %v", res.StatusCode, posted)
	}
	fu, _ := posted["message"].(map[string]any)["followup"].(map[string]any)
	if fu["outcome"] != "queue" || fu["handled"] != nil || fu["reason"] == "" {
		t.Fatalf("follow-up = %v", fu)
	}
	if waiting, _ := fu["waiting"].([]any); len(waiting) != 1 || waiting[0] != first {
		t.Fatalf("waiting = %v", fu["waiting"])
	}
	_, page := f.do(t, nil, "GET", "/api/threads/"+thread, ana, "")
	var shown map[string]any
	for _, turn := range page["turns"].([]any) {
		if m, ok := turn.(map[string]any)["message"].(map[string]any); ok && m["id"] == "m-2" {
			shown, _ = m["followup"].(map[string]any)
		}
	}
	if shown["outcome"] != "queue" {
		t.Fatalf("the thread shows %v", page["turns"])
	}

	close(release)
	released = true
	deadline := time.Now().Add(30 * time.Second)
	for {
		_, page = f.do(t, nil, "GET", "/api/threads/"+thread, ana, "")
		runs := 0
		for _, turn := range page["turns"].([]any) {
			if r, ok := turn.(map[string]any)["run"].(map[string]any); ok && r["message"] == "m-2" {
				runs++
			}
		}
		if runs == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the queued message never started: %v", page["turns"])
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// ended waits for a run to end, so a message posted after it is no
// follow-up to it.
func (f *fixture) ended(t *testing.T, tok, run string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		_, got := f.do(t, nil, "GET", "/api/runs/"+run, tok, "")
		if s := got["status"]; s == "completed" || s == "failed" || s == "stopped" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s never ended: %v", run, got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
