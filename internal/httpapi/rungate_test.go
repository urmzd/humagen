package httpapi

import (
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/runs"
)

// A run's first event reaches a stream on another replica before the run is
// recorded: the replica admitting it is the only one that can place it
// meanwhile. The stream waits for the record rather than drop the event, and
// gives up on a run that is never recorded, once.
func TestTheGateWaitsForARunRecordedLate(t *testing.T) {
	f := newFixture(t)
	who := auth.Principal{Subject: "ada", Tenant: "acme", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	gate := newRunGate(t.Context(), f.p, who)

	seen := make(chan bool, 1)
	go func() { seen <- gate.seen("late") }()
	time.Sleep(100 * time.Millisecond)
	if err := f.p.Runs.Record("late", wire.NewEvent(wire.Raise{ID: "late", Tenant: "acme", Agent: "lead", Origin: runs.OriginAPI, Text: "hi", At: time.Now()}), 1); err != nil {
		t.Fatal(err)
	}
	select {
	case ok := <-seen:
		if !ok {
			t.Fatal("a run recorded while the stream waited was not seen")
		}
	case <-time.After(runRecordWait + time.Second):
		t.Fatal("the gate never answered")
	}

	start := time.Now()
	if gate.seen("never") {
		t.Fatal("a run never recorded was seen")
	}
	if waited := time.Since(start); waited < runRecordWait {
		t.Fatalf("gave up on an unrecorded run after %v, want %v", waited, runRecordWait)
	}
	start = time.Now()
	if gate.seen("never") || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("a run already given up on was waited for again (%v)", time.Since(start))
	}
}
