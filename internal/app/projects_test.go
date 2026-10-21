package app

import (
	"errors"
	"testing"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/spec"
)

// The file's projects merge into the tree: a console project survives a
// start, an unchanged file makes no version, and a project that left the file
// stays, unmanaged.
func TestTheFilesProjectsMergeIntoTheTree(t *testing.T) {
	p, err := platform.New(platform.Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	const tenant = "acme"
	if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "ada"}, platform.ProjectInput{Name: "Console"}); err != nil {
		t.Fatal(err)
	}
	file := []spec.Project{{Name: "Launch", Description: "Everything for the Q4 launch"}, {Name: "Ops"}}
	for range 2 {
		if err := applyProjects(p, tenant, file); err != nil {
			t.Fatal(err)
		}
	}
	launch, err := p.GetProject(tenant, "Launch")
	if err != nil || launch.Managed != projects.ManagedFile || launch.Version != 1 || launch.Description != "Everything for the Q4 launch" {
		t.Fatalf("the file's project = %+v, %v", launch, err)
	}
	if _, err := p.UpdateProject(t.Context(), tenant, "Launch", projects.Writer{By: "ada"}, platform.ProjectUpdate{Version: 1, Description: "mine"}); !errors.Is(err, projects.ErrManaged) {
		t.Fatalf("changing what the file sets = %v", err)
	}
	if !p.HasProject(tenant, "Console") {
		t.Fatal("a console project did not survive the file")
	}
	// Ops leaves the file.
	if err := applyProjects(p, tenant, file[:1]); err != nil {
		t.Fatal(err)
	}
	ops, err := p.GetProject(tenant, "Ops")
	if err != nil || ops.Managed != "" {
		t.Fatalf("a project that left the file = %+v, %v", ops, err)
	}
	if err := p.DeleteProject(t.Context(), tenant, "Ops", projects.Writer{By: "ada"}); err != nil {
		t.Fatalf("deleting a project the file let go of: %v", err)
	}
}
