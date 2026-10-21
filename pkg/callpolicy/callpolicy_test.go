package callpolicy_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/warp/pkg/callpolicy"
)

func known(names ...string) func(string) bool {
	return func(n string) bool { return slices.Contains(names, n) }
}

func TestDefaultAdmitsEveryoneWithoutApproval(t *testing.T) {
	d := callpolicy.Default("helper")
	if !d.Admits("anyone") || d.NeedsApproval("anyone") || d.Expiry() != callpolicy.DefaultExpiry || d.Version != 0 {
		t.Fatalf("default = %+v", d)
	}
	if err := d.Validate(known()); err != nil {
		t.Fatalf("the default is invalid: %v", err)
	}
}

func TestAdmitsAndNeedsApproval(t *testing.T) {
	for _, tc := range []struct {
		name            string
		p               callpolicy.Policy
		caller          string
		admits, approve bool
	}{
		{"anyone", callpolicy.Policy{Callers: callpolicy.Anyone, Approval: callpolicy.Never}, "a", true, false},
		{"listed in", callpolicy.Policy{Callers: callpolicy.Listed, Allow: []string{"a"}, Approval: callpolicy.Never}, "a", true, false},
		{"listed out", callpolicy.Policy{Callers: callpolicy.Listed, Allow: []string{"a"}, Approval: callpolicy.Never}, "b", false, false},
		{"none", callpolicy.Policy{Callers: callpolicy.Nobody, Approval: callpolicy.Never}, "a", false, false},
		{"unknown callers", callpolicy.Policy{Callers: "x", Approval: callpolicy.Never}, "a", false, false},
		{"always", callpolicy.Policy{Callers: callpolicy.Anyone, Approval: callpolicy.Always}, "a", true, true},
		{"unlisted trusted", callpolicy.Policy{Callers: callpolicy.Anyone, Approval: callpolicy.Unlisted, Trusted: []string{"a"}}, "a", true, false},
		{"unlisted other", callpolicy.Policy{Callers: callpolicy.Anyone, Approval: callpolicy.Unlisted, Trusted: []string{"a"}}, "b", true, true},
		{"unknown approval", callpolicy.Policy{Callers: callpolicy.Anyone, Approval: "x"}, "a", true, true},
	} {
		if got := tc.p.Admits(tc.caller); got != tc.admits {
			t.Errorf("%s: admits %v", tc.name, got)
		}
		if got := tc.p.NeedsApproval(tc.caller); got != tc.approve {
			t.Errorf("%s: needs approval %v", tc.name, got)
		}
	}
}

func TestValidate(t *testing.T) {
	many := make([]string, callpolicy.MaxNames+1)
	for i := range many {
		many[i] = fmt.Sprintf("a%d", i)
	}
	all := func(string) bool { return true }
	for name, p := range map[string]callpolicy.Policy{
		"no agent":            {Callers: callpolicy.Anyone, Approval: callpolicy.Never},
		"callers mode":        {Agent: "h", Callers: "some", Approval: callpolicy.Never},
		"approval mode":       {Agent: "h", Callers: callpolicy.Anyone, Approval: "sometimes"},
		"allow without list":  {Agent: "h", Callers: callpolicy.Anyone, Allow: []string{"a"}, Approval: callpolicy.Never},
		"trusted not unlist":  {Agent: "h", Callers: callpolicy.Anyone, Approval: callpolicy.Always, Trusted: []string{"a"}},
		"approvers for never": {Agent: "h", Callers: callpolicy.Anyone, Approval: callpolicy.Never, Approvers: []string{"olu"}},
		"self for never":      {Agent: "h", Callers: callpolicy.Anyone, Approval: callpolicy.Never, SelfApproval: true},
		"unknown agent":       {Agent: "h", Callers: callpolicy.Listed, Allow: []string{"ghost"}, Approval: callpolicy.Never},
		"duplicate":           {Agent: "h", Callers: callpolicy.Listed, Allow: []string{"a", "a"}, Approval: callpolicy.Never},
		"empty name":          {Agent: "h", Callers: callpolicy.Listed, Allow: []string{""}, Approval: callpolicy.Never},
		"too many":            {Agent: "h", Callers: callpolicy.Listed, Allow: many, Approval: callpolicy.Never},
		"long name":           {Agent: "h", Callers: callpolicy.Anyone, Approval: callpolicy.Always, Approvers: []string{strings.Repeat("x", callpolicy.MaxNameLength+1)}},
		"empty role":          {Agent: "h", Callers: callpolicy.Anyone, Approval: callpolicy.Always, Approvers: []string{"role:"}},
		"expiry too short":    {Agent: "h", Callers: callpolicy.Anyone, Approval: callpolicy.Always, ExpiresSeconds: 1},
		"expiry too long":     {Agent: "h", Callers: callpolicy.Anyone, Approval: callpolicy.Always, ExpiresSeconds: int(callpolicy.MaxExpiry/time.Second) + 1},
	} {
		check := all
		if name == "unknown agent" {
			check = known("a")
		}
		if err := p.Normalize().Validate(check); !errors.Is(err, callpolicy.ErrInvalid) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	ok := callpolicy.Policy{Agent: "h", Callers: callpolicy.Listed, Allow: []string{"a"}, Approval: callpolicy.Unlisted, Trusted: []string{"a"},
		Approvers: []string{"olu", "role:manager"}, SelfApproval: true, ExpiresSeconds: 3600}
	if err := ok.Normalize().Validate(known("a")); err != nil {
		t.Fatal(err)
	}
}

func TestStoreKeepsTenantsApartAndIsBounded(t *testing.T) {
	s := callpolicy.NewStore()
	at := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	p := callpolicy.Policy{Agent: "h", Callers: callpolicy.Nobody, Approval: callpolicy.Never}
	got, err := s.Put("acme", p, known(), "olu", at)
	if err != nil || got.Version != 1 || got.UpdatedBy != "olu" || !got.UpdatedAt.Equal(at) {
		t.Fatalf("put = %+v, %v", got, err)
	}
	if got, _ := s.Put("acme", p, known(), "olu", at); got.Version != 2 {
		t.Fatalf("version %d", got.Version)
	}
	if got, ok := s.Get("globex", "h"); ok || got.Callers != callpolicy.Anyone {
		t.Fatalf("another tenant sees %+v", got)
	}
	stored, _ := s.Get("acme", "h")
	stored.Allow = append(stored.Allow, "x")
	if again, _ := s.Get("acme", "h"); len(again.Allow) != 0 {
		t.Fatal("a read shares the stored lists")
	}
	if err := s.Delete("acme", "h"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("acme", "h"); ok {
		t.Fatal("a deleted policy is still stored")
	}
	for i := range callpolicy.MaxPolicies {
		if _, err := s.Put("acme", callpolicy.Policy{Agent: fmt.Sprintf("a%d", i), Callers: callpolicy.Anyone, Approval: callpolicy.Never}, known(), "olu", at); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Put("acme", callpolicy.Policy{Agent: "one-more", Callers: callpolicy.Anyone, Approval: callpolicy.Never}, known(), "olu", at); !errors.Is(err, callpolicy.ErrLimit) {
		t.Fatalf("over the cap: %v", err)
	}
	if _, err := s.Put("acme", callpolicy.Policy{Agent: "a0", Callers: callpolicy.Nobody, Approval: callpolicy.Never}, known(), "olu", at); err != nil {
		t.Fatalf("replacing at the cap: %v", err)
	}
}
