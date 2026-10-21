package spec

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/urmzd/fiber/pkg/agentspec"
)

// Platform is what one workspace offers. Metadata.Name is its tenant.
type Platform struct {
	Metadata Metadata     `json:"metadata"`
	Spec     PlatformSpec `json:"spec"`
}

// PlatformSpec is a Platform document's spec.
type PlatformSpec struct {
	Workspace     Workspace     `json:"workspace,omitzero"`
	Models        []Model       `json:"models,omitempty"`
	ModelPolicies []ModelPolicy `json:"model_policies,omitempty"`
	Tools         []Tool        `json:"tools,omitempty"`
	Agents        []Agent       `json:"agents,omitempty"`
	Surfaces      Surfaces      `json:"surfaces,omitzero"`
}

// Workspace is the workspace's settings. A field left out is left to the
// console; a field set here is applied at every start.
type Workspace struct {
	Baseline    *string   `json:"baseline,omitempty"`
	History     *History  `json:"history,omitempty"`
	TitleAgent  *string   `json:"title_agent,omitempty"`
	MemoryAgent *string   `json:"memory_agent,omitempty"`
	Projects    []Project `json:"projects,omitempty"`
}

// History is how much of a conversation the next turn sees.
type History struct {
	Mode  string `json:"mode,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

// Project files conversations together.
type Project struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Model is a model provider agents name as their provider: a slot
// (anthropic, openai, ollama) with its name.
type Model struct {
	Slot
	ModelShared
}

// ModelShared is what every model kind has.
type ModelShared struct {
	Name string `json:"name"`
}

func (m *Model) UnmarshalJSON(b []byte) error { return unmarshalSlot(b, &m.Slot, &m.ModelShared) }
func (m Model) MarshalJSON() ([]byte, error)  { return marshalSlot(m.Slot, m.ModelShared) }

// ModelBlock is the built-in model kinds' block.
type ModelBlock struct {
	BaseURL string `json:"base_url,omitempty"`
	Key     *Ref   `json:"key,omitempty"`
}

// ModelPolicy is a named, ordered list of models and when a call moves on.
type ModelPolicy struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Models      []agentspec.ModelRef `json:"models"`
	FallbackOn  []string             `json:"fallback_on,omitempty"`
}

// Tool is a tool provider agents reach with an "mcp:<name>" grant: a slot
// (mcp) with its name.
type Tool struct {
	Slot
	ToolShared
}

// ToolShared is what every tool kind has.
type ToolShared struct {
	Name string `json:"name"`
}

func (t *Tool) UnmarshalJSON(b []byte) error { return unmarshalSlot(b, &t.Slot, &t.ToolShared) }
func (t Tool) MarshalJSON() ([]byte, error)  { return marshalSlot(t.Slot, t.ToolShared) }

// MCP is the mcp tool kind's block.
type MCP struct {
	Transport  string   `json:"transport"`
	Endpoint   string   `json:"endpoint"`
	Args       []string `json:"args,omitempty"`
	Credential *Ref     `json:"credential,omitempty"`
	// CredentialAs is the variable (stdio) or header (http) the credential
	// is sent as. Empty on http is a bearer token.
	CredentialAs string `json:"credential_as,omitempty"`
	// Accept pins the tools taken from the server by schema hash. A tool
	// left out, or changed, stays pending.
	Accept []AcceptTool `json:"accept,omitempty"`
}

// AcceptTool is one pinned tool.
type AcceptTool struct {
	Name       string `json:"name"`
	SchemaHash string `json:"schema_hash"`
	Class      string `json:"class,omitempty"`
}

// Agent is one agent, from a file or written inline in the agent JSON fiber
// reads (docs/agents.md).
type Agent struct {
	File string               `json:"file,omitempty"`
	Spec *agentspec.AgentSpec `json:"spec,omitempty"`
}

// Surfaces are where the workspace answers besides the console.
type Surfaces struct {
	Slack  *Slack  `json:"slack,omitempty"`
	GitHub *GitHub `json:"github,omitempty"`
}

// Slack runs the Slack app, a client of the server over Socket Mode.
type Slack struct {
	Enabled   bool     `json:"enabled"`
	Team      string   `json:"team"`
	BotToken  *Ref     `json:"bot_token"`
	AppToken  *Ref     `json:"app_token"`
	Operators []string `json:"operators,omitempty"`
	// AppName is the app's name in Slack. Empty is the workspace's or the
	// deployment's display name.
	AppName string `json:"app_name,omitempty"`
	Command string `json:"command,omitempty"`
}

// GitHub is a GitHub App installation answering through signed webhooks.
type GitHub struct {
	Enabled       bool     `json:"enabled"`
	Owners        []string `json:"owners"`
	App           string   `json:"app,omitempty"`
	Installation  string   `json:"installation,omitempty"`
	Token         *Ref     `json:"token"`
	WebhookSecret *Ref     `json:"webhook_secret"`
	ReadRepos     []string `json:"read_repositories,omitempty"`
	Checks        bool     `json:"checks,omitempty"`
	Guides        bool     `json:"guides,omitempty"`
	Mutations     bool     `json:"mutations,omitempty"`
}

var providerName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func (p Platform) check(kinds Kinds) error {
	where := fmt.Sprintf("platform %q", p.Metadata.Name)
	if !identifier.MatchString(p.Metadata.Name) {
		return fmt.Errorf("%s: metadata.name is the tenant: lowercase letters, digits and dashes", where)
	}
	sp := p.Spec
	if h := sp.Workspace.History; h != nil && h.Mode != "" && h.Mode != "recent" && h.Mode != "none" {
		return fmt.Errorf("%s: workspace.history.mode is recent or none", where)
	}
	seen := map[string]bool{}
	for i, m := range sp.Models {
		if !providerName.MatchString(m.Name) || seen["m/"+m.Name] {
			return fmt.Errorf("%s: models[%d]: a unique name of letters, digits, dots, dashes and underscores", where, i)
		}
		seen["m/"+m.Name] = true
		if m.Kind == "" {
			return fmt.Errorf("%s: models[%d] %q: kind is required", where, i, m.Name)
		}
		if err := kinds.check(SlotModels, m.Slot); err != nil {
			return fmt.Errorf("%s: models[%d] %q: %w", where, i, m.Name, err)
		}
		if blk, ok := builtinBlock(SlotModels, m.Kind); ok {
			_ = m.Decode(blk)
			if err := checkModel(m.Kind, *blk.(*ModelBlock)); err != nil {
				return fmt.Errorf("%s: models[%d] %q: %w", where, i, m.Name, err)
			}
		}
	}
	for i, t := range sp.Tools {
		if !providerName.MatchString(t.Name) || seen["t/"+t.Name] {
			return fmt.Errorf("%s: tools[%d]: a unique name of letters, digits, dots, dashes and underscores", where, i)
		}
		seen["t/"+t.Name] = true
		if t.Kind == "" {
			return fmt.Errorf("%s: tools[%d] %q: kind is required", where, i, t.Name)
		}
		if err := kinds.check(SlotTools, t.Slot); err != nil {
			return fmt.Errorf("%s: tools[%d] %q: %w", where, i, t.Name, err)
		}
		if t.Kind == "mcp" {
			var b MCP
			_ = t.Decode(&b)
			for _, a := range b.Accept {
				if a.Name == "" || a.SchemaHash == "" {
					return fmt.Errorf("%s: tools[%d] %q: every accepted tool needs a name and a schema_hash; pending tools are logged with theirs", where, i, t.Name)
				}
			}
		}
	}
	for i, a := range sp.Agents {
		if (a.File == "") == (a.Spec == nil) {
			return fmt.Errorf("%s: agents[%d]: file or spec, exactly one", where, i)
		}
	}
	if s := sp.Surfaces.Slack; s != nil && s.Enabled {
		if s.Team == "" || !s.BotToken.Set() || !s.AppToken.Set() {
			return fmt.Errorf("%s: surfaces.slack: team, bot_token and app_token are required", where)
		}
		if s.Command != "" && !strings.HasPrefix(s.Command, "/") {
			return fmt.Errorf("%s: surfaces.slack.command starts with /", where)
		}
	}
	if g := sp.Surfaces.GitHub; g != nil && g.Enabled {
		if len(g.Owners) == 0 || !g.Token.Set() || !g.WebhookSecret.Set() {
			return fmt.Errorf("%s: surfaces.github: owners, token and webhook_secret are required", where)
		}
	}
	return nil
}

func checkModel(kind string, b ModelBlock) error {
	if b.BaseURL != "" {
		u, err := url.Parse(b.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("base_url must be an http or https URL")
		}
		if u.User != nil {
			return fmt.Errorf("base_url carries credentials; use key")
		}
	}
	if kind != "ollama" && b.BaseURL == "" && !b.Key.Set() {
		return fmt.Errorf("a hosted API needs key, as {ref: env:NAME}")
	}
	return nil
}

// Check validates a config against the kinds a binary registered, without
// building anything. Nil kinds lets through any kind that is not built in.
func (c Config) Check(kinds Kinds) error {
	if err := c.System.check(kinds); err != nil {
		return err
	}
	models := map[string]string{}
	for _, p := range c.Platforms {
		if err := p.check(kinds); err != nil {
			return err
		}
		// Model providers are named process-wide: two workspaces may share
		// one, but not declare it two ways.
		for _, m := range p.Spec.Models {
			raw, _ := json.Marshal(m)
			if prev, ok := models[m.Name]; ok && prev != string(raw) {
				return fmt.Errorf("platform %q: model %q is declared differently by another workspace; model names are shared across workspaces", p.Metadata.Name, m.Name)
			}
			models[m.Name] = string(raw)
		}
	}
	if t := c.System.Tenant(); t != "" {
		for _, p := range c.Platforms {
			if p.Metadata.Name != t {
				return fmt.Errorf("platform %q: sign-in confines every caller to %q, so this workspace cannot be reached; use auth.tenant.claim for more than one", p.Metadata.Name, t)
			}
		}
	}
	return nil
}

// check is Load's check, on a copy with the defaults applied. Load cannot
// know which kinds a binary registered, so it lets any kind that is not built
// in through; the binary checks again with its own.
func (c Config) check() error {
	c.System.Defaults()
	return c.Check(nil)
}
