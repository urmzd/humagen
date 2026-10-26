package platform_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func projectForkSource(t *testing.T, p *platform.Platform) string {
	t.Helper()
	field(t, p)
	_, err := p.Agents.Create(t.Context(), tenant, agentspec.AgentSpec{Name: "planner", Model: "offline", Provider: "offline", SystemPrompt: "Plan carefully."}, admin.Subject)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Projects.SetHome(tenant, "planner", "Field", govern, time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Projects.CreateChannel(tenant, projects.Channel{Project: "Field", Name: "planning", Purpose: "Independent plans", Default: "planner"}, ada, time.Now()); err != nil {
		t.Fatal(err)
	}
	note, err := p.Memory.Add(tenant, memory.ProjectOwner("Field"), "A confirmed launch decision", memory.Person, memory.Approved, admin.Subject, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Memory.Add(tenant, memory.ProjectOwner("Field"), "An unconfirmed proposal", memory.Consolidated, memory.Pending, admin.Subject, time.Now()); err != nil {
		t.Fatal(err)
	}
	return note.ID
}

func TestProjectForkCopiesBoundedConfigurationAndOnlyConfirmedSelectedMemory(t *testing.T) {
	p := startWith(t, platform.Config{})
	note := projectForkSource(t, p)
	op, err := p.ForkProject(t.Context(), admin, "Field", platform.ProjectForkInput{Name: "Copy", Memory: []string{note}})
	if err != nil || op.State != "ready" {
		t.Fatalf("fork: %+v %v", op, err)
	}
	if op.Projects != 4 || op.Agents != 1 || op.Memory != 1 {
		t.Fatalf("bounded plan: %+v", op)
	}
	if !p.Seer(admin).Project("Copy") {
		t.Fatal("ready fork absent")
	}
	home, ok := p.ProjectTree(tenant).Home("copy-planner")
	if !ok || home != "Copy" {
		t.Fatalf("copied agent has no home: %s %v", home, ok)
	}
	rev, err := p.Agents.Get(t.Context(), tenant, "copy-planner", 0)
	if err != nil || rev.Spec.SystemPrompt != "Plan carefully." {
		t.Fatalf("agent config: %+v %v", rev, err)
	}
	channel, err := p.Projects.Channel(tenant, "Copy", "planning")
	if err != nil || channel.Default != "copy-planner" || channel.Slack != nil {
		t.Fatalf("channel: %+v %v", channel, err)
	}
	notes := p.Memory.List(tenant, memory.ProjectOwner("Copy"))
	if len(notes) != 1 || notes[0].Text != "A confirmed launch decision" {
		t.Fatalf("memory copied without selection: %+v", notes)
	}
	if rows, _, err := p.ThreadPage(tenant, runs.ThreadQuery{Projects: []string{"Copy"}}); err != nil || len(rows) != 0 {
		t.Fatalf("copied conversations: %+v %v", rows, err)
	}
	again, err := p.ForkProject(t.Context(), admin, "Field", platform.ProjectForkInput{Name: "Copy", Memory: []string{note}})
	if err != nil || again.ID != op.ID || again.State != "ready" {
		t.Fatalf("idempotent resume: %+v %v", again, err)
	}
	if _, err := p.ProjectFork(operator, "Copy"); err == nil {
		t.Fatal("another person read private operation")
	}
}

func TestFailedProjectForkRemainsHiddenAcrossRestartAndResumes(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		raw := open(t)
		fault := &faultyIn{Store: raw}
		p := startWith(t, platform.Config{Backends: platform.Backends{State: fault}})
		note := projectForkSource(t, p)
		fault.fail("memory")
		op, err := p.ForkProject(t.Context(), admin, "Field", platform.ProjectForkInput{Name: "Copy", Memory: []string{note}})
		if err != nil || op.State != "failed" {
			t.Fatalf("failed fork: %+v %v", op, err)
		}
		assertUnpublishedProjectFork(t, p)
		fault.fail("")
		again, err := platform.New(platform.Config{Workspace: t.TempDir(), Backends: platform.Backends{State: fault, Agents: p.Agents}})
		if err != nil {
			t.Fatal(err)
		}
		assertUnpublishedProjectFork(t, again)
		saved, err := again.ProjectFork(admin, "Copy")
		if err != nil || saved.ID != op.ID || saved.State != "failed" {
			t.Fatalf("saved workflow lost: %+v %v", saved, err)
		}
		done, err := again.ForkProject(t.Context(), admin, "Field", platform.ProjectForkInput{Name: "Copy"})
		if err != nil || done.State != "ready" || done.ID != op.ID {
			t.Fatalf("resume: %+v %v", done, err)
		}
		if len(again.Memory.List(tenant, memory.ProjectOwner("Copy"))) != 1 {
			t.Fatal("retry duplicated or omitted selected memory")
		}
	})
}
func assertUnpublishedProjectFork(t *testing.T, p *platform.Platform) {
	t.Helper()
	for _, who := range []struct{ principal string }{{admin.Subject}, {operator.Subject}} {
		reader := admin
		reader.Subject = who.principal
		if p.Seer(reader).Project("Copy") || p.Seer(reader).Project("Copy-Globex") || p.Seer(reader).Agent("copy-planner") {
			t.Fatal("pending config visible through govern")
		}
		if err := p.MayFileIn(reader, "Copy"); !errors.Is(err, projects.ErrNotFound) {
			t.Fatalf("pending project accepts work: %v", err)
		}
	}
	_, err := p.Invoke(t.Context(), envelope.Tag{ID: "must-not-run", Tenant: tenant, Agent: "copy-planner", Origin: "api", Text: "never run", Meta: map[string]string{runs.MetaProject: "Copy"}})
	if err == nil {
		t.Fatal("pending copied agent ran")
	}
}

type blockedForkStore struct {
	state.Store
	mu      sync.Mutex
	hold    bool
	reached chan struct{}
	release chan struct{}
}

func (s *blockedForkStore) Write(ctx context.Context, namespace string, ops ...state.Op) error {
	s.mu.Lock()
	hold := s.hold && namespace == "memory"
	if hold {
		s.hold = false
	}
	s.mu.Unlock()
	if hold {
		close(s.reached)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.release:
		}
	}
	return s.Store.Write(ctx, namespace, ops...)
}
func TestConcurrentReadersNeverObserveAPartialProjectFork(t *testing.T) {
	st := &blockedForkStore{Store: state.NewMemory(), reached: make(chan struct{}), release: make(chan struct{})}
	p := startWith(t, platform.Config{Backends: platform.Backends{State: st}})
	note := projectForkSource(t, p)
	st.mu.Lock()
	st.hold = true
	st.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		op, err := p.ForkProject(t.Context(), admin, "Field", platform.ProjectForkInput{Name: "Copy", Memory: []string{note}})
		if err == nil && op.State != "ready" {
			err = errors.New("copy did not finish")
		}
		done <- err
	}()
	select {
	case <-st.reached:
	case <-time.After(10 * time.Second):
		t.Fatal("copy did not reach staged memory")
	}
	for range 20 {
		assertUnpublishedProjectFork(t, p)
	}
	close(st.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !p.Seer(admin).Project("Copy") {
		t.Fatal("publication did not become visible")
	}
}

type publicationFault struct {
	state.Store
	fail bool
}

func (s *publicationFault) Write(ctx context.Context, namespace string, ops ...state.Op) error {
	if s.fail && namespace == "projects" {
		for _, op := range ops {
			var record struct {
				Fork *projects.ForkOrigin `json:"fork"`
			}
			if json.Unmarshal(op.Value, &record) == nil && record.Fork != nil && record.Fork.Ready {
				return statetest.ErrInjected
			}
		}
	}
	return s.Store.Write(ctx, namespace, ops...)
}
func TestProjectForkPublicationFailureKeepsEveryCopiedProjectHiddenAcrossRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := &publicationFault{Store: open(t)}
		p := startWith(t, platform.Config{Backends: platform.Backends{State: st}})
		note := projectForkSource(t, p)
		st.fail = true
		op, err := p.ForkProject(t.Context(), admin, "Field", platform.ProjectForkInput{Name: "Copy", Memory: []string{note}})
		if err != nil || op.State != "failed" {
			t.Fatalf("publication failure: %+v %v", op, err)
		}
		assertUnpublishedProjectFork(t, p)
		st.fail = false
		again, err := platform.New(platform.Config{Workspace: t.TempDir(), Backends: platform.Backends{State: st, Agents: p.Agents}})
		if err != nil {
			t.Fatal(err)
		}
		assertUnpublishedProjectFork(t, again)
		op, err = again.ForkProject(t.Context(), admin, "Field", platform.ProjectForkInput{Name: "Copy"})
		if err != nil || op.State != "ready" {
			t.Fatalf("publication retry: %+v %v", op, err)
		}
		for _, name := range []string{"Copy", "Copy-Globex", "Copy-Initech", "Copy-Renewals"} {
			if !again.Seer(admin).Project(name) {
				t.Fatalf("subtree not published together: %s", name)
			}
		}
	})
}

func TestProjectForkResumeRechecksSourceAccessAfterRevocation(t *testing.T) {
	st := &faultyIn{Store: state.NewMemory()}
	p := startWith(t, platform.Config{Backends: platform.Backends{State: st}})
	note := projectForkSource(t, p)
	st.fail("memory")
	author := operator
	author.Scopes = []auth.Permission{auth.ProjectsWrite, auth.AgentsWrite, auth.AgentsRead}
	op, err := p.ForkProject(t.Context(), author, "Field", platform.ProjectForkInput{Name: "Copy", Memory: []string{note}})
	if err != nil || op.State != "failed" {
		t.Fatalf("initial copy: %+v %v", op, err)
	}
	st.fail("")
	names := p.ProjectTree(tenant).Subtree("Field")
	for i := len(names) - 1; i >= 0; i-- {
		source, _ := p.Projects.Get(tenant, names[i])
		_, err = p.Projects.Update(tenant, source.Name, source.Version, govern, time.Now(), func(pr *projects.Project) error {
			pr.Members = []projects.Entry{{Who: "other", Role: projects.Owner}}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = p.ForkProject(t.Context(), author, "Field", platform.ProjectForkInput{Name: "Copy"})
	if !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("revoked source allowed resume: %v", err)
	}
	saved, err := p.ProjectFork(author, "Copy")
	if err != nil || saved.Source != "" || saved.State != "failed" {
		t.Fatalf("revoked source metadata exposed: %+v %v", saved, err)
	}
	if p.Seer(operator).Project("Copy") {
		t.Fatal("failed copy published after source revocation")
	}
}
