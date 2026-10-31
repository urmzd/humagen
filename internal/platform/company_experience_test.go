package platform_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestCompanyWelcomeRetainsOneAnnouncementAcrossReadersAndRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		p, err := newOn(t, st)
		if err != nil {
			t.Fatal(err)
		}
		first, err := p.PostWelcome(tenant)
		if err != nil {
			t.Fatal(err)
		}
		if first.Source.Project != "" || first.From.Kind != "system" {
			t.Fatal("welcome is not a Company system announcement")
		}
		if _, err := p.Inbox.Read(tenant, first.ID, "ada", true, time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, _, err := p.Inbox.Dismiss(tenant, first.ID, "ada", time.Now()); err != nil {
			t.Fatal(err)
		}
		again, err := newOn(t, st)
		if err != nil {
			t.Fatal(err)
		}
		kept, err := again.PostWelcome(tenant)
		if err != nil || kept.ID != first.ID {
			t.Fatalf("welcome reposted on restart: %v %v", kept, err)
		}
		ada := auth.Principal{Tenant: tenant, Subject: "ada", Kind: auth.User, Roles: []auth.Role{auth.Viewer}}
		ben := ada
		ben.Subject = "ben"
		if inboxCount(t, again, ada).Updates != 0 || inboxCount(t, again, ben).Updates != 1 {
			t.Fatal("welcome did not keep independent reader state")
		}
	})
}

func TestFindHelpNeverRanksHiddenProjectsOrAgents(t *testing.T) {
	p, err := newOn(t, state.NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	for _, pr := range []projects.Project{{Name: "Field", Description: "Renewal work"}, {Name: "HiddenCanary", Members: []projects.Entry{{Who: "ada", Role: projects.Owner}}}} {
		if _, err := p.Projects.Create(tenant, pr, projects.Writer{By: "ada"}, at); err != nil {
			t.Fatal(err)
		}
	}
	for _, agent := range []wire.AgentSpec{{Name: "helper", Description: "Renewal billing", Model: "offline", Provider: "offline", SystemPrompt: "Help."}, {Name: "private-canary", Description: "HiddenCanary renewal", Model: "offline", Provider: "offline", SystemPrompt: "Private."}} {
		if _, err := p.Agents.Create(t.Context(), tenant, agent, "ada"); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Projects.SetHome(tenant, "private-canary", "HiddenCanary", projects.Writer{By: "ada"}, at, nil); err != nil {
		t.Fatal(err)
	}
	ben := auth.Principal{Tenant: tenant, Subject: "ben", Kind: auth.User, Roles: []auth.Role{auth.Viewer}}
	suggestions, err := p.FindHelp(t.Context(), ben, "renewal")
	if err != nil || len(suggestions) == 0 {
		t.Fatalf("visible work not found: %v %v", suggestions, err)
	}
	raw, _ := json.Marshal(suggestions)
	if strings.Contains(string(raw), "Canary") || strings.Contains(string(raw), "canary") {
		t.Fatalf("hidden candidate contributed to ranking: %s", raw)
	}
	before := len(p.Runs.List(tenant, 0))
	if _, err := p.FindHelp(t.Context(), ben, "HiddenCanary"); err != nil {
		t.Fatal(err)
	}
	if len(p.Runs.List(tenant, 0)) != before {
		t.Fatal("discovery started work")
	}
	restricted := auth.Principal{Tenant: tenant, Subject: "service", Kind: auth.Machine, Scopes: []auth.Permission{auth.ProjectsRead}}
	if found, err := p.FindHelp(t.Context(), restricted, "renewal"); err != nil || len(found) != 0 {
		t.Fatal("discovery exposed agents without agents:read")
	}
}

type retainedPrivateContextGuard struct{}

func (retainedPrivateContextGuard) ConversationOperationsSettled(string, string) bool { return false }

func TestZeroRunPrivateContextGuardRequiresReviewedCopy(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		p, err := newOn(t, open(t))
		if err != nil {
			t.Fatal(err)
		}
		who := auth.Principal{Tenant: tenant, Subject: "ada", Kind: auth.User, Roles: []auth.Role{auth.Operator}}
		personal, err := p.PersonalProject(who)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Projects.Create(tenant, projects.Project{Name: "Field"}, projects.Writer{By: "ada"}, time.Now()); err != nil {
			t.Fatal(err)
		}
		source, _, err := p.Threads.Start(tenant, threads.Thread{Project: personal.Name, Channel: "general", Visibility: threads.Private}, threads.Message{By: "ada", Name: "Ada", Text: "PRIVATE-PASSIVE-CONTEXT"}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err := p.RegisterPublicationGuard(retainedPrivateContextGuard{}); err != nil {
			t.Fatal(err)
		}
		preview, err := p.PreviewPlacement(t.Context(), who, source.ID, "Field", "general")
		if err != nil || !preview.SummaryRequired || preview.Runs != 0 {
			t.Fatalf("zero-run classification: %+v %v", preview, err)
		}
		if _, err := p.Share(t.Context(), who, source.ID, platform.ThreadShare{Project: "Field", PlacementToken: preview.Token}); err == nil {
			t.Fatal("zero-run raw share bypassed registered privacy guard")
		}
		if _, err := p.UpdateThread(t.Context(), who, source.ID, platform.ThreadUpdate{Version: source.Version, Visibility: threads.Shared}); err == nil {
			t.Fatal("zero-run visibility edit bypassed privacy guard")
		}
		copy, err := p.PublishSummary(t.Context(), who, source.ID, platform.SummaryPublication{Project: "Field", Summary: "Reviewed public result", PlacementToken: preview.Token})
		if err != nil {
			t.Fatal(err)
		}
		if copy.ID == source.ID || copy.Project != "Field" {
			t.Fatal("reviewed copy mutated the private source")
		}
	})
}
