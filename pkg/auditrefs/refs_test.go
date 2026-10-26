package auditrefs

import (
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestLegacyAuditReferencesAreOpaqueStableAndTenantScoped(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		id, err := s.ID("acme", "project", "42")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(id, "legacy_") || len(id) != 39 {
			t.Fatalf("not opaque: %s", id)
		}
		s, err = Open(st)
		if err != nil {
			t.Fatal(err)
		}
		again, err := s.ID("acme", "project", "42")
		if err != nil || again != id {
			t.Fatal("reference changed after replay")
		}
		other, err := s.ID("other", "project", "42")
		if err != nil || other == id {
			t.Fatal("reference crossed tenant")
		}
	})
}
func TestUnrecordedAuditSeedReturnsNoReference(t *testing.T) {
	st := statetest.NewFaulty(state.NewMemory())
	s, _ := Open(st)
	st.Fail(statetest.ErrInjected)
	if id, err := s.ID("acme", "project", "42"); err == nil || id != "" {
		t.Fatal("unrecorded seed escaped")
	}
}
