package prefs

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database, each with its own store, read each other's
// writes at once, and changes to different settings of one person made
// through both at the same time are each kept.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, _ := Open(a)
		two, _ := Open(b)
		if _, err := one.Set("acme", "ada", Preferences{DisplayName: "Ada", Theme: Dark, Accent: "teal"}); err != nil {
			t.Fatal(err)
		}
		if got := get(t, two, "acme", "ada"); got.DisplayName != "Ada" || got.Theme != Dark {
			t.Fatalf("the other process reads %+v", got)
		}
		if _, err := two.SetPhoto("acme", "ada", []byte("\x89PNG\r\n\x1a\n0000")); err != nil {
			t.Fatal(err)
		}
		if _, mime, err := one.Photo("acme", "ada"); err != nil || mime != "image/png" {
			t.Fatalf("Photo = %q, %v", mime, err)
		}

		const pins = 20
		var wg sync.WaitGroup
		for i := range pins {
			s := []*Store{one, two}[i%2]
			wg.Go(func() {
				if _, err := s.change("acme", "ada", func(p *Preferences) error {
					p.Pinned = append(p.Pinned, fmt.Sprintf("t%02d", i))
					return nil
				}); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Go(func() {
			if _, err := one.MarkOnboarded("acme", "ada", time.Now()); err != nil {
				t.Error(err)
			}
		})
		wg.Go(func() {
			if _, err := two.SetMuted("acme", "ada", []string{"Launch"}); err != nil {
				t.Error(err)
			}
		})
		wg.Wait()
		got := get(t, one, "acme", "ada")
		slices.Sort(got.Pinned)
		if len(got.Pinned) != pins || got.OnboardedAt.IsZero() || !slices.Equal(got.Muted, []string{"Launch"}) || got.DisplayName != "Ada" || !got.HasPhoto {
			t.Fatalf("a change was lost: %+v", got)
		}
	})
}

// An email opt-in changed through one process makes a change from what was
// read before it, made through another, ErrVersion.
func TestTwoProcessesKeepEmailOptIn(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, _ := Open(a)
		two, _ := Open(b)
		base := Preferences{Theme: System, Accent: Accents[0]}
		read := get(t, two, "acme", "ada").Personal
		in := read
		in.Email, in.EmailVersion = true, read.EmailVersion+1
		if _, err := one.Set("acme", "ada", base, in); err != nil {
			t.Fatal(err)
		}
		stale := read
		stale.Email, stale.EmailVersion = false, read.EmailVersion+1
		if _, err := two.Set("acme", "ada", base, stale); !errors.Is(err, ErrVersion) {
			t.Fatalf("a stale opt-out = %v, want ErrVersion", err)
		}
		if got := get(t, two, "acme", "ada").Personal; !got.Email || got.EmailVersion != 1 {
			t.Fatalf("opt-in = %+v", got)
		}
	})
}
