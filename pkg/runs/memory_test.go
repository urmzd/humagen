package runs

import (
	"errors"
	"fmt"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestRecordMemoryKeepsTheInstallAcrossTheReleaseWithinBounds(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		x, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		record(t, x, "acme", "r1", "lead", "api", 0, nil)
		me := MemoryUse{Kind: "person", Person: "ada", Dir: "me/memory"}
		installed := me
		installed.Version, installed.Files = 3, 4
		if err := x.RecordMemory("acme", "r1", []MemoryUse{installed}); err != nil {
			t.Fatal(err)
		}
		finished := me
		finished.Finished, finished.Committed = true, 4
		finished.Proposals = make([]string, MaxMemoryProposals+2)
		if err := x.RecordMemory("acme", "r1", []MemoryUse{finished}); err != nil {
			t.Fatal(err)
		}
		got, _ := x.Get("acme", "r1")
		if len(got.Memory) != 1 {
			t.Fatalf("memory = %+v", got.Memory)
		}
		if u := got.Memory[0]; u.Version != 3 || u.Files != 4 || u.Committed != 4 || !u.Finished || len(u.Proposals) != MaxMemoryProposals {
			t.Fatalf("use = %+v", u)
		}
		many := make([]MemoryUse, MaxMemoryTrees+3)
		for i := range many {
			many[i] = MemoryUse{Kind: "agent", Agent: fmt.Sprintf("a%d", i)}
		}
		if err := x.RecordMemory("acme", "r1", many); err != nil {
			t.Fatal(err)
		}
		if again, _ := (func() (Run, error) { y, _ := Open(st); return y.Get("acme", "r1") })(); len(again.Memory) != MaxMemoryTrees {
			t.Fatalf("trees = %d", len(again.Memory))
		}
		if err := x.RecordMemory("other", "r1", []MemoryUse{me}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("another tenant = %v", err)
		}
	})
}
