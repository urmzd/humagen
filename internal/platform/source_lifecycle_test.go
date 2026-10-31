package platform_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/mcptools"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

type sourceBoundary struct {
	cause knowledge.Cause
	calls int
}

func (s *sourceBoundary) Fetch(_ context.Context, origin string, credential mcptools.Secret) ([]knowledge.ImportDocument, error) {
	s.calls++
	if origin != "folder1" || credential.Empty() {
		return nil, knowledge.ProviderError{Cause: knowledge.AuthExpired}
	}
	if s.cause != "" {
		return nil, knowledge.ProviderError{Cause: s.cause}
	}
	return []knowledge.ImportDocument{{ID: "runbook", Title: "Private runbook", Text: "release safely", URI: "https://example.com/runbook"}}, nil
}

func TestManagedSourceConfinementAttentionAndPersonalAccess(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		provider := &sourceBoundary{}
		p := startWith(t, platform.Config{Backends: platform.Backends{State: open(t)}, CredentialRefs: map[string][]string{tenant: {"op://People/{subject}/"}}, Ext: platform.Extensions{SourceProviders: map[string]knowledge.Provider{"test": provider}, Resolver: mcptools.ResolverFunc(func(_ context.Context, ref string) (mcptools.Secret, error) {
			if ref != "op://People/ana/source" {
				return mcptools.Secret{}, errors.New("not found")
			}
			return mcptools.NewSecret("test-only-secret"), nil
		})}})
		ana := auth.Principal{Tenant: tenant, Subject: "ana", Kind: auth.User, Roles: []auth.Role{auth.Manager}}
		outsider := auth.Principal{Tenant: tenant, Subject: "outsider", Kind: auth.User, Roles: []auth.Role{auth.Manager}}
		_, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "ana", Govern: true}, platform.ProjectInput{Name: "Secret", Members: []projects.Entry{{Who: "ana", Role: projects.Owner}}})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = p.Knowledge.Create(tenant, "source", "", "ana", time.Now())
		_, err = p.ConfigureSource(ana, "source", knowledge.SourceConfig{Mode: knowledge.Indexed, Provider: "test", Origin: "folder1", Project: "Secret"})
		if err != nil {
			t.Fatal(err)
		}
		if p.SourceVisible(outsider, "source") || len(must(p.SourcesAs(outsider))) != 0 {
			t.Fatal("hidden source revealed")
		}
		if _, err := p.SearchSources(t.Context(), outsider, []string{"source"}, "release", ""); !errors.Is(err, knowledge.ErrNotFound) {
			t.Fatalf("hidden source search: %v", err)
		}
		if err := p.ConnectSource(ana, "source", "op://People/outsider/source"); err == nil {
			t.Fatal("another person's reference accepted")
		}
		if err := p.ConnectSource(ana, "source", "op://People/ana/source"); err != nil {
			t.Fatal(err)
		}
		src, err := p.SyncSource(t.Context(), ana, "source")
		if err != nil || src.Lifecycle.Status != knowledge.InSync {
			t.Fatalf("sync: %+v %v", src, err)
		}
		provider.cause = knowledge.AuthExpired
		for range 3 {
			if _, err := p.SyncSource(t.Context(), ana, "source"); err != nil {
				t.Fatal(err)
			}
		}
		if count := p.InboxCount(ana); count.Open != 1 {
			t.Fatalf("dedup count: %+v", count)
		}
		if count := p.InboxCount(outsider); count.Open != 0 || count.Unread != 0 {
			t.Fatalf("hidden counts: %+v", count)
		}
		messages, _, err := p.InboxPage(ana, inbox.Query{Kind: inbox.KindSource})
		if err != nil || len(messages) != 1 || messages[0].Parts[0].Source == nil {
			t.Fatalf("source attention: %+v %v", messages, err)
		}
		if _, err := p.InboxGet(outsider, messages[0].ID); !errors.Is(err, inbox.ErrNotFound) {
			t.Fatal("hidden attention exposed")
		}
		read, err := p.SearchSourcesDetailed(t.Context(), ana, []string{"source"}, "release", "")
		if err != nil || len(read.Hits) != 0 || len(read.Warnings) != 1 || read.Warnings[0].Cause != knowledge.AuthExpired {
			t.Fatalf("revoked upstream access fell back to retained text: %+v %v", read, err)
		}
		provider.cause = ""
		if _, err := p.SyncSource(t.Context(), ana, "source"); err != nil {
			t.Fatal(err)
		}
		if count := p.InboxCount(ana); count.Open != 0 {
			t.Fatalf("attention not settled: %+v", count)
		}
		rows, err := p.Audit.List(tenant, audit.Query{From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour), Limit: 50}, nil)
		if err != nil {
			t.Fatal(err)
		}
		failed, succeeded, personal := 0, 0, 0
		for _, row := range rows {
			if row.Action == "source.refresh" {
				if row.Outcome == audit.Failed {
					failed++
				}
				if row.Outcome == audit.Succeeded {
					succeeded++
				}
			}
			if row.Action == "source.reference.save" {
				if row.Scope != "personal" || row.Subject != "ana" || row.Actor != "ana" {
					t.Fatalf("reference audience: %+v", row)
				}
				personal++
			}
		}
		if failed != 3 || succeeded != 2 || personal != 1 {
			t.Fatalf("audit outcomes failed=%d succeeded=%d personal=%d", failed, succeeded, personal)
		}
		raw, _ := json.Marshal(rows)
		if strings.Contains(string(raw), "op://") || strings.Contains(string(raw), "test-only-secret") {
			t.Fatal("source audit retained credential material")
		}
		if !src.HardStale(time.Now().Add(25 * time.Hour)) {
			t.Fatal("stop rule did not expire")
		}
	})
}
