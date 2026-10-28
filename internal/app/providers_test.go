package app

import (
	"testing"

	"github.com/urmzd/humagen/fiber/pkg/turn/native"

	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/spec"
)

func TestEveryModelKindBuildsWithItsKey(t *testing.T) {
	keys := credref.Resolver{LookupEnv: func(string) (string, bool) { return "k", true }}
	for _, kind := range []string{"anthropic", "openai", "ollama"} {
		b := spec.ModelBlock{Key: &spec.Ref{Ref: "env:K"}}
		if kind == "ollama" {
			b = spec.ModelBlock{BaseURL: "http://127.0.0.1:11434"}
		}
		p, err := modelProvider(kind, kind, b, keys)(native.Spec{Name: "a", Model: "m"})
		if err != nil || p == nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
	if _, err := modelProvider("anthropic", "a", spec.ModelBlock{Key: &spec.Ref{Ref: "env:K"}}, credref.Resolver{LookupEnv: func(string) (string, bool) { return "", false }})(native.Spec{Name: "a", Model: "m"}); err == nil {
		t.Error("an unset key built a provider")
	}
}
