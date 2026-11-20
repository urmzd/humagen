package platform_test

import (
	"testing"
	"time"

	"github.com/urmzd/saige/flow/bus"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
)

// noticeWithin is how long a change made on one process may take to reach a
// watcher on another.
const noticeWithin = 5 * time.Second

// Two platforms on one Postgres: a change to the inbox or the threads made
// through one wakes a watcher on the other.
func TestAChangeOnOnePlatformWakesAWatcherOnTheOther(t *testing.T) {
	pools := statetest.Pools(t, 2)
	var ps [2]*platform.Platform
	var relays [2]*bus.Notices
	for i, pool := range pools {
		b, release, err := platform.OpenPostgres(t.Context(), pool)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = release() })
		relays[i] = b.Notices.(*bus.Notices)
		ps[i], _ = runningWith(t, platform.Config{Backends: b})
	}
	a, b := ps[0], ps[1]

	inboxNotices, unwatchInbox := b.Inbox.Watch(tenant)
	defer unwatchInbox()
	threadNotices, unwatchThreads := b.Threads.Watch(tenant)
	defer unwatchThreads()
	// The other platform listens once its relay loops run.
	deadline := time.Now().Add(noticeWithin)
	for !relays[1].Listening() {
		if time.Now().After(deadline) {
			t.Fatal("the other platform never listened")
		}
		time.Sleep(10 * time.Millisecond)
	}

	m, _, err := a.Inbox.Raise(tenant, "alert:cross", inbox.Message{Kind: inbox.KindAlert, Subject: "cross", Severity: inbox.Warning}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	sent := time.Now()
	select {
	case n := <-inboxNotices:
		if n.ID != m.ID {
			t.Fatalf("inbox notice for %q, want %q", n.ID, m.ID)
		}
		t.Logf("inbox notice crossed in %s", time.Since(sent))
	case <-time.After(noticeWithin):
		t.Fatalf("no inbox notice on the other platform within %s", noticeWithin)
	}

	th, _, err := a.Threads.Start(tenant, threads.Thread{Channel: "general", Visibility: threads.Shared, CreatedBy: "dana"},
		threads.Message{By: "dana", Name: "Dana", Text: "hello"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	sent = time.Now()
	select {
	case n := <-threadNotices:
		if n.ID != th.ID {
			t.Fatalf("thread notice for %q, want %q", n.ID, th.ID)
		}
		t.Logf("thread notice crossed in %s", time.Since(sent))
	case <-time.After(noticeWithin):
		t.Fatalf("no thread notice on the other platform within %s", noticeWithin)
	}

	// The platform that made the change tells its own watchers once, not
	// again when its notice comes back.
	own, unwatchOwn := a.Inbox.Watch(tenant)
	defer unwatchOwn()
	if _, _, err := a.Inbox.Dismiss(tenant, m.ID, "dana", time.Now()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-own:
	case <-time.After(noticeWithin):
		t.Fatal("the platform that made a change did not tell its own watcher")
	}
	select {
	case n := <-own:
		t.Fatalf("a platform heard its own notice twice: %+v", n)
	case <-time.After(500 * time.Millisecond):
	}
}
