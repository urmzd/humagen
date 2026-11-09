package httpapi

import (
	"encoding/json"
	"fmt"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"net/http"
	"testing"
)

func TestPackageAgentsCanBeCustomizedAndReset(t *testing.T) {
	f := newFixtureWith(t, platform.Config{TestBuiltinProvider: "offline", TestBuiltinModel: "offline"})
	admin := f.machine(t, "acme", everything...)
	if err := f.p.EnsureBuiltins(t.Context(), "acme"); err != nil {
		t.Fatal(err)
	}
	want(t, f, admin, "POST", "/api/store/packages/core/install", "", http.StatusOK)
	original, err := f.p.Agents.Get(t.Context(), "acme", platform.BuiltinWriter, 0)
	if err != nil {
		t.Fatal(err)
	}
	spec := original.Spec.Clone()
	spec.SystemPrompt = "Workspace guidance."
	raw, _ := json.Marshal(spec)
	res, custom := f.do(t, nil, "PUT", "/api/agents/humagen-writer", admin, fmt.Sprintf(`{"spec":%s,"expected_rev":%d}`, raw, original.Rev))
	if res.StatusCode != http.StatusOK || custom["customized"] != true || custom["update_available"] != false {
		t.Fatalf("customize: %d %v", res.StatusCode, custom)
	}
	pkg, ok := custom["package"].(map[string]any)
	if !ok || pkg["name"] != "humagen/core" || pkg["version"] != pkg["latest"] {
		t.Fatalf("package: %v", custom)
	}
	if err := f.p.EnsureBuiltins(t.Context(), "acme"); err != nil {
		t.Fatal(err)
	}
	saved, _ := f.p.Agents.Get(t.Context(), "acme", platform.BuiltinWriter, 0)
	if saved.Spec.SystemPrompt != spec.SystemPrompt {
		t.Fatal("customization overwritten")
	}
	res, reset := f.do(t, nil, "POST", "/api/agents/humagen-writer/reset", admin, fmt.Sprintf(`{"expected_rev":%d}`, saved.Rev))
	if res.StatusCode != http.StatusOK || reset["created_by"] != "platform" || reset["customized"] != false || reset["update_available"] != false {
		t.Fatalf("reset: %d %v", res.StatusCode, reset)
	}
	if got := reset["spec"].(map[string]any)["system_prompt"]; got != original.Spec.SystemPrompt {
		t.Fatalf("reset prompt: %v", got)
	}
	res, directory := f.do(t, nil, "GET", "/api/agents", admin, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("directory: %d", res.StatusCode)
	}
	found := false
	for _, item := range directory["agents"].([]any) {
		row := item.(map[string]any)
		if row["spec"].(map[string]any)["name"] == platform.BuiltinWriter {
			found = row["package"].(map[string]any)["name"] == "humagen/core" && row["customized"] == false && row["update_available"] == false
		}
	}
	if !found {
		t.Fatalf("directory omitted package state: %v", directory)
	}
	reader := f.machine(t, "acme", "humagen:agents:read")
	res, _ = f.do(t, nil, "POST", "/api/agents/humagen-writer/reset", reader, `{"expected_rev":0}`)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("reader reset: %d", res.StatusCode)
	}
	other := f.machine(t, "globex", everything...)
	res, _ = f.do(t, nil, "POST", "/api/agents/humagen-writer/reset", other, `{"expected_rev":0}`)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("other tenant reset: %d", res.StatusCode)
	}
}

func TestPackageUpdateAvailableInAPI(t *testing.T) {
	store := wire.NewMemoryAgents()
	spec := wire.AgentSpec{Name: platform.BuiltinWriter, Provider: "offline", Model: "offline", SystemPrompt: "Earlier package prompt.", Package: &wire.PackageBase{Name: "humagen/core", Version: "previous-version"}}
	base, err := store.Create(t.Context(), "acme", spec, "platform")
	if err != nil {
		t.Fatal(err)
	}
	spec.SystemPrompt = "Workspace prompt."
	if _, err := store.Revise(t.Context(), "acme", spec, base.Rev, "ana"); err != nil {
		t.Fatal(err)
	}
	f := newFixtureWith(t, platform.Config{TestBuiltinProvider: "offline", TestBuiltinModel: "offline", Backends: platform.Backends{Agents: store}})
	admin := f.machine(t, "acme", everything...)
	res, history := f.do(t, nil, "GET", "/api/agents/humagen-writer/history", admin, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("history: %d %v", res.StatusCode, history)
	}
	current := history["revisions"].([]any)[0].(map[string]any)
	pkg := current["package"].(map[string]any)
	if current["customized"] != true || current["update_available"] != true || pkg["version"] != "previous-version" || pkg["latest"] == "previous-version" {
		t.Fatalf("update: %v", current)
	}
}
