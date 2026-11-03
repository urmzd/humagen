package runs

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestRecordSandboxMergesBoundsAndWritesThrough(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		x, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		record(t, x, "acme", "r1", "lead", "api", 0, nil)
		if err := x.RecordSandbox("acme", "r1", true, []SandboxFile{{Path: "a.md", Size: 3, Revision: "x"}}, nil); err != nil {
			t.Fatal(err)
		}
		if err := x.RecordSandbox("acme", "r1", false, []SandboxFile{{Path: "a.md", Size: 9, Revision: "y", Removed: true}}, []string{"b.md changed"}); err != nil {
			t.Fatal(err)
		}
		got, _ := x.Get("acme", "r1")
		if got.Sandbox == nil || !got.Sandbox.Isolated || len(got.Sandbox.Files) != 1 {
			t.Fatalf("sandbox = %+v", got.Sandbox)
		}
		if f := got.Sandbox.Files[0]; !f.Removed || f.Revision != "" || f.Size != 0 {
			t.Fatalf("a removed file keeps no content facts: %+v", f)
		}
		if len(got.Sandbox.Conflicts) != 1 {
			t.Fatal(got.Sandbox.Conflicts)
		}

		// Bounded: past the cap, files are counted, not named.
		many := make([]SandboxFile, MaxSandboxFiles+5)
		for i := range many {
			many[i] = SandboxFile{Path: fmt.Sprintf("f%03d", i), Size: 1}
		}
		long := []string{strings.Repeat("é", 600)}
		for range MaxSandboxConflicts + 3 {
			long = append(long, "x")
		}
		if err := x.RecordSandbox("acme", "r1", true, many, long); err != nil {
			t.Fatal(err)
		}
		got, _ = x.Get("acme", "r1")
		if len(got.Sandbox.Files) != MaxSandboxFiles || got.Sandbox.More != 6 {
			t.Fatalf("files %d more %d", len(got.Sandbox.Files), got.Sandbox.More)
		}
		if len(got.Sandbox.Conflicts) != MaxSandboxConflicts || len(got.Sandbox.Conflicts[1]) > maxSandboxText {
			t.Fatalf("conflicts %d", len(got.Sandbox.Conflicts))
		}

		// Persisted: a second index over the same store reads it back.
		y, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		again, _ := y.Get("acme", "r1")
		if again.Sandbox == nil || len(again.Sandbox.Files) != MaxSandboxFiles {
			t.Fatal("the sandbox record was not written through")
		}

		// Another tenant's run is not there to record on.
		if err := x.RecordSandbox("other", "r1", true, []SandboxFile{{Path: "a"}}, nil); !errors.Is(err, ErrNotFound) {
			t.Fatalf("another tenant: %v", err)
		}
		if err := x.RecordSandbox("acme", "r1", true, nil, nil); err != nil {
			t.Fatal(err)
		}
	})
}
