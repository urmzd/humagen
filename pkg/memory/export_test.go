package memory

import (
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/blob"
	"github.com/urmzd/humagen/pkg/drive"
	"github.com/urmzd/humagen/pkg/state"
)

// openOn opens a store whose files are in blobs, so a test can open it again
// on the same records and bytes.
func openOn(st state.Store, blobs blob.Store) (*Store, error) {
	d, err := drive.Open(st, blobs, drive.WithScreen(Screen))
	if err != nil {
		return nil, err
	}
	return Open(st, d)
}

// OpenOn is openOn for the external tests.
var OpenOn = openOn

func must[T any](v T, err error) func(testing.TB) T {
	return func(t testing.TB) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func listOf(t testing.TB, s *Store, tenant, owner string) []Note {
	t.Helper()
	return must(s.List(tenant, owner))(t)
}

func promptOf(t testing.TB, s *Store, tenant, owner string) string {
	t.Helper()
	return must(s.Prompt(tenant, owner))(t)
}

func promptAtOf(t testing.TB, s *Store, tenant, owner, heading string, at time.Time) string {
	t.Helper()
	return must(s.PromptAt(tenant, owner, heading, at))(t)
}

func pendingOf(t testing.TB, s *Store, tenant string) []Proposal {
	t.Helper()
	return must(s.Pending(tenant))(t)
}

func originsOf(t testing.TB, s *Store, tenant string, runs []string, owners ...map[string]bool) []ReviewedOrigin {
	t.Helper()
	return must(s.ReviewedOrigins(tenant, runs, owners...))(t)
}

// For the external tests.
var (
	ListOf    = listOf
	OriginsOf = originsOf
)
