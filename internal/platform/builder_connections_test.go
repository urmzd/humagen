package platform

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/runtime"
	flowturn "github.com/urmzd/orchagent/flow/turn"
)

func TestBuilderConnectionStatusAndConsentStayInCurrentOwnersPrivateSession(t *testing.T) {
	policy, err := auth.ParsePolicy("connections", `role user { allow * on * } grant user to subject "alice"`)
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(Config{Workspace: t.TempDir(), Policy: policy, ConnectionAuth: nativeConfig("drive")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	th, err := p.Threads.Create("t", threads.Thread{ID: "th-0123456789abcdef", Channel: "general", Visibility: threads.Private, CreatedBy: "alice", Participants: []threads.Participant{{Kind: threads.Person, Name: "alice"}}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tag := wire.NewEvent(wire.Raise{ID: "builder-turn", Tenant: "t", Agent: BuiltinBuilder, Origin: runs.OriginAPI, Actor: event.Actor{ID: "alice", User: "alice"}, At: time.Now(), Context: map[string]string{runs.MetaThread: th.ID}})
	if err = p.Runs.Record("builder-run", tag, 1); err != nil {
		t.Fatal(err)
	}
	req := runtime.Request{Tenant: "t", RunID: "builder-run", Definition: flowturn.Pinned{Spec: flowturn.Spec{Name: BuiltinBuilder}}}
	authorizeNative(t, p, p.byPolicy("t", "alice"), "drive", "person")
	for _, connect := range []bool{false, true} {
		raw, err := p.builderConnectionsTool(req, connect).Execute(t.Context(), map[string]any{"provider": "drive"})
		if err != nil || raw == "" || strings.Contains(raw, "drive-token") {
			t.Fatalf("own session connect=%v: %s %v", connect, raw, err)
		}
	}
	assertDenied := func(request runtime.Request) {
		t.Helper()
		for _, connect := range []bool{false, true} {
			raw, err := p.builderConnectionsTool(request, connect).Execute(t.Context(), map[string]any{"provider": "drive"})
			if !errors.Is(err, auth.ErrForbidden) || raw != "" {
				t.Fatalf("connection metadata escaped connect=%v: %q %v", connect, raw, err)
			}
		}
	}
	foreign := req
	foreign.Tenant = "other"
	assertDenied(foreign)
	unknown := req
	unknown.RunID = "missing"
	assertDenied(unknown)
	if _, err = p.Threads.Participants("t", th.ID, th.Version, []threads.Participant{{Kind: threads.Person, Name: "bob"}}, nil, nil, "alice", time.Now()); err != nil {
		t.Fatal(err)
	}
	assertDenied(req)
	th, err = p.Threads.Get("t", th.ID)
	if err != nil {
		t.Fatal(err)
	}
	th, err = p.Threads.Participants("t", th.ID, th.Version, nil, []string{"bob"}, nil, "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Threads.Update("t", th.ID, th.Version, time.Now(), func(th *threads.Thread) error {
		th.Visibility = threads.Shared
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	assertDenied(req)
}
