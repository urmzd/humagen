package inbox

import (
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestProjectCountUsesReaderCountersAndConfinement(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		var first Message
		for i, project := range []string{"Field", "Field", "Other"} {
			m, _, err := s.Raise(acme, project+string(rune('a'+i)), Message{Kind: KindAlert, Subject: project, Source: Source{Project: project}, Parts: []Part{{Type: PartAlert}}}, t0)
			if err != nil {
				t.Fatal(err)
			}
			if first.ID == "" {
				first = m
			}
		}
		if _, err := s.Read(acme, first.ID, "ada", true, t0); err != nil {
			t.Fatal(err)
		}
		rebuilt, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []*Store{s, rebuilt} {
			q := Query{Reader: "ada", Sees: func(m Message) bool { return m.Source.Project != "Other" }}
			if got := countProject(s, acme, "Field", q); got.Unread != 1 {
				t.Fatalf("ada Field = %+v", got)
			}
			q.Reader = "bo"
			if got := countProject(s, acme, "Field", q); got.Unread != 2 {
				t.Fatalf("bo Field = %+v", got)
			}
			if got := countProject(s, acme, "Other", q); got != (Counts{}) {
				t.Fatalf("hidden Other = %+v", got)
			}
			if got := countProject(s, acme, "", q); got != (Counts{}) {
				t.Fatalf("workspace includes its descendants: %+v", got)
			}
		}
	})
}
