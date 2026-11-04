package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urmzd/orchagent/flow/turn/native"

	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/spec"
	"github.com/urmzd/humagen/pkg/speech"
)

func TestTheSpecChoosesTheTranscriber(t *testing.T) {
	ctx := context.Background()
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "hello", "language": "english"})
	}))
	defer srv.Close()
	keys := credref.Resolver{LookupEnv: func(k string) (string, bool) { return "gsk_" + k, k == "GROQ_API_KEY" }}

	if tr, err := openSpeech(ctx, Options{}, loaded{}, keys); err != nil || tr != nil {
		t.Fatalf("left out: %v %v", tr, err)
	}
	var l loaded
	l.cfg.System.Spec.Speech = spec.Slot{Kind: "groq", Block: spec.MustJSON(spec.GroqSpeech{BaseURL: srv.URL})}
	tr, err := openSpeech(ctx, Options{}, l, keys)
	if err != nil || tr == nil {
		t.Fatalf("groq: %v %v", tr, err)
	}
	text, _, err := tr.Transcribe(ctx, strings.NewReader("x"), "audio/ogg")
	if err != nil || text != "hello" || auth != "Bearer gsk_GROQ_API_KEY" {
		t.Fatalf("groq with GROQ_API_KEY by default: %q %q %v", text, auth, err)
	}
	if _, ok := tr.(speech.Synthesizer); !ok {
		t.Fatal("groq does not speak")
	}
}

// A groq model is the OpenAI-compatible adapter on Groq's API, keyed by
// GROQ_API_KEY unless the block names another reference.
func TestAGroqModelIsOpenAIOnGroq(t *testing.T) {
	b := spec.ModelDefaults("groq", spec.ModelBlock{})
	if b.BaseURL != "https://api.groq.com/openai/v1" || b.Key.Ref != "env:GROQ_API_KEY" {
		t.Fatalf("defaults = %+v %+v", b, b.Key)
	}
	own := spec.ModelDefaults("groq", spec.ModelBlock{Key: &spec.Ref{Ref: "env:MY_GROQ"}, BaseURL: "http://127.0.0.1:1/v1"})
	if own.Key.Ref != "env:MY_GROQ" || own.BaseURL != "http://127.0.0.1:1/v1" {
		t.Fatalf("a block's own key and base_url were replaced: %+v", own)
	}
	if b := spec.ModelDefaults("openai", spec.ModelBlock{}); b.BaseURL != "" || b.Key != nil {
		t.Fatalf("openai took groq's defaults: %+v", b)
	}
	keys := credref.Resolver{LookupEnv: func(k string) (string, bool) { return "gsk", k == "GROQ_API_KEY" }}
	if p, err := modelProvider("groq", "groq", b, keys)(native.Spec{Name: "a", Model: "llama-3.3-70b-versatile"}); err != nil || p == nil {
		t.Fatalf("groq: %v", err)
	}
	var key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/models" {
			t.Errorf("probe path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	probe := modelProbe("groq", spec.ModelDefaults("groq", spec.ModelBlock{BaseURL: srv.URL + "/v1"}), keys)
	if err := probe(context.Background()); err != nil || key != "Bearer gsk" {
		t.Fatalf("probe: %v %q", err, key)
	}
}
