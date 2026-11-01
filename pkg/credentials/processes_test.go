package credentials_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/credentials"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database, each with its own store, read each other's
// credentials at once, and credentials one person adds through both at the
// same time are each kept, up to the cap and never past it.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		when := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
		one, _ := credentials.Open(a, nil)
		two, _ := credentials.Open(b, nil)
		if _, err := one.Put("acme", "ada", "issues", credentials.Input{Ref: "op://Ada/issues/token"}, when); err != nil {
			t.Fatal(err)
		}
		if c, ok, err := two.Get("acme", "ada", "issues"); err != nil || !ok || c.Ref != "op://Ada/issues/token" {
			t.Fatalf("the other process reads %+v, %v, %v", c, ok, err)
		}
		// Both mark it used at once: the later time is kept.
		var wg sync.WaitGroup
		for i := range 10 {
			s := []*credentials.Store{one, two}[i%2]
			wg.Go(func() {
				if err := s.MarkUsed("acme", "ada", "issues", when.Add(time.Duration(i+1)*time.Minute)); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if c, _, _ := one.Get("acme", "ada", "issues"); !c.LastUsedAt.Equal(when.Add(10 * time.Minute)) {
			t.Fatalf("last used %v", c.LastUsedAt)
		}

		tries := credentials.MaxPerPerson + 6
		var mu sync.Mutex
		limited := 0
		for i := range tries {
			s := []*credentials.Store{one, two}[i%2]
			wg.Go(func() {
				_, err := s.Put("acme", "ada", fmt.Sprintf("p%02d", i), credentials.Input{Ref: "op://Ada/issues/token"}, when)
				if errors.Is(err, credentials.ErrLimit) {
					mu.Lock()
					limited++
					mu.Unlock()
					return
				}
				if err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		got, err := two.List("acme", "ada")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != credentials.MaxPerPerson || limited != tries+1-credentials.MaxPerPerson {
			t.Fatalf("%d held and %d refused; want %d held", len(got), limited, credentials.MaxPerPerson)
		}
		// Removing one makes room through the other process.
		if err := one.Delete("acme", "ada", "issues"); err != nil {
			t.Fatal(err)
		}
		if _, err := two.Put("acme", "ada", "late", credentials.Input{Ref: "op://Ada/issues/token"}, when); err != nil {
			t.Fatal(err)
		}
		if got, _ := one.List("acme", "bo"); len(got) != 0 {
			t.Fatalf("bo holds %+v", got)
		}
	})
}
