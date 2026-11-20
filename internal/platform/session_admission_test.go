package platform_test

import (
	"context"
	"errors"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/usage"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/session"
)

type missingSessionRecord struct {
	session.Store
}

func (s missingSessionRecord) Get(context.Context, string, string) (session.Record, error) {
	return session.Record{}, session.ErrNotFound
}

func TestSessionLookupFailureReleasesAdmission(t *testing.T) {
	p := start(t, call.Limits{})
	root, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Agent: "lead", Text: "Begin"})
	if err != nil {
		t.Fatal(err)
	}
	var first runs.Run
	for _, m := range root.Messages {
		if m.Run != nil {
			first = await(t, p, m.Run.ID, runs.Completed)
		}
	}
	if first.ID == "" {
		t.Fatal("initial run missing")
	}
	endings(p, first.ID)
	p.Sessions = missingSessionRecord{Store: p.Sessions}
	tag := first.Tag()
	tag.ID = "session-lookup-failure"
	if _, err := p.Invoke(t.Context(), tag); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("lookup failure: %v", err)
	}
	runID := runtime.RunID(admin.Tenant, tag.ID)
	admitted, ended := 0, 0
	for _, entry := range entriesOf(p, admin.Tenant) {
		if entry.RunID != runID {
			continue
		}
		if entry.Kind == usage.Admitted {
			admitted++
		}
		if entry.Kind == usage.Ended {
			ended++
		}
	}
	if admitted != 1 || ended != 1 {
		t.Fatalf("lookup failure leaked admission: admitted=%d ended=%d", admitted, ended)
	}
	if _, err := p.Runs.Get(admin.Tenant, runID); !errors.Is(err, runs.ErrNotFound) {
		t.Fatalf("unaccepted run recorded: %v", err)
	}
}
