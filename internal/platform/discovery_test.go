package platform_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
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
		if _, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: agent, Model: "offline", Provider: "offline", SystemPrompt: "Work.", Description: "needle worker"}, admin.Subject); err != nil {
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

func TestDiscoverySearchesSavedContentWithoutDisclosingHiddenWork(t *testing.T) {
	p := startWith(t, platform.Config{})
	if _, err := p.CreateProject(t.Context(), tenant, govern, platform.ProjectInput{Name: "SearchLaunch", Members: []projects.Entry{{Who: projects.Everyone, Role: projects.Member}, {Who: admin.Subject, Role: projects.Owner}}}); err != nil {
		t.Fatal(err)
	}
	visible := startThread(t, p, admin, platform.ThreadInput{Project: "SearchLaunch", Text: "Ordinary agenda", Visibility: threads.Shared, PostOnly: true})
	hidden := startThread(t, p, admin, platform.ThreadInput{Project: "SearchLaunch", Text: "Private agenda", Visibility: threads.Private, PostOnly: true})
	for _, id := range []string{visible.Thread.ID, hidden.Thread.ID} {
		if _, _, err := p.Threads.Post(tenant, id, threads.Message{By: admin.Subject, Name: "Ada", Text: "Zephyr evidence in a later message"}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.PutFile(tenant, visible.Thread.ID, "ordinary.md", "Zephyr published file evidence"); err != nil {
		t.Fatal(err)
	}
	file, err := p.Publish(t.Context(), platform.Publication{Tenant: tenant, Project: "SearchLaunch", Thread: visible.Thread.ID, Path: "ordinary.md"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Knowledge.Create(tenant, "search-reference", "Reference", "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Knowledge.Add(tenant, "search-reference", "Reference page", "", "Zephyr retained knowledge evidence", "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := p.Discover(t.Context(), operator, platform.DiscoveryQuery{Q: "zephyr", Content: true})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, hit := range got {
		if hit.Thread == hidden.Thread.ID || strings.Contains(hit.Title, "Private") {
			t.Fatalf("private content leaked: %+v", hit)
		}
		if hit.Kind == "message" && (hit.Thread != visible.Thread.ID || hit.Message != "m-2" || !strings.Contains(hit.Snippet, "Zephyr")) {
			t.Fatal(hit)
		}
		if hit.Kind == "file" && hit.ID != file.ID {
			t.Fatal(hit)
		}
		kinds[hit.Kind] = true
	}
	if !kinds["message"] || !kinds["file"] || !kinds["source"] {
		t.Fatalf("missing indexed content kinds: %+v", got)
	}
	legacy, err := p.Discover(t.Context(), operator, platform.DiscoveryQuery{Q: "zephyr"})
	if err != nil || len(legacy) != 0 {
		t.Fatalf("content opt-in changed navigation search: %v %v", legacy, err)
	}
	nobody := auth.Principal{Subject: "nobody", Tenant: tenant, Kind: auth.User}
	if got, err := p.Discover(t.Context(), nobody, platform.DiscoveryQuery{Q: "zephyr", Content: true}); err != nil || len(got) != 0 {
		t.Fatalf("ungranted content: %v %v", got, err)
	}
}
