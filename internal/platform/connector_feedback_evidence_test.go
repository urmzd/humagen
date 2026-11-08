package platform

import (
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/saige/flow/address"
	"github.com/urmzd/saige/flow/connectors"
	"github.com/urmzd/saige/flow/event"
)

func TestVerifiedSuggestionEvidenceNeverBecomesHumanRating(t *testing.T) {
	st := state.NewMemory()
	cfg := Config{Workspace: t.TempDir(), Backends: Backends{State: st}}
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	c, err := p.Clients.Create("t", clients.Client{ID: "app", Name: "App", Surface: clients.GitHub, Identity: clients.Identity{Owners: []string{"acme"}}, Enabled: true, Agents: []string{"a"}}, clients.DefaultCaps(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	target := address.MustParse("github://acme/repo/pull/7")
	message := target.WithParam("review_comment", "42")
	tag := wire.NewEvent(wire.Raise{ID: "request", Tenant: "t", Agent: "a", Origin: "github", Channel: target.String(), At: time.Now(), Context: map[string]string{runs.MetaClient: c.ID}})
	if err := p.Runs.Record("r", tag, 1); err != nil {
		t.Fatal(err)
	}
	if err := p.deliveredFor(c)(t.Context(), connectors.Delivery{Tenant: "t", Run: "r", Target: target, Message: message, Finding: "f", Channel: target.String()}); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 40)
	id := "github:suggestion:42:" + sha
	b := event.Feedback{RunID: "r", MessageID: message.String(), FindingID: "f", Actor: event.Actor{ID: "commit:" + sha, Bot: true}, Rating: "up", Reasons: []string{"suggestion_committed"}, Surface: "github"}
	h := p.feedbackFor(c)
	if err := h.Record(t.Context(), "t", id, b); err == nil {
		t.Fatal("system observation accepted as human feedback")
	}
	if err := h.Evidence(t.Context(), "t", id, b); err != nil {
		t.Fatal(err)
	}
	key := "evidence:" + surfaceHash(c.ID, id)
	stored, err := p.surfaceRecords.table.Lookup("t", key)
	if err != nil || stored.Value.Evidence == nil {
		t.Fatal(stored, err)
	}
	if got := stored.Value.Evidence.Actor; got.ID != "system:github:commit:"+sha || !got.Bot {
		t.Fatal(got)
	}
	other, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })
	if err := other.feedbackFor(c).Evidence(t.Context(), "t", id, b); err != nil {
		t.Fatal(err)
	}
	replay, err := other.surfaceRecords.table.Lookup("t", key)
	if err != nil || !stored.Value.Updated.Equal(replay.Value.Updated) {
		t.Fatal("replay changed evidence timestamp", replay, err)
	}
	if ratings, err := other.Feedback.List("t", "r", 0); err != nil || len(ratings) != 0 {
		t.Fatal("commit evidence became human ratings", ratings, err)
	}
	for _, test := range []struct {
		name, tenant, id string
		change           func(*event.Feedback)
	}{
		{"tenant", "else", id, func(*event.Feedback) {}},
		{"actor", "t", id, func(b *event.Feedback) { b.Actor = event.Actor{ID: "alice"} }},
		{"comment identity", "t", "github:suggestion:43:" + sha, func(*event.Feedback) {}},
		{"unknown handle", "t", id, func(b *event.Feedback) { b.MessageID = target.WithParam("review_comment", "43").String() }},
		{"finding", "t", id, func(b *event.Feedback) { b.FindingID = "other" }},
		{"run", "t", id, func(b *event.Feedback) { b.RunID = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := b
			test.change(&changed)
			if err := h.Evidence(t.Context(), test.tenant, test.id, changed); err == nil {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
}
