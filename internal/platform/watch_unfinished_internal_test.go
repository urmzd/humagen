package platform

import (
	"context"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/runs"
)

// A run admitted on another replica is recorded in the shared stores but not
// watched here. Once that replica stops, this one must observe the tenant, or
// the run goes on here while its events reach neither the index nor its
// surfaces. A running process joins every tenant with a run not yet ended.
func TestAProcessWatchesTenantsWithRunsItDidNotAdmit(t *testing.T) {
	was := unfinishedScan
	unfinishedScan = 20 * time.Millisecond
	t.Cleanup(func() { unfinishedScan = was })

	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("platform stopped with %v", err)
		}
	})

	watched := func(tenant string) bool {
		p.watching.mu.Lock()
		defer p.watching.mu.Unlock()
		return p.watching.seen[tenant]
	}
	if watched("elsewhere") {
		t.Fatal("a tenant with no run is watched")
	}
	// What another replica's admission leaves in the shared stores.
	if err := p.Runs.Record("r-elsewhere", wire.NewEvent(wire.Raise{ID: "r-elsewhere", Tenant: "elsewhere", Agent: "lead", Origin: runs.OriginAPI, Text: "hi", At: time.Now()}), 1); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); !watched("elsewhere"); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the tenant of a run admitted elsewhere was never watched")
		}
	}
}
