package app

import (
	"context"
	"fmt"

	"github.com/urmzd/saige/agent/provider/anthropic"
	"github.com/urmzd/saige/agent/provider/ollama"
	"github.com/urmzd/saige/agent/provider/openai"
	"github.com/urmzd/saige/agent/provider/retry"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/fiber/pkg/agentrt"

	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/spec"
)

// The model kinds are built on saige's adapters, as fiber's built-in
// providers are, so a workspace model named like a built-in behaves like it:
// Anthropic sends prompt cache breakpoints (the short lifetime fiber uses by
// default) and Ollama uses its native API. When fiber exposes its provider
// construction (an Endpoint with a key function), this file becomes a call
// to it.

// modelProvider builds a built-in model kind. The key is read when an agent
// is built, which is every turn, and is held only by the adapter that turn
// uses.
func modelProvider(kind, name string, b spec.ModelBlock, keys credref.Resolver) agentrt.ProviderFunc {
	return func(s agentrt.Spec) (saigetypes.Provider, error) {
		if s.Model == "" {
			return nil, fmt.Errorf("%w: agent %q asks for the %s provider with no model", agentrt.ErrInvalid, s.Name, name)
		}
		var key string
		if b.Key.Set() {
			v, err := keys.Resolve(context.Background(), b.Key.Ref)
			if err != nil {
				return nil, fmt.Errorf("%w: agent %q asks for the %s provider: %v", agentrt.ErrInvalid, s.Name, name, err)
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
				a, err = anthropic.New(cfg, append(opts, anthropic.WithPromptCachePolicy(anthropic.DefaultPromptCachePolicy(agentrt.PromptCacheShort)))...)
			}
			p = a
		case "ollama":
			// The native API, at the daemon's address (no /v1).
			p, err = ollama.New(ollama.Config{Host: b.BaseURL, Model: saigetypes.ModelID(s.Model)})
		default:
			base := b.BaseURL
			if base == "" {
				base = agentrt.DefaultOpenAIBaseURL
			}
			p, err = openai.New(openai.Config{APIKey: key, Model: saigetypes.ModelID(s.Model)}, openai.WithBaseURL(base))
		}
		if err != nil {
			return nil, fmt.Errorf("%w: agent %q: %v", agentrt.ErrInvalid, s.Name, err)
		}
		return retry.New(p, retry.DefaultConfig())
	}
}
