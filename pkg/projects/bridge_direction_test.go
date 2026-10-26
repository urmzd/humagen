package projects_test

import (
	"encoding/json"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"strings"
	"testing"
	"time"
)

func TestBridgeDirectionIsRetainedInBindings(t *testing.T) {
	var binding projects.SlackBinding
	if err := json.Unmarshal([]byte(`{"client":"slack","channel":"C01","direction":"inbound"}`), &binding); err != nil {
		t.Fatal(err)
	}
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	if _, err := s.CreateChannel("acme", projects.Channel{Project: "Field", Name: "chat", Slack: &binding}, ada, t0); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s.Bindings("acme", "slack"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"direction":"inbound"`) {
		t.Fatalf("bridge direction was lost: %s", raw)
	}
}

func TestBridgeDirectionModesValidateAndResetOutboundBoundary(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		mustCreate(t, s, "acme", team("Field", ""), ada)
		original, err := s.CreateChannel("acme", projects.Channel{Project: "Field", Name: "bridge", Slack: &projects.SlackBinding{Client: "slack", Channel: "C01"}}, ada, t0)
		if err != nil {
			t.Fatal(err)
		}
		normalized, err := s.UpdateChannel("acme", "Field", "bridge", original.Version, ada, t0.Add(time.Hour), func(c *projects.Channel) error { c.Slack.Direction = projects.BridgeTwoWay; return nil })
		if err != nil || !normalized.BoundAt.Equal(original.BoundAt) {
			t.Fatal("legacy default changed binding identity", err)
		}
		changed, err := s.UpdateChannel("acme", "Field", "bridge", normalized.Version, ada, t0.Add(2*time.Hour), func(c *projects.Channel) error { c.Slack.Direction = projects.BridgeInbound; return nil })
		if err != nil || !changed.BoundAt.Equal(t0.Add(2*time.Hour)) {
			t.Fatal("direction change did not reset outbound boundary", err)
		}
		replay, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		bindings := replay.Bindings("acme", "slack")
		if len(bindings) != 1 || bindings[0].Direction != projects.BridgeInbound || bindings[0].Direction.AllowsOutcomes() {
			t.Fatal("direction lost on restart")
		}
		if _, err := s.UpdateChannel("acme", "Field", "bridge", changed.Version, ada, t0.Add(3*time.Hour), func(c *projects.Channel) error { c.Slack.Direction = "unknown"; return nil }); err == nil {
			t.Fatal("unknown bridge direction accepted")
		}
	})
}
