package platform_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/call"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/projects"
)

// posted is when the announcements here are posted.
var posted = time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)

func newsPlatform(t *testing.T) *platform.Platform {
	t.Helper()
	p, err := platform.New(platform.Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	w := projects.Writer{By: "ada"}
	if _, err := p.CreateProject(t.Context(), tenant, w, platform.ProjectInput{Name: "Field"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateProject(t.Context(), tenant, w, platform.ProjectInput{
		Name: "Globex", Parent: "Field", Members: []projects.Entry{{Who: "*", Role: projects.Viewer}, {Who: "cy", Role: projects.Member}},
	}); err != nil {
		t.Fatal(err)
	}
	return p
}

// A notice on the event stream names a message and nothing more, and it
// reaches only a reader the message is for: an announcement in a project the
// reader is only a viewer of never reaches their stream, and one for them
// does.
func TestTheStreamCarriesOnlyNoticesForTheReader(t *testing.T) {
	p := newsPlatform(t)
	cy := auth.Principal{Subject: "cy", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	bo := auth.Principal{Subject: "bo", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	m, err := p.Announce(tenant, "Globex", inbox.From{Kind: "person", Name: "ada"}, platform.Announcement{Subject: "Renewal signed", Text: "Three more years."}, posted)
	if err != nil {
		t.Fatal(err)
	}
	n := inbox.Notice{ID: m.ID, Kind: m.Kind, State: m.State}
	if !p.InboxNoticeFor(cy, n) {
		t.Fatal("a member of Globex is not told")
	}
	if p.InboxNoticeFor(bo, n) {
		t.Fatal("a viewer of Globex is told of an announcement not for them")
	}
	other := auth.Principal{Subject: "cy", Tenant: "globex", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	if p.InboxNoticeFor(other, n) {
		t.Fatal("another workspace is told")
	}
	noProjects := auth.Principal{Subject: "cy", Tenant: tenant, Kind: auth.Machine, Scopes: []auth.Permission{auth.RunsRead}}
	if p.InboxNoticeFor(noProjects, n) {
		t.Fatal("a caller without projects:read is told of an announcement")
	}
	if _, err := p.Announce(tenant, "Ghost", inbox.From{Kind: "person", Name: "ada"}, platform.Announcement{Subject: "x", Text: "x"}, posted); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("an announcement in no project = %v", err)
	}
}

// A Slack app posts a project's announcements in the Slack channel its
// general is bound to, so its machine is told of them where it may see the
// project, though it is only a viewer there, and not of a project whose
// general is bound nowhere, nor of a members-only one it may not see until
// the project names it.
func TestAMachineIsToldOfAnnouncementsWhereItsClientIsBound(t *testing.T) {
	p := start(t, call.Limits{})
	for _, in := range []platform.ProjectInput{
		{Name: "Field"},
		{Name: "Globex", Parent: "Field", Members: []projects.Entry{{Who: "*", Role: projects.Viewer}, {Who: "cy", Role: projects.Member}}},
	} {
		if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "ada"}, in); err != nil {
			t.Fatal(err)
		}
	}
	in := slackClient("app", "A0APP", team, "")
	in.Slack.Machine = "svc-slack"
	create(t, p, tenant, in)
	machine := auth.Principal{Subject: "svc-slack", Tenant: tenant, Kind: auth.Machine, Scopes: []auth.Permission{auth.RunsRead, auth.ProjectsRead}}
	if _, err := p.UpdateChannel(t.Context(), tenant, "Globex", projects.General, projects.Writer{By: "ada"}, platform.ChannelUpdate{Slack: &projects.SlackBinding{Client: "app", Channel: "C0GLOBEX"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "ada"}, platform.ProjectInput{Name: "Vault", Kind: projects.Account}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpdateChannel(t.Context(), tenant, "Vault", projects.General, projects.Writer{By: "ada"}, platform.ChannelUpdate{Slack: &projects.SlackBinding{Client: "app", Channel: "C0VAULT"}}); err != nil {
		t.Fatal(err)
	}
	notice := func(project string) bool {
		t.Helper()
		m, err := p.Announce(tenant, project, inbox.From{Kind: "person", Name: "ada"}, platform.Announcement{Subject: "News in " + project, Text: "x"}, posted)
		if err != nil {
			t.Fatal(err)
		}
		return p.InboxNoticeFor(machine, inbox.Notice{ID: m.ID, Kind: m.Kind, State: m.State})
	}
	if !notice("Globex") {
		t.Fatal("the machine of the app Globex's general is bound to is not told, as a viewer")
	}
	other := auth.Principal{Subject: "svc-other", Tenant: tenant, Kind: auth.Machine, Scopes: []auth.Permission{auth.RunsRead, auth.ProjectsRead}}
	if m, _ := p.Announce(tenant, "Globex", inbox.From{Kind: "person", Name: "ada"}, platform.Announcement{Subject: "Again", Text: "x"}, posted); p.InboxNoticeFor(other, inbox.Notice{ID: m.ID, Kind: m.Kind, State: m.State}) {
		t.Fatal("another machine, a viewer of Globex, is told")
	}
	if notice("Vault") {
		t.Fatal("the machine is told of a members-only project it may not see")
	}
	// Named among Vault's members, as a viewer, the machine is told: a
	// members-only project's news reaches Slack only where its owners let
	// the app read it.
	vault, err := p.Projects.Get(tenant, "Vault")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Projects.Update(tenant, "Vault", vault.Version, projects.Writer{By: "ada"}, time.Now(), func(pr *projects.Project) error {
		pr.Members = append(pr.Members, projects.Entry{Who: "svc-slack", Role: projects.Viewer})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !notice("Vault") {
		t.Fatal("the machine Vault names as a viewer is not told")
	}
}

// Racing posts stop at a project's daily cap, all through Announce, the one
// way in for people and agents alike.
func TestRacingAnnouncementsStopAtTheProjectsCap(t *testing.T) {
	p := newsPlatform(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	made, refused := 0, 0
	for i := range 40 {
		wg.Go(func() {
			_, err := p.Announce(tenant, "Field", inbox.From{Kind: "agent", Name: "warp-analyst"}, platform.Announcement{Subject: fmt.Sprintf("Recap %d", i), Text: "What changed."}, posted)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				made++
			case errors.Is(err, inbox.ErrLimit):
				refused++
			default:
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if made != 20 || refused != 20 {
		t.Fatalf("posted %d, refused %d; want 20 and 20", made, refused)
	}
}
