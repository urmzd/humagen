package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/urmzd/saige/agent/provider/anthropic"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/spec"
)

func TestEveryModelKindBuildsWithItsKey(t *testing.T) {
	keys := credref.Resolver{LookupEnv: func(string) (string, bool) { return "k", true }}
	for _, kind := range []string{"anthropic", "openai", "google", "groq"} {
		b := spec.ModelBlock{Key: &spec.Ref{Ref: "env:K"}}
		p, err := modelProvider(kind, kind, b, keys)(native.Spec{Name: "a", Model: "m"})
		if err != nil || p == nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
	// A local server such as Ollama serves the OpenAI API and needs no key.
	if p, err := modelProvider("openai", "local", spec.ModelBlock{BaseURL: "http://127.0.0.1:11434/v1"}, keys)(native.Spec{Name: "a", Model: "m"}); err != nil || p == nil {
		t.Errorf("a local openai server: %v", err)
	}
	if _, err := modelProvider("anthropic", "a", spec.ModelBlock{Key: &spec.Ref{Ref: "env:K"}}, credref.Resolver{LookupEnv: func(string) (string, bool) { return "", false }})(native.Spec{Name: "a", Model: "m"}); err == nil {
		t.Error("an unset key built a provider")
	}
}

// Anthropic takes an output cap on every request. A request carries the most
// its model accepts, not the adapter's 4096, which cut a document written in
// one tool call off and ended the run with nothing.
func TestAnAnthropicRequestCarriesTheModelsOutputLimit(t *testing.T) {
	const model = "claude-sonnet-5-5"
	known, err := anthropic.New(anthropic.Config{APIKey: "k", Model: model})
	if err != nil {
		t.Fatal(err)
	}
	limit := known.Capabilities().MaxOutputTokens
	if limit <= 4096 {
		t.Fatalf("the catalog gives %s an output limit of %d", model, limit)
	}
	sent := make(chan int, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			MaxTokens int `json:"max_tokens"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		sent <- body.MaxTokens
		http.Error(w, `{"type":"error","error":{"type":"invalid_request_error","message":"stop"}}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	keys := credref.Resolver{LookupEnv: func(string) (string, bool) { return "k", true }}
	b := spec.ModelBlock{Key: &spec.Ref{Ref: "env:K"}, BaseURL: srv.URL}
	p, err := modelProvider("anthropic", "anthropic", b, keys)(native.Spec{Name: "a", Model: model})
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Stream(t.Context(), saigetypes.Request{Messages: []saigetypes.Message{saigetypes.UserMsg(saigetypes.TextPart{Text: "hi"})}})
	if err == nil {
		for range out {
		}
	}
	select {
	case got := <-sent:
		if got != limit {
			t.Fatalf("max_tokens = %d, want the model's %d", got, limit)
		}
	default:
		t.Fatal("no request reached the provider")
	}
}
