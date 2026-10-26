package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
)

func sourceGroundingPlatform(t *testing.T) *Platform {
	t.Helper()
	ops, err := operations.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	p := &Platform{Operations: ops, Runs: runs.NewIndex(), Knowledge: knowledge.NewStore(), SourceEvidence: knowledge.NewEvidenceStore(), Projects: projects.NewStore(), now: time.Now}
	for _, name := range []string{"Secret", "Other"} {
		if _, err := p.Projects.Create("tenant", projects.Project{Name: name, Members: []projects.Entry{{Who: "owner", Role: projects.Owner}}}, projects.Writer{By: "owner", Govern: true}, p.now()); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = p.Knowledge.Create("tenant", "private", "", "owner", p.now())
	if _, err := p.Knowledge.Configure("tenant", "private", knowledge.SourceConfig{Mode: knowledge.Indexed, Provider: "manual", Owner: "owner", Project: "Secret"}, p.now()); err != nil {
		t.Fatal(err)
	}
	return p
}
func sourceRequest() runtime.Request {
	return runtime.Request{Tenant: "tenant", RunID: "run1", Revision: agentrt.Revision{Spec: agentrt.Spec{Name: "worker", Sources: []agentrt.Source{{Name: "private", URI: "knowledge://private"}}}}}
}

func TestPrivateSourceGroundingBindsActorAndConsumesOpaqueGrant(t *testing.T) {
	p := sourceGroundingPlatform(t)
	_, _ = p.Knowledge.Add("tenant", "private", "Runbook", "", "private canary", "owner", p.now())
	original := sourceRequest()
	req, err := p.withSourceGrounding(original, "Secret", "owner")
	if err != nil {
		t.Fatal(err)
	}
	token := req.Revision.Spec.Sources[0].Name
	if token == "private" || original.Revision.Spec.Sources[0].Name != "private" {
		t.Fatal("stored source identity changed")
	}
	if names := p.sourceNamesForTools(req); len(names) != 1 || names[0] != "private" {
		t.Fatal("catalog lost declared source identity", names)
	}
	if r := p.knowledgeFor("other", []string{token}); r != nil {
		t.Fatal("cross-tenant grant consumed")
	}
	retriever := p.knowledgeFor("tenant", []string{token})
	if retriever == nil {
		t.Fatal("authorized source missing")
	}
	if p.knowledgeFor("tenant", []string{token}) != nil {
		t.Fatal("opaque grant reused")
	}
	wrong := envelope.WithActor(t.Context(), envelope.Actor{ID: "peer", User: "peer"})
	if got, err := retriever.Retrieve(wrong, "canary"); err != nil || got != nil {
		t.Fatal("personal source reached another actor", got, err)
	}
	owner := envelope.WithActor(t.Context(), envelope.Actor{ID: "owner", User: "owner"})
	got, err := retriever.Retrieve(owner, "canary")
	if err != nil || got == nil || !strings.Contains(got.Prompt, "private canary") || strings.Contains(got.Prompt, token) {
		t.Fatalf("bad grounding: %+v %v", got, err)
	}
}

func TestPrivateSourceReconfigurationCannotSwapDataIntoAuthorizedTurn(t *testing.T) {
	p := sourceGroundingPlatform(t)
	req, err := p.withSourceGrounding(sourceRequest(), "Secret", "owner")
	if err != nil {
		t.Fatal(err)
	}
	// The owner belongs to both projects, but this turn belongs only to Secret.
	if _, err := p.Knowledge.Configure("tenant", "private", knowledge.SourceConfig{Mode: knowledge.Indexed, Provider: "manual", Owner: "owner", Project: "Other"}, p.now()); err != nil {
		t.Fatal(err)
	}
	_, _ = p.Knowledge.Add("tenant", "private", "Other", "", "other-project canary", "owner", p.now())
	retriever := p.knowledgeFor("tenant", []string{req.Revision.Spec.Sources[0].Name})
	if retriever == nil {
		t.Fatal("request grant missing before scope check")
	}
	owner := envelope.WithActor(t.Context(), envelope.Actor{ID: "owner", User: "owner"})
	if got, err := retriever.Retrieve(owner, "canary"); err != nil || got != nil {
		t.Fatal("changed source audience leaked", got, err)
	}
}

type canceledSourceExecutor struct{}

func (canceledSourceExecutor) Execute(context.Context, runtime.Request, func(context.Context, agentrt.Chunk) error) (runtime.Outcome, error) {
	return runtime.Outcome{}, context.Canceled
}

func TestSourceGrantsExpireAreBoundedAndReleaseOnEarlyExecutorFailure(t *testing.T) {
	p := sourceGroundingPlatform(t)
	at := p.now()
	p.now = func() time.Time { return at }
	p.grounding.grants = map[string]sourceGrant{}
	for i := 0; i < maxSourceGrants; i++ {
		p.grounding.grants[fmt.Sprint(i)] = sourceGrant{Tenant: "tenant", Run: "other", Expires: at.Add(sourceGrantTTL)}
	}
	if _, err := p.withSourceGrounding(sourceRequest(), "Secret", "owner"); !errors.Is(err, knowledge.ErrExists) {
		t.Fatalf("unbounded source grants: %v", err)
	}
	if len(p.grounding.grants) != maxSourceGrants {
		t.Fatal("capacity refusal changed unrelated grants")
	}
	at = at.Add(sourceGrantTTL + time.Second)
	executor := callExecutor{inner: canceledSourceExecutor{}, resolve: func(req runtime.Request) (runtime.Request, error) {
		return p.withSourceGrounding(req, "Secret", "owner")
	}, release: p.releaseSourceGrounding}
	if _, err := executor.Execute(t.Context(), sourceRequest(), nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(p.grounding.grants) != 0 {
		t.Fatal("expired or canceled request left active grants")
	}
	executor.resolve = func(req runtime.Request) (runtime.Request, error) {
		resolved, err := p.withSourceGrounding(req, "Secret", "owner")
		if err != nil {
			return req, err
		}
		return resolved, fmt.Errorf("model unavailable")
	}
	if _, err := executor.Execute(t.Context(), sourceRequest(), nil); err == nil {
		t.Fatal("model failure ignored")
	}
	if len(p.grounding.grants) != 0 {
		t.Fatal("failed resolution left active grants")
	}
}
