package platform_test

import (
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/readaudit"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestBreakGlassChecksOrdinaryRolesAndCachesWrites(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := statetest.NewFaulty(open(t))
		p, err := newOn(t, st)
		if err != nil {
			t.Fatal(err)
		}
		gov := auth.Principal{Subject: "governor", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Admin}}
		_, err = p.CreateProject(t.Context(), tenant, projects.Writer{By: "owner", Govern: true}, platform.ProjectInput{Name: "Secret", Members: []projects.Entry{{Who: "owner", Role: projects.Owner}}})
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now().Add(-time.Second)
		count := func() int { return len(p.ReadAudit.List(tenant, start, time.Now().Add(time.Second), 500)) }
		ordinary := gov
		ordinary.Subject = "owner"
		if !p.Seer(p.AuditedReader(ordinary, "GET /api/projects/{name}")).Project("Secret") || count() != 0 {
			t.Fatal("own role should read without audit")
		}
		reader := p.AuditedReader(gov, "GET /api/projects")
		for range 3 {
			if !p.Seer(reader).Project("Secret") {
				t.Fatal("governor denied")
			}
		}
		if count() != 1 {
			t.Fatalf("one request recorded %d times", count())
		}
		st.Fail(statetest.ErrInjected)
		denied := p.AuditedReader(gov, "GET /api/projects/{name}")
		if p.Seer(denied).Project("Secret") {
			t.Fatal("audit failure permitted read")
		}
		st.Fail(nil)
		if p.Seer(denied).Project("Secret") {
			t.Fatal("failed audit not cached")
		}
		if !p.Seer(p.AuditedReader(gov, "GET /api/projects/{name}")).Project("Secret") {
			t.Fatal("new request did not retry audit")
		}
		// A member who governs still needs an audit to read a private thread they do not participate in.
		_, err = p.Threads.Create(tenant, threads.Thread{ID: "th-0123456789abcdef", Project: "Secret", Channel: "general", Visibility: threads.Private, Participants: []threads.Participant{{Kind: threads.Person, Name: "someone"}}}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		n := count()
		if !p.Seer(p.AuditedReader(ordinary, "GET /api/threads/{id}")).Place(platform.Place{Project: "Secret", Thread: "th-0123456789abcdef"}) || count() != n+1 {
			t.Fatal("private nonparticipant read not recorded")
		}
		// Count and event-stream reads carry their own route through fresh Seers.
		counting := p.AuditedReader(ordinary, "GET /api/projects")
		n = count()
		p.ProjectCounts(counting)
		p.ProjectCounts(counting)
		if count() != n+1 {
			t.Fatal("count probes were not attributed once")
		}
		streaming := p.AuditedReader(ordinary, "GET /api/events")
		n = count()
		for range 3 {
			if !p.Seer(streaming).Conversation("th-0123456789abcdef", "Secret") {
				t.Fatal("stream read denied")
			}
		}
		if count() != n+1 {
			t.Fatal("stream probes were not attributed once")
		}
		_, err = p.CreateProject(t.Context(), tenant, projects.Writer{By: "owner", Govern: true}, platform.ProjectInput{Name: "Gone", Members: []projects.Entry{{Who: "owner", Role: projects.Owner}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := p.DeleteProject(t.Context(), tenant, "Gone", projects.Writer{By: "owner", Govern: true}); err != nil {
			t.Fatal(err)
		}
		n = count()
		if !p.Seer(p.AuditedReader(ordinary, "GET /api/runs")).Project("Gone") || count() != n {
			t.Fatal("deleted project lost ordinary role")
		}
		if !p.Seer(p.AuditedReader(gov, "GET /api/runs")).Project("Gone") || count() != n+1 {
			t.Fatal("deleted project bypass not recorded")
		}

	})
}

func TestAuditOnlyWorkspacesFollowTheirInboxRetention(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		now := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
		p, err := platform.New(platform.Config{Backends: platform.Backends{State: st}, Workspace: t.TempDir(), Now: func() time.Time { return now }, InboxRetention: map[string]time.Duration{"short": 7 * 24 * time.Hour}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.Close() })
		for _, tenant := range []string{"short", "default"} {
			if err := p.ReadAudit.Add(tenant, readaudit.Record{Subject: "ana", Project: "customer", Route: "GET /api/projects", At: now.Add(-8 * 24 * time.Hour)}); err != nil {
				t.Fatal(err)
			}
		}
		p.RetainInbox()
		again, err := readaudit.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if len(again.List("short", now.Add(-10*24*time.Hour), now, 100)) != 0 {
			t.Fatal("short retention was not replayed")
		}
		if len(again.List("default", now.Add(-10*24*time.Hour), now, 100)) != 1 {
			t.Fatal("default retention discarded a kept read")
		}
	})
}
