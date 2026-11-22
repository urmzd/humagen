package accounts_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/accounts"
	"github.com/urmzd/humagen/pkg/state"
)

func account(surface, id, subject string) accounts.Account {
	return accounts.Account{Surface: surface, ID: id, Login: "login-" + id, Subject: subject, At: time.Unix(1, 0).UTC()}
}

func TestOnePersonPerSurfaceUser(t *testing.T) {
	s := accounts.Open(state.NewMemory())
	if _, err := s.Link("t", account(accounts.GitHub, "42", "ada")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Link("t", account(accounts.GitHub, "42", "bob")); !errors.Is(err, accounts.ErrClaimed) {
		t.Fatalf("second claim: %v", err)
	}
	got, err := s.Get("t", accounts.GitHub, "42")
	if err != nil || got.Subject != "ada" {
		t.Fatalf("claim moved: %+v %v", got, err)
	}
	// The same user again refreshes the login.
	again := account(accounts.GitHub, "42", "ada")
	again.Login = "renamed"
	if _, err := s.Link("t", again); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Of("t", accounts.GitHub, "ada"); got.Login != "renamed" {
		t.Fatalf("login not refreshed: %+v", got)
	}
	// The same id on another surface is another user.
	if _, err := s.Link("t", account(accounts.Slack, "42", "bob")); err != nil {
		t.Fatal(err)
	}
}

func TestUnlinkFreesTheClaim(t *testing.T) {
	s := accounts.Open(state.NewMemory())
	if _, err := s.Link("t", account(accounts.GitHub, "42", "ada")); err != nil {
		t.Fatal(err)
	}
	if err := s.Unlink("t", accounts.GitHub, "ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Of("t", accounts.GitHub, "ada"); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatalf("still linked: %v", err)
	}
	if _, err := s.Link("t", account(accounts.GitHub, "42", "bob")); err != nil {
		t.Fatalf("freed claim refused: %v", err)
	}
	if err := s.Unlink("t", accounts.GitHub, "nobody"); err != nil {
		t.Fatalf("unlinking nothing: %v", err)
	}
}

func TestRelinkingAnotherUserDropsTheFirst(t *testing.T) {
	s := accounts.Open(state.NewMemory())
	if _, err := s.Link("t", account(accounts.GitHub, "1", "ada")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Link("t", account(accounts.GitHub, "2", "ada")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("t", accounts.GitHub, "1"); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatalf("old account kept: %v", err)
	}
	if got, _ := s.Of("t", accounts.GitHub, "ada"); got.ID != "2" {
		t.Fatalf("person points at %q", got.ID)
	}
	list, err := s.List("t", accounts.GitHub, 10)
	if err != nil || len(list) != 1 || list[0].ID != "2" {
		t.Fatalf("list: %+v %v", list, err)
	}
}

func TestTenantsAreApart(t *testing.T) {
	s := accounts.Open(state.NewMemory())
	if _, err := s.Link("t1", account(accounts.GitHub, "42", "ada")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("t2", accounts.GitHub, "42"); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatalf("another tenant read it: %v", err)
	}
	if _, err := s.Link("t2", account(accounts.GitHub, "42", "bob")); err != nil {
		t.Fatalf("another tenant's claim blocked this one: %v", err)
	}
	if list, _ := s.List("t2", accounts.GitHub, 10); len(list) != 1 || list[0].Subject != "bob" {
		t.Fatalf("t2 list: %+v", list)
	}
}

func TestConcurrentClaimsOneWins(t *testing.T) {
	s := accounts.Open(state.NewMemory())
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.Link("t", account(accounts.GitHub, "42", string(rune('a'+i))))
		}()
	}
	wg.Wait()
	won := 0
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, accounts.ErrClaimed):
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if won != 1 {
		t.Fatalf("%d claims won", won)
	}
}

func TestInvalidAccounts(t *testing.T) {
	s := accounts.Open(nil)
	for _, a := range []accounts.Account{
		account("gitlab", "1", "ada"),
		account(accounts.GitHub, "", "ada"),
		account(accounts.GitHub, "a/b", "ada"),
		account(accounts.GitHub, "1", ""),
	} {
		if _, err := s.Link("t", a); !errors.Is(err, accounts.ErrInvalid) {
			t.Fatalf("%+v: %v", a, err)
		}
	}
}
