package platform

import (
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
	if p.SourceMayPublishIn("tenant", "private", "") || !p.SourceMayPublishIn("tenant", "private", "Secret") {
		t.Fatal("publication audience widened")
	}
	_, _ = p.Knowledge.Create("tenant", "legacy", "", "owner", at)
	if !p.runtimeSourceAllowed("tenant", "legacy", "") {
		t.Fatal("legacy workspace source changed")
	}
}
