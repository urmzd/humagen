package platform_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/agentspec"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
)

var (
	admin  = auth.Principal{Subject: "ada", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	viewer = auth.Principal{Subject: "vi", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Viewer}}
	// An operator may start runs and acknowledge alerts, not edit agents.
	operator = auth.Principal{Subject: "opal", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
)

// proposals runs one turn with a memory agent and waits for what it proposed
// to reach the inbox.
func proposals(t *testing.T) (*platform.Platform, inbox.Message) {
	t.Helper()
	p := withMemoryAgent(t)
	return p, proposed(t, p, "api-1", "")
}

// withMemoryAgent runs a platform with the agent lead and the memory agent
// scribe, whose proposals wait for a person.
func withMemoryAgent(t *testing.T) *platform.Platform {
	t.Helper()
	p, err := platform.New(platform.Config{Workspace: t.TempDir(), OfflineHooks: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("platform shutdown: %v", err)
		}
	})
	for _, name := range []string{"lead", "scribe"} {
		spec := agentspec.AgentSpec{Name: name, Model: "offline", Provider: "offline", SystemPrompt: "x"}
		if _, err := p.Agents.Create(ctx, tenant, spec, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.SetSettings(ctx, tenant, platform.Settings{MemoryAgent: "scribe"}); err != nil {
		t.Fatal(err)
	}
	return p
}

// proposed runs one turn filed in a project and waits for the message about
// what the memory agent proposed after it.
func proposed(t *testing.T, p *platform.Platform, id, project string) inbox.Message {
	t.Helper()
	first, err := p.Converse(t.Context(), tenant, platform.Turn{ID: id, Agent: "lead", Text: "Plan the launch", Project: project})
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, first.ID, runs.Completed)
	deadline := time.Now().Add(10 * time.Second)
	for {
		page, _, err := p.InboxPage(admin, inbox.Query{Kind: inbox.KindMemory})
		if err != nil {
			t.Fatal(err)
		}
		// A turn at the root teaches its agent, and one in a project the
		// project.
		owner := "lead"
		if project != "" {
			owner = memory.ProjectOwner(project)
		}
		for _, m := range page {
			if m.Parts[0].Memory != nil && m.Parts[0].Memory.Owner == owner {
				return m
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no memory message reached the inbox: %+v", page)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// What the memory agent proposes is one message. Keeping some of it in the
// inbox approves those and discards the rest, and needs the right to edit the
// agent, not just to read the inbox.
func TestInboxDecidesProposedMemory(t *testing.T) {
	p, msg := proposals(t)
	ctx := t.Context()
	part := msg.Parts[0]
	if part.Type != inbox.PartMemory || part.Memory.Agent != "lead" || len(part.Memory.Notes) == 0 || len(part.Actions) != 2 {
		t.Fatalf("memory part = %+v", part)
	}
	keep := part.Memory.Notes[0].ID

	for name, who := range map[string]auth.Principal{"a viewer": viewer, "an operator": operator} {
		seen, err := p.InboxGet(who, msg.ID)
		if err != nil || len(seen.Parts[0].Actions) != 0 {
			t.Fatalf("%s is offered %v (%v)", name, seen.Parts[0].Actions, err)
		}
		if _, err := p.InboxResolve(ctx, who, msg.ID, part.ID, platform.Resolution{Action: platform.ActKeep, IDs: []string{keep}}); !errors.Is(err, platform.ErrForbidden) {
			t.Fatalf("%s keeping a memory = %v, want forbidden", name, err)
		}
		if _, err := p.InboxDismiss(who, msg.ID); !errors.Is(err, platform.ErrForbidden) {
			t.Fatalf("%s dismissing = %v, want forbidden", name, err)
		}
	}
	for name, r := range map[string]platform.Resolution{
		"keeping nothing":             {Action: platform.ActKeep},
		"keeping a note from nowhere": {Action: platform.ActKeep, IDs: []string{"n-404"}},
		"retrying a memory":           {Action: platform.ActRetry},
	} {
		if _, err := p.InboxResolve(ctx, admin, msg.ID, part.ID, r); !errors.Is(err, platform.ErrAction) {
			t.Fatalf("%s = %v, want a refused action", name, err)
		}
	}
	for _, n := range p.Memory.List(tenant, "lead") {
		if n.Status != memory.Pending {
			t.Fatalf("a refused attempt changed a note: %+v", n)
		}
	}

	done, err := p.InboxResolve(ctx, admin, msg.ID, part.ID, platform.Resolution{Action: platform.ActKeep, IDs: []string{keep}})
	if err != nil || done.State != inbox.Resolved || done.Parts[0].Outcome.By != "ada" || len(done.Parts[0].Actions) != 0 {
		t.Fatalf("keep = %+v, %v", done, err)
	}
	notes := p.Memory.List(tenant, "lead")
	if len(notes) != 1 || notes[0].ID != keep || notes[0].Status != memory.Approved {
		t.Fatalf("after keeping one, the agent holds %+v", notes)
	}

	// A second decision does nothing and answers with the first.
	again, err := p.InboxResolve(ctx, admin, msg.ID, part.ID, platform.Resolution{Action: platform.ActDiscard})
	if err != nil || again.Parts[0].Outcome.Action != platform.ActKeep {
		t.Fatalf("a second decision = %+v, %v", again.Parts[0].Outcome, err)
	}
	if got := p.Memory.List(tenant, "lead"); len(got) != 1 {
		t.Fatalf("a second decision changed the notes: %+v", got)
	}
}

// A proposal for a project's notes is the project's to decide, as its own
// memory routes ask: projects:write and owning the project, and
// projects:govern as well for a company project's and the workspace's, which
// every agent in a wide part of the tree is sent. The message reaches the
// project's owners only, and agents:write is not a way around either rule.
func TestProposalsForAProjectsNotesNeedWhatItsRoutesNeed(t *testing.T) {
	p := withMemoryAgent(t)
	ctx := t.Context()
	manager := auth.Principal{Subject: "mo", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Manager}}
	outsider := auth.Principal{Subject: "max", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Manager}}
	if _, err := p.CreateProject(ctx, tenant, govern, platform.ProjectInput{
		Name: "News", Kind: projects.Company, Members: []projects.Entry{{Who: "*", Role: projects.Member}, {Who: "mo", Role: projects.Owner}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateProject(ctx, tenant, ada, platform.ProjectInput{
		Name: "Launch", Members: []projects.Entry{{Who: "*", Role: projects.Member}, {Who: "opal", Role: projects.Owner}},
	}); err != nil {
		t.Fatal(err)
	}
	news, launch := proposed(t, p, "api-news", "News"), proposed(t, p, "api-launch", "Launch")
	pending := func(owner string) int {
		n := 0
		for _, note := range p.Memory.List(tenant, owner) {
			if note.Status == memory.Pending {
				n++
			}
		}
		return n
	}

	// A company project's: a manager who owns it holds agents:write and
	// projects:write, not govern, and is offered nothing. One who does not
	// own it is not sent it.
	part := news.Parts[0]
	seen, err := p.InboxGet(manager, news.ID)
	if err != nil {
		t.Fatalf("News's owner reading its proposals = %v", err)
	}
	if len(seen.Parts[0].Actions) != 0 {
		t.Fatalf("a manager is offered %v on a company project's proposals", seen.Parts[0].Actions)
	}
	for _, action := range []string{platform.ActKeep, platform.ActDiscard} {
		r := platform.Resolution{Action: action, IDs: []string{part.Memory.Notes[0].ID}}
		if _, err := p.InboxResolve(ctx, manager, news.ID, part.ID, r); !errors.Is(err, projects.ErrForbidden) {
			t.Fatalf("a manager's %s on a company project's proposals = %v, want forbidden", action, err)
		}
	}
	if _, err := p.InboxGet(outsider, news.ID); !errors.Is(err, inbox.ErrNotFound) {
		t.Fatalf("a manager who owns nothing reading News's proposals = %v, want not found", err)
	}
	if n := pending(memory.ProjectOwner("News")); n != len(part.Memory.Notes) {
		t.Fatalf("a refused decision changed News's proposals: %d pending", n)
	}
	if seen, _ := p.InboxGet(admin, news.ID); len(seen.Parts) == 0 || len(seen.Parts[0].Actions) != 2 {
		t.Fatalf("an admin is offered %+v", seen.Parts)
	}
	if _, err := p.InboxResolve(ctx, admin, news.ID, part.ID, platform.Resolution{Action: platform.ActKeep, IDs: []string{part.Memory.Notes[0].ID}}); err != nil {
		t.Fatal(err)
	}

	// A team project's: projects:write and owning it, without agents:write.
	part = launch.Parts[0]
	if _, err := p.InboxGet(viewer, launch.ID); !errors.Is(err, inbox.ErrNotFound) {
		t.Fatalf("a viewer reading Launch's proposals = %v, want not found", err)
	}
	if seen, err := p.InboxGet(operator, launch.ID); err != nil || len(seen.Parts[0].Actions) != 2 {
		t.Fatalf("Launch's owner, an operator, is offered %+v (%v)", seen.Parts, err)
	}
	if _, err := p.InboxResolve(ctx, operator, launch.ID, part.ID, platform.Resolution{Action: platform.ActDiscard}); err != nil {
		t.Fatalf("an operator discarding a team project's proposals = %v", err)
	}
	if n := len(p.Memory.List(tenant, memory.ProjectOwner("Launch"))); n != 0 {
		t.Fatalf("%d of Launch's proposals outlived the discard", n)
	}

	// The workspace's own: govern again.
	note, err := p.Memory.Add(tenant, memory.ProjectOwner(""), "Everyone ships on Tuesdays.", memory.Consolidated, memory.Pending, "run-x", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	root, _, err := p.Inbox.Raise(tenant, "memory:root", inbox.Message{
		Kind: inbox.KindMemory, Subject: "1 proposed memory for the workspace", Severity: inbox.Info,
		Parts: []inbox.Part{{Type: inbox.PartMemory, Memory: &inbox.MemoryPart{Owner: memory.ProjectOwner(""), Notes: []inbox.NoteRef{{ID: note.ID, Text: note.Text}}}}},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.InboxResolve(ctx, manager, root.ID, root.Parts[0].ID, platform.Resolution{Action: platform.ActKeep, IDs: []string{note.ID}}); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("a manager keeping the workspace's proposal = %v, want forbidden", err)
	}
	if _, err := p.InboxResolve(ctx, admin, root.ID, root.Parts[0].ID, platform.Resolution{Action: platform.ActKeep, IDs: []string{note.ID}}); err != nil {
		t.Fatal(err)
	}
	if got := p.Memory.List(tenant, memory.ProjectOwner("")); len(got) != 1 || got[0].Status != memory.Approved {
		t.Fatalf("the workspace's notes = %+v", got)
	}
}

// With the workspace keeping what the memory agent writes without asking, an
// agent's notes are kept as they come, and a team project's when the turn
// was one of its owners' (KeepsNotes, which the API sets from
// MayChangeNotes). A member's turn only proposes, and a company project's
// notes still wait for someone holding projects:govern: the turn that taught
// them needed only runs:write, and agents:write turns the setting on.
func TestAutoApproveStillAsksGovernForACompanyProjectsNotes(t *testing.T) {
	p := withMemoryAgent(t)
	ctx := t.Context()
	if err := p.SetSettings(ctx, tenant, platform.Settings{MemoryAgent: "scribe", MemoryAutoApprove: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateProject(ctx, tenant, govern, platform.ProjectInput{
		Name: "News", Kind: projects.Company, Members: []projects.Entry{{Who: "*", Role: projects.Member}, {Who: "mo", Role: projects.Owner}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateProject(ctx, tenant, ada, platform.ProjectInput{Name: "Launch"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		id, project, owner string
		keeps              bool
	}{
		{"api-launch", "Launch", memory.ProjectOwner("Launch"), true},
		{"api-root", "", "lead", false},
	} {
		first, err := p.Converse(ctx, tenant, platform.Turn{ID: c.id, Agent: "lead", Text: "Plan the launch", Project: c.project, KeepsNotes: c.keeps})
		if err != nil {
			t.Fatal(err)
		}
		await(t, p, first.ID, runs.Completed)
		within(t, c.owner+"'s notes are written", func() bool { return len(p.Memory.List(tenant, c.owner)) > 0 })
		for _, n := range p.Memory.List(tenant, c.owner) {
			if n.Status != memory.Approved {
				t.Fatalf("with approval off, %s's note waits: %+v", c.owner, n)
			}
		}
	}

	// A member's turn in a team project teaches it only proposals, for its
	// owners.
	if _, err := p.CreateProject(ctx, tenant, ada, platform.ProjectInput{Name: "Ops"}); err != nil {
		t.Fatal(err)
	}
	ops := proposed(t, p, "api-ops", "Ops")
	if !slices.Equal(ops.Audience, []string{inbox.OwnersAudience("Ops")}) {
		t.Fatalf("a member's proposals for Ops are addressed to %v", ops.Audience)
	}
	for _, n := range p.Memory.List(tenant, memory.ProjectOwner("Ops")) {
		if n.Status != memory.Pending {
			t.Fatalf("a member's turn kept a note of Ops unasked: %+v", n)
		}
	}

	msg := proposed(t, p, "api-news", "News")
	notes := p.Memory.List(tenant, memory.ProjectOwner("News"))
	if len(notes) == 0 || len(notes) != len(msg.Parts[0].Memory.Notes) {
		t.Fatalf("News holds %+v, and the inbox proposes %+v", notes, msg.Parts[0].Memory.Notes)
	}
	for _, n := range notes {
		if n.Status != memory.Pending {
			t.Fatalf("a company project's note was kept unasked: %+v", n)
		}
	}
	manager := auth.Principal{Subject: "mo", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Manager}}
	if seen, err := p.InboxGet(manager, msg.ID); err != nil || len(seen.Parts[0].Actions) != 0 {
		t.Fatalf("News's owner, a manager, is offered %+v on its proposals (%v)", seen.Parts, err)
	}
}

// Proposals dealt with on the agent's own page close their message.
func TestMemoryDecidedElsewhereClosesItsMessage(t *testing.T) {
	p, msg := proposals(t)
	notes := msg.Parts[0].Memory.Notes
	if _, err := p.ApproveMemory(tenant, "lead", []string{notes[0].ID}); err != nil {
		t.Fatal(err)
	}
	if len(notes) > 1 {
		if got, _ := p.InboxGet(admin, msg.ID); got.State != inbox.Open {
			t.Fatalf("the message closed with proposals still waiting: %s", got.State)
		}
		for _, n := range notes[1:] {
			if err := p.RemoveMemory(tenant, "lead", n.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	got, err := p.InboxGet(admin, msg.ID)
	if err != nil || got.State != inbox.Dismissed {
		t.Fatalf("after every proposal was dealt with the message is %s (%v)", got.State, err)
	}
	if open := p.InboxCount(admin).Open; open != 0 {
		t.Fatalf("%d still open", open)
	}
}

// A failed run is one message whether or not a rule was watching for it, and
// an alert and its message are settled together from either side.
// refusing is a model that fails every call at once.
type refusing struct{ saigetypes.Provider }

// spoken reports whether an operator's inbox holds a message about a run.
func spoken(p *platform.Platform, run string) bool {
	page, _, _ := p.InboxPage(operator, inbox.Query{})
	for _, m := range page {
		for _, part := range m.Parts {
			if m.Source.Run == run || (part.Failure != nil && part.Failure.Run == run) {
				return true
			}
		}
	}
	return false
}

func (refusing) Stream(context.Context, saigetypes.Request) (<-chan saigetypes.Delta, error) {
	return nil, errors.New("the model is unavailable")
}

func TestInboxHoldsFailures(t *testing.T) {
	// A run fails only after every attempt has. A real provider that cannot
	// be reached is retried inside each attempt as well, which made three
	// failing runs take seconds each and, under load, longer than the test
	// waited. The test is about the inbox: the model refuses at once, and
	// the attempts are not spaced out.
	p := startWith(t, platform.Config{
		Backoff: func(int) time.Duration { return time.Millisecond },
		AgentOptions: []agentrt.Option{agentrt.WithProvider(agentrt.ProviderOffline, func(agentrt.Spec) (saigetypes.Provider, error) {
			return refusing{}, nil
		})},
	})
	ctx := t.Context()
	broken := agentspec.AgentSpec{Name: "broken", Model: "offline", Provider: "offline", SystemPrompt: "x"}
	if _, err := p.Agents.Create(ctx, tenant, broken, "test"); err != nil {
		t.Fatal(err)
	}
	fail := func(id string) runs.Run {
		t.Helper()
		r, err := p.Invoke(ctx, tag(id, "broken"))
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(60 * time.Second)
		for {
			got, _ := p.Runs.Get(tenant, r.ID)
			// A run is failed before its ending is dealt with, so the
			// message about it is waited for as well. Every message here
			// reaches an operator.
			if got.Status == runs.Failed && spoken(p, got.ID) {
				return got
			}
			if time.Now().After(deadline) {
				t.Fatalf("run is %q, and spoken of in the inbox: %v", got.Status, spoken(p, got.ID))
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	// With no rule watching, the failure speaks for itself.
	bare := fail("run-1")
	page, _, _ := p.InboxPage(admin, inbox.Query{})
	if len(page) != 1 || page[0].Kind != inbox.KindFailure || page[0].Source.Run != bare.ID || page[0].Parts[0].Failure.Error == "" {
		t.Fatalf("inbox = %+v", page)
	}
	if _, err := p.InboxResolve(ctx, viewer, page[0].ID, "p1", platform.Resolution{Action: platform.ActAcknowledge}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("a viewer acknowledging a failure = %v", err)
	}
	if got, err := p.InboxResolve(ctx, operator, page[0].ID, "p1", platform.Resolution{Action: platform.ActAcknowledge}); err != nil || got.State != inbox.Resolved {
		t.Fatalf("acknowledge = %s, %v", got.State, err)
	}

	// With a rule, the alert is the message and the failure is a part of it.
	if err := p.Alerts.AddRule(tenant, alerts.Rule{Name: "any", Kind: alerts.RunFailed, Severity: alerts.Critical}); err != nil {
		t.Fatal(err)
	}
	fail("run-2")
	page, _, _ = p.InboxPage(admin, inbox.Query{})
	if len(page) != 1 || page[0].Kind != inbox.KindAlert || len(page[0].Parts) != 2 || page[0].From.Name != "any" {
		t.Fatalf("with a rule the inbox is %+v", page)
	}
	raised := p.Alerts.Alerts(tenant)
	if len(raised) != 1 || !raised[0].Open() {
		t.Fatalf("alerts = %+v", raised)
	}
	// Acknowledged from the alert's side: the message is resolved, by the
	// same person, and the alert is acknowledged once.
	if _, err := p.AckAlert(tenant, raised[0].ID, "opal"); err != nil {
		t.Fatal(err)
	}
	got, _ := p.InboxGet(admin, page[0].ID)
	if got.State != inbox.Resolved || got.Parts[0].Outcome.By != "opal" || got.Parts[1].Outcome.By != "opal" {
		t.Fatalf("after the alert was acknowledged its message is %+v", got)
	}

	// And from the inbox's side: dismissing the message acknowledges the alert.
	fail("run-3")
	page, _, _ = p.InboxPage(admin, inbox.Query{})
	if len(page) != 1 {
		t.Fatalf("inbox = %+v", page)
	}
	if got, err := p.InboxDismiss(operator, page[0].ID); err != nil || got.State != inbox.Resolved {
		t.Fatalf("dismiss = %s, %v", got.State, err)
	}
	for _, a := range p.Alerts.Alerts(tenant) {
		if a.Open() {
			t.Fatalf("an alert is open with its message settled: %+v", a)
		}
		if a.RunID == "run-3" && a.AckedBy != "opal" {
			t.Fatalf("the alert was acknowledged by %q", a.AckedBy)
		}
	}
	if open := p.InboxCount(admin).Open; open != 0 {
		t.Fatalf("%d still open", open)
	}
	// A rule that names whom to notify addresses its message to them alone,
	// and still only to people who may read alerts.
	if err := p.Alerts.RemoveRule(tenant, "any"); err != nil {
		t.Fatal(err)
	}
	if err := p.Alerts.AddRule(tenant, alerts.Rule{
		Name: "ops", Kind: alerts.RunFailed, Severity: alerts.Critical, Notify: []string{"role:operator", "vi", "nell"},
	}); err != nil {
		t.Fatal(err)
	}
	fourth := fail("run-4")
	nobody := auth.Principal{Subject: "nell", Tenant: tenant, Kind: auth.User}
	for _, c := range []struct {
		who  auth.Principal
		sees int
	}{{operator, 1}, {viewer, 1}, {admin, 0}, {nobody, 0}} {
		if page, _, _ := p.InboxPage(c.who, inbox.Query{}); len(page) != c.sees {
			t.Fatalf("%s sees %d messages, want %d: %+v", c.who.Subject, len(page), c.sees, page)
		}
	}
	page, _, _ = p.InboxPage(operator, inbox.Query{})
	if _, err := p.InboxGet(admin, page[0].ID); !errors.Is(err, inbox.ErrNotFound) {
		t.Fatalf("someone not notified read the message: %v", err)
	}
	// Anyone who may acknowledge alerts can still acknowledge it from the
	// alert's side, and its message is settled though they were not notified.
	var opsAlert alerts.Alert
	for _, a := range p.Alerts.Alerts(tenant) {
		if a.RunID == fourth.ID {
			opsAlert = a
		}
	}
	if _, err := p.AckAlert(tenant, opsAlert.ID, admin.Subject); err != nil {
		t.Fatal(err)
	}
	if got, _ := p.InboxGet(operator, page[0].ID); got.State != inbox.Resolved || got.Parts[0].Outcome.By != admin.Subject {
		t.Fatalf("after an ack by someone not notified the message is %+v", got)
	}
}

// A proposal for the notes of an agent that lives in a project is decided as
// the agent's own memory routes decide it: agents:write, and ownership of its
// home as well, so the inbox is no way around the home's owners.
func TestKeepingAHomedAgentsNotesNeedsItsHome(t *testing.T) {
	p, msg := proposals(t)
	ctx := t.Context()
	part := msg.Parts[0]
	if _, err := p.CreateProject(ctx, tenant, projects.Writer{By: "ada"}, platform.ProjectInput{
		Name: "Field", Members: []projects.Entry{{Who: "*", Role: projects.Member}, {Who: "own", Role: projects.Owner}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.SetAgentHome(ctx, tenant, "lead", "Field", projects.Writer{By: "ada"}); err != nil {
		t.Fatal(err)
	}
	manager := auth.Principal{Subject: "mo", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Manager}}
	owner := auth.Principal{Subject: "own", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Manager}}
	keep := platform.Resolution{Action: platform.ActKeep, IDs: []string{part.Memory.Notes[0].ID}}

	seen, err := p.InboxGet(manager, msg.ID)
	if err != nil || len(seen.Parts[0].Actions) != 0 {
		t.Fatalf("a manager who does not own Field is offered %v (%v)", seen.Parts[0].Actions, err)
	}
	if _, err := p.InboxResolve(ctx, manager, msg.ID, part.ID, keep); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("a manager who does not own Field keeping = %v, want forbidden", err)
	}
	if seen, err := p.InboxGet(owner, msg.ID); err != nil || len(seen.Parts[0].Actions) != 2 {
		t.Fatalf("Field's owner is offered %v (%v)", seen.Parts[0].Actions, err)
	}
	if done, err := p.InboxResolve(ctx, owner, msg.ID, part.ID, keep); err != nil || done.Parts[0].Outcome.By != "own" {
		t.Fatalf("Field's owner keeping = %+v, %v", done, err)
	}
}

// Retrying a failed turn starts it again, so it takes what starting one there
// takes: being a member of the project it is filed in, whoever asked it
// first. A viewer of the project reads the failure and may acknowledge it,
// but is not offered Retry and is refused it; a member's retry starts the
// turn again in the project.
func TestRetryingAFailedTurnInAProjectTakesAMember(t *testing.T) {
	p := startWith(t, platform.Config{
		Backoff: func(int) time.Duration { return time.Millisecond },
		AgentOptions: []agentrt.Option{agentrt.WithProvider(agentrt.ProviderOffline, func(agentrt.Spec) (saigetypes.Provider, error) {
			return refusing{}, nil
		})},
	})
	ctx := t.Context()
	broken := agentspec.AgentSpec{Name: "broken", Model: "offline", Provider: "offline", SystemPrompt: "x"}
	if _, err := p.Agents.Create(ctx, tenant, broken, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateProject(ctx, tenant, ada, platform.ProjectInput{
		Name: "Globex", Kind: projects.Account,
		Members: []projects.Entry{{Who: "*", Role: projects.Viewer}, {Who: "ada", Role: projects.Owner}, {Who: "mel", Role: projects.Member}},
	}); err != nil {
		t.Fatal(err)
	}
	first, err := p.Converse(ctx, tenant, platform.Turn{ID: "api-globex", Agent: "broken", Text: "Plan the renewal", Project: "Globex"})
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, first.ID, runs.Failed)
	var msg inbox.Message
	within(t, "the failure reached the inbox", func() bool {
		page, _, _ := p.InboxPage(admin, inbox.Query{Kind: inbox.KindFailure})
		for _, m := range page {
			if m.Source.Run == first.ID {
				msg = m
				return true
			}
		}
		return false
	})
	part := msg.Parts[0]
	retry := platform.Resolution{Action: platform.ActRetry}

	// opal is an operator, so holds runs:write, and only a viewer of Globex.
	seen, err := p.InboxGet(operator, msg.ID)
	if err != nil || !slices.Equal(seen.Parts[0].Actions, []string{platform.ActAcknowledge}) {
		t.Fatalf("a viewer of Globex is offered %+v (%v)", seen.Parts, err)
	}
	if _, err := p.InboxResolve(ctx, operator, msg.ID, part.ID, retry); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("a viewer of Globex retrying = %v, want forbidden", err)
	}
	for _, r := range p.Runs.List(tenant, 10) {
		if r.ID != first.ID {
			t.Fatalf("a refused retry started %+v", r)
		}
	}

	member := auth.Principal{Subject: "mel", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	if seen, err := p.InboxGet(member, msg.ID); err != nil || !slices.Equal(seen.Parts[0].Actions, []string{platform.ActRetry, platform.ActAcknowledge}) {
		t.Fatalf("a member of Globex is offered %+v (%v)", seen.Parts, err)
	}
	if _, err := p.InboxResolve(ctx, member, msg.ID, part.ID, retry); err != nil {
		t.Fatalf("a member of Globex retrying = %v", err)
	}
	var again []runs.Run
	for _, r := range p.Runs.List(tenant, 10) {
		if r.ID != first.ID {
			again = append(again, r)
		}
	}
	if len(again) != 1 || again[0].Project != "Globex" || again[0].Actor.ID != "mel" {
		t.Fatalf("the retry is %+v, want one run filed in Globex for mel", again)
	}
}
