package projects_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestChannelsAndExpertsSurviveARestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		mustCreate(t, s, "acme", team("Field", ""), ada)
		mustCreate(t, s, "acme", team("Launch", ""), ada)
		if _, err := s.CreateChannel("acme", projects.Channel{Project: "Field", Name: "renewals", Purpose: "Renewal work", Default: "globex-helper"}, ada, t0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateChannel("acme", channel("Field", "old"), ada, t0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ArchiveChannel("acme", "Field", "old", ada, t0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateChannel("acme", channel("Launch", "press"), ada, t0); err != nil {
			t.Fatal(err)
		}
		if err := s.Delete("acme", "Launch", ada, t0, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpsertChannel("acme", projects.Channel{Project: projects.Root, Name: "announcements", Purpose: "Company news"}, "file", t0); err != nil {
			t.Fatal(err)
		}
		setExperts(t, s, "Field", ada, expert("okta|ana", "billing"))
		mustCreate(t, s, "globex", team("Field", ""), ada)

		again, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		for _, project := range []string{projects.Root, "Field"} {
			want, _ := s.Channels("acme", project)
			got, err := again.Channels("acme", project)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("channels of %q after restart = %+v, %v; want %+v", project, got, err, want)
			}
		}
		if _, err := again.Channels("acme", "Launch"); !errors.Is(err, projects.ErrNotFound) {
			t.Fatalf("a deleted project's channels came back: %v", err)
		}
		if got, _ := again.Channels("globex", "Field"); len(got) != 1 {
			t.Fatalf("another tenant's channels = %v", names(got))
		}
		if who, from := again.Tree("acme").ExpertsFor("Field", "billing"); from != "Field" || !reflect.DeepEqual(who, []string{"okta|ana"}) {
			t.Fatalf("experts after restart = %v from %q", who, from)
		}
		h1, _ := s.History("acme", "Field", 0, 0)
		h2, _ := again.History("acme", "Field", 0, 0)
		if !reflect.DeepEqual(h1, h2) {
			t.Fatalf("history after restart = %+v, want %+v", h2, h1)
		}
		if _, err := again.UpdateChannel("acme", projects.Root, "announcements", 1, admin, t0, func(c *projects.Channel) error { c.Purpose = "x"; return nil }); !errors.Is(err, projects.ErrManaged) {
			t.Fatalf("a managed channel after restart = %v", err)
		}
	})
}

func TestAFailedChannelOrExpertWriteChangesNothing(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	s, err := projects.Open(f)
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, "acme", team("Field", ""), ada)
	if _, err := s.CreateChannel("acme", channel("Field", "renewals"), ada, t0); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Channels("acme", "Field")
	f.Fail(statetest.ErrInjected)
	if _, err := s.CreateChannel("acme", channel("Field", "press"), ada, t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("CreateChannel = %v", err)
	}
	if _, err := s.UpdateChannel("acme", "Field", "renewals", 1, ada, t0, func(c *projects.Channel) error { c.Title = "x"; return nil }); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("UpdateChannel = %v", err)
	}
	if _, err := s.ArchiveChannel("acme", "Field", "renewals", ada, t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("ArchiveChannel = %v", err)
	}
	if _, err := s.UpsertChannel("acme", channel("Field", "renewals"), "file", t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("UpsertChannel = %v", err)
	}
	if _, err := s.SetExperts("acme", "Field", 1, []projects.Expert{expert("okta|ana", "billing")}, ada, t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("SetExperts = %v", err)
	}
	if err := s.Delete("acme", "Field", ada, t0, nil); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Delete = %v", err)
	}
	after, _ := s.Channels("acme", "Field")
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("a failed write changed the channels: %+v", after)
	}
	if p, _ := s.Get("acme", "Field"); len(p.Experts) != 0 || p.Version != 1 {
		t.Fatalf("a failed write changed the experts: %+v", p)
	}
	if h, _ := s.History("acme", "Field", 0, 0); len(h) != 2 {
		t.Fatalf("a failed write recorded a change: %+v", h)
	}
	f.Fail(nil)
	again, err := projects.Open(f)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := again.Channels("acme", "Field"); !reflect.DeepEqual(got, before) {
		t.Fatalf("after restart = %+v, want %+v", got, before)
	}
}

func TestOpenRefusesChannelsThatAreNotAProjects(t *testing.T) {
	put := func(st state.Store, key string, v any) {
		t.Helper()
		raw, _ := json.Marshal(v)
		if err := st.Write(context.Background(), "projects", state.Put("acme", key, raw)); err != nil {
			t.Fatal(err)
		}
	}
	field := projects.Project{Name: "Field", Kind: projects.Team, Version: 1}
	for _, c := range []struct {
		name  string
		write func(st state.Store)
	}{
		{"a channel of a missing project", func(st state.Store) {
			put(st, "c:Gone/renewals", projects.Channel{Project: "Gone", Name: "renewals", Version: 1})
		}},
		{"a channel under another key", func(st state.Store) {
			put(st, "p:Field", field)
			put(st, "c:Field/renewals", projects.Channel{Project: "Field", Name: "press", Version: 1})
		}},
		{"an archived general", func(st state.Store) {
			put(st, "p:Field", field)
			put(st, "c:Field/general", projects.Channel{Project: "Field", Name: "general", Archived: true, Version: 1})
		}},
		{"experts that break the rules", func(st state.Store) {
			put(st, "p:Field", projects.Project{Name: "Field", Kind: projects.Team, Version: 1, Experts: []projects.Expert{{Who: "*", Topics: []string{"billing"}}}})
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			st := state.NewMemory()
			c.write(st)
			if _, err := projects.Open(st); !errors.Is(err, projects.ErrInvalid) {
				t.Fatalf("Open = %v, want ErrInvalid", err)
			}
		})
	}
}
