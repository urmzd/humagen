package platform

import (
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/orchagent/flow/address"
	"github.com/urmzd/orchagent/flow/connectors"
	"github.com/urmzd/orchagent/flow/event"
	"testing"
	"time"
)

func TestNativeHandlesSurviveRestartAndConfineFeedback(t *testing.T) {
	st := state.NewMemory()
	cfg := Config{Workspace: t.TempDir(), Backends: Backends{State: st}}
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	c, err := p.Clients.Create("t", clients.Client{ID: "app", Name: "App", Surface: clients.Slack, Identity: clients.Identity{Team: "T01"}, Enabled: true, Agents: []string{"a"}}, clients.DefaultCaps(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	target := address.MustParse("slack://T01/C01?thread=1")
	tag := wire.NewEvent(wire.Raise{ID: "request", Tenant: "t", Agent: "a", Origin: "slack", Channel: target.String(), At: time.Now(), Context: map[string]string{runs.MetaClient: c.ID}})
	if err = p.Runs.Record("r", tag, 1); err != nil {
		t.Fatal(err)
	}
	message := address.MustParse("slack://T01/C01?thread=2")
	if err = p.deliveredFor(c)(t.Context(), connectors.Delivery{Tenant: "t", Run: "r", Target: target, Message: message, Channel: target.String()}); err != nil {
		t.Fatal(err)
	}
	other, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })
	h := other.feedbackFor(c)
	found, err := h.Resolve(t.Context(), "t", message)
	if err != nil || !found.Found || found.Run != "r" {
		t.Fatal(found, err)
	}
	if got, _ := h.Resolve(t.Context(), "else", message); got.Found {
		t.Fatal("cross tenant handle resolved")
	}
	if other.continueSurface(c, address.MustParse("slack://T02/C01?thread=2")) {
		t.Fatal("cross workspace handle resolved")
	}
	if !other.continueSurface(c, target) {
		t.Fatal("existing thread forgotten")
	}
	b := event.Feedback{RunID: found.Run, MessageID: found.Message, Actor: event.Actor{ID: "U01"}, Rating: "up", Surface: "slack"}
	if err = h.Record(t.Context(), "t", "event-1", b); err != nil {
		t.Fatal(err)
	}
	b.Rating = "down"
	if err = h.Record(t.Context(), "t", "event-2", b); err != nil {
		t.Fatal(err)
	}
	b.Rating = "up"
	if err = h.Record(t.Context(), "t", "event-1", b); err != nil {
		t.Fatal(err)
	}
	// A late older delivery cannot move the thread's latest-run pointer back.
	next := wire.Clone(tag)
	next.ID = "next"
	next.Time = tag.Time.Add(time.Second)
	if err := other.Runs.Record("r2", next, 1); err != nil {
		t.Fatal(err)
	}
	newer := address.MustParse("slack://T01/C01?thread=3")
	if err := other.deliveredFor(c)(t.Context(), connectors.Delivery{Tenant: "t", Run: "r2", Target: target, Message: newer, Channel: target.String()}); err != nil {
		t.Fatal(err)
	}
	if err := other.deliveredFor(c)(t.Context(), connectors.Delivery{Tenant: "t", Run: "r", Target: target, Message: message, Channel: target.String()}); err != nil {
		t.Fatal(err)
	}
	latest, err := h.Resolve(t.Context(), "t", target)
	if err != nil || latest.Run != "r2" {
		t.Fatal("late delivery replaced latest", latest, err)
	}
	ratings, err := other.Feedback.List("t", "r", 0)
	if err != nil || len(ratings) != 1 || ratings[0].Rating != "down" {
		t.Fatal(ratings, err)
	}
}
