package contextflags

import (
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"testing"
)

func TestPrivateClassificationSurvivesReplayAndFailedWrites(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := statetest.NewFaulty(open(t))
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		st.Fail(statetest.ErrInjected)
		if err := s.Mark("acme", "run-one"); err == nil || s.Has("acme", "run-one") {
			t.Fatal("failed security classification was accepted")
		}
		st.Fail(nil)
		if err := s.Mark("acme", "run-one"); err != nil {
			t.Fatal(err)
		}
		s, err = Open(st)
		if err != nil || !s.Has("acme", "run-one") || s.Has("other", "run-one") {
			t.Fatal("classification replay or tenant boundary failed")
		}
	})
}
