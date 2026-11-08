package githubinstall

import (
	"net/url"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
)

func TestInstallationReceiptTracksCurrentVerifiedOwnerAcrossRestart(t *testing.T) {
	f := fixture(t, "")
	st := state.NewMemory()
	m, err := New(st, f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	connect := func(person string) string {
		a, err := m.Begin(t.Context(), "acme", person)
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(a.URL)
		nonce := u.Query().Get("state")
		if _, err = m.Complete(t.Context(), "acme", person, nonce, "one-use-code", "42"); err != nil {
			t.Fatal(err)
		}
		return nonce
	}
	if _, err = m.Receipt(t.Context(), "acme", "123", "42"); err == nil {
		t.Fatal("invented legacy owner")
	}
	aliceNonce := connect("alice")
	alice, err := m.Receipt(t.Context(), "acme", "123", "42")
	if err != nil || alice.Person != "alice" || alice.Generation == "" {
		t.Fatal(alice, err)
	}
	connect("bob")
	m, err = New(st, f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := m.Receipt(t.Context(), "acme", "123", "42")
	if err != nil || bob.Person != "bob" || bob.Generation == alice.Generation {
		t.Fatal(bob, err)
	}
	if _, err = m.Complete(t.Context(), "acme", "alice", aliceNonce, "one-use-code", "42"); err == nil {
		t.Fatal("old callback recovered superseded receipt")
	}
	for _, scope := range [][3]string{{"other", "123", "42"}, {"acme", "456", "42"}, {"acme", "123", "43"}} {
		if _, err = m.Receipt(t.Context(), scope[0], scope[1], scope[2]); err == nil {
			t.Fatal("cross-scope owner receipt")
		}
	}
	if err = m.Revoke(t.Context(), "acme"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Receipt(t.Context(), "acme", "123", "42"); err == nil {
		t.Fatal("disconnected receipt still current")
	}
}

func TestInstallationDisconnectFencesInFlightVerifiedReceipt(t *testing.T) {
	f := fixture(t, "blocked")
	m, err := New(state.NewMemory(), f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	nonce := begin(t, m)
	finished := make(chan error, 1)
	go func() {
		_, err := m.Complete(t.Context(), "acme", "alice", nonce, "one-use-code", "42")
		finished <- err
	}()
	<-f.entered
	if err = m.Revoke(t.Context(), "acme"); err != nil {
		t.Fatal(err)
	}
	close(f.release)
	if err = <-finished; err == nil {
		t.Fatal("late verified callback revived disconnected receipt")
	}
	if _, err = m.Receipt(t.Context(), "acme", "123", "42"); err == nil {
		t.Fatal("late callback persisted owner")
	}
}
