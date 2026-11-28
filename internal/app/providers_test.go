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

// toolsSent answers every request with a refusal and reports what the one
// tool of each request carried as eager_input_streaming.
func toolsSent(t *testing.T) (*httptest.Server, chan any) {
	t.Helper()
	seen := make(chan any, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []map[string]any `json:"tools"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		if len(body.Tools) == 1 {
			seen <- body.Tools[0]["eager_input_streaming"]
		}
		http.Error(w, `{"type":"error","error":{"type":"invalid_request_error","message":"stop"}}`, http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	return srv, seen
}

func askWithATool(t *testing.T, p saigetypes.Provider) {
	t.Helper()
	out, err := p.Stream(t.Context(), saigetypes.Request{
		Messages: []saigetypes.Message{saigetypes.UserMsg(saigetypes.TextPart{Text: "hi"})},
		Tools:    []saigetypes.ToolDef{{Name: "write", Parameters: saigetypes.ParameterSchema{Type: "object"}}},
	})
	if err == nil {
		for range out {
		}
	}
}

// A tool's input is asked for as it is written from Anthropic itself, and not
// from a gateway, which may not know the field: one the model block names, or
// one the environment names for the SDK.
func TestToolInputIsStreamedAsWrittenFromAnthropicAndNotFromAGateway(t *testing.T) {
	const model = "claude-sonnet-5-5"
	keys := credref.Resolver{LookupEnv: func(string) (string, bool) { return "k", true }}

	// Anthropic's own address: the request carries the field. The test's
	// server stands in for it without making the block a gateway.
	srv, seen := toolsSent(t)
	own, err := anthropicAdapter(anthropic.Config{APIKey: "k", Model: model}, spec.ModelBlock{}, "", anthropic.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	askWithATool(t, own)
	select {
	case got := <-seen:
		if got != true {
			t.Fatalf("Anthropic's own address was sent eager_input_streaming = %v, want true", got)
		}
	default:
		t.Fatal("no request with the tool was sent")
	}

	// A gateway the block names.
	srv, seen = toolsSent(t)
	p, err := modelProvider("anthropic", "anthropic", spec.ModelBlock{Key: &spec.Ref{Ref: "env:K"}, BaseURL: srv.URL}, keys)(native.Spec{Name: "a", Model: model})
	if err != nil {
		t.Fatal(err)
	}
	askWithATool(t, p)
	select {
	case got := <-seen:
		if got != nil {
			t.Fatalf("a gateway the block names was sent eager_input_streaming = %v", got)
		}
	default:
		t.Fatal("no request with the tool reached the gateway")
	}

	// A gateway the environment names, with no base_url on the block.
	srv, seen = toolsSent(t)
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
	p, err = modelProvider("anthropic", "anthropic", spec.ModelBlock{Key: &spec.Ref{Ref: "env:K"}}, keys)(native.Spec{Name: "a", Model: model})
	if err != nil {
		t.Fatal(err)
	}
	askWithATool(t, p)
	select {
	case got := <-seen:
		if got != nil {
			t.Fatalf("a gateway the environment names was sent eager_input_streaming = %v", got)
		}
	default:
		t.Fatal("no request with the tool reached the gateway the environment names")
	}
}
