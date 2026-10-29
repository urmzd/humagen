package app

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/urmzd/saige/agent/provider/anthropic"
	"github.com/urmzd/saige/agent/provider/google"
	"github.com/urmzd/saige/agent/provider/openai"
	"github.com/urmzd/saige/agent/provider/retry"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/fiber/pkg/turn/native"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/spec"
)

// The model kinds are built on saige's adapters, as fiber's built-in
// providers are, so a workspace model named like a built-in behaves like it:
// Anthropic sends prompt cache breakpoints (the short lifetime fiber uses by
// default) and Google reaches Gemini or Vertex AI. A local server such as
// Ollama serves the OpenAI API, so it is openai. When fiber exposes its provider
// construction (an Endpoint with a key function), this file becomes a call
// to it.

// modelProvider builds a built-in model kind. The key is read when an agent
// is built, which is every turn, and is held only by the adapter that turn
// uses.
func modelProvider(kind, name string, b spec.ModelBlock, keys credref.Resolver) native.ProviderFunc {
	return func(s native.Spec) (saigetypes.Provider, error) {
		if s.Model == "" {
			return nil, fmt.Errorf("%w: agent %q asks for the %s provider with no model", native.ErrInvalid, s.Name, name)
		}
		var key string
		if b.Key.Set() {
			v, err := keys.Resolve(context.Background(), b.Key.Ref)
			if err != nil {
				return nil, fmt.Errorf("%w: agent %q asks for the %s provider: %v", native.ErrInvalid, s.Name, name, err)
			}
			key = v
		}
		var (
			p   saigetypes.Provider
			err error
		)
		switch kind {
		case "anthropic":
			var opts []anthropic.Option
			if b.BaseURL != "" {
				opts = append(opts, anthropic.WithBaseURL(b.BaseURL))
			}
			cfg := anthropic.Config{APIKey: key, Model: saigetypes.ModelID(s.Model)}
			var a *anthropic.Adapter
			if a, err = anthropic.New(cfg, opts...); err == nil && a.Capabilities().Supports(saigetypes.CapPromptCacheMarkers) {
				a, err = anthropic.New(cfg, append(opts, anthropic.WithPromptCachePolicy(anthropic.DefaultPromptCachePolicy(native.PromptCacheShort)))...)
			}
			p = a
		case "google":
			var opts []google.Option
			if b.Vertex != nil {
				opts = append(opts, google.WithVertex(b.Vertex.Project, b.Vertex.Location))
			}
			p, err = google.New(context.Background(), google.Config{APIKey: key, Model: saigetypes.ModelID(s.Model)}, opts...)
		default:
			base := b.BaseURL
			if base == "" {
				base = native.DefaultOpenAIBaseURL
			}
			p, err = openai.New(openai.Config{APIKey: key, Model: saigetypes.ModelID(s.Model)}, openai.WithBaseURL(base))
		}
		if err != nil {
			return nil, fmt.Errorf("%w: agent %q: %v", native.ErrInvalid, s.Name, err)
		}
		return retry.New(p, retry.DefaultConfig())
	}
}

// modelProbe checks a declared model for Health by listing the provider's
// models with its key: it proves the address answers and the key is
// accepted, and spends no tokens. Vertex has no check, since its
// credentials are the environment's, not a key.
func modelProbe(kind string, b spec.ModelBlock, keys credref.Resolver) platform.Probe {
	if b.Vertex != nil {
		return nil
	}
	return func(ctx context.Context) error {
		var key string
		if b.Key.Set() {
			v, err := keys.Resolve(ctx, b.Key.Ref)
			if err != nil || v == "" {
				return fmt.Errorf("key %s is not set", b.Key.Ref)
			}
			key = v
		}
		var (
			url    string
			header = http.Header{}
		)
		switch kind {
		case "anthropic":
			url = strings.TrimSuffix(cmp.Or(b.BaseURL, "https://api.anthropic.com"), "/") + "/v1/models?limit=1"
			header.Set("x-api-key", key)
			header.Set("anthropic-version", "2023-06-01")
		case "google":
			url = "https://generativelanguage.googleapis.com/v1beta/models?pageSize=1"
			header.Set("x-goog-api-key", key)
		default:
			url = strings.TrimSuffix(cmp.Or(b.BaseURL, native.DefaultOpenAIBaseURL), "/") + "/models"
			if key != "" {
				header.Set("Authorization", "Bearer "+key)
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header = header
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("unreachable: %v", err)
		}
		_ = res.Body.Close()
		switch {
		case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
			return fmt.Errorf("the key was refused (%d)", res.StatusCode)
		case res.StatusCode >= 400:
			return fmt.Errorf("answered %d", res.StatusCode)
		}
		return nil
	}
}
