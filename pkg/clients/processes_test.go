package clients

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database, each with its own store, see each other's
// clients at once; creates racing through both never pass the caps; and
// updates racing on one client through both are each kept, one after the
// other.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, err := Open(a)
		if err != nil {
			t.Fatal(err)
		}
		two, err := Open(b)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := one.Create("acme", slackOf("docs", "A0DOCS", "T01ABCDEF"), DefaultCaps(), t0); err != nil {
			t.Fatal(err)
		}
		if got, err := two.Get("acme", "docs"); err != nil || got.Tenant != "acme" || got.Version != 1 {
			t.Fatalf("the other process reads %+v, %v", got, err)
		}
		if got := on(t, two, Slack); len(got) != 1 || got[0].ID != "docs" {
			t.Fatalf("the other process finds on slack %+v", got)
		}
		// The same identity through the other process is refused.
		if _, err := two.Create("acme", slackOf("again", "A0DOCS", "T01ABCDEF"), DefaultCaps(), t0); !errors.Is(err, ErrExists) {
			t.Fatalf("a second client for one identity: %v", err)
		}

		caps := Caps{Max: 6, MaxPerSurface: 6}
		var made atomic.Int32
		var wg sync.WaitGroup
		for i := range 16 {
			s := []*Memory{one, two}[i%2]
			wg.Go(func() {
				_, err := s.Create("acme", githubOf(fmt.Sprintf("g%d", i), fmt.Sprintf("org%d", i)), caps, t0)
				switch {
				case err == nil:
					made.Add(1)
				case !errors.Is(err, ErrLimit):
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if made.Load() != 5 {
			t.Fatalf("%d creates passed a cap with room for 5", made.Load())
		}
		for _, s := range []*Memory{one, two} {
			if got := list(t, s, "acme"); len(got) != 6 || got[0].ID != "docs" {
				t.Fatalf("lists %+v", got)
			}
		}

		const updates = 20
		for i := range updates {
			s := []*Memory{one, two}[i%2]
			wg.Go(func() {
				c := slackOf("docs", "A0DOCS", "T01ABCDEF")
				c.Name = fmt.Sprintf("Docs %d", i)
				if _, err := s.Update("acme", c, t0); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		got, err := one.Get("acme", "docs")
		if err != nil || got.Version != updates+1 {
			t.Fatalf("after %d racing updates: %+v, %v", updates, got, err)
		}

		if err := two.Delete("acme", "docs"); err != nil {
			t.Fatal(err)
		}
		if _, err := one.Get("acme", "docs"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("a deleted client reads %v", err)
		}
		if got := list(t, one, "globex"); len(got) != 0 {
			t.Fatalf("another tenant lists %+v", got)
		}
	})
}

// Secrets written through one process read through another, and a drop
// through either forgets them for both.
func TestTwoProcessesShareSecrets(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, _ := OpenSecrets(a)
		two, _ := OpenSecrets(b)
		if err := one.Put("acme", "docs", SecretToken, "xoxb"); err != nil {
			t.Fatal(err)
		}
		if err := one.Put("acme", "docs/x", SecretToken, "other"); err != nil {
			t.Fatal(err)
		}
		if v, ok := two.Get("acme", "docs", SecretToken); !ok || v != "xoxb" {
			t.Fatalf("the other process reads %q, %v", v, ok)
		}
		if err := two.Drop("acme", "docs"); err != nil {
			t.Fatal(err)
		}
		if _, ok := one.Get("acme", "docs", SecretToken); ok {
			t.Fatal("a dropped secret is still read")
		}
		if v, _ := one.Get("acme", "docs/x", SecretToken); v != "other" {
			t.Fatal("dropping one client dropped another that shares its prefix")
		}
	})
}
