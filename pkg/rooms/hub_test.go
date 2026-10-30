package rooms

import (
	"context"
	"errors"
	"testing"
	"time"
)

// idle is a source that publishes nothing: tests publish through the hub.
func idle(ctx context.Context, _, _ string, _ func(Item)) error {
	<-ctx.Done()
	return nil
}

func recv(t *testing.T, v *Viewer) Item {
	t.Helper()
	select {
	case it := <-v.Items():
		return it
	case <-time.After(2 * time.Second):
		t.Fatal("no item arrived")
	}
	return Item{}
}

func none(t *testing.T, v *Viewer) {
	t.Helper()
	select {
	case it := <-v.Items():
		t.Fatalf("an item arrived that should not have: %+v", it)
	case <-time.After(20 * time.Millisecond):
	}
}

// A room at or under the threshold is a broadcast, exactly as before.
func TestASmallRoomSendsEveryoneEverything(t *testing.T) {
	h := NewHub(Config{CoalesceAbove: 2, TailInterval: time.Hour}, idle)
	defer func() { _ = h.Close() }()
	ana, bo := h.Join("acme", "th-1", "ana"), h.Join("acme", "th-1", "bo")
	h.Publish("acme", "th-1", Item{Kind: KindRun, Involves: []string{"ana"}, Value: 1})
	h.Publish("acme", "th-1", Item{Kind: KindThread, Value: 2})
	for _, v := range []*Viewer{ana, bo} {
		if a, b := recv(t, v), recv(t, v); a.Value != 1 || b.Value != 2 {
			t.Fatalf("got %v %v", a.Value, b.Value)
		}
	}
	// Another tenant's room of the same id is another room.
	h.Publish("globex", "th-1", Item{Kind: KindRun, Value: 3})
	none(t, ana)
}

// Over the threshold, a viewer is sent at once only what involves them, and
// the rest arrives folded into a page.
func TestALargeRoomSendsWhatInvolvesYouAndPagesTheRest(t *testing.T) {
	h := NewHub(Config{CoalesceAbove: 2, TailInterval: 50 * time.Millisecond}, idle)
	defer func() { _ = h.Close() }()
	ana, bo, cy := h.Join("acme", "th-1", "ana"), h.Join("acme", "th-1", "bo"), h.Join("acme", "th-1", "cy")
	// Ana twice over: each of her names is sent once.
	h.Publish("acme", "th-1", Item{Kind: KindRun, Involves: []string{"ana", "ana"}, Value: "hers"})
	h.Publish("acme", "th-1", Item{Kind: KindThread, Value: "nobody's"})
	h.Publish("acme", "th-1", Item{Kind: KindRun, Value: "nobody's"})
	if it := recv(t, ana); it.Value != "hers" {
		t.Fatalf("ana got %v", it.Value)
	}
	none(t, ana)
	none(t, bo)
	for _, v := range []*Viewer{ana, bo, cy} {
		select {
		case <-v.Pages():
		case <-time.After(2 * time.Second):
			t.Fatal("no page arrived")
		}
		p, ok := v.Page()
		if !ok || p.Seq != 1 || p.Thread != "th-1" || p.Events != 2 || p.Messages != 1 || p.Viewers != 3 || p.From.IsZero() || p.Through.Before(p.From) {
			t.Fatalf("page = %+v %v", p, ok)
		}
		if _, again := v.Page(); again {
			t.Fatal("the same page was read twice")
		}
	}
	// Nothing happened, so no page follows.
	time.Sleep(120 * time.Millisecond)
	if _, ok := bo.Page(); ok {
		t.Fatal("a page with nothing in it was sent")
	}
}

// The last viewer out closes the room and stops its source; the next one in
// opens a new one.
func TestARoomReadsItsSourceOnlyWhileItHasViewers(t *testing.T) {
	started, stopped := make(chan string, 4), make(chan string, 4)
	src := func(ctx context.Context, tenant, thread string, _ func(Item)) error {
		started <- thread
		<-ctx.Done()
		stopped <- thread
		return nil
	}
	h := NewHub(Config{}, src)
	defer func() { _ = h.Close() }()
	a, b := h.Join("acme", "th-1", "ana"), h.Join("acme", "th-1", "bo")
	if <-started != "th-1" || h.Viewers("acme", "th-1") != 2 {
		t.Fatal("the room did not open once")
	}
	a.Leave()
	a.Leave()
	select {
	case <-stopped:
		t.Fatal("the room closed with a viewer in it")
	case <-time.After(20 * time.Millisecond):
	}
	b.Leave()
	if <-stopped != "th-1" || h.Viewers("acme", "th-1") != 0 {
		t.Fatal("the room stayed open with nobody in it")
	}
	c := h.Join("acme", "th-1", "cy")
	defer c.Leave()
	if <-started != "th-1" {
		t.Fatal("the room did not open again")
	}
}

// A source that fails ends its room's viewers, who reconnect.
func TestASourceThatFailsEndsItsViewers(t *testing.T) {
	boom := errors.New("bus gone")
	h := NewHub(Config{}, func(context.Context, string, string, func(Item)) error { return boom })
	defer func() { _ = h.Close() }()
	v := h.Join("acme", "th-1", "ana")
	select {
	case <-v.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the viewer was not ended")
	}
	if !errors.Is(v.Err(), boom) {
		t.Fatalf("err = %v", v.Err())
	}
}

// A viewer that falls ViewerBuffer behind is ended, as the bus ends a slow
// consumer, and never slows the room.
func TestASlowViewerIsEnded(t *testing.T) {
	h := NewHub(Config{}, idle)
	defer func() { _ = h.Close() }()
	slow, fast := h.Join("acme", "th-1", "ana"), h.Join("acme", "th-1", "bo")
	for i := range ViewerBuffer + 1 {
		h.Publish("acme", "th-1", Item{Kind: KindRun, Value: i})
		<-fast.Items()
	}
	if !errors.Is(slow.Err(), ErrSlow) || fast.Err() != nil {
		t.Fatalf("slow = %v, fast = %v", slow.Err(), fast.Err())
	}
}

func TestCloseEndsEveryViewer(t *testing.T) {
	h := NewHub(Config{}, idle)
	v := h.Join("acme", "th-1", "ana")
	_ = h.Close()
	if !errors.Is(v.Err(), ErrClosed) {
		t.Fatalf("err = %v", v.Err())
	}
	if late := h.Join("acme", "th-1", "bo"); !errors.Is(late.Err(), ErrClosed) {
		t.Fatalf("a viewer joined a closed hub: %v", late.Err())
	}
}

func TestConfigDefaultsAndBounds(t *testing.T) {
	c := Config{}.Defaults()
	if c.CoalesceAbove != DefaultCoalesceAbove || c.TailInterval != DefaultTailInterval || c.RoomRunsPerMinute != DefaultRoomRunsPerMinute || c.PersonRuns != DefaultPersonRuns {
		t.Fatalf("defaults = %+v", c)
	}
	if err := (Config{}).Check(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Config{{CoalesceAbove: -1}, {TailInterval: time.Millisecond}, {TailInterval: time.Hour}, {RoomRunsPerMinute: -1}, {PersonRuns: MaxPersonRuns + 1}} {
		if (bad).Check() == nil {
			t.Errorf("%+v was accepted", bad)
		}
	}
}
