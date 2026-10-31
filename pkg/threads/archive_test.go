package threads_test

import (
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestArchiveIsDueAfterTheDelayOrWhenAskedFor(t *testing.T) {
	a := &threads.Archive{At: t0, DeleteAfter: t0.Add(threads.DeletionDelay)}
	switch {
	case a.Due(t0.Add(threads.DeletionDelay - 1)):
		t.Fatal("due before the delay ran out")
	case !a.Due(t0.Add(threads.DeletionDelay)):
		t.Fatal("not due once the delay ran out")
	}
	a.Requested = true
	if !a.Due(t0) {
		t.Fatal("asked for deletion, not due")
	}
	var none *threads.Archive
	if none.Due(t0) {
		t.Fatal("a conversation that is not archived is due")
	}
}

func TestArchiveCannotChangeThroughUpdate(t *testing.T) {
	s := threads.NewStore()
	th, _ := start(t, s, "acme", shared("Globex", "renewals"), msg("dana", "hello"))
	_, err := s.Update("acme", th.ID, th.Version, t0, func(n *threads.Thread) error {
		n.Archive = &threads.Archive{By: "dana", At: t0}
		return nil
	})
	if !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("archived through Update = %v", err)
	}
}

func TestPurgeDeletesMessagesAndLeavesATombstone(t *testing.T) {
	s := threads.NewStore()
	th, _ := start(t, s, "acme", shared("Globex", "renewals"), msg("dana", "the secret renewal price"))
	post(t, s, "acme", th.ID, msg("eve", "noted"), t0)
	if err := s.Purge("acme", th.ID, "admin", t0); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("purged without archiving = %v", err)
	}
	if _, err := s.SetArchive("acme", th.ID, &threads.Archive{By: "dana", At: t0, DeleteAfter: t0, Requested: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.Purge("acme", th.ID, "admin", t0); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("acme", th.ID)
	if err != nil || got.Archive == nil || got.Archive.Deleted == nil || got.Archive.DeletedBy != "admin" {
		t.Fatalf("tombstone = %+v, %v", got, err)
	}
	if msgs, _, err := s.Messages("acme", th.ID, "", 10); err != nil || len(msgs) != 0 {
		t.Fatalf("messages after purge = %v, %v", msgs, err)
	}
	if hits, err := s.Search("acme", "secret", time.Time{}, 10, func(threads.Thread) bool { return true }); err != nil || len(hits) != 0 {
		t.Fatalf("search still finds the deleted text: %v", hits)
	}
	if _, _, err := s.Post("acme", th.ID, msg("dana", "again"), t0); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("post into a deleted thread = %v", err)
	}
	if _, err := s.SetArchive("acme", th.ID, nil); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("unarchived a deleted thread = %v", err)
	}
}

func TestArchiveAndApprovedDeletionSurviveRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := threads.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		th, _ := start(t, s, "acme", shared("Globex", "renewals"), msg("dana", "retained secret"))
		if _, err := s.SetArchive("acme", th.ID, &threads.Archive{By: "dana", At: t0, DeleteAfter: t0, Requested: true}); err != nil {
			t.Fatal(err)
		}
		again, err := threads.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := again.Get("acme", th.ID); err != nil || got.Archive == nil || !got.Archive.Due(t0) {
			t.Fatalf("archived after restart = %+v, %v", got, err)
		}
		if err := again.Purge("acme", th.ID, "admin", t0); err != nil {
			t.Fatal(err)
		}
		last, err := threads.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		got, err := last.Get("acme", th.ID)
		if err != nil || got.Archive == nil || got.Archive.Deleted == nil {
			t.Fatalf("tombstone after restart = %+v, %v", got, err)
		}
		if messages, _, err := last.Messages("acme", th.ID, "", 10); err != nil || len(messages) != 0 {
			t.Fatalf("purged messages after restart = %v, %v", messages, err)
		}
		if hits, err := last.Search("acme", "secret", time.Time{}, 10, func(threads.Thread) bool { return true }); err != nil || len(hits) != 0 {
			t.Fatalf("purged search after restart = %v", hits)
		}
	})
}
