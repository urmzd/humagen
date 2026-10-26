package notifications

import (
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/prefs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestNativeDeliveryModesQuietHoursReplayAndRevocation(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, 10, 26, 2, 30, 0, 0, time.UTC)
		policy := prefs.DefaultPersonal()
		policy.Timezone = "America/Toronto"
		policy.QuietStart = "22:00"
		policy.QuietEnd = "07:00"
		policy.Notifications = map[string]prefs.Delivery{"mention": prefs.Immediate, "result": prefs.Digest, "alert": prefs.Off}
		candidates := []Candidate{{Message: "mention", Kind: "mention", Stamp: now}, {Message: "result", Kind: "result", Stamp: now}, {Message: "off", Kind: "alert", Stamp: now}}
		current := map[string]Candidate{}
		for _, c := range candidates {
			current[c.Message] = c
		}
		lookup := func(id string) (Candidate, bool) { candidate, ok := current[id]; return candidate, ok }
		reconcile := func(at time.Time) []Delivery {
			t.Helper()
			rows, err := s.Reconcile("acme", "ada", "", candidates, policy, at, now.Add(-24*time.Hour), lookup)
			if err != nil {
				t.Fatal(err)
			}
			return rows
		}
		if rows := reconcile(now); len(rows) != 0 {
			t.Fatal("quiet hours delivered")
		}
		s, err = Open(st)
		if err != nil {
			t.Fatal(err)
		}
		// Membership was revoked while the digest was pending.
		delete(current, "result")
		release := time.Date(2026, 10, 26, 11, 0, 0, 0, time.UTC)
		rows := reconcile(release)
		if len(rows) != 1 || len(rows[0].Messages) != 1 || rows[0].Messages[0] != "mention" {
			t.Fatalf("quiet/revocation: %+v", rows)
		}
		id := rows[0].ID
		s, err = Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if got := reconcile(release.Add(time.Minute)); len(got) != 1 || got[0].ID != id {
			t.Fatalf("replay duplicated delivery: %+v", got)
		}
		if err := s.Ack("acme", "other", id, func(string) bool { return true }); !errors.Is(err, ErrNotFound) {
			t.Fatal("another person acked")
		}
		if err := s.Ack("acme", "ada", id, func(string) bool { return true }); err != nil {
			t.Fatal(err)
		}
		s, err = Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if rows := reconcile(release.Add(2 * time.Minute)); len(rows) != 0 {
			t.Fatal("ack replayed")
		}
	})
}

func TestImmediateAndHourlyDigestAreDurableWithoutDuplicateOrSecretBody(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, _ := Open(st)
		now := time.Date(2026, 10, 26, 12, 10, 0, 0, time.UTC)
		policy := prefs.DefaultPersonal()
		policy.Notifications = map[string]prefs.Delivery{"mention": prefs.Immediate, "result": prefs.Digest}
		candidates := []Candidate{{Message: "first", Kind: "mention", Stamp: now}, {Message: "second", Kind: "result", Stamp: now}, {Message: "third", Kind: "result", Stamp: now}}
		lookup := func(id string) (Candidate, bool) {
			for _, c := range candidates {
				if c.Message == id {
					return c, true
				}
			}
			return Candidate{}, false
		}
		rows, err := s.Reconcile("acme", "ada", "", candidates, policy, now, now.Add(-time.Hour), lookup)
		if err != nil || len(rows) != 1 || rows[0].Mode != prefs.Immediate {
			t.Fatalf("immediate: %+v %v", rows, err)
		}
		s, err = Open(st)
		if err != nil {
			t.Fatal(err)
		}
		rows, err = s.Reconcile("acme", "ada", "", candidates, policy, now.Add(50*time.Minute), now.Add(-time.Hour), lookup)
		if err != nil || len(rows) != 2 {
			t.Fatalf("digest: %+v %v", rows, err)
		}
		digests := 0
		for _, row := range rows {
			if row.Mode == prefs.Digest {
				digests++
				if len(row.Messages) != 2 {
					t.Fatalf("digest not grouped: %+v", row)
				}
			}
		}
		if digests != 1 {
			t.Fatal("wrong digest count")
		}
	})
}

func TestFailedNotificationWriteDoesNotClaimDelivery(t *testing.T) {
	st := statetest.NewFaulty(state.NewMemory())
	s, _ := Open(st)
	now := time.Now()
	candidate := Candidate{Message: "one", Kind: "mention", Stamp: now}
	policy := prefs.DefaultPersonal()
	policy.Notifications["mention"] = prefs.Immediate
	lookup := func(string) (Candidate, bool) { return candidate, true }
	st.Fail(statetest.ErrInjected)
	if rows, err := s.Reconcile("acme", "ada", "", []Candidate{candidate}, policy, now, now.Add(-time.Hour), lookup); err == nil || len(rows) != 0 {
		t.Fatal("unrecorded delivery escaped")
	}
	st.Fail(nil)
	rows, err := s.Reconcile("acme", "ada", "", []Candidate{candidate}, policy, now, now.Add(-time.Hour), lookup)
	if err != nil || len(rows) != 1 {
		t.Fatalf("retry: %+v %v", rows, err)
	}
}
