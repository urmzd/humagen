package platform_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestDiscoveryReturnsOnlyPermittedReadableWorkWithinBounds(t *testing.T) {
	p := startWith(t, platform.Config{})
	for _, name := range []string{"PublicNeedle", "SecretNeedle"} {
		members := []projects.Entry{{Who: projects.Everyone, Role: projects.Member}, {Who: admin.Subject, Role: projects.Owner}}
		if name == "SecretNeedle" {
			members = []projects.Entry{{Who: admin.Subject, Role: projects.Owner}}
		}
		if _, err := p.CreateProject(t.Context(), tenant, govern, platform.ProjectInput{Name: name, Members: members}); err != nil {
			t.Fatal(err)
		}
		agent := strings.ToLower(name)
		if _, err := p.Agents.Create(t.Context(), tenant, agentspec.AgentSpec{Name: agent, Model: "offline", Provider: "offline", SystemPrompt: "Work.", Description: "needle worker"}, admin.Subject); err != nil {
			t.Fatal(err)
		}
		if err := p.Projects.SetHome(tenant, agent, name, govern, time.Now(), nil); err != nil {
			t.Fatal(err)
		}
		startThread(t, p, admin, platform.ThreadInput{Project: name, Text: name + " conversation needle", PostOnly: true})
	}
	startThread(t, p, admin, platform.ThreadInput{Project: "PublicNeedle", Text: "private needle conversation", Visibility: threads.Private, PostOnly: true})
	got, err := p.Discover(t.Context(), operator, platform.DiscoveryQuery{Q: "needle", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, item := range got {
		if strings.Contains(strings.ToLower(item.Title), "secret") || strings.Contains(item.Title, "private") || item.Project == "SecretNeedle" {
			t.Fatalf("hidden work disclosed: %+v", item)
		}
		kinds[item.Kind] = true
	}
	if len(kinds) != 3 {
		t.Fatalf("missing native work kinds: %+v", got)
	}
	reader := auth.Principal{Subject: "nobody", Tenant: tenant, Kind: auth.User}
	if rows, err := p.Discover(t.Context(), reader, platform.DiscoveryQuery{Q: "needle"}); err != nil || len(rows) != 0 {
		t.Fatalf("no read permissions: %+v %v", rows, err)
	}
	reader.Scopes = []auth.Permission{auth.ProjectsRead}
	rows, err := p.Discover(t.Context(), reader, platform.DiscoveryQuery{Q: "needle"})
	if err != nil || len(rows) != 1 || rows[0].Kind != "project" {
		t.Fatalf("permission-specific result: %+v %v", rows, err)
	}
	secret := "SecretNeedle"
	if _, err := p.Discover(t.Context(), operator, platform.DiscoveryQuery{Project: &secret}); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("hidden scope: %v", err)
	}
	for _, query := range []platform.DiscoveryQuery{{Limit: 51}, {Q: strings.Repeat("x", 201)}, {Kinds: []string{"unknown"}}} {
		if _, err := p.Discover(t.Context(), operator, query); !errors.Is(err, projects.ErrInvalid) {
			t.Fatalf("unbounded query accepted: %+v %v", query, err)
		}
	}
	for i := range 30 {
		if _, _, err := p.Threads.Start(tenant, threads.Thread{Project: "PublicNeedle", Channel: "general", Visibility: threads.Shared, Title: fmt.Sprintf("needle %02d", i)}, threads.Message{By: operator.Subject, Text: "needle conversation"}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	rows, err = p.Discover(t.Context(), operator, platform.DiscoveryQuery{Q: "needle", Limit: 5, Kinds: []string{"conversation"}})
	if err != nil || len(rows) != 5 {
		t.Fatalf("result cap: %+v %v", rows, err)
	}
}
