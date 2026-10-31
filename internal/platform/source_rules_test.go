package platform_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/sourcelinks"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestSourceRulesConfinePreviewAndRetainDisabledForkRules(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		p := startWith(t, platform.Config{Backends: platform.Backends{State: open(t)}})
		for _, name := range []string{"Visible", "Hidden"} {
			owner := "owner"
			if name == "Hidden" {
				owner = "peer"
			}
			_, err := p.Projects.Create(tenant, projects.Project{Name: name, Members: []projects.Entry{{Who: owner, Role: projects.Owner}}}, projects.Writer{By: owner, Govern: true}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
		}
		who := auth.Principal{Tenant: tenant, Subject: "owner", Kind: auth.Machine, Scopes: []auth.Permission{auth.ClientsRead, auth.ClientsWrite}}
		rule, err := p.SaveSourceRule(who, "", sourcelinks.Rule{Provider: "github", Attribute: "repository", Pattern: "urmzd/humagen", Project: "Visible", Mode: sourcelinks.Auto})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.SaveSourceRule(who, "", sourcelinks.Rule{Provider: "github", Attribute: "repository", Pattern: "secret", Project: "Hidden"}); !errors.Is(err, projects.ErrNotFound) {
			t.Fatalf("hidden write: %v", err)
		}
		hidden := sourcelinks.Rule{ID: "secret-rule", Provider: "github", Attribute: "repository", Pattern: "urmzd/humagen", Project: "Hidden", Mode: sourcelinks.Auto}
		_, _ = p.SourceLinks.Put(tenant, hidden)
		resource := sourcelinks.Resource{Provider: "github", ID: "repo", Attributes: map[string][]string{"repository": {"urmzd/humagen"}}}
		before, err := p.PreviewSourceRules(who, resource)
		if err != nil || len(before) != 1 || before[0].Mode != sourcelinks.Suggest {
			t.Fatalf("preview %+v %v", before, err)
		}
		if rules, err := p.SourceRulesAs(who); err != nil || len(rules) != 1 {
			t.Fatal("hidden rule listed")
		}
		if err := p.DeleteSourceRule(who, hidden.ID); !errors.Is(err, sourcelinks.ErrNotFound) {
			t.Fatal("hidden delete differs from missing", err)
		}
		if err := p.SourceLinks.Delete(tenant, hidden.ID); err != nil {
			t.Fatal(err)
		}
		after, _ := p.PreviewSourceRules(who, resource)
		a, _ := json.Marshal(before)
		b, _ := json.Marshal(after)
		if string(a) != string(b) || strings.Contains(string(a), "Hidden") {
			t.Fatal("hidden competition changed preview")
		}
		if err := p.RestoreSourceLinkRulesDisabled(tenant, "Visible", "fork1", []sourcelinks.Rule{rule}); err != nil {
			t.Fatal(err)
		}
		rules, _ := p.SourceLinks.List(tenant)
		for _, copy := range rules {
			if copy.CopiedFrom != "" {
				copy.Mode = sourcelinks.Auto
				if _, err := p.SaveSourceRule(who, copy.ID, copy); !errors.Is(err, sourcelinks.ErrInvalid) {
					t.Fatal("fork rule activated", err)
				}
			}
		}
	})
}

func TestConfiguredSourceCreationNeverPublishesAnUnscopedName(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		p := startWith(t, platform.Config{Backends: platform.Backends{State: open(t)}})
		_, err := p.Projects.Create(tenant, projects.Project{Name: "Private", Members: []projects.Entry{{Who: "owner", Role: projects.Owner}}}, projects.Writer{By: "owner", Govern: true}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		owner := auth.Principal{Tenant: tenant, Subject: "owner", Kind: auth.Machine, Scopes: []auth.Permission{auth.KnowledgeRead, auth.KnowledgeWrite}}
		outsider := auth.Principal{Tenant: tenant, Subject: "peer", Kind: auth.Machine, Scopes: owner.Scopes}
		cfg := &knowledge.SourceConfig{Mode: knowledge.Indexed, Provider: "drive", Origin: "folder", Project: "Private"}
		if _, err := p.CreateSource(owner, "private-name", "", cfg); err != nil {
			t.Fatal(err)
		}
		if p.SourceVisible(outsider, "private-name") || len(must(p.SourcesAs(outsider))) != 0 {
			t.Fatal("private creation exposed an intermediate name")
		}
		if _, err := p.CreateSource(outsider, "private-name", "", nil); !errors.Is(err, knowledge.ErrNotFound) {
			t.Fatal("duplicate hidden source revealed by create", err)
		}
		cfg.Project = "missing"
		if _, err := p.CreateSource(owner, "rejected-source", "", cfg); !errors.Is(err, knowledge.ErrNotFound) {
			t.Fatal("invalid scope accepted", err)
		}
		if _, err := p.Knowledge.Get(tenant, "rejected-source"); !errors.Is(err, knowledge.ErrNotFound) {
			t.Fatal("failed configuration left an unscoped source")
		}
	})
}
