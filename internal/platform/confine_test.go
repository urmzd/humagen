package platform_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentspec"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
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
