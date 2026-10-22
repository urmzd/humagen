package platform_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
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
