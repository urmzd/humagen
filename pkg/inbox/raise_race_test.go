package inbox

import (
	"fmt"
	"sync"
	"testing"
)

// Raises of one key at the same time give every caller the one message,
// raised once. A raise that loses the race returns the message that won,
// never an empty one: the platform raises a question both from its event
// and from an answer that arrives first, and an empty message reads as one
// that asks nothing.
func TestRaisesOfOneKeyAtOnceReturnOneMessage(t *testing.T) {
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
				got[i], raised[i], errs[i] = s.Raise(acme, key, Message{Kind: KindAlert, Subject: key, From: From{Kind: "agent", Name: "lead"}, Severity: Warning, Parts: []Part{{Type: PartApproval}}}, t0)
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
}
