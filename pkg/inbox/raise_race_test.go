package inbox

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// Raises of one key at the same time give every caller the one message,
// raised once. A raise that loses the race returns the message that won,
// never an empty one: the platform raises a question both from its event
// and from an answer that arrives first, and an empty message reads as one
// that asks nothing.
func TestRaisesOfOneKeyAtOnceReturnOneMessage(t *testing.T) {
	raises := map[string]func(s *Store, key string, m Message) (Message, bool, error){
		"Raise":     func(s *Store, key string, m Message) (Message, bool, error) { return s.Raise(acme, key, m, t0) },
		"RaiseOnce": func(s *Store, key string, m Message) (Message, bool, error) { return s.RaiseOnce(acme, key, m, t0) },
	}
	for name, raise := range raises {
		t.Run(name, func(t *testing.T) {
			for round := range 200 {
				s := NewStore()
				key := fmt.Sprintf("question:r%d:q1", round)
				const n = 8
				got := make([]Message, n)
				raised := make([]bool, n)
				errs := make([]error, n)
				start := make(chan struct{})
				var wg sync.WaitGroup
				for i := range n {
					wg.Go(func() {
						<-start
						got[i], raised[i], errs[i] = raise(s, key, Message{Kind: KindAlert, Subject: key, From: From{Kind: "agent", Name: "lead"}, Severity: Warning, Parts: []Part{{Type: PartApproval}}})
					})
				}
				close(start)
				wg.Wait()
				first := 0
				for i := range n {
					if errs[i] != nil {
						t.Fatalf("round %d: raise %d: %v", round, i, errs[i])
					}
					if got[i].ID == "" || len(got[i].Parts) != 1 {
						t.Fatalf("round %d: raise %d returned an empty message: %+v", round, i, got[i])
					}
					if got[i].ID != got[0].ID {
						t.Fatalf("round %d: raises returned %q and %q", round, got[0].ID, got[i].ID)
					}
					if raised[i] {
						first++
					}
				}
				if first != 1 {
					t.Fatalf("round %d: %d raises said they raised it, want 1", round, first)
				}
			}
		})
	}
}

// A message raised once is not raised again when it is closed: a raise that
// comes after the question was answered gets the answered message back.
// Raise, for what can happen again, raises a new one.
func TestRaiseOnceKeepsOneMessageWhateverItsState(t *testing.T) {
	s := NewStore()
	const key = "question:r1:q1"
	msg := Message{Kind: KindAlert, Subject: key, From: From{Kind: "agent", Name: "lead"}, Severity: Warning, Parts: []Part{{Type: PartApproval}}}
	m, raised, err := s.RaiseOnce(acme, key, msg, t0)
	if err != nil || !raised || m.ID == "" {
		t.Fatalf("the first raise = %+v, %v, %v", m, raised, err)
	}
	if _, _, err := s.Resolve(acme, m.ID, "p1", Outcome{Action: "approve", By: "ada", At: t0.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	again, raised, err := s.RaiseOnce(acme, key, msg, t0.Add(2*time.Minute))
	if err != nil || raised || again.ID != m.ID || again.State != Resolved {
		t.Fatalf("a raise after the answer = %s (%s), raised %v, %v; want %s back, resolved", again.ID, again.State, raised, err, m.ID)
	}
	if n, _ := s.Snapshot(acme); len(n) != 1 {
		t.Fatalf("the inbox holds %d messages, want 1", len(n))
	}
	if next, raised, err := s.Raise(acme, key, msg, t0.Add(3*time.Minute)); err != nil || !raised || next.ID == m.ID {
		t.Fatalf("Raise after the first was closed = %s, raised %v, %v; want a new message", next.ID, raised, err)
	}
}
