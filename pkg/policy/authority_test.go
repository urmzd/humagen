package policy

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

func TestAnOwnersLimitIsOnAProjectAndRoundTrips(t *testing.T) {
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	st := state.NewMemory()
	s, err := Open(st)
	if err != nil {
		t.Fatal(err)
	}
	owned := Policy{Scope: Scope{Project, "Globex"}, Period: Day, MaxRuns: 5, Action: Block, Authority: AuthorityOwner}
	if _, err := s.Put("acme", owned, "ada", at); err != nil {
		t.Fatal(err)
	}
	admin := Policy{Scope: Scope{Project, "Field"}, Period: Day, MaxRuns: 50, Action: Block}
	if _, err := s.Put("acme", admin, "root", at); err != nil {
		t.Fatal(err)
	}
	again, err := Open(st)
	if err != nil {
		t.Fatal(err)
	}
	// List orders project scopes by name: Field, then Globex.
	got := again.List("acme")
	if len(got) != 2 || got[1].Scope.Name != "Globex" || got[1].Authority != AuthorityOwner || !got[1].ByOwner() {
		t.Fatalf("after restart = %+v", got)
	}
	if got[0].ByOwner() || got[0].Authority != "" {
		t.Fatalf("an admin's policy after restart = %+v", got[0])
	}
	for name, p := range map[string]Policy{
		"an owner on the workspace": {Scope: Scope{Kind: Tenant}, Period: Day, MaxRuns: 5, Action: Block, Authority: AuthorityOwner},
		"an owner on an agent":      {Scope: Scope{Agent, "lead"}, Period: Day, MaxRuns: 5, Action: Block, Authority: AuthorityOwner},
		"an unknown authority":      {Scope: Scope{Project, "Globex"}, Period: Day, MaxRuns: 5, Action: Block, Authority: "root"},
	} {
		if err := p.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
	if err := (Policy{Scope: Scope{Project, "Globex"}, Period: Day, MaxRuns: 5, Action: Block, Authority: AuthorityAdmin}).Validate(); err != nil {
		t.Fatalf("an admin named as such: %v", err)
	}
}

func TestTighterNamesTheScopeThatBinds(t *testing.T) {
	chain := []Policy{
		{Scope: Scope{Project, "Globex"}, Period: Day, MaxRuns: 100, Action: Block},
		{Scope: Scope{Project, "Field"}, Period: Day, MaxRuns: 50, SpendLimit: 10_000_000, MaxStorage: 1 << 20, Action: Block},
		{Scope: Scope{Kind: Tenant}, Period: Month, MaxRuns: 1000, Action: Block},
	}
	p := Policy{Scope: Scope{Project, "Globex"}, Period: Day, MaxRuns: 40, Action: Block, Authority: AuthorityOwner}
	// Its own scope is what it replaces, not what binds it.
	if err := Tighter(p, chain); err != nil {
		t.Fatalf("a tighter limit: %v", err)
	}
	for name, c := range map[string]struct {
		edit func(*Policy)
		want string
	}{
		"more runs":    {func(p *Policy) { p.MaxRuns = 60 }, "project Field"},
		"more spend":   {func(p *Policy) { p.SpendLimit = 20_000_000 }, "project Field"},
		"more storage": {func(p *Policy) { p.Period, p.MaxStorage = Month, 2<<20 }, "project Field"},
	} {
		q := p
		c.edit(&q)
		err := Tighter(q, chain)
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want the error to name %s", name, err, c.want)
		}
	}
	// Limits of different periods are not compared number for number.
	if err := Tighter(Policy{Scope: Scope{Project, "Globex"}, Period: Month, MaxRuns: 900, Action: Block}, chain); err != nil {
		t.Fatalf("a monthly limit under a daily one: %v", err)
	}
	if err := Tighter(p, nil); err != nil {
		t.Fatalf("nothing in force: %v", err)
	}
}

// An owner's limit only tightens what is over it, and never takes the place
// of an admin's: both are decided under the store's lock, so a racing admin
// write is never overwritten by an owner.
func TestAnOwnersLimitOnlyTightensAndNeverReplacesAnAdmins(t *testing.T) {
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	s := NewStore()
	if _, err := s.Put("acme", Policy{Scope: Scope{Project, "Field"}, Period: Day, MaxRuns: 50, Action: Block}, "root", at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put("acme", Policy{Scope: Scope{Kind: Tenant}, Period: Day, SpendLimit: 9_000_000, Action: Block}, "root", at); err != nil {
		t.Fatal(err)
	}
	looser := Policy{Scope: Scope{Project, "Globex"}, Period: Day, MaxRuns: 60, Action: Block}
	if _, err := s.PutOwned("acme", looser, []string{"Field"}, "ada", at); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "project Field") {
		t.Fatalf("looser than Field = %v", err)
	}
	spend := Policy{Scope: Scope{Project, "Globex"}, Period: Day, SpendLimit: 10_000_000, Action: Block}
	if _, err := s.PutOwned("acme", spend, []string{"Field"}, "ada", at); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "the workspace") {
		t.Fatalf("looser than the tenant = %v", err)
	}
	tight := Policy{Scope: Scope{Project, "Globex"}, Period: Day, MaxRuns: 5, Action: Block}
	saved, err := s.PutOwned("acme", tight, []string{"Field"}, "ada", at)
	if err != nil || !saved.ByOwner() || saved.UpdatedBy != "ada" {
		t.Fatalf("a tighter limit = %+v, %v", saved, err)
	}
	// An admin may replace it; then the owner may not.
	if _, err := s.Put("acme", Policy{Scope: Scope{Project, "Globex"}, Period: Day, MaxRuns: 8, Action: Block}, "root", at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutOwned("acme", tight, []string{"Field"}, "ada", at); !errors.Is(err, ErrAdmin) {
		t.Fatalf("an owner over an admin's limit = %v", err)
	}
	if err := s.DeleteOwned("acme", Scope{Project, "Globex"}, "ada", at); !errors.Is(err, ErrAdmin) {
		t.Fatalf("an owner removing an admin's limit = %v", err)
	}
	if err := s.DeleteOwned("acme", Scope{Project, "Initech"}, "ada", at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removing a limit nobody set = %v", err)
	}
	if err := s.Delete("acme", Scope{Project, "Globex"}, "root", at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutOwned("acme", tight, []string{"Field"}, "ada", at); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteOwned("acme", Scope{Project, "Globex"}, "ada", at); err != nil {
		t.Fatalf("an owner removing their own limit = %v", err)
	}
	if got := s.List("acme"); len(got) != 2 {
		t.Fatalf("left = %+v", got)
	}
}
