package reposettings

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

var now = time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)

// Settings are kept per client and repository, read back across a reopen,
// and never seen by another workspace or another client.
func TestPutGetListAcrossReopen(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s := Open(st)
		kept, err := s.Put("acme", "gh", Settings{
			Repository:   " Acme/Site ",
			AutoReview:   &AutoReview{Opened: true},
			Agent:        "@reviewer",
			Instructions: "  Run make check first.  ",
			Reads:        []string{"linked_issues", "pull_diff", "pull_diff"},
		}, 0, "ada", now)
		if err != nil {
			t.Fatal(err)
		}
		if kept.Version != 1 || kept.Repository != "Acme/Site" || kept.Agent != "reviewer" || kept.Instructions != "Run make check first." ||
			strings.Join(kept.Reads, ",") != "pull_diff,linked_issues" || kept.UpdatedBy != "ada" || !kept.UpdatedAt.Equal(now) {
			t.Fatalf("kept %+v", kept)
		}
		again, err := Open(st).Get("acme", "gh", "acme/site")
		if err != nil || again.Agent != "reviewer" || again.AutoReview == nil || !again.AutoReview.Opened || again.AutoReview.Pushes {
			t.Fatalf("read after reopen %+v, %v", again, err)
		}
		if _, err := s.Get("globex", "gh", "acme/site"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("another workspace reads it: %v", err)
		}
		if _, err := s.Get("acme", "gh-2", "acme/site"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("another client reads it: %v", err)
		}
		if _, err := s.Put("acme", "gh", Settings{Repository: "acme/api", Agent: "triage"}, 0, "ada", now); err != nil {
			t.Fatal(err)
		}
		// A client id that begins another's never lists the other's.
		if _, err := s.Put("acme", "gh/x", Settings{Repository: "acme/api", Agent: "triage"}, 0, "ada", now); err != nil {
			t.Fatal(err)
		}
		list, err := s.List("acme", "gh")
		if err != nil || len(list) != 2 || list[0].Repository != "acme/api" || list[1].Repository != "Acme/Site" {
			t.Fatalf("list %+v, %v", list, err)
		}
		if other, _ := s.List("globex", "gh"); len(other) != 0 {
			t.Fatalf("another workspace lists %+v", other)
		}
	})
}

// A write names the version it read: creating twice, or writing over a
// change made since, is refused, and nothing is lost.
func TestVersionsGuardEveryWrite(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		s := Open(open(t))
		first, err := s.Put("acme", "gh", Settings{Repository: "acme/site", Agent: "reviewer"}, 0, "ada", now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Put("acme", "gh", Settings{Repository: "acme/site", Agent: "other"}, 0, "bo", now); !errors.Is(err, ErrVersion) {
			t.Fatalf("a second create: %v, want ErrVersion", err)
		}
		second, err := s.Put("acme", "gh", Settings{Repository: "acme/site", Agent: "triage"}, first.Version, "bo", now)
		if err != nil || second.Version != first.Version+1 || second.Agent != "triage" {
			t.Fatalf("update %+v, %v", second, err)
		}
		if _, err := s.Put("acme", "gh", Settings{Repository: "acme/site", Agent: "late"}, first.Version, "cy", now); !errors.Is(err, ErrVersion) {
			t.Fatalf("a stale write: %v, want ErrVersion", err)
		}
		if err := s.Delete("acme", "gh", "acme/site", first.Version); !errors.Is(err, ErrVersion) {
			t.Fatalf("a stale delete: %v, want ErrVersion", err)
		}
		if err := s.Delete("acme", "gh", "acme/site", second.Version); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Get("acme", "gh", "acme/site"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleted settings read back: %v", err)
		}
		if err := s.Delete("acme", "gh", "acme/site", 1); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleting what is not kept: %v", err)
		}
	})
}

// Settings that change nothing are not kept: writing them returns the
// repository to its client.
func TestZeroSettingsRemoveTheRecord(t *testing.T) {
	s := Open(nil)
	kept, err := s.Put("acme", "gh", Settings{Repository: "acme/site", Instructions: "x"}, 0, "ada", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put("acme", "gh", Settings{Repository: "acme/site", Instructions: "   "}, kept.Version, "ada", now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("acme", "gh", "acme/site"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("zero settings are kept: %v", err)
	}
	if got, err := s.Put("acme", "gh", Settings{Repository: "acme/site"}, 0, "ada", now); err != nil || got.Version != 0 {
		t.Fatalf("zero settings on nothing: %+v, %v", got, err)
	}
	// Automatic review switched off is a setting, not nothing.
	off, err := s.Put("acme", "gh", Settings{Repository: "acme/site", AutoReview: &AutoReview{}}, 0, "ada", now)
	if err != nil || off.Version != 1 || off.AutoReview == nil || off.AutoReview.On() {
		t.Fatalf("off is kept as %+v, %v", off, err)
	}
}

// Every problem is reported, and nothing invalid is kept.
func TestInvalidSettings(t *testing.T) {
	s := Open(nil)
	for _, v := range []Settings{
		{Repository: "acme", Agent: "x"},
		{Repository: "acme/", Agent: "x"},
		{Repository: "acme/../x", Agent: "x"},
		{Repository: "ac_me/site", Agent: "x"},
		{Repository: "acme/si te", Agent: "x"},
		{Repository: "acme/site", Agent: "two words"},
		{Repository: "acme/site", Agent: "a:b"},
		{Repository: "acme/site", Instructions: strings.Repeat("i", MaxInstructions+1)},
		{Repository: "acme/site", Instructions: "a\x00b"},
		{Repository: "acme/site", Reads: []string{"everything"}},
	} {
		if _, err := s.Put("acme", "gh", v, 0, "ada", now); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v, want ErrInvalid", v, err)
		}
	}
	if _, err := s.Put("acme", "", Settings{Repository: "acme/site", Agent: "x"}, 0, "ada", now); !errors.Is(err, ErrInvalid) {
		t.Errorf("no client: %v", err)
	}
	if list, _ := s.List("acme", "gh"); len(list) != 0 {
		t.Fatalf("invalid settings were kept: %+v", list)
	}
}

// A client keeps settings for at most MaxPerClient repositories.
func TestSettingsAreBounded(t *testing.T) {
	s := Open(nil)
	for i := range MaxPerClient {
		if _, err := s.Put("acme", "gh", Settings{Repository: fmt.Sprintf("acme/r%04d", i), Agent: "x"}, 0, "ada", now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Put("acme", "gh", Settings{Repository: "acme/one-more", Agent: "x"}, 0, "ada", now); !errors.Is(err, ErrLimit) {
		t.Fatalf("past the bound: %v, want ErrLimit", err)
	}
	// Another client has its own bound.
	if _, err := s.Put("acme", "gh-2", Settings{Repository: "acme/one-more", Agent: "x"}, 0, "ada", now); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteClient("acme", "gh"); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.List("acme", "gh"); len(list) != 0 {
		t.Fatalf("a removed client keeps %d", len(list))
	}
	if list, _ := s.List("acme", "gh-2"); len(list) != 1 {
		t.Fatalf("removing one client removed another's: %d", len(list))
	}
}
