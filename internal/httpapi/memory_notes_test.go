package httpapi

import (
	"testing"
	"time"

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

func promptOf(t testing.TB, s *memory.Store, tenant, owner string) string {
	t.Helper()
	text, err := s.Prompt(tenant, owner)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func promptAtOf(t testing.TB, s *memory.Store, tenant, owner, heading string, at time.Time) string {
	t.Helper()
	text, err := s.PromptAt(tenant, owner, heading, at)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func pendingOf(t testing.TB, s *memory.Store, tenant string) []memory.Proposal {
	t.Helper()
	out, err := s.Pending(tenant)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
