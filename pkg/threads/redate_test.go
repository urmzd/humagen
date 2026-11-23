package threads_test

import (
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/threads"
)

// A thread redated is never last active before its latest message: a
// message posted after the caller read the thread keeps it where that
// message put it, and the times given are taken otherwise. A deleted
// thread is not found.
func TestRedateKeepsAThreadAsActiveAsItsLatestMessage(t *testing.T) {
	s := threads.NewStore()
	th, _ := start(t, s, "acme", shared("", "general"), msg("dana", "first"))
	late := t0.Add(72 * time.Hour)
	post(t, s, "acme", th.ID, msg("dana", "posted since"), late)
	got, err := s.Redate("acme", th.ID, t0.Add(-time.Hour), t0)
	if err != nil || !got.CreatedAt.Equal(t0.Add(-time.Hour)) || !got.UpdatedAt.Equal(late) {
		t.Fatalf("redated to %s, %s, %v; want started %s and active %s", got.CreatedAt, got.UpdatedAt, err, t0.Add(-time.Hour), late)
	}
	// The same again changes nothing.
	again, err := s.Redate("acme", th.ID, t0.Add(-time.Hour), t0)
	if err != nil || !again.UpdatedAt.Equal(late) || again.Version != got.Version {
		t.Fatalf("redated again to %s at version %d, %v", again.UpdatedAt, again.Version, err)
	}
	// A time after the latest message is taken as given.
	later := late.Add(time.Hour)
	if got, err = s.Redate("acme", th.ID, t0.Add(-time.Hour), later); err != nil || !got.UpdatedAt.Equal(later) {
		t.Fatalf("redated to %s, %v; want %s", got.UpdatedAt, err, later)
	}
	// A thread with no message takes both times.
	bare, err := s.Create("acme", shared("", "general"), t0.Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got, err = s.Redate("acme", bare.ID, t0, t0.Add(time.Minute)); err != nil || !got.CreatedAt.Equal(t0) || !got.UpdatedAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("a thread with no message redated to %s, %s, %v", got.CreatedAt, got.UpdatedAt, err)
	}
	// Archived, it is still dated; deleted, it is not found.
	if _, err := s.SetArchive("acme", bare.ID, &threads.Archive{By: "dana", At: t0, DeleteAfter: t0}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Redate("acme", bare.ID, t0, t0.Add(2*time.Minute)); err != nil {
		t.Fatalf("redating an archived thread = %v", err)
	}
	if err := s.Purge("acme", bare.ID, "dana", t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Redate("acme", bare.ID, t0, t0.Add(3*time.Minute)); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("redating a deleted thread = %v", err)
	}
}
