package platform

import (
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/projects"
	"testing"
	"time"
)

func TestRuntimeSourcePrivacyFailsClosedWithoutAuthoritativeAccess(t *testing.T) {
	p := &Platform{Knowledge: knowledge.NewStore(), Projects: projects.NewStore()}
	at := time.Now()
	if p.runtimeSourceAllowed("tenant", "missing", "") || p.runtimeReaderSourceAllowed("tenant", "missing", "", "owner") || p.SourceMayPublishIn("tenant", "missing", "") {
		t.Fatal("missing metadata grants source access")
	}
	_, err := p.Projects.Create("tenant", projects.Project{Name: "Secret", Members: []projects.Entry{{Who: "owner", Role: projects.Owner}, {Who: "peer", Role: projects.Member}}}, projects.Writer{By: "owner", Govern: true}, at)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = p.Knowledge.Create("tenant", "private", "", "owner", at)
	_, err = p.Knowledge.Configure("tenant", "private", knowledge.SourceConfig{Mode: knowledge.Indexed, Provider: "drive", Origin: "folder1", Owner: "owner", Project: "Secret"}, at)
	if err != nil {
		t.Fatal(err)
	}
	if !p.runtimeReaderSourceAllowed("tenant", "private", "Secret", "owner") {
		t.Fatal("exact owner with current role denied")
	}
	for _, person := range []string{"", "peer", "outsider"} {
		if p.runtimeReaderSourceAllowed("tenant", "private", "Secret", person) {
			t.Fatalf("unproven reader %q allowed", person)
		}
	}
	if p.runtimeReaderSourceAllowed("tenant", "private", "", "owner") {
		t.Fatal("source scope widened")
	}
	if p.SourceMayPublishIn("tenant", "private", "") || p.SourceMayPublishIn("tenant", "private", "Secret") {
		t.Fatal("publication audience widened")
	}
	_, _ = p.Knowledge.Create("tenant", "legacy", "", "owner", at)
	if !p.runtimeSourceAllowed("tenant", "legacy", "") {
		t.Fatal("legacy workspace source changed")
	}
}

func TestPublicExternalSourceViewsWithholdRetainedVariantInventory(t *testing.T) {
	f := newSourceQueryFixture(t, knowledge.Indexed, &citationProvider{})
	if _, err := f.p.Knowledge.SetExcluded("tenant", "external", "selected", true, f.p.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Knowledge.SaveVariant("tenant", "external", "historical-files", f.p.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Knowledge.SaveVariant("tenant", "private", "local-view", f.p.now()); err != nil {
		t.Fatal(err)
	}
	shown, err := f.p.SourceAs(f.who, "external")
	if err != nil || len(shown.Variants) != 0 || shown.Lifecycle == nil || len(shown.Lifecycle.Resources) != 1 || shown.Lifecycle.Resources[0] != "selected" {
		t.Fatal("external view disclosed derived file inventory or erased selection configuration", shown, err)
	}
	shown.Lifecycle.Resources[0] = "altered"
	retained, err := f.p.Knowledge.Get("tenant", "external")
	if err != nil || len(retained.Variants) != 1 || len(retained.Variants[0].Excluded) != 1 || retained.Lifecycle.Resources[0] != "selected" {
		t.Fatal("projection changed authoritative source metadata", retained, err)
	}
	local, err := f.p.SourceAs(f.who, "private")
	if err != nil || len(local.Variants) != 1 || local.Variants[0].Name != "local-view" {
		t.Fatal("manual saved views were removed", local, err)
	}
	found := false
	for _, source := range f.p.SourcesAs(f.who) {
		if source.Name == "external" {
			found = true
			if len(source.Variants) != 0 || len(source.Lifecycle.Resources) != 1 || source.Lifecycle.Resources[0] != "selected" {
				t.Fatal("source list disclosed variants or erased collection selection", source)
			}
		}
	}
	if !found {
		t.Fatal("authorized source configuration disappeared")
	}
	outsider := auth.Principal{Tenant: "tenant", Subject: "outsider", Kind: auth.User, Scopes: []auth.Permission{auth.KnowledgeRead}}
	if _, err := f.p.SourceAs(outsider, "external"); err == nil {
		t.Fatal("sanitized metadata bypassed source scope")
	}
	outsider.Subject, outsider.Tenant = "owner", "different-tenant"
	if _, err := f.p.SourceAs(outsider, "external"); err == nil {
		t.Fatal("sanitized metadata bypassed tenant scope")
	}
}
