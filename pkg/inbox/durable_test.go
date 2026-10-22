package inbox

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// two is Raise's message and whether it was raised, for a store that writes
// nowhere that fails.
func two(m Message, raised bool, err error) (Message, bool) {
	if err != nil {
		panic(err)
	}
	return m, raised
}

func TestRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		q, _ := two(s.Raise(acme, "q1", Message{
			Kind: KindApproval, To: "", Audience: []string{"role:operator", "ada"}, Excluded: []string{"eve"},
			From: From{Kind: "agent", Name: "lead"}, Severity: Warning, Source: Source{Run: "r1", Agent: "lead"},
			Parts: []Part{{Type: PartText, State: PartInfo, Text: "Context."}, {Type: PartApproval, Question: &QuestionPart{
				Run: "r1", ID: "q1", Kind: "approval", Tool: &QuestionTool{Name: "issues__search", ActsForPerson: true},
			}}},
		}, t0))
		a := raise(s, "alert:al-1", t0.Add(time.Second), Part{Type: PartAlert, Alert: &AlertPart{ID: "al-1"}})
		if _, err := s.Read(acme, a.ID, "ada", true, t0.Add(2*time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Resolve(acme, q.ID, "p2", Outcome{Action: "approve", By: "ada", At: t0.Add(3 * time.Second), Note: "fine"}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Snooze(acme, a.ID, "ada", t0.Add(time.Hour), t0.Add(4*time.Second)); err != nil {
			t.Fatal(err)
		}
		d := raise(s, "alert:al-2", t0.Add(5*time.Second), Part{Type: PartAlert})
		if _, _, err := s.Dismiss(acme, d.ID, "ada", t0.Add(6*time.Second)); err != nil {
			t.Fatal(err)
		}
		raise(s, "globex-free", t0)

		again, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		gotMsgs, gotRead := again.Snapshot(acme)
		wantMsgs, wantRead := s.Snapshot(acme)
		if !reflect.DeepEqual(gotMsgs, wantMsgs) || !reflect.DeepEqual(gotRead, wantRead) {
			t.Fatalf("after restart = %+v %v, want %+v %v", gotMsgs, gotRead, wantMsgs, wantRead)
		}
		if !reflect.DeepEqual(again.Changes(acme), s.Changes(acme)) {
			t.Fatalf("record after restart = %+v, want %+v", again.Changes(acme), s.Changes(acme))
		}
		// That an approval's call acts as the person survives too.
		if m, err := again.Get(acme, q.ID, "ada"); err != nil || !m.Parts[1].Question.Tool.ActsForPerson {
			t.Fatalf("after restart the approval is %+v (%v)", m.Parts[1].Question, err)
		}
		// Who a message is for survives, though no wire form carries it.
		if _, err := again.GetAs(acme, q.ID, Reader{Subject: "eve", Roles: []string{"operator"}}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("an excluded reader sees the message after restart: %v", err)
		}
		if _, err := again.GetAs(acme, q.ID, Reader{Subject: "bob", Roles: []string{"operator"}}); err != nil {
			t.Fatalf("a reader by role after restart: %v", err)
		}
		// A key still open is not raised twice, and ids carry on.
		if _, raised := two(again.Raise(acme, "alert:al-1", Message{Kind: KindAlert}, t0)); raised {
			t.Fatal("an open key was raised again after restart")
		}
		next := raise(again, "alert:al-3", t0.Add(time.Minute))
		if next.ID == a.ID || next.ID == q.ID || next.ID == d.ID {
			t.Fatalf("a restarted inbox issued %q again", next.ID)
		}
		third, _ := OpenStore(st)
		if m, err := third.Get(acme, next.ID, "ada"); err != nil || m.Subject != "alert:al-3" {
			t.Fatalf("a message raised after the restart = %+v, %v", m, err)
		}
	})
}

func TestFailedWriteChangesNothing(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	s, _ := OpenStore(f)
	m := raise(s, "alert:al-1", t0, Part{Type: PartAlert})
	notices, stop := s.Watch(acme)
	defer stop()
	f.Fail(statetest.ErrInjected)
	if _, _, err := s.Raise(acme, "alert:al-2", Message{Kind: KindAlert}, t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Raise = %v", err)
	}
	if _, _, err := s.Dismiss(acme, m.ID, "ada", t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Dismiss = %v", err)
	}
	if _, err := s.Read(acme, m.ID, "ada", true, t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Read = %v", err)
	}
	select {
	case n := <-notices:
		t.Fatalf("a change that was not kept was announced: %+v", n)
	default:
	}
	msgs, read := s.Snapshot(acme)
	if len(msgs) != 1 || msgs[0].State != Open || len(read) != 0 || len(s.Changes(acme)) != 1 {
		t.Fatalf("after failed writes = %+v, read %v", msgs, read)
	}
	f.Fail(nil)
	// The watcher is still there, and ids were not used.
	next := raise(s, "alert:al-2", t0)
	if next.ID != "m-2" {
		t.Fatalf("next id = %q", next.ID)
	}
	select {
	case <-notices:
	case <-time.After(time.Second):
		t.Fatal("the watcher was lost when a failed change was undone")
	}
}
