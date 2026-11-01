package httpapi

import (
	"testing"

	"github.com/urmzd/humagen/pkg/memory"
)

// The memory store reads per request and reports a read that fails; a test
// that reads it fails with it.

func notesOf(t testing.TB, s *memory.Store, tenant, owner string) []memory.Note {
	t.Helper()
	notes, err := s.List(tenant, owner)
	if err != nil {
		t.Fatal(err)
	}
	return notes
}
