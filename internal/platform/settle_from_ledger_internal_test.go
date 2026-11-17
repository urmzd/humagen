package platform

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/runs"
	drun "github.com/urmzd/saige/durable/run"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
)

// scriptedLedger is the platform's ledger with the ending of some runs said
// by the test: a run it names has ended there with the given output, and
// reading its records fails while fail is set. Every other run is the real
// ledger's.
type scriptedLedger struct {
	Ledger
	ended map[string]drun.Run
	fail  error
	loads int
}

func (l *scriptedLedger) GetRun(ctx context.Context, id string) (drun.Run, error) {
	if rec, ok := l.ended[id]; ok {
		return rec, nil
	}
	return l.Ledger.GetRun(ctx, id)
}

func (l *scriptedLedger) Load(ctx context.Context, id string) (drun.Run, []drun.Record, error) {
	rec, ok := l.ended[id]
	if !ok {
		return l.Ledger.Load(ctx, id)
	}
	l.loads++
	if l.fail != nil {
		return drun.Run{}, nil, l.fail
	}
	return rec, nil, nil
}

// ledgerEnded is a platform whose index holds one run still working, which
// its ledger says succeeded with the given usage (nil for a run that made no
// model call). Nothing observes events: only the ledger can end the run.
func ledgerEnded(t *testing.T, id string, used *event.RunUsage) (*Platform, *scriptedLedger) {
	t.Helper()
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	if err := p.Runs.Record(id, wire.NewEvent(wire.Raise{ID: "evt-" + id, Tenant: "t", Agent: "lead", Origin: runs.OriginAPI, Text: "go", At: at}), 1); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(runtime.Output{Text: "Done.", Usage: used})
	if err != nil {
		t.Fatal(err)
	}
	l := &scriptedLedger{Ledger: p.ledger, ended: map[string]drun.Run{id: {ID: id, Status: drun.StatusSucceeded, Output: out, CompletedAt: at}}}
	p.ledger = l
	return p, l
}

// A run the ledger says is over is not ended while what it used cannot be
// read: a reader sees it still working, with what was tallied, and nothing
// is acted on. The next pass that reads the record ends it, with the record
// on it in the same write.
func TestARunIsNotEndedFromTheLedgerWhileItsUsageCannotBeRead(t *testing.T) {
	one := event.Usage{ModelCalls: 1, Requests: 1}
	used := &event.RunUsage{Own: one, Children: one.Add(one), Total: one.Add(one).Add(one)}
	p, l := ledgerEnded(t, "r1", used)
	ctx := t.Context()
	// One of its two children's reports had been observed.
	p.Runs.ObserveUsage(event.Envelope{Run: "r1", Tenant: "t", Seq: 1, Type: event.TypeUsageChild,
		Data: event.UsageChild{AccountingID: event.ChildRunAccountingID("c1"), RunID: "c1", Agent: "ops", Depth: 1, Usage: one}})

	l.fail = errors.New("ledger: connection reset")
	for pass := 1; pass <= 2; pass++ {
		p.reconcile(ctx)
		if l.loads != pass {
			t.Fatalf("pass %d read the run's records %d times", pass, l.loads)
		}
		r, err := p.Runs.Get("t", "r1")
		if err != nil {
			t.Fatal(err)
		}
		if r.Status.Terminal() || r.UsageRecorded() || r.Answer != "" {
			t.Fatalf("pass %d ended the run without its usage: status %q, answer %q, usage %+v", pass, r.Status, r.Answer, r.Usage)
		}
		if pending, err := p.Runs.PendingCompletions(); err != nil || len(pending) != 0 {
			t.Fatalf("pass %d left an ending to act on: %+v (%v)", pass, pending, err)
		}
		if n := len(p.Runs.Unfinished()); n != 1 {
			t.Fatalf("pass %d: %d runs are unfinished, want the one to read again", pass, n)
		}
	}

	l.fail = nil
	p.reconcile(ctx)
	r, err := p.Runs.Get("t", "r1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != runs.Completed || !r.UsageRecorded() || r.Usage.Children.ModelCalls != 2 || r.Usage.Own.ModelCalls != 1 || r.Usage.Total.ModelCalls != 3 {
		t.Fatalf("the run ended as %q with usage %+v (recorded %v), want the ledger's", r.Status, r.Usage, r.UsageRecorded())
	}
	if n := len(p.Runs.Unfinished()); n != 0 {
		t.Fatalf("%d runs are still unfinished", n)
	}
}

// A run with no recorded usage, one that made no model call, is not a run
// whose usage could not be read: the ledger ends it in the first pass.
func TestARunWithNoRecordedUsageIsEndedFromTheLedgerAtOnce(t *testing.T) {
	p, l := ledgerEnded(t, "r1", nil)
	p.reconcile(t.Context())
	r, err := p.Runs.Get("t", "r1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != runs.Completed || r.Usage != nil || r.UsageRecorded() {
		t.Fatalf("the run is %q with usage %+v (recorded %v), want completed with none", r.Status, r.Usage, r.UsageRecorded())
	}
	if l.loads == 0 {
		t.Fatal("the run's records were never read")
	}
}
