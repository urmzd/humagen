package threads_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestForkHistoryIsImmutableAndNeverAliasesTheCaller(t *testing.T) {
	s := threads.NewStore()
	source, first := start(t, s, "acme", shared("Globex", "general"), msg("dana", "original"))
	history := &threads.Fork{SourceThread: source.ID, Through: first.ID, By: "dana", At: t0, Context: []threads.ForkTurn{{Kind: threads.Person, By: "dana", Text: "original", At: t0}}}
	fork, err := s.Create("acme", threads.Thread{Project: "Globex", Channel: "general", Visibility: threads.Private, CreatedBy: "dana", Fork: history}, t0)
	if err != nil {
		t.Fatal(err)
	}
	history.Context[0].Text = "changed"
	fork.Fork.Context[0].Text = "changed again"
	got, _ := s.Get("acme", fork.ID)
	if got.Fork.Context[0].Text != "original" {
		t.Fatalf("snapshot aliases caller: %+v", got.Fork)
	}
	_, err = s.Update("acme", fork.ID, got.Version, t0, func(th *threads.Thread) error { th.Fork.Context[0].Text = "rewritten"; th.Title = "edited"; return nil })
	if !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("mutated lineage: %v", err)
	}
}

func TestForkHistoryIsBoundedAndCannotNameItself(t *testing.T) {
	s := threads.NewStore()
	base := threads.Thread{ID: "th-0123456789abcdef", Project: "Globex", Channel: "general", Visibility: threads.Private, CreatedBy: "dana", Fork: &threads.Fork{SourceThread: "th-0123456789abcdef", Through: "m-1", By: "dana", At: t0, Context: []threads.ForkTurn{{Kind: threads.Person, By: "dana", Text: "x", At: t0}}}}
	if _, err := s.Create("acme", base, t0); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("self lineage: %v", err)
	}
	base.Fork.SourceThread = "th-1111111111111111"
	base.Fork.Context = make([]threads.ForkTurn, threads.MaxForkTurns+1)
	if _, err := s.Create("acme", base, t0); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("unbounded context: %v", err)
	}
}

func TestForkHistorySurvivesRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := threads.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		source, first := start(t, s, "acme", shared("Globex", "general"), msg("dana", "context"))
		fork, err := s.Create("acme", threads.Thread{Project: source.Project, Channel: source.Channel, Visibility: threads.Private, CreatedBy: "dana", Fork: &threads.Fork{SourceThread: source.ID, Through: first.ID, By: "dana", At: t0, Context: []threads.ForkTurn{{Kind: threads.Person, By: "dana", Text: "context", At: t0}}}}, t0)
		if err != nil {
			t.Fatal(err)
		}
		again, err := threads.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		got, err := again.Get("acme", fork.ID)
		if err != nil || !reflect.DeepEqual(got, fork) {
			t.Fatalf("restart: %+v %v", got, err)
		}
	})
}
