package platform_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/inbox"
)

// Each workspace's inbox keeps what was settled for as long as its Platform
// document says, 90 days when it says nothing, and never less than a week.
// A pass, which the platform runs when it starts and every hour after,
// drops the rest, on memory and on Postgres. A release whose notes were
// dropped is not posted again, across a restart.
func TestEachInboxKeepsWhatWasSettledForItsRetention(t *testing.T) {
	eachBackend(t, func(t *testing.T, first platform.Backends, again func() platform.Backends) {
		var clock atomic.Int64
		clock.Store(posted.UnixNano())
		cfg := func(b platform.Backends) platform.Config {
			return platform.Config{
				Backends: b, Now: func() time.Time { return time.Unix(0, clock.Load()).UTC() },
				InboxRetention: map[string]time.Duration{tenant: 10 * 24 * time.Hour, "initech": time.Hour},
			}
		}
		p, stop := runningWith(t, cfg(first))
		notes := platform.Announcement{Subject: "What's new in warp 0.16.0", Text: "Inbox retention."}
		release, ok, err := p.PostReleaseNotes(tenant, "0.16.0", notes)
		if err != nil || !ok {
			t.Fatalf("release notes = %v, %v", ok, err)
		}
		news, err := p.Announce(tenant, "", inbox.From{Kind: "person", Name: "ada"}, platform.Announcement{Subject: "Offsite", Text: "Friday."}, posted)
		if err != nil {
			t.Fatal(err)
		}
		withdrawn := map[string]inbox.Message{tenant: release}
		for _, ws := range []string{"globex", "initech"} {
			if withdrawn[ws], err = p.Announce(ws, "", inbox.From{Kind: "person", Name: "ada"}, platform.Announcement{Subject: "Gone", Text: "Gone."}, posted); err != nil {
				t.Fatal(err)
			}
		}
		for ws, m := range withdrawn {
			if _, _, err := p.Inbox.Withdraw(ws, m.ID, "ada", "", posted.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
		kept := func(q *platform.Platform, ws string) bool {
			_, ok := q.Inbox.ByID(ws, withdrawn[ws].ID)
			return ok
		}

		// Two days on, initech's hour is a week: nothing is dropped.
		clock.Store(posted.Add(2 * 24 * time.Hour).UnixNano())
		p.RetainInbox()
		for ws := range withdrawn {
			if !kept(p, ws) {
				t.Fatalf("%s dropped what was settled two days ago", ws)
			}
		}
		stop()

		// Eleven days on, the start's own pass drops acme's and initech's,
		// and globex keeps its own for 90 days.
		clock.Store(posted.Add(11 * 24 * time.Hour).UnixNano())
		q, _ := runningWith(t, cfg(again()))
		eventually(t, "retention at the start", func() bool { return !kept(q, tenant) && !kept(q, "initech") })
		if !kept(q, "globex") {
			t.Fatal("globex dropped what it keeps for 90 days")
		}
		if _, ok := q.Inbox.ByID(tenant, news.ID); !ok {
			t.Fatal("an open announcement was dropped")
		}
		if _, ok, err := q.PostReleaseNotes(tenant, "0.16.0", notes); ok || err != nil {
			t.Fatalf("a dropped release was posted again: %v, %v", ok, err)
		}
		ada := auth.Principal{Subject: "ada", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
		if c := q.InboxCount(ada); c.Updates != 0 {
			t.Fatalf("ada, who posted the one open announcement, counts %+v", c)
		}
		bo := auth.Principal{Subject: "bo", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
		if c := q.InboxCount(bo); c.Updates != 1 {
			t.Fatalf("bo counts %+v", c)
		}
	})
}
