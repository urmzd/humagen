package platform

import (
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"testing"
)

func TestAgentPackageUpgradeCustomizeReset(t *testing.T) {
	ctx := t.Context()
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	InstallPackagesForTest(t, p, "acme")
	if err := p.EnsureBuiltins(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	original, err := p.Agents.Get(ctx, "acme", BuiltinWriter, 0)
	if err != nil {
		t.Fatal(err)
	}
	old := original.Spec.Clone()
	old.Package.Version = "sha256:previous-package"
	old.SystemPrompt = "An earlier package prompt."
	oldRev, err := p.Agents.Revise(ctx, "acme", old, original.Rev, "platform")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.EnsureBuiltins(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	upgraded, _ := p.Agents.Get(ctx, "acme", BuiltinWriter, 0)
	if upgraded.Rev != oldRev.Rev+1 || !upgraded.Spec.SameContent(original.Spec) {
		t.Fatalf("upgrade: %+v", upgraded)
	}
	// A customization inherits the actual base even when a caller supplies another.
	base := old
	baseRev, err := p.Agents.Revise(ctx, "acme", base, upgraded.Rev, "platform")
	if err != nil {
		t.Fatal(err)
	}
	custom := base.Clone()
	custom.SystemPrompt = "Workspace instructions."
	custom.Package = &wire.PackageBase{Name: "spoofed", Version: "spoofed"}
	saved, err := p.Agents.Revise(ctx, "acme", custom, baseRev.Rev, "ana")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.EnsureBuiltins(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	rows, _, err := p.AgentDirectory(ctx, "acme", wire.Page{Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.Spec.Name != BuiltinWriter {
			continue
		}
		found = true
		if r.Rev != saved.Rev || r.Spec.SystemPrompt != custom.SystemPrompt || !r.Customized || !r.UpdateAvailable || r.Package.Name != "humagen/core" || r.Package.Version != old.Package.Version || r.Package.Latest != original.Spec.Package.Version {
			t.Fatalf("customization: %+v", r)
		}
	}
	if !found {
		t.Fatal("missing package agent")
	}
	who := auth.Principal{Tenant: "acme", Subject: "ana", Scopes: []auth.Permission{auth.AgentsWrite}}
	reset, err := p.ResetAgent(ctx, who, BuiltinWriter, saved.Rev)
	if err != nil {
		t.Fatal(err)
	}
	if reset.CreatedBy != "platform" || reset.Customized || reset.UpdateAvailable || reset.Rev != saved.Rev+1 || !reset.Spec.SameContent(original.Spec) {
		t.Fatalf("reset: %+v", reset)
	}
	pinned, err := p.Agents.Get(ctx, "acme", BuiltinWriter, original.Rev)
	if err != nil || pinned.Hash != original.Hash {
		t.Fatalf("pinned revision changed: %+v %v", pinned, err)
	}
}
