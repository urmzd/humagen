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

	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/spec"
)

// The model kinds are built on saige's adapters, as fiber's built-in
// providers are, so a workspace model named like a built-in behaves like it:
// Anthropic sends prompt cache breakpoints (the short lifetime fiber uses by
// default) and Google reaches Gemini or Vertex AI. A local server such as
// Ollama serves the OpenAI API, so it is openai, and so is groq, whose block
// arrives with Groq's base URL and key filled in (spec.ModelDefaults). When fiber exposes its provider
// construction (an Endpoint with a key function), this file becomes a call
// to it.

// modelProvider builds a built-in model kind. The key is read when an agent
// is built, which is every turn, and is held only by the adapter that turn
// uses.
func modelProvider(kind, name string, b spec.ModelBlock, keys credref.Resolver) native.ProviderFunc {
	return modelProviderKey(kind, name, b, refKey(b, keys))
}

// refKey reads a declared model's key reference with keys; a model with no
// reference has no key.
func refKey(b spec.ModelBlock, keys credref.Resolver) platform.KeyFunc {
	return func(ctx context.Context) (string, error) {
		if !b.Key.Set() {
			return "", nil
		}
		return keys.Resolve(ctx, b.Key.Ref)
	}
}

// workspaceModel builds a provider a workspace's administrators set up
// (platform.ModelKinds.Build), with its key read by the platform.
func workspaceModel(kind, name string, b spec.ModelBlock, key platform.KeyFunc) native.ProviderFunc {
	return modelProviderKey(kind, name, spec.ModelDefaults(kind, b), key)
}

// workspaceProbe checks a provider a workspace set up, spending no tokens
// (platform.ModelKinds.Probe).
func workspaceProbe(kind string, b spec.ModelBlock, key platform.KeyFunc) platform.Probe {
	return modelProbeKey(kind, spec.ModelDefaults(kind, b), key)
}

// modelProviderKey is modelProvider with its key read by key, when a turn
// builds the provider.
func modelProviderKey(kind, name string, b spec.ModelBlock, keyOf platform.KeyFunc) native.ProviderFunc {
	return func(s native.Spec) (saigetypes.Provider, error) {
		if s.Model == "" {
			return nil, fmt.Errorf("%w: agent %q asks for the %s provider with no model", native.ErrInvalid, s.Name, name)
		}
		key, err := keyOf(context.Background())
		if err != nil {
			return nil, fmt.Errorf("%w: agent %q asks for the %s provider: %v", native.ErrInvalid, s.Name, name, err)
		}
		var p saigetypes.Provider
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
	return modelProbeKey(kind, b, func(ctx context.Context) (string, error) {
		if !b.Key.Set() {
			return "", nil
		}
		v, err := keys.Resolve(ctx, b.Key.Ref)
		if err != nil || v == "" {
			return "", fmt.Errorf("key %s is not set", b.Key.Ref)
		}
		return v, nil
	})
}

// modelProbeKey is modelProbe with its key read by keyOf.
func modelProbeKey(kind string, b spec.ModelBlock, keyOf platform.KeyFunc) platform.Probe {
	if b.Vertex != nil {
		return nil
	}
	return func(ctx context.Context) error {
		key, err := keyOf(ctx)
		if err != nil {
			return err
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

// declaredKinds is the kind of each model the spec's Platform documents
// declare, by name, so the console can show its logo. A kind that speaks the
// OpenAI API, such as groq, is openai.
func declaredKinds(cfg spec.Config) map[string]string {
	out := map[string]string{}
	for _, p := range cfg.Platforms {
		for _, m := range p.Spec.Models {
			switch m.Kind {
			case "anthropic", "google", "openai":
				out[m.Name] = m.Kind
			case "groq":
				out[m.Name] = "openai"
			}
		}
	}
	return out
}
