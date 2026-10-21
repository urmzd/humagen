package app

import (
	"context"
	"fmt"

	"github.com/urmzd/saige/agent/provider/anthropic"
	"github.com/urmzd/saige/agent/provider/openai"
	"github.com/urmzd/saige/agent/provider/retry"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/fiber/pkg/agentrt"

	"github.com/urmzd/warp/pkg/credref"
	"github.com/urmzd/warp/pkg/spec"
)

// ollamaBaseURL is where a local Ollama serves its OpenAI-compatible API.
const ollamaBaseURL = "http://127.0.0.1:11434/v1"

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
			p, err = anthropic.New(anthropic.Config{APIKey: key, Model: saigetypes.ModelID(s.Model)}, opts...)
		default:
			base := b.BaseURL
			switch {
			case base == "" && kind == "ollama":
				base = ollamaBaseURL
			case base == "":
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
