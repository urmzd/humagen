package wire

import "testing"

func TestPackageMetadataKeepsPinnedOldRevisionHash(t *testing.T) {
	store := NewMemoryAgents()
	spec := AgentSpec{Name: "pinned", Provider: "offline", Model: "offline", SystemPrompt: "Keep this prompt."}
	old, err := store.Create(t.Context(), "acme", spec, "platform")
	if err != nil {
		t.Fatal(err)
	}
	const pinned = "sha256:5a7f2d06a95e55da93ed7fcc9e983ad256101656f53f79810ffe93a65f85c898"
	if old.Hash != pinned {
		t.Fatalf("old revision hash = %q", old.Hash)
	}
	spec.Package = &PackageBase{Name: "humagen/core", Version: "v1"}
	next, err := store.Revise(t.Context(), "acme", spec, old.Rev, "platform")
	if err != nil {
		t.Fatal(err)
	}
	if next.Spec.Package == nil || *next.Spec.Package != *spec.Package || next.Hash == old.Hash {
		t.Fatalf("package metadata did not round trip: %+v", next)
	}
	oldAgain, err := store.Get(t.Context(), "acme", "pinned", 1)
	if err != nil || oldAgain.Hash != pinned || oldAgain.Spec.Package != nil {
		t.Fatalf("pinned revision changed: %+v %v", oldAgain, err)
	}
}
