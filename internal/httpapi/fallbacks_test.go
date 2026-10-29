package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// An agent's fallbacks are part of what it is: saved with the revision, read
// back in order, and refused when they repeat a model or name no provider.
func TestAgentFallbacks(t *testing.T) {
	f := newFixture(t)
	admin := f.machine(t, "acme", everything...)

	spec := `{"name":"steady","provider":"offline","model":"offline","system_prompt":"Help.",
		"fallbacks":[{"provider":"openai","model":"llama3.2"},{"provider":"anthropic","model":"claude-haiku-5-5"}]}`
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
		"no model":            `{"name":"c","provider":"offline","model":"offline","system_prompt":"x","fallbacks":[{"provider":"openai","model":""}]}`,
	} {
		if res, out := f.do(t, nil, "POST", "/api/agents", admin, body); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: = %d %v, want 400", name, res.StatusCode, out)
		}
	}
}

// A workspace model policy: agents name it, runs use its models, and it cannot
// be removed while an agent still runs on it.
func TestModelPolicies(t *testing.T) {
	// The policy's second model is a local OpenAI-compatible server, which
	// needs no key; it is never reached, since the first model completes.
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:11434/v1")
	t.Setenv("OPENAI_API_KEY", "")
	f := newFixture(t)
	admin := f.machine(t, "acme", everything...)

	_, current := f.do(t, nil, "GET", "/api/settings", admin, "")
	current["model_policies"] = []any{map[string]any{
		"name": "steady", "description": "Offline, then offline again.",
		"models":      []any{map[string]any{"provider": "offline", "model": "offline"}, map[string]any{"provider": "openai", "model": "llama3.2"}},
		"fallback_on": []any{"unavailable", "rate_limit"},
	}}
	body, _ := json.Marshal(current)
	if res, out := f.do(t, nil, "PUT", "/api/settings", admin, string(body)); res.StatusCode != http.StatusOK {
		t.Fatalf("save policy = %d %v", res.StatusCode, out)
	}

	if res, out := f.do(t, nil, "POST", "/api/agents", admin, `{"name":"governed","provider":"policy","model":"steady","system_prompt":"Help."}`); res.StatusCode != http.StatusCreated {
		t.Fatalf("create on policy = %d %v", res.StatusCode, out)
	}
	for name, spec := range map[string]string{
		"unknown policy":       `{"name":"x1","provider":"policy","model":"nope","system_prompt":"x"}`,
		"own fallbacks on top": `{"name":"x2","provider":"policy","model":"steady","system_prompt":"x","fallbacks":[{"provider":"offline","model":"offline"}]}`,
	} {
		if res, out := f.do(t, nil, "POST", "/api/agents", admin, spec); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %d %v, want 400", name, res.StatusCode, out)
		}
	}

	// The run resolves the policy and completes on its first model.
	res, run := f.do(t, nil, "POST", "/api/runs", admin, `{"agent":"governed","text":"hi"}`)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("run = %d %v", res.StatusCode, run)
	}
	id := run["id"].(string)
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, got := f.do(t, nil, "GET", "/api/runs/"+id, admin, "")
		if got["status"] == "completed" {
			break
		}
		if got["status"] == "failed" || time.Now().After(deadline) {
			t.Fatalf("run on a policy = %v", got)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Removing the policy while an agent runs on it is refused.
	current["model_policies"] = []any{}
	body, _ = json.Marshal(current)
	if res, out := f.do(t, nil, "PUT", "/api/settings", admin, string(body)); res.StatusCode != http.StatusConflict {
		t.Fatalf("remove used policy = %d %v, want 409", res.StatusCode, out)
	}

	// A malformed policy is refused by fiber's own rules.
	current["model_policies"] = []any{map[string]any{"name": "bad", "models": []any{map[string]any{"provider": "offline", "model": "offline"}}, "fallback_on": []any{"rate_limit"}}}
	body, _ = json.Marshal(current)
	if res, out := f.do(t, nil, "PUT", "/api/settings", admin, string(body)); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("fallback_on with no fallback = %d %v, want 400", res.StatusCode, out)
	}
}
