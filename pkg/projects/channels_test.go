package projects_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/projects"
)

func channel(project, name string) projects.Channel {
	return projects.Channel{Project: project, Name: name}
}

func names(cs []projects.Channel) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Name
	}
	return out
}

func TestGeneralAlwaysExistsAndCannotBeArchived(t *testing.T) {
	s := projects.NewStore()
	got, err := s.Channels("acme", projects.Root)
	if err != nil || len(got) != 1 || got[0].Name != projects.General || got[0].Version != 0 {
		t.Fatalf("a new workspace's channels = %+v, %v; want general alone, unwritten", got, err)
	}
	mustCreate(t, s, "acme", team("Field", ""), ada)
	if c, err := s.Channel("acme", "Field", projects.General); err != nil || c.Project != "Field" {
		t.Fatalf("general in Field = %+v, %v", c, err)
	}
	if _, err := s.CreateChannel("acme", channel("Field", projects.General), ada, t0); !errors.Is(err, projects.ErrExists) {
		t.Fatalf("creating general = %v, want ErrExists", err)
	}
	if _, err := s.ArchiveChannel("acme", "Field", projects.General, ada, t0); !errors.Is(err, projects.ErrInUse) {
		t.Fatalf("archiving general = %v, want ErrInUse", err)
	}
	// Writing general makes it version 1, and it still cannot be archived.
	c, err := s.UpdateChannel("acme", "Field", projects.General, 0, ada, t0, func(c *projects.Channel) error {
		c.Purpose = "Everything about Field"
		return nil
	})
	if err != nil || c.Version != 1 || c.CreatedBy != "ada" {
		t.Fatalf("writing general = %+v, %v", c, err)
	}
	if _, err := s.ArchiveChannel("acme", "Field", projects.General, ada, t0); !errors.Is(err, projects.ErrInUse) {
		t.Fatalf("archiving general once written = %v, want ErrInUse", err)
	}
	if _, err := s.UpdateChannel("acme", "Field", projects.General, 1, ada, t0, func(c *projects.Channel) error {
		c.Archived = true
		return nil
	}); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("archiving general through an update = %v, want ErrInvalid", err)
	}
	if _, err := s.Channel("acme", "Field", "renewals"); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("a channel never made = %v", err)
	}
	if _, err := s.Channels("acme", "Nowhere"); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("the channels of a missing project = %v", err)
	}
}

func TestAProjectKeepsAtMostTwentyChannelsArchivedOnesIncluded(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	for i := range projects.MaxChannels - 1 {
		if _, err := s.CreateChannel("acme", channel("Field", fmt.Sprintf("c-%02d", i)), ada, t0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ArchiveChannel("acme", "Field", "c-00", ada, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateChannel("acme", channel("Field", "one-more"), ada, t0); !errors.Is(err, projects.ErrLimit) {
		t.Fatalf("the 21st channel = %v, want ErrLimit", err)
	}
	got, _ := s.Channels("acme", "Field")
	if len(got) != projects.MaxChannels || got[0].Name != projects.General || got[1].Name != "c-00" || !got[1].Archived {
		t.Fatalf("channels = %v", names(got))
	}
}

func TestTheChannelCapHoldsUnderRacingCreates(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	var wg sync.WaitGroup
	var made, limited atomic.Int64
	for i := range 40 {
		wg.Go(func() {
			_, err := s.CreateChannel("acme", channel("Field", fmt.Sprintf("c-%02d", i)), ada, t0)
			switch {
			case err == nil:
				made.Add(1)
			case errors.Is(err, projects.ErrLimit):
				limited.Add(1)
			default:
				t.Errorf("create: %v", err)
			}
		})
	}
	wg.Wait()
	got, _ := s.Channels("acme", "Field")
	if len(got) != projects.MaxChannels || made.Load() != projects.MaxChannels-1 || limited.Load() != 40-(projects.MaxChannels-1) {
		t.Fatalf("%d kept, %d made, %d refused", len(got), made.Load(), limited.Load())
	}
}

func TestChannelRules(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	mustCreate(t, s, "acme", projects.Project{Name: "News", Kind: projects.Company}, admin)
	for _, c := range []struct {
		name string
		ch   projects.Channel
		w    projects.Writer
		want error
	}{
		{"upper case", channel("Field", "Renewals"), ada, projects.ErrInvalid},
		{"a slash", channel("Field", "a/b"), ada, projects.ErrInvalid},
		{"too long", channel("Field", strings.Repeat("a", projects.MaxChannelName+1)), ada, projects.ErrInvalid},
		{"a long purpose", projects.Channel{Project: "Field", Name: "x", Purpose: strings.Repeat("p", projects.MaxPurpose+1)}, ada, projects.ErrInvalid},
		{"a purpose of two lines", projects.Channel{Project: "Field", Name: "x", Purpose: "a\nb"}, ada, projects.ErrInvalid},
		{"a default that names nothing", projects.Channel{Project: "Field", Name: "x", Default: "@ignite"}, ada, projects.ErrInvalid},
		{"a missing project", channel("Nowhere", "x"), ada, projects.ErrNotFound},
		{"the root without govern", channel(projects.Root, "x"), ada, projects.ErrForbidden},
		{"a company project without govern", channel("News", "x"), ada, projects.ErrForbidden},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := s.CreateChannel("acme", c.ch, c.w, t0); !errors.Is(err, c.want) {
				t.Fatalf("create = %v, want %v", err, c.want)
			}
		})
	}
	if _, err := s.CreateChannel("acme", channel(projects.Root, "announcements"), admin, t0); err != nil {
		t.Fatalf("a channel at the root with govern: %v", err)
	}
	made, err := s.CreateChannel("acme", projects.Channel{Project: "Field", Name: "renewals", Purpose: "Renewal work", Default: "globex-helper"}, ada, t0)
	if err != nil || made.Version != 1 {
		t.Fatalf("create = %+v, %v", made, err)
	}
	if _, err := s.CreateChannel("acme", channel("Field", "renewals"), ada, t0); !errors.Is(err, projects.ErrExists) {
		t.Fatalf("a second #renewals = %v", err)
	}
	if _, err := s.UpdateChannel("acme", "Field", "renewals", 2, ada, t0, func(c *projects.Channel) error { c.Title = "x"; return nil }); !errors.Is(err, projects.ErrVersion) {
		t.Fatalf("a stale version = %v, want ErrVersion", err)
	}
	if _, err := s.UpdateChannel("acme", "Field", "renewals", 1, ada, t0, func(c *projects.Channel) error { c.Name = "renewal"; return nil }); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("a rename = %v, want ErrInvalid", err)
	}
	same, err := s.UpdateChannel("acme", "Field", "renewals", 1, ada, t0, func(*projects.Channel) error { return nil })
	if err != nil || same.Version != 1 {
		t.Fatalf("an update that changes nothing = %+v, %v", same, err)
	}
	if _, err := s.ArchiveChannel("acme", "Field", "renewals", ada, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateChannel("acme", "Field", "renewals", 2, ada, t0, func(c *projects.Channel) error { c.Title = "x"; return nil }); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("changing an archived channel = %v, want ErrInvalid", err)
	}
	if again, err := s.ArchiveChannel("acme", "Field", "renewals", ada, t0); err != nil || again.Version != 2 {
		t.Fatalf("archiving twice = %+v, %v; want no new version", again, err)
	}
	if _, err := s.CreateChannel("acme", channel("Field", "renewals"), ada, t0); !errors.Is(err, projects.ErrExists) {
		t.Fatalf("an archived channel's name = %v, want ErrExists", err)
	}
}

func TestManagedChannelsAreTheFilesToChange(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	if _, err := s.CreateChannel("acme", channel("Field", "renewals"), ada, t0); err != nil {
		t.Fatal(err)
	}
	took, err := s.UpsertChannel("acme", projects.Channel{Project: "Field", Name: "renewals", Purpose: "Renewal work"}, "file", t0)
	if err != nil || took.Managed != projects.ManagedFile || took.Version != 2 || took.CreatedBy != "ada" {
		t.Fatalf("taking a channel over = %+v, %v", took, err)
	}
	again, err := s.UpsertChannel("acme", projects.Channel{Project: "Field", Name: "renewals", Purpose: "Renewal work"}, "file", t0)
	if err != nil || again.Version != 2 {
		t.Fatalf("an unchanged declaration = %+v, %v; want no new version", again, err)
	}
	if _, err := s.UpdateChannel("acme", "Field", "renewals", 2, ada, t0, func(c *projects.Channel) error { c.Purpose = "mine"; return nil }); !errors.Is(err, projects.ErrManaged) {
		t.Fatalf("changing a managed channel = %v, want ErrManaged", err)
	}
	if _, err := s.ArchiveChannel("acme", "Field", "renewals", ada, t0); !errors.Is(err, projects.ErrManaged) {
		t.Fatalf("archiving a managed channel = %v, want ErrManaged", err)
	}
	// The file may declare general, and channels at the root.
	if g, err := s.UpsertChannel("acme", projects.Channel{Project: projects.Root, Name: projects.General, Purpose: "Company news"}, "file", t0); err != nil || g.Version != 1 {
		t.Fatalf("declaring the root's general = %+v, %v", g, err)
	}
	if _, err := s.ReleaseChannel("acme", "Field", "renewals", "file", t0); err != nil {
		t.Fatal(err)
	}
	if c, err := s.UpdateChannel("acme", "Field", "renewals", 3, ada, t0, func(c *projects.Channel) error { c.Purpose = "mine"; return nil }); err != nil || c.Managed != "" {
		t.Fatalf("a released channel = %+v, %v", c, err)
	}
}

func TestDeleteDropsAProjectsChannels(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Launch", ""), ada)
	if _, err := s.CreateChannel("acme", channel("Launch", "press"), ada, t0); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("acme", "Launch", ada, t0, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Channels("acme", "Launch"); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("a deleted project's channels = %v", err)
	}
	mustCreate(t, s, "acme", team("Launch", ""), admin)
	if got, _ := s.Channels("acme", "Launch"); len(got) != 1 {
		t.Fatalf("a recreated project's channels = %v, want general alone", names(got))
	}
}

func TestChannelChangesAreRecorded(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	if _, err := s.CreateChannel("acme", projects.Channel{Project: "Field", Name: "renewals", Title: "Renewals"}, ada, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateChannel("acme", "Field", "renewals", 1, ada, t0.Add(time.Minute), func(c *projects.Channel) error { c.Title = "Renewal work"; return nil }); err != nil {
		t.Fatal(err)
	}
	h, _ := s.History("acme", "Field", 0, 0)
	if len(h) != 3 || h[0].What != projects.ChannelChanged || h[0].Channel == nil || h[0].Channel.Before.Title != "Renewals" || h[0].Channel.After.Title != "Renewal work" {
		t.Fatalf("history = %+v", h)
	}
	if h[1].Channel.Before != nil || h[1].Channel.After.Version != 1 {
		t.Fatalf("the change that made it = %+v", h[1].Channel)
	}
}

func TestChannelsAreTheirTenants(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	mustCreate(t, s, "globex", team("Field", ""), ada)
	if _, err := s.CreateChannel("acme", channel("Field", "renewals"), ada, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Channel("globex", "Field", "renewals"); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("another tenant's channel = %v", err)
	}
	if _, err := s.ArchiveChannel("globex", "Field", "renewals", ada, t0); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("archiving another tenant's channel = %v", err)
	}
}
