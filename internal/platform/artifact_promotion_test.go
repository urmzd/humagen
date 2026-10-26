package platform_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestAcceptedArtifactPromotionKeepsItsExactVersionAudienceAndProvenance(t *testing.T) {
	p := start(t, call.Limits{})
	if _, err := p.Projects.Create(tenant, projects.Project{Name: "Launch"}, ada, time.Now()); err != nil {
		t.Fatal(err)
	}
	shared := startThread(t, p, admin, platform.ThreadInput{Project: "Launch", Visibility: threads.Shared, Text: "Accepted deliverable"})
	if err := p.PutFile(tenant, shared.Thread.ID, "decision.md", "Accepted original decision"); err != nil {
		t.Fatal(err)
	}
	file, err := p.Publish(t.Context(), platform.Publication{Tenant: tenant, Thread: shared.Thread.ID, Project: "Launch", Agent: "lead", Path: "decision.md"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.PutFile(tenant, shared.Thread.ID, "decision.md", "Newer unaccepted decision"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Publish(t.Context(), platform.Publication{Tenant: tenant, Thread: shared.Thread.ID, Project: "Launch", Agent: "lead", Path: "decision.md"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Knowledge.Create(tenant, "accepted", "Accepted project results", admin.Subject, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Knowledge.Configure(tenant, "accepted", knowledge.SourceConfig{Mode: knowledge.Indexed, Provider: "manual", Owner: admin.Subject, Project: "Launch"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	doc, err := p.PromoteArtifactKnowledge(t.Context(), admin, file.ID, 1, "accepted")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.URI, file.ID+"?version=1") {
		t.Fatalf("provenance = %q", doc.URI)
	}
	hits, err := p.Knowledge.Search(tenant, []string{"accepted"}, "original", 5, "")
	if err != nil || len(hits) == 0 || !strings.Contains(hits[0].Text, "Accepted original") {
		t.Fatalf("promoted content = %+v,%v", hits, err)
	}
	if _, err := p.PromoteArtifactKnowledge(t.Context(), admin, file.ID, 2, "accepted"); !errors.Is(err, artifacts.ErrInvalid) {
		t.Fatalf("wrong version = %v", err)
	}
	if _, err := p.Knowledge.Create(tenant, "broad", "", admin.Subject, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.PromoteArtifactKnowledge(t.Context(), admin, file.ID, 1, "broad"); !errors.Is(err, knowledge.ErrInvalid) {
		t.Fatalf("wider destination = %v", err)
	}
	private := startThread(t, p, admin, platform.ThreadInput{Project: "Launch", Visibility: threads.Private, Text: "Private output"})
	if err := p.PutFile(tenant, private.Thread.ID, "private.md", "Private content"); err != nil {
		t.Fatal(err)
	}
	hidden, err := p.Publish(t.Context(), platform.Publication{Tenant: tenant, Thread: private.Thread.ID, Project: "Launch", Path: "private.md"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.PromoteArtifactKnowledge(t.Context(), admin, hidden.ID, 1, "accepted"); !errors.Is(err, artifacts.ErrInvalid) {
		t.Fatalf("private promotion = %v", err)
	}
	other := auth.Principal{Subject: "foreign", Tenant: "other", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	if _, err := p.PromoteArtifactKnowledge(t.Context(), other, file.ID, 1, "accepted"); !errors.Is(err, artifacts.ErrNotFound) {
		t.Fatalf("foreign artifact = %v", err)
	}
}
