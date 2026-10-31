package policy

import (
	"testing"
	"time"
)

func list(t testing.TB, s *Store, tenant string) []Policy {
	t.Helper()
	out, err := s.List(tenant)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func forRun(t testing.TB, s *Store, tenant string, chain []string, agent, client string) []Policy {
	t.Helper()
	out, err := s.For(tenant, chain, agent, client)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func history(t testing.TB, s *Store, tenant string) []Change {
	t.Helper()
	out, err := s.History(tenant)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func policyAt(t testing.TB, s *Store, tenant string, scope Scope, when time.Time) (Policy, bool) {
	t.Helper()
	p, ok, err := s.At(tenant, scope, when)
	if err != nil {
		t.Fatal(err)
	}
	return p, ok
}
