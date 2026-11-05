package usage

import (
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"testing"
)

func checkSettlementReceipt(t *testing.T, one, two *Ledger) {
	t.Helper()
	if _, _, err := one.Record(Entry{Kind: Spent, Tenant: "acme", RunID: "r1", Key: "call:1", At: t0, Spend: &Spend{Micros: 20}}); err != nil {
		t.Fatal(err)
	}
	e := Entry{Kind: Settled, Tenant: "acme", RunID: "r1", Key: "run:r1", At: t0, Spend: &Spend{Micros: 70}}
	if added, err := one.Settle(e); err != nil || added.Micros != 50 {
		t.Fatalf("first = %+v %v", added, err)
	}
	if added, err := two.Settle(e); err != nil || added.Micros != 50 {
		t.Fatalf("retry = %+v %v", added, err)
	}
	entries, err := two.Entries("acme")
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries = %v %v", entries, err)
	}
}
func TestSettlementReceiptSurvivesRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		one, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		two, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		checkSettlementReceipt(t, one, two)
	})
}
func TestSharedSettlementReceiptSurvivesRestart(t *testing.T) {
	ls, _ := processes(t, 2)
	checkSettlementReceipt(t, ls[0], ls[1])
}
