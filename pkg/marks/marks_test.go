package marks

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

var t0 = time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)

func TestArchiveIsPerPersonAndReversible(t *testing.T) {
	s := NewStore()
	if m, err := s.Get("acme", "ada"); err != nil || len(m.Archived) != 0 || m.Read == nil || m.Tips == nil || !m.Since.IsZero() {
		t.Fatalf("empty = %+v, %v", m, err)
	}
	before := time.Now().Add(-time.Second)
	_, _ = s.SetArchived("acme", "ada", "t1", true)
	m, err := s.SetArchived("acme", "ada", "t2", true)
	if err != nil || len(m.Archived) != 2 || m.Archived[0] != "t2" || m.Since.Before(before) {
		t.Fatalf("archived = %v since %v, %v", m.Archived, m.Since, err)
	}
	if m, _ := s.SetArchived("acme", "ada", "t1", true); m.Archived[0] != "t1" || len(m.Archived) != 2 {
		t.Fatalf("again = %v", m.Archived)
	}
	if m, _ := s.SetArchived("acme", "ada", "t1", false); len(m.Archived) != 1 || m.Archived[0] != "t2" {
		t.Fatalf("unarchived = %v", m.Archived)
	}
	if m, _ := s.Get("acme", "bob"); len(m.Archived) != 0 {
		t.Fatalf("another person sees %v", m.Archived)
	}
	if m, _ := s.Get("globex", "ada"); len(m.Archived) != 0 {
		t.Fatalf("another tenant sees %v", m.Archived)
	}
	for _, id := range []string{"", "a b", "a/b", "<x>", string(make([]byte, maxID+1))} {
		if _, err := s.SetArchived("acme", "ada", id, true); !errors.Is(err, ErrInvalid) {
			t.Fatalf("id %q: %v", id, err)
		}
	}
	if _, err := s.SetArchived("acme", "", "t1", true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("no subject: %v", err)
	}
}

func TestArchivedIsBounded(t *testing.T) {
	s := NewStore()
	for i := range MaxArchived {
		if _, err := s.SetArchived("acme", "ada", fmt.Sprintf("t%d", i), true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.SetArchived("acme", "ada", "one-more", true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("past the bound: %v", err)
	}
	if _, err := s.SetArchived("acme", "ada", "t3", true); err != nil {
		t.Fatalf("one already archived moves to the front: %v", err)
	}
}

func TestReadKeepsTheLatestAndForgetsTheOldest(t *testing.T) {
	s := NewStore()
	_, _ = s.MarkRead("acme", "ada", "t1", t0.Add(time.Minute))
	if m, _ := s.MarkRead("acme", "ada", "t1", t0); !m.Read["t1"].Equal(t0.Add(time.Minute)) {
		t.Fatalf("an earlier read moved it back: %v", m.Read["t1"])
	}
	for i := range MaxRead {
		_, _ = s.MarkRead("acme", "ada", fmt.Sprintf("r%d", i), t0.Add(time.Hour+time.Duration(i)*time.Second))
	}
	m, err := s.Get("acme", "ada")
	if err != nil || len(m.Read) != MaxRead {
		t.Fatalf("%d reads kept, %v", len(m.Read), err)
	}
	if _, ok := m.Read["t1"]; ok {
		t.Fatal("the read longest ago was kept past the bound")
	}
}

func TestTipsAreDismissedOnce(t *testing.T) {
	s := NewStore()
	_, _ = s.DismissTip("acme", "ada", "search")
	if m, _ := s.DismissTip("acme", "ada", "search"); len(m.Tips) != 1 {
		t.Fatalf("tips = %v", m.Tips)
	}
	for i := range MaxTips - 1 {
		_, _ = s.DismissTip("acme", "ada", fmt.Sprintf("tip-%d", i))
	}
	if _, err := s.DismissTip("acme", "ada", "past"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("past the bound: %v", err)
	}
}

func TestMarksOutliveAProcess(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, _ := Open(st)
		_, _ = s.SetArchived("acme", "ada", "t1", true)
		_, _ = s.MarkRead("acme", "ada", "t2", t0)
		_, _ = s.DismissTip("acme", "ada", "search")
		again, _ := Open(st)
		m, err := again.Get("acme", "ada")
		if err != nil || len(m.Archived) != 1 || !m.Read["t2"].Equal(t0) || len(m.Tips) != 1 {
			t.Fatalf("after restart = %+v, %v", m, err)
		}
	})
}
