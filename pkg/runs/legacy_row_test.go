package runs

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// A run row written before the event union keeps its trigger as an old tag.
// It is read as the event it is now: the agent it named, its text, its meta
// as the request context and the surface it answers on.
func TestARunRowWrittenBeforeTheUnionIsReadAsItsEvent(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		row := json.RawMessage(strings.ReplaceAll(`{
			"id": "r-old", "tenant": "acme", "agent": "lead", "status": "completed",
			"seq": 1, "final": true, "closed": true,
			"tag": {
				"id": "Ev1", "tenant": "acme", "agent": "lead", "origin": "slack",
				"source": {"Connector": "slack", "Workspace": "T01", "Path": ["C02"], "Params": {"thread": "1700000000.000100"}},
				"text": "summarize the incident",
				"actor": {"id": "U01", "user": "ada"},
				"meta": {"slack_event": "app_mention", "slack_channel": "C02", "THREAD": "th-1", "_humagen.private": "kept"},
				"at": "2025-01-01T00:00:00Z"
			}
		}`, "THREAD", MetaThread))
		if err := state.NewTable[json.RawMessage](st, "runs").Put("acme", "r-old", row); err != nil {
			t.Fatal(err)
		}
		x, err := Open(st)
		if err != nil {
			t.Fatalf("an old row did not load: %v", err)
		}
		r, err := x.Get("acme", "r-old")
		if err != nil {
			t.Fatalf("the old row is missing after load: %v", err)
		}
		tag := r.Tag()
		if got := wire.AgentOf(tag); got != "lead" {
			t.Errorf("agent = %q, want lead", got)
		}
		if got := wire.TextOf(tag); got != "summarize the incident" {
			t.Errorf("text = %q", got)
		}
		if got := wire.Origin(tag); got != "slack" {
			t.Errorf("origin = %q, want slack", got)
		}
		if got := wire.ActorOf(tag).User; got != "ada" {
			t.Errorf("actor user = %q, want ada", got)
		}
		meta := wire.Meta(tag)
		if meta[MetaThread] != "th-1" || meta["_humagen.private"] != "kept" {
			t.Errorf("meta = %v, want the thread and the private key kept", meta)
		}
		if got := wire.SurfaceAddress(tag).String(); got != "slack://T01/C02?thread=1700000000.000100" {
			t.Errorf("surface = %q", got)
		}
	})
}
