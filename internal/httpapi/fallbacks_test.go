package httpapi

import (
	"net/http"
	"testing"
)

// An agent's fallbacks are part of what it is: saved with the revision, read
// back in order, and refused when they repeat a model or name no provider.
func TestAgentFallbacks(t *testing.T) {
	f := newFixture(t)
	admin := f.machine(t, "acme", everything...)

	spec := `{"name":"steady","provider":"offline","model":"offline","system_prompt":"Help.",
		"fallbacks":[{"provider":"ollama","model":"llama3.2"},{"provider":"anthropic","model":"claude-haiku-5-5"}]}`
	res, out := f.do(t, nil, "POST", "/api/agents", admin, spec)
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		t.Fatalf("create = %d %v", res.StatusCode, out)
	}
	saved, _ := out["spec"].(map[string]any)
	got, _ := saved["fallbacks"].([]any)
	if len(got) != 2 || got[0].(map[string]any)["model"] != "llama3.2" || got[1].(map[string]any)["provider"] != "anthropic" {
		t.Fatalf("fallbacks = %v", saved["fallbacks"])
	}

	for name, body := range map[string]string{
		"repeats the primary": `{"name":"a","provider":"offline","model":"offline","system_prompt":"x","fallbacks":[{"provider":"offline","model":"offline"}]}`,
		"unknown provider":    `{"name":"b","provider":"offline","model":"offline","system_prompt":"x","fallbacks":[{"provider":"nowhere","model":"m"}]}`,
		"no model":            `{"name":"c","provider":"offline","model":"offline","system_prompt":"x","fallbacks":[{"provider":"ollama","model":""}]}`,
	} {
		if res, out := f.do(t, nil, "POST", "/api/agents", admin, body); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: = %d %v, want 400", name, res.StatusCode, out)
		}
	}
}
