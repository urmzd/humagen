package wire

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/definition"
	"github.com/urmzd/humagen/pkg/agents"
)

// TestDefinitionCatalogReadsWhatTheRegistryHeld moves revisions through
// FromAgentSpec and reads them back as the registry gave them.
func TestDefinitionCatalogReadsWhatTheRegistryHeld(t *testing.T) {
	ctx := context.Background()
	reg := NewMemoryAgents(KnownProviders()...)
	first := AgentSpec{Name: "docs-bot", Provider: "anthropic", Model: "claude-sonnet-4-5", SystemPrompt: "Answer."}
	second := AgentSpec{
		Name: "docs-bot", Description: "Answers docs questions", Provider: "anthropic", Model: "claude-sonnet-4-5",
		SystemPrompt: "Answer well.",
		Tools:        []string{"web_search", "mcp:github:write", "mcp:linear"},
		Sources:      []Source{{Name: "docs", URI: "kb://docs", Options: map[string]string{"k": "v"}}},
		Access:       Access{Spawn: []string{"helper"}, WorkspaceAreas: []string{"notes/"}},
		Fallbacks:    []ModelRef{{Provider: "openai", Model: "gpt-5"}},
		FallbackOn:   []string{"rate_limit"},
		Approvals:    []string{"web_search"},
		Approvers:    []string{"role:admin"},
		SelfApproval: true,
		Answerers:    []string{"ada"},
		TaggedOnly:   true,
	}
	if _, err := reg.Create(ctx, "acme", first, "ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(ctx, "acme", AgentSpec{Name: "helper", Provider: "anthropic", Model: "claude-sonnet-4-5", SystemPrompt: "Help."}, "ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Revise(ctx, "acme", second, 1, "grace"); err != nil {
		t.Fatal(err)
	}
	src := agents.New()
	for _, name := range []string{"docs-bot", "helper"} {
		hist, err := reg.History(ctx, "acme", name)
		if err != nil {
			t.Fatal(err)
		}
		var recs []definition.Record
		for _, r := range hist {
			raw, _ := json.Marshal(r.Spec)
			var old definition.AgentSpec
			if err := json.Unmarshal(raw, &old); err != nil {
				t.Fatal(err)
			}
			rec, err := definition.FromAgentSpec(old, r.Rev)
			if err != nil {
				t.Fatal(err)
			}
			rec.Tenant, rec.CreatedAt, rec.CreatedBy = "acme", r.CreatedAt.Add(time.Nanosecond), r.CreatedBy
			recs = append(recs, rec)
		}
		if _, err := src.Import(ctx, recs); err != nil {
			t.Fatal(err)
		}
	}

	cat := NewDefinitionCatalog(src)
	want, err := reg.Get(ctx, "acme", "docs-bot", 0)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cat.Get(ctx, "acme", "Docs-Bot", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Spec, want.Spec) || got.Rev != 2 || got.CreatedBy != "grace" || got.Hash == "" {
		t.Fatalf("latest =\n%+v\nwant\n%+v", got, want)
	}
	one, err := cat.Get(ctx, "acme", "docs-bot", 1)
	if err != nil || one.Rev != 1 || one.Spec.SystemPrompt != "Answer." {
		t.Fatalf("revision 1 = %+v, %v", one, err)
	}
	hist, err := cat.History(ctx, "acme", "docs-bot")
	if err != nil || len(hist) != 2 || hist[0].Rev != 1 || hist[1].Rev != 2 {
		t.Fatalf("history = %+v, %v", hist, err)
	}
	list, next, err := cat.List(ctx, "acme", Page{})
	if err != nil || next != "" || len(list) != 2 || list[0].Spec.Name != "docs-bot" || list[0].Rev != 2 {
		t.Fatalf("list = %+v, %q, %v", list, next, err)
	}
	for _, c := range []struct {
		tenant, name string
		rev          int
	}{{"globex", "docs-bot", 0}, {"acme", "docs-bot", 3}, {"acme", "nobody", 0}} {
		if _, err := cat.Get(ctx, c.tenant, c.name, c.rev); !errors.Is(err, ErrNotFound) {
			t.Errorf("get %+v = %v, want ErrNotFound", c, err)
		}
	}
	if err := src.Retire(ctx, "acme", "docs-bot", "ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.Get(ctx, "acme", "docs-bot", 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("latest of a retired agent = %v", err)
	}
	if r, err := cat.Get(ctx, "acme", "docs-bot", 1); err != nil || r.Rev != 1 {
		t.Errorf("a retired agent's revision 1 = %+v, %v", r, err)
	}
	if _, _, err := cat.List(ctx, "acme", Page{Token: "nonsense!"}); !errors.Is(err, ErrInvalidPage) {
		t.Errorf("bad token = %v", err)
	}
}
