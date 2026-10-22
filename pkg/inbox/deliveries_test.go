package inbox

import (
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/urmzd/warp/pkg/state"
	"github.com/urmzd/warp/pkg/state/statetest"
)

// A delivery is the client's own receipt: kept and replayed, never announced,
// and recorded once however often the same post is reported.
func TestADeliveryIsReplayedAndNeverAnnounced(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		m := raise(s, "announce:Field:1", t0, Part{Type: PartText, State: PartInfo, Text: "We shipped."})
		notices, stop := s.Watch(acme)
		defer stop()

		post := Delivery{Channel: "C0FIELD", TS: "1700000000.000100", At: t0.Add(time.Second)}
		for range 3 {
			if err := s.Deliver(acme, m.ID, "slack-acme", post, nil); err != nil {
				t.Fatal(err)
			}
		}
		select {
		case n := <-notices:
			t.Fatalf("a delivery was announced: %+v", n)
		default:
		}
		if n := len(s.History(acme, m.ID)); n != 2 {
			t.Fatalf("the same post was recorded %d times", n-1)
		}
		got, _ := s.Get(acme, m.ID, "ada")
		if !reflect.DeepEqual(got.Deliveries, map[string]Delivery{"slack-acme": post}) || !got.UpdatedAt.Equal(m.UpdatedAt) {
			t.Fatalf("deliveries = %+v, updated %v", got.Deliveries, got.UpdatedAt)
		}
		// A new post replaces the old one; another client's is its own.
		moved := Delivery{Channel: "C0FIELD", TS: "1700000099.000100", At: t0.Add(2 * time.Second)}
		if err := s.Deliver(acme, m.ID, "slack-acme", moved, nil); err != nil {
			t.Fatal(err)
		}
		if err := s.Deliver(acme, m.ID, "slack-other", post, nil); err != nil {
			t.Fatal(err)
		}
		got, _ = s.Get(acme, m.ID, "ada")
		got.Deliveries["slack-acme"] = Delivery{}
		if again, _ := s.Get(acme, m.ID, "ada"); again.Deliveries["slack-acme"] != moved {
			t.Fatal("a reader's copy of the deliveries changed the store")
		}

		reopened, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		after, _ := reopened.Get(acme, m.ID, "ada")
		if !reflect.DeepEqual(after.Deliveries, map[string]Delivery{"slack-acme": moved, "slack-other": post}) {
			t.Fatalf("after restart = %+v", after.Deliveries)
		}
		rebuilt, _ := Rebuild(s.Changes(acme)).Get(acme, m.ID, "ada")
		if !reflect.DeepEqual(rebuilt.Deliveries, after.Deliveries) {
			t.Fatalf("rebuilt = %+v", rebuilt.Deliveries)
		}
	})
}

func TestADeliveryNeedsAMessageTheClientMaySee(t *testing.T) {
	s := NewStore()
	m := raise(s, "a", t0)
	post := Delivery{Channel: "C0FIELD", TS: "1.2", At: t0}
	if err := s.Deliver(acme, m.ID, "slack-acme", post, func(Message) bool { return false }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a message the client may not see: %v", err)
	}
	if err := s.Deliver("globex", m.ID, "slack-acme", post, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another tenant's message: %v", err)
	}
	if err := s.Deliver(acme, "m-99", "slack-acme", post, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no such message: %v", err)
	}
	for name, d := range map[string]Delivery{
		"no channel": {TS: "1.2"}, "no post": {Channel: "C1"}, "a space": {Channel: "C 1", TS: "1.2"},
		"too long": {Channel: string(make([]byte, 65)), TS: "1.2"},
	} {
		if err := s.Deliver(acme, m.ID, "slack-acme", d, nil); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := s.Deliver(acme, m.ID, "", post, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("no client: %v", err)
	}
	// A raised message carries no deliveries of its own.
	r, _ := two(s.Raise(acme, "b", Message{Kind: KindAlert, Deliveries: map[string]Delivery{"slack-acme": post}}, t0))
	if r.Deliveries != nil {
		t.Fatalf("raised with deliveries %+v", r.Deliveries)
	}
	// At most MaxDeliveries clients, then a new one is not recorded.
	for i := range MaxDeliveries + 1 {
		if err := s.Deliver(acme, m.ID, "c"+strconv.Itoa(i), post, nil); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := s.Get(acme, m.ID, "ada"); len(got.Deliveries) != MaxDeliveries {
		t.Fatalf("%d deliveries kept", len(got.Deliveries))
	}
}

func TestAFailedDeliveryIsNotKept(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	s, _ := OpenStore(f)
	m := raise(s, "a", t0)
	f.Fail(statetest.ErrInjected)
	if err := s.Deliver(acme, m.ID, "slack-acme", Delivery{Channel: "C1", TS: "1.2", At: t0}, nil); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Deliver = %v", err)
	}
	f.Fail(nil)
	if got, _ := s.Get(acme, m.ID, "ada"); got.Deliveries != nil || len(s.Changes(acme)) != 1 {
		t.Fatalf("a delivery that was not written = %+v", got.Deliveries)
	}
}

// A client's receipt finds the message it posted there, after a restart and a
// rebuild too, and only while the receipt says so: a post the receipt moved
// from finds nothing, and another client's post is its own.
func TestAReceiptFindsTheMessagePostedThere(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		m := raise(s, "announce:Field:1", t0, Part{Type: PartText, State: PartInfo, Text: "We shipped."})
		first := Delivery{Channel: "C0FIELD", TS: "1700000000.000100", At: t0}
		moved := Delivery{Channel: "C0FIELD", TS: "1700000099.000100", At: t0.Add(time.Second)}
		for _, d := range []Delivery{first, moved} {
			if err := s.Deliver(acme, m.ID, "slack-acme", d, nil); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.Deliver(acme, m.ID, "slack-other", first, nil); err != nil {
			t.Fatal(err)
		}
		reopened, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		for name, store := range map[string]*Store{"the store": s, "reopened": reopened, "rebuilt": Rebuild(s.Changes(acme))} {
			if got, ok := store.ByDelivery(acme, "slack-acme", moved); !ok || got.ID != m.ID {
				t.Errorf("%s: the receipt finds %+v, %v", name, got.ID, ok)
			}
			if _, ok := store.ByDelivery(acme, "slack-acme", first); ok {
				t.Errorf("%s: the post the receipt moved from still finds the message", name)
			}
			if got, ok := store.ByDelivery(acme, "slack-other", first); !ok || got.ID != m.ID {
				t.Errorf("%s: another client's receipt finds %+v, %v", name, got.ID, ok)
			}
			if _, ok := store.ByDelivery("globex", "slack-acme", moved); ok {
				t.Errorf("%s: another tenant finds the message", name)
			}
		}
		got, _ := s.ByDelivery(acme, "slack-acme", Delivery{Channel: moved.Channel, TS: moved.TS})
		got.Parts[0].Text = "changed"
		if again, _ := s.ByID(acme, m.ID); again.Parts[0].Text != "We shipped." {
			t.Fatal("a copy of the message changed the store")
		}
		if _, ok := s.ByID(acme, "m-99"); ok {
			t.Fatal("ByID found a message that is not there")
		}
	})
}
