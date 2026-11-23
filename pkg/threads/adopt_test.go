package threads_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/threads"
)

// A conversation is adopted as a session under its own id: its record
// gains the session and its one agent, its version moves, and where it is
// filed, whether it is shared or private, who takes part, its surface and
// its messages stay as they are. Adopting it again changes nothing, and a
// record that cannot be a session is refused unchanged.
func TestAdoptKeepsEverythingButTheSessionAndItsAgent(t *testing.T) {
	s := threads.NewStore()
	private := threads.Thread{Project: "Globex", Channel: "renewals", Visibility: threads.Private, Surface: &threads.Surface{Client: "app", Channel: "D0ANA", TS: "1700000000.000100"}}
	th, _ := start(t, s, "acme", private, msg("dana", "Where are we on the renewal?"))
	if _, err := s.Participants("acme", th.ID, th.Version, []threads.Participant{{Kind: threads.Person, Name: "eve"}}, nil, func(threads.Participant) bool { return true }, "dana", t0); err != nil {
		t.Fatal(err)
	}
	before, err := s.Get("acme", th.ID)
	if err != nil {
		t.Fatal(err)
	}
	identity := threads.SessionIdentity{Agent: "lead", Parent: "slack://T0TEAM/D0ANA/1700000000.000100"}
	if _, err := s.Adopt("acme", th.ID, before.Version-1, identity, t0.Add(time.Hour)); !errors.Is(err, threads.ErrVersion) {
		t.Fatalf("adopting at a stale version = %v", err)
	}
	got, err := s.Adopt("acme", th.ID, before.Version, identity, t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case got.ID != before.ID || got.Version != before.Version+1:
		t.Fatalf("adopted as %s at version %d, from %s at %d", got.ID, got.Version, before.ID, before.Version)
	case got.Session == nil || got.Session.Agent != "lead" || got.Session.Parent != identity.Parent || got.Session.Label != "":
		t.Fatalf("its session = %+v", got.Session)
	case got.Default != "lead" || len(got.Agents) != 1 || got.Agents[0] != "lead" || got.Mode != "":
		t.Fatalf("its agents = %q %v %q", got.Default, got.Agents, got.Mode)
	case got.Project != before.Project || got.Channel != before.Channel || got.Visibility != before.Visibility || got.CreatedBy != before.CreatedBy:
		t.Fatalf("it moved: %+v", got)
	case fmt.Sprint(got.Participants) != fmt.Sprint(before.Participants):
		t.Fatalf("its participants = %+v, were %+v", got.Participants, before.Participants)
	case got.Surface == nil || *got.Surface != *before.Surface:
		t.Fatalf("its surface = %+v", got.Surface)
	case !got.CreatedAt.Equal(before.CreatedAt) || !got.UpdatedAt.Equal(before.UpdatedAt):
		t.Fatalf("its times = %s, %s; were %s, %s", got.CreatedAt, got.UpdatedAt, before.CreatedAt, before.UpdatedAt)
	case !threads.Adopted(got) || threads.Adopted(before):
		t.Fatal("Adopted does not tell the two apart")
	}
	if found, ok, err := s.BySurface("acme", *before.Surface); err != nil || !ok || found.ID != th.ID {
		t.Fatalf("its surface finds %q, %v, %v", found.ID, ok, err)
	}
	if ms, _, err := s.Messages("acme", th.ID, "", 10); err != nil || len(ms) != 1 || ms[0].Text != "Where are we on the renewal?" {
		t.Fatalf("its messages = %+v, %v", ms, err)
	}
	again, err := s.Adopt("acme", th.ID, before.Version, identity, t0.Add(2*time.Hour))
	if err != nil || again.Version != got.Version {
		t.Fatalf("adopted again = version %d, %v; want %d unchanged", again.Version, err, got.Version)
	}
	if _, err := s.Adopt("acme", th.ID, got.Version, threads.SessionIdentity{Agent: "helper", Parent: identity.Parent}, t0); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("adopting a session as another agent's = %v", err)
	}
	// Another tenant's conversation of the same id is another record.
	if _, err := s.Adopt("globex", th.ID, before.Version, identity, t0); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("adopting another tenant's = %v", err)
	}

	refuse := func(name string, th threads.Thread, m threads.Message, id threads.SessionIdentity) {
		t.Helper()
		made, _ := start(t, s, "acme", th, m)
		if _, err := s.Adopt("acme", made.ID, made.Version, id, t0); !errors.Is(err, threads.ErrInvalid) {
			t.Fatalf("%s: adopt = %v", name, err)
		}
		if kept, err := s.Get("acme", made.ID); err != nil || kept.Session != nil || kept.Version != made.Version {
			t.Fatalf("%s: the refused record changed: %+v, %v", name, kept, err)
		}
	}
	two := shared("Globex", "renewals")
	two.Agents, two.Mode = []string{"lead", "helper"}, threads.ModeAll
	refuse("two agents", two, msg("dana", "both of you"), threads.SessionIdentity{Agent: "lead", Parent: "console://x"})
	other := shared("Globex", "renewals")
	other.Agents = []string{"helper"}
	refuse("another agent", other, msg("dana", "helper only"), threads.SessionIdentity{Agent: "lead", Parent: "console://x"})
	refuse("no agent", shared("Globex", "renewals"), msg("dana", "nobody"), threads.SessionIdentity{Parent: "console://x"})
	refuse("no parent", shared("Globex", "renewals"), msg("dana", "nowhere"), threads.SessionIdentity{Agent: "lead"})
	// A conversation from before threads is kept under its first run's id.
	if _, err := s.Create("acme", threads.Thread{ID: run, Project: "Globex", Channel: "renewals", Visibility: threads.Shared, CreatedBy: "dana"}, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Adopt("acme", run, 1, threads.SessionIdentity{Agent: "lead", Parent: "console://x"}, t0); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("adopting a conversation from before threads = %v", err)
	}
}

// A session's parent is another session when it is a session key's id or a
// thread's id, and a place otherwise.
func TestSessionParentTellsASessionFromAPlace(t *testing.T) {
	for parent, want := range map[string]bool{
		"s_0123456789abcdef0123456789abcdef": true, "th-0123456789abcdef": true,
		"console://th-0123456789abcdef": false, "slack://T0TEAM/C01/1700000000.000100": false,
		"github://acme/handbook/pull/12": false, "schedule://nightly": false, "": false,
	} {
		if got := threads.SessionParent(parent); got != want {
			t.Errorf("SessionParent(%q) = %v, want %v", parent, got, want)
		}
	}
}
