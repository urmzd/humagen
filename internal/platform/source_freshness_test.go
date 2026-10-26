package platform_test

import (
	"errors"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"sync/atomic"
	"testing"
	"time"
)

func TestElapsedFreshnessProjectsHonestHealthAndOneConfinedAttentionItem(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		base := time.Now()
		clock := base
		var elapsed atomic.Int64
		p := startWith(t, platform.Config{Backends: platform.Backends{State: open(t)}, Now: func() time.Time { return base.Add(time.Duration(elapsed.Load())) }})
		_, err := p.Projects.Create(tenant, projects.Project{Name: "Secret", Members: []projects.Entry{{Who: "owner", Role: projects.Owner}}}, projects.Writer{By: "owner", Govern: true}, clock)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = p.Knowledge.Create(tenant, "copy", "", "owner", clock)
		_, err = p.Knowledge.Configure(tenant, "copy", knowledge.SourceConfig{Mode: knowledge.Indexed, Provider: "drive", Origin: "folder", Owner: "owner", Project: "Secret", StopAfterSeconds: 60}, clock)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := p.Knowledge.BeginSync(tenant, "copy", clock)
		_, err = p.Knowledge.CompleteSync(tenant, "copy", id, []knowledge.ImportDocument{{ID: "doc", Title: "Runbook", Text: "retained source copy"}}, "owner", clock)
		if err != nil {
			t.Fatal(err)
		}
		owner := auth.Principal{Tenant: tenant, Subject: "owner", Kind: auth.Machine, Scopes: []auth.Permission{auth.KnowledgeRead, auth.KnowledgeWrite}}
		outsider := owner
		outsider.Subject = "peer"
		elapsed.Store(int64(2 * time.Minute))
		shown, err := p.SourceAs(owner, "copy")
		if err != nil || shown.Lifecycle.Status != knowledge.NeedsAttention || shown.Lifecycle.Cause != knowledge.Stale {
			t.Fatal("elapsed stop rule not shown honestly", shown, err)
		}
		raw, _ := p.Knowledge.Get(tenant, "copy")
		if raw.Lifecycle.Status != knowledge.InSync || raw.Lifecycle.Attempts[0].Status != "succeeded" {
			t.Fatal("clock invented a failed refresh")
		}
		for range 2 {
			if err := p.CheckAssignmentSources(tenant, []string{"copy"}); !errors.Is(err, platform.ErrSourceStop) {
				t.Fatal("stale dependency admitted", err)
			}
		}
		if p.InboxCount(owner).Open != 1 || p.InboxCount(outsider).Open != 0 {
			t.Fatal("stale cause not deduplicated or confined")
		}
		if _, err := p.SnapshotSource(owner, "copy"); err != nil {
			t.Fatal(err)
		}
		if p.InboxCount(owner).Open != 0 {
			t.Fatal("operator snapshot decision did not settle source attention")
		}
	})
}
