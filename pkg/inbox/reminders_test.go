package inbox

import (
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestReminderIsOneDurableUpdateOfTheExistingConfinedMessage(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		fault := statetest.NewFaulty(st)
		s, err := OpenStore(fault)
		if err != nil {
			t.Fatal(err)
		}
		at := time.Now().UTC()
		m, _, err := s.Raise("t", "pending", Message{Kind: KindApproval, Subject: "Review private work", Audience: []string{"reader"}, Source: Source{Project: "private"}, Parts: []Part{{Type: PartApproval, Question: &QuestionPart{ID: "q", Run: "run", Agent: "worker"}}}}, at)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Read("t", m.ID, "reader", true, at); err != nil {
			t.Fatal(err)
		}
		fault.Fail(statetest.ErrInjected)
		if _, _, err := s.Remind("t", m.ID, at.Add(4*time.Hour)); !errors.Is(err, statetest.ErrInjected) {
			t.Fatalf("failed reminder persistence=%v", err)
		}
		unchanged, err := s.Get("t", m.ID, "reader")
		if err != nil || unchanged.Unread {
			t.Fatal("failed reminder changed read state")
		}
		fault.Fail(nil)
		reminded, changed, err := s.Remind("t", m.ID, at.Add(4*time.Hour))
		if err != nil || !changed || !reminded.UpdatedAt.Equal(at.Add(4*time.Hour)) {
			t.Fatalf("reminder=%+v,%v,%v", reminded, changed, err)
		}
		unread, err := s.Get("t", m.ID, "reader")
		if err != nil || !unread.Unread {
			t.Fatal("reminder did not reach its reader")
		}
		messages, _ := s.Snapshot("t")
		if len(messages) != 1 {
			t.Fatal("reminder duplicated the cause")
		}
		if count := s.Count("t", Query{Reader: "reader", Sees: func(Message) bool { return false }}); count.Unread != 0 {
			t.Fatal("reminder leaked a hidden unread count")
		}
		s, err = OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		if _, changed, err := s.Remind("t", m.ID, at.Add(5*time.Hour)); err != nil || changed {
			t.Fatalf("restart repeated reminder: %v,%v", changed, err)
		}
		current, _ := s.ByID("t", m.ID)
		if _, _, err := s.Resolve("t", m.ID, current.Parts[0].ID, Outcome{Action: "deny", By: "reader", At: at.Add(6 * time.Hour)}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Retain("t", at.Add(8*24*time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, ok := s.ByID("t", m.ID); ok {
			t.Fatal("reminder escaped its message retention")
		}
	})
}
