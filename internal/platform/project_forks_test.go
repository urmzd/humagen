package platform_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/sourcelinks"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
)

func projectForkSource(t *testing.T, p *platform.Platform) string {
	t.Helper()
	field(t, p)
	_, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: "planner", Model: "offline", Provider: "offline", SystemPrompt: "Plan carefully."}, admin.Subject)
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
	notes := notesOf(t, p.Memory, tenant, memory.ProjectOwner("Copy"))
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
		fault.fail("drive.trees")
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
		if len(notesOf(t, again.Memory, tenant, memory.ProjectOwner("Copy"))) != 1 {
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
	_, err := p.Invoke(t.Context(), wire.NewEvent(wire.Raise{ID: "must-not-run", Tenant: tenant, Agent: "copy-planner", Origin: "api", Text: "never run", Context: map[string]string{runs.MetaProject: "Copy"}}))
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
	hold := s.hold && namespace == "drive.trees"
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
	st.fail("drive.trees")
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

func TestProjectForkArchivesDoNotShareConversationLockStripe(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		p, err := platform.New(platform.Config{Workspace: t.TempDir(), Backends: platform.Backends{State: open(t)}})
		if err != nil {
			t.Fatal(err)
		}
		projectForkSource(t, p)
		// This ID and project-fork:Copy share placement lock stripe 20.
		// An archive must not recursively acquire its conversation's stripe.
		_, _, err = p.Threads.Start(tenant,
			threads.Thread{ID: "th-0000000000000008", Project: "Field", Channel: "general", Visibility: threads.Shared},
			threads.Message{By: admin.Subject, Text: "archive without locking itself"}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		type result struct {
			view platform.ProjectForkOperation
			err  error
		}
		finished := make(chan result, 2)
		ctx := t.Context()
		for range 2 {
			go func() {
				view, err := p.ForkProject(ctx, admin, "Field", platform.ProjectForkInput{Name: "Copy", ArchiveConversations: true})
				finished <- result{view, err}
			}()
		}
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		var operation string
		for range 2 {
			select {
			case got := <-finished:
				if got.err != nil || got.view.State != "ready" || got.view.Archives != 1 {
					t.Fatalf("archive fork: %+v %v", got.view, got.err)
				}
				if operation != "" && got.view.ID != operation {
					t.Fatal("concurrent retries created different fork operations")
				}
				operation = got.view.ID
			case <-timer.C:
				t.Fatal("archive fork blocked on a colliding conversation lock stripe")
			}
		}
		rows, _, err := p.ThreadPageAs(admin, runs.ThreadQuery{Projects: []string{"Copy"}})
		if err != nil || len(rows) != 1 {
			t.Fatalf("archive rows: %+v %v", rows, err)
		}
		archive, err := p.ThreadRecord(tenant, rows[0].ID)
		if err != nil || !archive.ReadOnly || archive.Fork == nil || len(archive.Fork.Context) != 1 || archive.Fork.Context[0].Text != "archive without locking itself" {
			t.Fatalf("archive lost passive history: %+v %v", archive, err)
		}
	})
}

func TestProjectForkExplicitArchivesArePassiveConfinedAndReplayable(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := &faultyIn{Store: open(t)}
		p := startWith(t, platform.Config{Backends: platform.Backends{State: st}})
		projectForkSource(t, p)
		first := startThread(t, p, admin, platform.ThreadInput{Project: "Field", Text: "saved conversation text", PostOnly: true})
		if _, err := p.SourceLinks.Put(tenant, sourcelinks.Rule{ID: "launch-label", Provider: "github", Attribute: "label", Pattern: "original", Project: "Field", Mode: sourcelinks.Auto}); err != nil {
			t.Fatal(err)
		}
		st.fail("threads")
		failed, err := p.ForkProject(t.Context(), admin, "Field", platform.ProjectForkInput{Name: "Copy", ArchiveConversations: true})
		if err != nil || failed.State != "failed" || failed.Archives != 1 {
			t.Fatalf("archive copy failure: %+v %v", failed, err)
		}
		assertUnpublishedProjectFork(t, p)
		st.fail("")
		post(t, p, admin, first.Thread.ID, platform.MessageInput{Text: "later source message must not enter saved archive", PostOnly: true})
		if _, err := p.SourceLinks.Put(tenant, sourcelinks.Rule{ID: "launch-label", Provider: "github", Attribute: "label", Pattern: "changed", Project: "Field", Mode: sourcelinks.Auto}); err != nil {
			t.Fatal(err)
		}
		again, err := platform.New(platform.Config{Workspace: t.TempDir(), Backends: platform.Backends{State: st, Agents: p.Agents}})
		if err != nil {
			t.Fatal(err)
		}
		ready, err := again.ForkProject(t.Context(), admin, "Field", platform.ProjectForkInput{Name: "Copy"})
		if err != nil || ready.State != "ready" || ready.Archives != 1 {
			t.Fatalf("archive resume: %+v %v", ready, err)
		}
		rows, _, err := again.ThreadPageAs(admin, runs.ThreadQuery{Projects: []string{"Copy"}})
		if err != nil || len(rows) != 1 {
			t.Fatalf("archive rows: %+v %v", rows, err)
		}
		archive, err := again.ThreadRecord(tenant, rows[0].ID)
		if err != nil || !archive.ReadOnly || archive.Fork == nil || len(archive.Fork.Context) != 1 || archive.Fork.Context[0].Text != "saved conversation text" {
			t.Fatalf("archive was not immutable passive context: %+v %v", archive, err)
		}
		if _, err := again.PostMessage(t.Context(), admin, archive.ID, platform.MessageInput{Text: "@planner never execute"}); !errors.Is(err, platform.ErrForbidden) {
			t.Fatalf("archive accepted message: %v", err)
		}
		if _, _, err := again.Threads.Post(tenant, archive.ID, threads.Message{By: admin.Subject, Text: "no store bypass"}, time.Now()); !errors.Is(err, threads.ErrInvalid) {
			t.Fatalf("archive accepted direct store message: %v", err)
		}
		_, err = again.Invoke(t.Context(), wire.NewEvent(wire.Raise{ID: "archive-no-run", Tenant: tenant, Agent: "copy-planner", Origin: "api", Text: "never execute", Context: map[string]string{runs.MetaProject: "Copy", runs.MetaThread: archive.ID}}))
		if err == nil {
			t.Fatal("archive accepted direct run")
		}
		rules, _ := again.SourceLinks.List(tenant)
		for _, rule := range rules {
			if rule.Project == "Copy" && (rule.Mode != sourcelinks.Off || rule.Pattern != "original") {
				t.Fatalf("copied rule activated or read a new snapshot: %+v", rule)
			}
		}
	})
}

func TestCopiedMemoryStripsNonportableProvenanceWhenAudienceExpands(t *testing.T) {
	p := startWith(t, platform.Config{})
	projectForkSource(t, p)
	expires := time.Now().Add(time.Hour)
	note, err := p.Memory.AddDetailed(tenant, memory.ProjectOwner("Field"), "A selected confirmed decision", memory.Consolidated, memory.Approved, "r-private-original", time.Now(), memory.Details{Kind: memory.Decision, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	author := operator
	author.Scopes = []auth.Permission{auth.ProjectsWrite, auth.AgentsWrite, auth.AgentsRead}
	op, err := p.ForkProject(t.Context(), author, "Field", platform.ProjectForkInput{Name: "Copy", Memory: []string{note.ID}})
	if err != nil || op.State != "ready" {
		t.Fatalf("copy: %+v %v", op, err)
	}
	copied, _ := p.Projects.Get(tenant, "Copy")
	_, err = p.Projects.Update(tenant, "Copy", copied.Version, govern, time.Now(), func(pr *projects.Project) error {
		pr.Members = append(pr.Members, projects.Entry{Who: "peer", Role: projects.Member})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	names := p.ProjectTree(tenant).Subtree("Field")
	for i := len(names) - 1; i >= 0; i-- {
		source, _ := p.Projects.Get(tenant, names[i])
		if _, err = p.Projects.Update(tenant, source.Name, source.Version, govern, time.Now(), func(pr *projects.Project) error {
			pr.Members = []projects.Entry{{Who: "original-owner", Role: projects.Owner}}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	peer := operator
	peer.Subject = "peer"
	if !p.Seer(peer).Project("Copy") || p.Seer(peer).Project("Field") {
		t.Fatal("audience fixture does not isolate original source")
	}
	notes := notesOf(t, p.Memory, tenant, memory.ProjectOwner("Copy"))
	if len(notes) != 1 || notes[0].Kind != memory.Decision || !notes[0].ExpiresAt.Equal(expires) || notes[0].By != author.Subject || notes[0].ReviewedBy != author.Subject || notes[0].Source != memory.Person {
		t.Fatalf("copied note retained nonportable metadata: %+v", notes)
	}
	encoded, _ := json.Marshal(notes)
	if bytes.Contains(encoded, []byte("r-private-original")) {
		t.Fatal("copied provenance disclosed original private run")
	}
	copied, _ = p.Projects.Get(tenant, "Copy")
	shown := p.Seer(peer).ProjectForkOrigin(copied)
	if shown.Fork == nil || shown.Fork.Source != "" || shown.Fork.By != "" {
		t.Fatalf("lineage disclosed hidden source: %+v", shown.Fork)
	}
}
