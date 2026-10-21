package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/urmzd/saige/agent/provider/anthropic"
	"github.com/urmzd/saige/agent/provider/openai"
	"github.com/urmzd/saige/agent/provider/retry"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/toolprovider"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/credref"
)

// ProvidersFile is the shape of `warp serve -providers FILE`: the model
// providers and MCP tool providers a deployment declares, so the common cases
// need no Go. It is a superset of fiber's providers file: a file written for
// `fiber` reads the same here. Unknown fields are refused, so "api_key" where
// "api_key_ref" belongs is an error rather than ignored.
//
// A key is never written in the file. A model names where its key is kept
// (api_key_ref: env:, keychain: or op://, or api_key_env as fiber spells an
// env: reference), and the key is read each time an agent is built.
type ProvidersFile struct {
	Models        []ModelProvider `json:"models"`
	ToolProviders []ToolProvider  `json:"tool_providers"`
}

// Model provider kinds.
const (
	KindOpenAI    = "openai-compatible"
	KindAnthropic = "anthropic"
)

// ModelProvider is one model endpoint, selectable by Name as a spec's provider.
type ModelProvider struct {
	Name string `json:"name"`
	// Kind is openai-compatible (the default) or anthropic.
	Kind string `json:"kind,omitempty"`
	// BaseURL is where the API is served. Empty is the vendor's own.
	BaseURL string `json:"base_url,omitempty"`
	// APIKeyRef names where the key is kept. Empty, with no APIKeyEnv, sends
	// no key, which a local server expects.
	APIKeyRef string `json:"api_key_ref,omitempty"`
	// APIKeyEnv is fiber's spelling of APIKeyRef "env:NAME".
	APIKeyEnv string `json:"api_key_env,omitempty"`
}

// ToolProvider is one MCP server and the tools the deployment accepts from it.
type ToolProvider struct {
	Name      string   `json:"name"`
	Transport string   `json:"transport"`
	Endpoint  string   `json:"endpoint"`
	Args      []string `json:"args,omitempty"`
	// CredentialRef names where the server's credential is kept, as
	// api_key_ref does. CredentialEnv is the header or variable it is sent as.
	CredentialRef string `json:"credential_ref,omitempty"`
	CredentialEnv string `json:"credential_env,omitempty"`
	// Tools are accepted, each pinned by the hash of its schema. A tool left
	// out stays pending (the log prints each pending tool's hash); a class
	// left out is write.
	Tools []AcceptedTool `json:"tools"`
}

// AcceptedTool is one pinned tool.
type AcceptedTool struct {
	Name       string `json:"name"`
	SchemaHash string `json:"schema_hash"`
	Class      string `json:"class,omitempty"`
}

var providerName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ReadProvidersFile reads and checks a providers file. An empty path, or a
// path that does not exist when missingOK, is an empty file.
func ReadProvidersFile(path string, missingOK bool) (ProvidersFile, error) {
	var f ProvidersFile
	if strings.TrimSpace(path) == "" {
		return f, nil
	}
	b, err := os.ReadFile(path)
	if missingOK && errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, fmt.Errorf("providers: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return f, fmt.Errorf("providers %s: %w", path, err)
	}
	if err := f.Check(); err != nil {
		return f, fmt.Errorf("providers %s: %w", path, err)
	}
	return f, nil
}

// WriteProvidersFile writes f where ReadProvidersFile reads it, readable only
// by its owner: it names where keys are, which is worth keeping to oneself.
func WriteProvidersFile(path string, f ProvidersFile) error {
	if err := f.Check(); err != nil {
		return err
	}
	return writeJSON(path, f)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// keyRef is the model's key reference in one spelling.
func (m ModelProvider) keyRef() string {
	if m.APIKeyRef != "" {
		return m.APIKeyRef
	}
	if m.APIKeyEnv != "" {
		return "env:" + m.APIKeyEnv
	}
	return ""
}

// Check validates every entry, so a typo stops the server at startup.
func (f ProvidersFile) Check() error {
	seen := map[string]bool{}
	for _, m := range f.Models {
		if !providerName.MatchString(m.Name) {
			return fmt.Errorf("model %q: a name is letters, digits, dots, dashes and underscores", m.Name)
		}
		if seen[m.Name] {
			return fmt.Errorf("model %q is declared twice", m.Name)
		}
		seen[m.Name] = true
		switch m.Kind {
		case "", KindOpenAI, KindAnthropic:
		default:
			return fmt.Errorf("model %q: kind is %s or %s", m.Name, KindOpenAI, KindAnthropic)
		}
		if m.APIKeyRef != "" && m.APIKeyEnv != "" {
			return fmt.Errorf("model %q: set api_key_ref or api_key_env, not both", m.Name)
		}
		if ref := m.keyRef(); ref != "" {
			if _, err := credref.Parse(ref); err != nil {
				return fmt.Errorf("model %q: %w", m.Name, err)
			}
		}
		if m.BaseURL != "" {
			u, err := url.Parse(m.BaseURL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return fmt.Errorf("model %q: base_url must be an http or https URL", m.Name)
			}
			if u.User != nil {
				return fmt.Errorf("model %q: base_url carries credentials; use api_key_ref", m.Name)
			}
		}
		if m.BaseURL == "" && m.keyRef() == "" {
			return fmt.Errorf("model %q: a hosted API needs api_key_ref", m.Name)
		}
	}
	seen = map[string]bool{}
	for _, tp := range f.ToolProviders {
		if seen[tp.Name] {
			return fmt.Errorf("tool provider %q is declared twice", tp.Name)
		}
		seen[tp.Name] = true
		if err := tp.provider("check").Validate(); err != nil {
			return err
		}
		if tp.CredentialRef != "" {
			if _, err := credref.Parse(tp.CredentialRef); err != nil {
				return fmt.Errorf("tool provider %q: %w", tp.Name, err)
			}
		}
		for _, t := range tp.Tools {
			switch {
			case t.Name == "":
				return fmt.Errorf("tool provider %q declares a tool with no name", tp.Name)
			case t.SchemaHash == "":
				return fmt.Errorf("tool provider %q: tool %q has no schema_hash; pending tools are logged with theirs", tp.Name, t.Name)
			}
		}
	}
	return nil
}

func (tp ToolProvider) provider(tenant string) toolprovider.Provider {
	return toolprovider.Provider{
		Tenant: tenant, Name: tp.Name, Transport: toolprovider.Transport(tp.Transport), Endpoint: tp.Endpoint,
		Args: tp.Args, CredentialRef: tp.CredentialRef, CredentialEnv: tp.CredentialEnv,
	}
}

// Extensions turns the file into what the platform adds, for tenant.
func (f ProvidersFile) Extensions(tenant string, keys credref.Resolver) platform.Extensions {
	ext := platform.Extensions{Providers: map[string]agentrt.ProviderFunc{}}
	for _, m := range f.Models {
		ext.Providers[m.Name] = modelProvider(m, keys)
	}
	for _, tp := range f.ToolProviders {
		seed := platform.ToolProviderSeed{Provider: tp.provider(tenant)}
		for _, t := range tp.Tools {
			seed.Accept = append(seed.Accept, toolprovider.Tool{Name: t.Name, SchemaHash: t.SchemaHash, Class: toolprovider.Class(t.Class)})
		}
		ext.ToolProviders = append(ext.ToolProviders, seed)
	}
	return ext
}

// modelProvider builds the provider for one declared model. The key is read
// when an agent is built, which is every turn, and is held only by the
// adapter that turn uses.
func modelProvider(m ModelProvider, keys credref.Resolver) agentrt.ProviderFunc {
	return func(spec agentrt.Spec) (saigetypes.Provider, error) {
		if spec.Model == "" {
			return nil, fmt.Errorf("%w: agent %q asks for the %s provider with no model", agentrt.ErrInvalid, spec.Name, m.Name)
		}
		var key string
		if ref := m.keyRef(); ref != "" {
			v, err := keys.Resolve(context.Background(), ref)
			if err != nil {
				return nil, fmt.Errorf("%w: agent %q asks for the %s provider: %v", agentrt.ErrInvalid, spec.Name, m.Name, err)
			}
			key = v
		}
		var (
			p   saigetypes.Provider
			err error
		)
		switch m.Kind {
		case KindAnthropic:
			var opts []anthropic.Option
			if m.BaseURL != "" {
				opts = append(opts, anthropic.WithBaseURL(m.BaseURL))
			}
			p, err = anthropic.New(anthropic.Config{APIKey: key, Model: saigetypes.ModelID(spec.Model)}, opts...)
		default:
			base := m.BaseURL
			if base == "" {
				base = agentrt.DefaultOpenAIBaseURL
			}
			p, err = openai.New(openai.Config{APIKey: key, Model: saigetypes.ModelID(spec.Model)}, openai.WithBaseURL(base))
		}
		if err != nil {
			return nil, fmt.Errorf("%w: agent %q: %v", agentrt.ErrInvalid, spec.Name, err)
		}
		return retry.New(p, retry.DefaultConfig())
	}
}
