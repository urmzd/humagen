package platform_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/call"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
	"github.com/urmzd/warp/pkg/threads"
)

// What is said in a private thread stays with its participants: the
// housekeeping run that titles it is read where the thread is, and nothing
// said there is proposed as a note anyone else would be sent.
func TestAPrivateThreadsHousekeepingStaysInIt(t *testing.T) {
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
		if _, err := p.Agents.Create(ctx, tenant, agentspec.AgentSpec{Name: name, Model: "offline", Provider: "offline", SystemPrompt: "x"}, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.SetProjectAgents(ctx, tenant, projects.RootAddress, govern, platform.AgentsInput{Enable: []string{projects.Everyone}}); err != nil {
		t.Fatal(err)
	}
	if err := p.SetSettings(ctx, tenant, platform.Settings{TitleAgent: "scribe", MemoryAgent: "scribe"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateProject(ctx, tenant, ada, platform.ProjectInput{Name: "Field"}); err != nil {
		t.Fatal(err)
	}
	posted, err := p.StartThread(ctx, operator, platform.ThreadInput{Project: "Field", Visibility: threads.Private, Text: "@lead plan the secret launch"})
	if err != nil || len(posted.Runs) != 1 {
		t.Fatalf("a private thread = %+v, %v", posted, err)
	}
	await(t, p, posted.Runs[0].ID, runs.Completed)

	// A shared thread in a members-only project: its housekeeping run is
	// read where the thread is.
	if _, err := p.CreateProject(ctx, tenant, projects.Writer{By: operator.Subject}, platform.ProjectInput{Name: "Vault", Kind: projects.Account}); err != nil {
		t.Fatal(err)
	}
	open, err := p.StartThread(ctx, operator, platform.ThreadInput{Project: "Vault", Visibility: threads.Shared, Text: "@lead plan the vault launch"})
	if err != nil || len(open.Runs) != 1 {
		t.Fatalf("a shared thread in Vault = %+v, %v", open, err)
	}
	await(t, p, open.Runs[0].ID, runs.Completed)
	var hook runs.Run
	eventually(t, "the shared thread had no housekeeping", func() bool {
		for _, r := range p.Runs.List(tenant, 50) {
			if r.Origin == runs.OriginSystem && strings.Contains(r.Text, "vault launch") {
				hook = r
				return true
			}
		}
		return false
	})
	outsider := auth.Principal{Subject: "vi", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	if p.Seer(outsider).Run(hook) || !p.Seer(operator).Run(hook) {
		t.Fatalf("a housekeeping run is read where its turn is not: outsider %v, owner %v", p.Seer(outsider).Run(hook), p.Seer(operator).Run(hook))
	}
	// So is the conversation of its own it makes, which has no record.
	if p.Seer(outsider).Conversation(hook.Thread, "") || !p.Seer(operator).Conversation(hook.Thread, "") {
		t.Fatalf("a housekeeping run's conversation is read where its turn is not")
	}
	listed := func(who auth.Principal) bool {
		page, _, err := p.ThreadPageAs(who, runs.ThreadQuery{Limit: runs.MaxThreadPage})
		if err != nil {
			t.Fatal(err)
		}
		return slices.ContainsFunc(page, func(s platform.ThreadSummary) bool { return s.ID == hook.Thread })
	}
	if listed(outsider) || !listed(operator) {
		t.Fatalf("a housekeeping run's conversation is listed where its turn is not: outsider %v, owner %v", listed(outsider), listed(operator))
	}
	// And it is counted, at the workspace where it is filed, only for those
	// it is listed for.
	shown := func(who auth.Principal) (counted, listed int) {
		page, _, err := p.ThreadPageAs(who, runs.ThreadQuery{Projects: []string{projects.Root}, Limit: runs.MaxThreadPage})
		if err != nil {
			t.Fatal(err)
		}
		return p.ProjectCounts(who)[projects.Root].Threads, len(page)
	}
	if counted, listed := shown(outsider); counted != 0 || listed != 0 {
		t.Fatalf("the workspace counts %d conversations for the outsider, who is shown %d there", counted, listed)
	}
	eventually(t, "the workspace counts the owner what they are shown there", func() bool {
		counted, listed := shown(operator)
		return counted > 0 && counted == listed
	})

	// The private thread sent nothing to be remembered.
	time.Sleep(100 * time.Millisecond)
	for _, r := range p.Runs.List(tenant, 50) {
		if r.Origin == runs.OriginSystem && strings.Contains(r.Text, "secret launch") {
			t.Fatalf("a private thread's words went to housekeeping: %+v", r)
		}
	}
	for _, owner := range []string{"lead", "project:Field"} {
		for _, n := range p.Memory.List(tenant, owner) {
			if strings.Contains(n.Text, "secret") {
				t.Fatalf("a private thread proposed a note for %s: %+v", owner, n)
			}
		}
	}
}

// Tagging a person in a private thread invites them: they read it from then
// on, and someone nobody tagged still reads nothing of it.
func TestTaggingAPersonInAPrivateThreadInvitesThem(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	seePeople(t, p, operator, admin, vic)
	got := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Visibility: threads.Private, Text: "@vic can you check the price?"})
	if !slices.Equal(got.Notified, []string{"vic"}) {
		t.Fatalf("notified %v", got.Notified)
	}
	if !p.SeesThread(vic, got.Thread.ID) {
		t.Fatal("a person tagged in a private thread cannot read it")
	}
	if p.SeesThread(viewer, got.Thread.ID) {
		t.Fatal("someone nobody tagged reads a private thread")
	}
	if _, ok := p.Inbox.ByKey(tenant, inbox.MentionKey(got.Thread.ID, "m-1", "vic")); !ok {
		t.Fatal("the tagged person was not told")
	}
}

// Posting is starting work, so it takes a member of the project, and in a
// private thread a participant: a viewer starts nothing, a project the poster
// cannot see answers as one that is not there, and reading a private thread
// with projects:govern is not taking part in it.
func TestPostingTakesAMemberAndInAPrivateThreadAParticipant(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	ctx := t.Context()
	runsIn := func(project string) int {
		n := 0
		for _, r := range p.Runs.List(tenant, 100) {
			if r.Project == project {
				n++
			}
		}
		return n
	}

	// opal is only a viewer of Globex.
	if _, err := p.StartThread(ctx, operator, platform.ThreadInput{Project: "Globex", Visibility: threads.Shared, Text: "@helper hi"}); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("a viewer starting a thread = %v, want forbidden", err)
	}
	room := startThread(t, p, admin, platform.ThreadInput{Project: "Globex", Text: "noting this", PostOnly: true})
	if _, err := p.PostMessage(ctx, operator, room.Thread.ID, platform.MessageInput{Text: "@helper hi"}); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("a viewer posting = %v, want forbidden", err)
	}
	if n := runsIn("Globex"); n != 0 {
		t.Fatalf("a viewer started %d runs in Globex", n)
	}

	// Vault is members only, and opal has no role in it.
	if _, err := p.CreateProject(ctx, tenant, projects.Writer{By: "olu"}, platform.ProjectInput{Name: "Vault", Kind: projects.Account}); err != nil {
		t.Fatal(err)
	}
	_, hidden := p.StartThread(ctx, operator, platform.ThreadInput{Project: "Vault", Visibility: threads.Shared, Text: "@helper hi from outside"})
	_, missing := p.StartThread(ctx, operator, platform.ThreadInput{Project: "Nowhere", Visibility: threads.Shared, Text: "@helper hi"})
	if !errors.Is(hidden, projects.ErrNotFound) || !errors.Is(missing, projects.ErrNotFound) || strings.ReplaceAll(hidden.Error(), "Vault", "Nowhere") != missing.Error() {
		t.Fatalf("a hidden project answers %v, a missing one %v", hidden, missing)
	}
	if n := runsIn("Vault"); n != 0 {
		t.Fatalf("someone with no role started %d runs in Vault", n)
	}

	// A private thread in Field, which grace is a member of.
	private := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Visibility: threads.Private, Text: "just us", PostOnly: true})
	if _, err := p.PostMessage(ctx, grace, private.Thread.ID, platform.MessageInput{Text: "@helper what was said?"}); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("someone not taking part posting in a private thread = %v, want not found", err)
	}
	if _, err := p.PostMessage(ctx, admin, private.Thread.ID, platform.MessageInput{Text: "@helper what was said?"}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("projects:govern posting in a private thread it takes no part in = %v, want forbidden", err)
	}
	if n := runsIn("Field"); n != 0 {
		t.Fatalf("a refused post started %d runs in Field", n)
	}
	post(t, p, operator, private.Thread.ID, platform.MessageInput{Text: "still just us", PostOnly: true})
}

// A thread's visibility, title and default, and who else takes part, are its
// owner's or its project's owners' to change: a participant cannot open a
// private thread to the project, and a viewer cannot hide a shared one or
// choose who answers in it.
func TestOnlyAThreadsOwnerOrAProjectOwnerChangesIt(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	seePeople(t, p, operator, grace, vic)
	ctx := t.Context()
	got := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Visibility: threads.Private, Text: "@grace a word?", PostOnly: true})
	th, err := p.SetParticipants(ctx, operator, got.Thread.ID, platform.ParticipantsUpdate{Version: got.Thread.Version, Add: []threads.Participant{{Kind: threads.Person, Name: "vic"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpdateThread(ctx, grace, th.ID, platform.ThreadUpdate{Version: th.Version, Visibility: threads.Shared}); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("a participant opening a private thread = %v, want forbidden", err)
	}
	if _, err := p.SetParticipants(ctx, grace, th.ID, platform.ParticipantsUpdate{Version: th.Version, Remove: []string{"vic"}}); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("a participant removing someone else = %v, want forbidden", err)
	}
	if p.SeesThread(viewer, th.ID) {
		t.Fatal("a refused change opened the thread")
	}
	// Leaving is anyone's own.
	if th, err = p.SetParticipants(ctx, grace, th.ID, platform.ParticipantsUpdate{Version: th.Version, Remove: []string{"grace"}}); err != nil {
		t.Fatalf("leaving a thread = %v", err)
	}
	machine := auth.Principal{Subject: "svc-bot", Tenant: tenant, Kind: auth.Machine, Scopes: []auth.Permission{auth.RunsRead, auth.RunsWrite}}
	shared := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Text: "out loud", PostOnly: true})
	if _, err := p.UpdateThread(ctx, machine, shared.Thread.ID, platform.ThreadUpdate{Version: shared.Thread.Version, Title: "taken"}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("a credential retitling a thread = %v, want forbidden", err)
	}
	// The thread's owner opens it; an owner of the project retitles it.
	if th, err = p.UpdateThread(ctx, operator, th.ID, platform.ThreadUpdate{Version: th.Version, Visibility: threads.Shared}); err != nil || th.Visibility != threads.Shared {
		t.Fatalf("the owner opening their thread = %+v, %v", th, err)
	}
	if th, err = p.UpdateThread(ctx, admin, th.ID, platform.ThreadUpdate{Version: th.Version, Title: "Pricing", Visibility: threads.Shared}); err != nil || th.Title != "Pricing" {
		t.Fatalf("a project owner retitling = %+v, %v", th, err)
	}

	// opal is only a viewer of Globex.
	room := startThread(t, p, admin, platform.ThreadInput{Project: "Globex", Text: "noting this", PostOnly: true})
	if _, err := p.UpdateThread(ctx, operator, room.Thread.ID, platform.ThreadUpdate{Version: room.Thread.Version, Visibility: threads.Private, Default: "helper"}); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("a viewer hiding a shared thread = %v, want forbidden", err)
	}
	if _, err := p.SetParticipants(ctx, operator, room.Thread.ID, platform.ParticipantsUpdate{Version: room.Thread.Version, Add: []threads.Participant{{Kind: threads.Agent, Name: "helper"}}}); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("a viewer adding to a thread = %v, want forbidden", err)
	}
	if cur, _ := p.ThreadRecord(tenant, room.Thread.ID); cur.Visibility != threads.Shared || cur.Default != "" {
		t.Fatalf("a refused change changed %+v", cur)
	}
}

// A reader's counts are decided as of one moment. A conversation opened
// while they are read, a housekeeping run's or a private thread's, is not
// counted until a later read has decided who reads it, so no count ever
// tells a reader of a conversation they may not read.
func TestCountsNeverTellOfAConversationOpenedWhileTheyAreRead(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: operator.Subject}, platform.ProjectInput{Name: "Vault", Kind: projects.Account}); err != nil {
		t.Fatal(err)
	}
	outsider := auth.Principal{Subject: "vi", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	if seer := p.Seer(outsider); !seer.Project("Field") || seer.Project("Vault") {
		t.Fatalf("the outsider reads Field %v and Vault %v", seer.Project("Field"), seer.Project("Vault"))
	}
	const each = 150
	var wg sync.WaitGroup
	// Housekeeping for a turn in Vault, which opens a conversation of its
	// own at the workspace.
	wg.Go(func() {
		for i := range each {
			id := fmt.Sprintf("sys-title-%d", i)
			if err := p.Runs.Record(runtime.RunID(tenant, id), platform.HousekeepingTag(tenant, id, "helper", platform.Place{Project: "Vault", Thread: "th-0123456789abcdef"}), 1); err != nil {
				t.Error(err)
				return
			}
		}
	})
	// Private threads in Field the outsider takes no part in.
	wg.Go(func() {
		for i := range each {
			th, _, err := p.Threads.Start(tenant, threads.Thread{Project: "Field", Channel: projects.General, Visibility: threads.Private}, threads.Message{By: operator.Subject, Text: "plan the launch"}, time.Now())
			if err != nil {
				t.Error(err)
				return
			}
			id := fmt.Sprintf("api-private-%d", i)
			tg := envelope.Tag{ID: id, Tenant: tenant, Agent: "helper", Origin: runs.OriginAPI, Text: "plan the launch", At: time.Now(), Meta: map[string]string{runs.MetaThread: th.ID, runs.MetaProject: "Field"}}
			if err := p.Runs.Record(runtime.RunID(tenant, id), tg, 1); err != nil {
				t.Error(err)
				return
			}
		}
	})
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	for reading := true; reading; {
		select {
		case <-done:
			reading = false
		default:
		}
		counts := p.ProjectCounts(outsider)
		if n := counts[projects.Root].Threads + counts["Field"].Threads; n != 0 {
			t.Fatalf("the outsider is counted %d conversations they may not read: the workspace %+v, Field %+v", n, counts[projects.Root], counts["Field"])
		}
	}
	// A later read counts each conversation for those who read it.
	counts := p.ProjectCounts(operator)
	if counts[projects.Root].Threads != each || counts["Field"].Threads != each {
		t.Fatalf("the operator is counted %d conversations at the workspace and %d in Field, not %d each", counts[projects.Root].Threads, counts["Field"].Threads, each)
	}
}
