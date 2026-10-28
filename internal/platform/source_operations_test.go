package platform

import (
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/fiber/pkg/turn/native"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/projects"
)

func TestSourceGroundingHonorsReadPolicyBeforeAnyRetriever(t *testing.T) {
	for _, tt := range []struct {
		name  string
		level operations.Level
		force bool
		want  int
	}{
		{"default", operations.Unspecified, false, 1}, {"allow", operations.Allow, false, 1}, {"ask", operations.Ask, false, 0}, {"never", operations.Never, false, 0}, {"forced", operations.Allow, true, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New(Config{Workspace: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			_, err = p.Knowledge.Create("tenant", "shared", "", "owner", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if tt.level != "" {
				_, err = p.Operations.SetPolicy("tenant", operations.Policy{Scope: operations.Scope{Kind: "workspace"}, Rules: map[operations.Category]operations.Level{operations.ReadContext: tt.level}}, 0, "owner", time.Now())
				if err != nil {
					t.Fatal(err)
				}
			}
			req := runtime.Request{Tenant: "tenant", RunID: "run", Definition: native.Revision{Spec: native.Spec{Name: "worker", Provider: "offline", Model: "offline", Sources: []native.Source{{Name: "shared", URI: "knowledge://shared"}}}}}
			if tt.force {
				req.Meta = map[string]string{operations.MetaForceAsk: "true"}
			}
			got, err := p.resolveRevision(req)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Definition.Spec.Sources) != tt.want {
				t.Fatalf("automatic source reads=%d, want%d", len(got.Definition.Spec.Sources), tt.want)
			}
			if len(req.Definition.Spec.Sources) != 1 || len(got.Definition.Spec.Tools) != 0 {
				t.Fatal("policy mutated stored grants or added source_search")
			}
			if len(p.grounding.grants) != tt.want {
				t.Fatal("withheld source minted a capability")
			}
			p.releaseSourceGrounding(got)
		})
	}
}

func TestSourceGroundingUsesAuthoritativeAdmissionBeforeRunRecord(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Projects.Create("tenant", projects.Project{Name: "Secret", Members: []projects.Entry{{Who: "owner", Role: projects.Owner}}}, projects.Writer{By: "owner", Govern: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = p.Knowledge.Create("tenant", "private", "", "owner", time.Now())
	_, err = p.Knowledge.Configure("tenant", "private", knowledge.SourceConfig{Mode: knowledge.Indexed, Provider: "manual", Project: "Secret", Owner: "owner"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	p.admitting.Store("run", admitted{tenant: "tenant", person: "owner", place: Place{Project: "Secret"}})
	req := runtime.Request{Tenant: "tenant", RunID: "run", Definition: native.Revision{Spec: native.Spec{Name: "worker", Provider: "offline", Model: "offline", Sources: []native.Source{{Name: "private", URI: "knowledge://private"}}}}}
	got, err := p.resolveRevision(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Definition.Spec.Sources) != 1 || got.Definition.Spec.Sources[0].Name == "private" {
		t.Fatal("authoritative first-turn placement lost scoped grounding")
	}
	p.releaseSourceGrounding(got)
	req.Tenant = "other"
	if project, person := p.operationPlace(req); project != "" || person != "" {
		t.Fatal("admission identity crossed tenants")
	}
}

func TestRetrieverRechecksTightenedReadContextBeforeActualRead(t *testing.T) {
	for _, level := range []operations.Level{operations.Ask, operations.Never} {
		t.Run(string(level), func(t *testing.T) {
			p, err := New(Config{Workspace: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = p.Close() })
			_, _ = p.Knowledge.Create("tenant", "shared", "", "owner", time.Now())
			_, _ = p.Knowledge.Add("tenant", "shared", "Runbook", "", "private retrieval canary", "owner", time.Now())
			req := runtime.Request{Tenant: "tenant", RunID: "run-late-policy", Definition: native.Revision{Spec: native.Spec{Name: "worker", Provider: "offline", Sources: []native.Source{{Name: "shared", URI: "knowledge://shared"}}}}}
			resolved, err := p.withSourceGrounding(req, "", "")
			if err != nil {
				t.Fatal(err)
			}
			retriever := p.knowledgeFor("tenant", []string{resolved.Definition.Spec.Sources[0].Name})
			if retriever == nil {
				t.Fatal("allowed initial retriever missing")
			}
			_, err = p.Operations.SetPolicy("tenant", operations.Policy{Scope: operations.Scope{Kind: "workspace"}, Rules: map[operations.Category]operations.Level{operations.ReadContext: level}}, 0, "owner", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			got, err := retriever.Retrieve(t.Context(), "canary")
			if err != nil || got != nil {
				t.Fatal("tightened policy still read source content", got, err)
			}
			if _, ok := p.SourceCitationEvidence("tenant", req.RunID, "[1]", ""); ok {
				t.Fatal("denied retrieval recorded evidence")
			}
		})
	}
}
