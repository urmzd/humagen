package credentials_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/credentials"
	"github.com/urmzd/warp/pkg/state"
	"github.com/urmzd/warp/pkg/state/statetest"
)

var at = time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)

const token = "tok-ada-0123456789"

func open(t *testing.T, st state.Store, secrets credentials.Secrets) *credentials.Store {
	t.Helper()
	s, err := credentials.Open(st, secrets)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A reference is kept as a name and a value is kept in the secrets store,
// and both come back after a restart.
func TestCredentialsSurviveARestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, newStore func(*testing.T) state.Store) {
		st := newStore(t)
		secrets, err := clients.OpenSecrets(st)
		if err != nil {
			t.Fatal(err)
		}
		s := open(t, st, secrets)
		if _, err := s.Put("acme", "ada", "issues", credentials.Input{Ref: "op://Ada/issues/token"}, at); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Put("acme", "ada", "drive", credentials.Input{Value: token}, at); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Put("acme", "okta|00u/bo", "issues", credentials.Input{Ref: "keychain:issues/bo"}, at); err != nil {
			t.Fatal(err)
		}

		reopened, err := clients.OpenSecrets(st)
		if err != nil {
			t.Fatal(err)
		}
		again := open(t, st, reopened)
		got := again.List("acme", "ada")
		if len(got) != 2 || got[0].Provider != "drive" || got[0].Kind != credentials.Value || got[1].Ref != "op://Ada/issues/token" {
			t.Fatalf("after a restart ada holds %+v", got)
		}
		if v, ok := again.Value("acme", "ada", "drive"); !ok || v != token {
			t.Fatalf("the stored value came back as %q, %v", v, ok)
		}
		if c, ok := again.Get("acme", "okta|00u/bo", "issues"); !ok || c.Ref != "keychain:issues/bo" {
			t.Fatalf("a subject with a slash in it lost its credential: %+v, %v", c, ok)
		}
	})
}

// A value is never part of what the store gives out, however it is printed.
func TestAValueIsNeverShown(t *testing.T) {
	s := credentials.New(clients.NewMemorySecrets())
	c, err := s.Put("acme", "ada", "drive", credentials.Input{Value: token}, at)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(s.List("acme", "ada"))
	in := credentials.Input{Value: token}
	for _, shown := range []string{string(raw), fmt.Sprintf("%v %+v %#v %s", c, c, c, c), fmt.Sprintf("%v %+v %#v %s", in, in, in, in)} {
		if strings.Contains(shown, token) {
			t.Fatalf("a value was shown: %s", shown)
		}
	}
	if _, ok := s.Value("acme", "ada", "issues"); ok {
		t.Fatal("a provider with no credential gave a value")
	}
}

// What the value is kept in is the store client secrets are kept in, under
// an owner no client can have.
func TestAValueIsKeptWhereClientSecretsAreAndApartFromThem(t *testing.T) {
	secrets := clients.NewMemorySecrets()
	s := credentials.New(secrets)
	if _, err := s.Put("acme", "ada", "drive", credentials.Input{Value: token}, at); err != nil {
		t.Fatal(err)
	}
	if v, ok := secrets.Get("acme", "person/ada", "drive"); !ok || v != token {
		t.Fatalf("the value is not in the secrets store: %q, %v", v, ok)
	}
	// Dropping a client's secrets leaves a person's alone.
	if err := secrets.Drop("acme", "ada"); err != nil {
		t.Fatal(err)
	}
	if v, ok := s.Value("acme", "ada", "drive"); !ok || v != token {
		t.Fatal("dropping a client named like a person dropped the person's value")
	}
}

// Replacing a value with a reference, or removing it, forgets the value.
func TestAValueNoLongerHeldIsForgotten(t *testing.T) {
	secrets := clients.NewMemorySecrets()
	s := credentials.New(secrets)
	if _, err := s.Put("acme", "ada", "drive", credentials.Input{Value: token}, at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put("acme", "ada", "drive", credentials.Input{Ref: "env:WARP_ADA_DRIVE"}, at); err != nil {
		t.Fatal(err)
	}
	if v, _ := secrets.Get("acme", "person/ada", "drive"); v != "" {
		t.Fatal("a value replaced by a reference is still held")
	}
	if _, ok := s.Value("acme", "ada", "drive"); ok {
		t.Fatal("a reference gave a value")
	}
	if _, err := s.Put("acme", "ada", "drive", credentials.Input{Value: token}, at); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("acme", "ada", "drive"); err != nil {
		t.Fatal(err)
	}
	if v, _ := secrets.Get("acme", "person/ada", "drive"); v != "" {
		t.Fatal("a removed credential's value is still held")
	}
	if err := s.Delete("acme", "ada", "drive"); !errors.Is(err, credentials.ErrNotFound) {
		t.Fatalf("removing it twice = %v", err)
	}
}

// One person's credentials are not another's, nor another workspace's.
func TestCredentialsArePerPersonAndPerWorkspace(t *testing.T) {
	s := credentials.New(clients.NewMemorySecrets())
	if _, err := s.Put("acme", "ada", "drive", credentials.Input{Value: token}, at); err != nil {
		t.Fatal(err)
	}
	for _, who := range [][2]string{{"acme", "bo"}, {"globex", "ada"}} {
		if _, ok := s.Get(who[0], who[1], "drive"); ok {
			t.Errorf("%s in %s holds ada's credential", who[1], who[0])
		}
		if _, ok := s.Value(who[0], who[1], "drive"); ok {
			t.Errorf("%s in %s reads ada's value", who[1], who[0])
		}
		if err := s.Delete(who[0], who[1], "drive"); !errors.Is(err, credentials.ErrNotFound) {
			t.Errorf("%s in %s removed ada's credential: %v", who[1], who[0], err)
		}
	}
}

func TestOnlyACredentialOfAValidFormIsKept(t *testing.T) {
	s := credentials.New(clients.NewMemorySecrets())
	for name, c := range map[string]struct {
		subject, provider string
		in                credentials.Input
	}{
		"nothing":            {"ada", "drive", credentials.Input{}},
		"both":               {"ada", "drive", credentials.Input{Ref: "env:WARP_X", Value: "v"}},
		"a bare secret":      {"ada", "drive", credentials.Input{Ref: "sk-live-123"}},
		"another scheme":     {"ada", "drive", credentials.Input{Ref: "vault:secret/x"}},
		"a value of lines":   {"ada", "drive", credentials.Input{Value: "one\r\nX-Evil: two"}},
		"a blank value":      {"ada", "drive", credentials.Input{Value: "   "}},
		"a huge value":       {"ada", "drive", credentials.Input{Value: strings.Repeat("v", credentials.MaxValue+1)}},
		"no person":          {"", "drive", credentials.Input{Value: "v"}},
		"no provider":        {"ada", "", credentials.Input{Value: "v"}},
		"a provider path":    {"ada", "a/b", credentials.Input{Value: "v"}},
		"a provider of caps": {"ada", "Drive", credentials.Input{Value: "v"}},
	} {
		if _, err := s.Put("acme", c.subject, c.provider, c.in, at); !errors.Is(err, credentials.ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
	if got := s.List("acme", "ada"); len(got) != 0 {
		t.Fatalf("a refused credential was kept: %+v", got)
	}
	// A secret pasted where a reference belongs is not said back.
	if _, err := s.Put("acme", "ada", "drive", credentials.Input{Ref: token}, at); err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("a pasted secret = %v", err)
	}
}

func TestAPersonHoldsABoundedNumber(t *testing.T) {
	s := credentials.New(nil)
	for i := range credentials.MaxPerPerson {
		if _, err := s.Put("acme", "ada", fmt.Sprintf("p%d", i), credentials.Input{Ref: "env:WARP_X"}, at); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Put("acme", "ada", "one-more", credentials.Input{Ref: "env:WARP_X"}, at); !errors.Is(err, credentials.ErrLimit) {
		t.Fatalf("one over the cap = %v", err)
	}
	if _, err := s.Put("acme", "ada", "p0", credentials.Input{Ref: "env:WARP_Y"}, at); err != nil {
		t.Fatalf("replacing one at the cap = %v", err)
	}
	if _, err := s.Put("acme", "ada", "p1", credentials.Input{Value: "v"}, at); !errors.Is(err, credentials.ErrInvalid) {
		t.Fatalf("a store with no secrets kept a value: %v", err)
	}
}

// A write that fails changes nothing: not the record, and not the value the
// record would have named.
func TestAFailedWriteChangesNothing(t *testing.T) {
	records := statetest.NewFaulty(state.NewMemory())
	secrets := clients.NewMemorySecrets()
	s := open(t, records, secrets)
	if _, err := s.Put("acme", "ada", "drive", credentials.Input{Value: "first"}, at); err != nil {
		t.Fatal(err)
	}
	records.Fail(statetest.ErrInjected)
	if _, err := s.Put("acme", "ada", "drive", credentials.Input{Value: "second"}, at); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("a failed write = %v", err)
	}
	if v, _ := s.Value("acme", "ada", "drive"); v != "first" {
		t.Fatalf("a failed write left the value %q", v)
	}
	if _, err := s.Put("acme", "ada", "drive", credentials.Input{Ref: "env:WARP_X"}, at); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("a failed write = %v", err)
	}
	if c, _ := s.Get("acme", "ada", "drive"); c.Kind != credentials.Value {
		t.Fatalf("a failed write changed the record: %+v", c)
	}
	if err := s.Delete("acme", "ada", "drive"); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("a failed delete = %v", err)
	}
	if v, _ := s.Value("acme", "ada", "drive"); v != "first" {
		t.Fatal("a failed delete dropped the value")
	}
}

func TestOnlyAStartTheServerCanConfineIsAccepted(t *testing.T) {
	for _, ok := range []string{"env:WARP_PERSON_", "env:WARP_ADA_GITHUB", "keychain:warp-people/", "keychain:warp", "op://People/", "op://People/ada/"} {
		if err := credentials.CheckStart(ok); err != nil {
			t.Errorf("%s was refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "env:", "env:ANTHROPIC_", "env:WARP_OIDC_", "env:WARP_OIDC", "env:WARP_A-B", "keychain:", "op://", "vault:x", "op://a,op://b", "op://a;b"} {
		if err := credentials.CheckStart(bad); !errors.Is(err, credentials.ErrInvalid) {
			t.Errorf("%q was accepted", bad)
		}
	}
}
