package spec

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/toolprovider"
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

// Workspace is the workspace's settings and its project tree. A field left
// out is left to the console; a field set here is applied at every start.
type Workspace struct {
	// Baseline is gone: workspace.agents.default replaces it. It is read
	// only so that a file still setting it is refused with where it went.
	Baseline    *string  `json:"baseline,omitempty"`
	History     *History `json:"history,omitempty"`
	TitleAgent  *string  `json:"title_agent,omitempty"`
	MemoryAgent *string  `json:"memory_agent,omitempty"`
	// Instructions are the workspace's own, the root project's: sent first
	// to every agent answering anywhere in it. Set here, the console cannot
	// change them.
	Instructions *string `json:"instructions,omitempty"`
	// Agents are the workspace's own, the root project's: which agents are
	// available everywhere, and the workspace default, which answers a
	// message that tags no agent. Set here, the console cannot change them.
	Agents *Agents `json:"agents,omitempty"`
	// Members are who holds a role in the workspace itself, the root
	// project. Set here, the console cannot change them. Left out, everyone
	// is a member, and projects:govern owns it.
	Members []Member `json:"members,omitempty"`
	// ProjectKinds are the workspace's own kinds, each a name over team or
	// account. Set here, the console cannot change them.
	ProjectKinds []ProjectKind `json:"project_kinds,omitempty"`
	// Projects are merged into the tree at every start, parents first.
	Projects []Project `json:"projects,omitempty"`
	// Channels are the workspace's own, the root project's, besides
	// general, which every project has.
	Channels []Channel `json:"channels,omitempty"`
	// Experts are who answers for some topics anywhere in the workspace,
	// unless a project nearer names someone for the topic. Set here, even
	// as [], the console cannot change them.
	Experts []Expert `json:"experts,omitempty"`
	// AgentPosts is what becomes of what an agent scheduled in the
	// workspace itself posts: publish, or review (the default) by its
	// owners. Set here, the console cannot change it.
	AgentPosts *string `json:"agent_posts,omitempty"`
	// Knowledge is the knowledge sources every run in the workspace is
	// grounded in, besides its agent's own. Set here, even as [], the
	// console cannot change them.
	Knowledge []string `json:"knowledge,omitempty"`
	// PersonCredentials is what people may give for the tools that take
	// their own credential.
	PersonCredentials *PersonCredentials `json:"person_credentials,omitempty"`
	// ReleaseNotes posts what each release of warp brings in the
	// workspace's own inbox, from warp: the breaking changes and features
	// the changelog lists for the release this process runs, once a
	// release. Off by default.
	ReleaseNotes bool `json:"release_notes,omitempty"`
}

// PersonCredentials is what a workspace's people may give for a
// person-scoped tool. A reference is always theirs to give, from where the
// operator allows (System person_refs). Values are off by default.
type PersonCredentials struct {
	// Values lets a person store the credential itself with warp, kept as a
	// client's secret is. Off, they may only give a reference to where it
	// is kept, and nothing of theirs is stored but that name.
	Values bool `json:"values,omitempty"`
}

// Member is one entry of a project's members: who, "*" for everyone,
// "role:<name>" for the holders of a policy role, or a subject; and the role
// it grants, viewer, member or owner.
type Member struct {
	Who  string `json:"who"`
	Role string `json:"role"`
}

// ProjectKind is a workspace's own kind: a name, what people see, and the
// built-in kind it behaves as, team or account.
type ProjectKind struct {
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
	Like  string `json:"like"`
}

// Agents is what a project turns on: which agents are available there and
// below, and which answers a message that tags none (docs/projects.md).
type Agents struct {
	// Enable is platform agents available here and below: agents in this
	// file or the platform's own. "*" alone is every platform agent. It
	// adds to what the projects above enable.
	Enable []string `json:"enable,omitempty"`
	// Allow, when set, is the only agents that may run here and below. A
	// project below can only narrow it. Left out, it narrows nothing.
	Allow []string `json:"allow,omitempty"`
	// Default answers a message that tags no agent, here and below: an
	// agent available here, or auto for @auto. Left out, it inherits.
	Default string `json:"default,omitempty"`
}

// History is how much of a conversation the next turn sees.
type History struct {
	Mode  string `json:"mode,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

// Project is one project in the workspace's tree. A field set here is the
// file's, and the console cannot change it; a field left out is the
// console's.
type Project struct {
	// Name is the project's identity and never changes: letters, digits,
	// spaces, dashes and underscores, unique ignoring case.
	Name string `json:"name"`
	// Title is what people see. Empty shows the name.
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	// Kind is company, team or account. Left out, a new project is a team.
	Kind string `json:"kind,omitempty"`
	// Parent is another project in this file. Left out, a new project sits
	// at the top, under the workspace.
	Parent string `json:"parent,omitempty"`
	// Account is what an account project records about its customer.
	Account *Account `json:"account,omitempty"`
	// Instructions are sent to every agent answering here and below.
	Instructions string `json:"instructions,omitempty"`
	// Members are who holds a role here. Left out, they are the console's:
	// a new project has everyone as a member (a viewer of an account) and
	// projects:govern as its owner. Until reads are confined to members,
	// they hold an entry for everyone ("*") of at least viewer.
	Members []Member `json:"members,omitempty"`
	// Agents is what the project turns on. Set here, the console cannot
	// change them.
	Agents *Agents `json:"agents,omitempty"`
	// Channels are the project's, besides general.
	Channels []Channel `json:"channels,omitempty"`
	// Experts are who answers for some topics here and below. Set here,
	// even as [], the console cannot change them.
	Experts []Expert `json:"experts,omitempty"`
	// Knowledge is the knowledge sources every run filed here and below is
	// grounded in, besides its agent's own. Set here, even as [], the
	// console cannot change them.
	Knowledge []string `json:"knowledge,omitempty"`
	// AgentPosts is what becomes of what an agent scheduled here posts:
	// publish, or review by the project's owners. Left out, it is the
	// console's, and a new project takes its kind's default.
	AgentPosts string `json:"agent_posts,omitempty"`
	// Schedules are agents scheduled here, filed in the project. They are
	// the file's: the console cannot change or delete them, only pause,
	// resume and run them.
	Schedules []Schedule `json:"schedules,omitempty"`
}

// Schedule is an agent scheduled in a project: a recap is one that posts
// its answer to the project.
type Schedule struct {
	// Name is letters, digits, dashes and underscores, unique across the
	// deployment, since every run it raises carries it.
	Name string `json:"name"`
	// Cron is a 5-field cron expression, in UTC.
	Cron  string `json:"cron"`
	Agent string `json:"agent"`
	// Text is what the agent is asked each time.
	Text string `json:"text"`
	// Description says what the schedule is for. The agent is not shown it.
	Description string `json:"description,omitempty"`
	// Deliver is where each answer goes: project, posted in the project
	// (or held for its owners, as agent_posts says). Left out, it stays on
	// the run.
	Deliver string `json:"deliver,omitempty"`
}

// Channel is one place in a project where threads live. Its title, purpose
// and default are the file's; general may be declared to set them.
type Channel struct {
	// Name is lower case letters, digits and dashes, at most 40, and never
	// changes.
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
	// Purpose is sent to every agent answering in the channel.
	Purpose string `json:"purpose,omitempty"`
	// Default answers untagged messages in the channel: an agent, or auto.
	// Left out, the project's default answers.
	Default string `json:"default,omitempty"`
	// Slack binds the channel to a Slack channel of a Slack client: a Slack
	// thread there is a shared thread here. The client is named by its id
	// and need not exist yet; until it does the binding binds nothing.
	Slack *ChannelSlack `json:"slack,omitempty"`
}

// ChannelSlack is the Slack channel a channel is bound to.
type ChannelSlack struct {
	Client  string `json:"client"`
	Channel string `json:"channel"`
}

// Expert is who answers for some topics: a person by subject, or
// role:<name>.
type Expert struct {
	Who    string   `json:"who"`
	Topics []string `json:"topics"`
}

// Account is an account project's customer.
type Account struct {
	Customer   string `json:"customer,omitempty"`
	ExternalID string `json:"external_id,omitempty"`
	// URL is an https link, such as the customer's record in a CRM.
	URL string `json:"url,omitempty"`
}

// Project kinds.
const (
	ProjectCompany = "company"
	ProjectTeam    = "team"
	ProjectAccount = "account"
)

// RootParent is how a project names the workspace as its parent, the same
// as leaving parent out.
const RootParent = "~"

// ParentOf is the project a declared project sits under: "" is the
// workspace.
func ParentOf(p Project) string {
	if p.Parent == RootParent {
		return ""
	}
	return p.Parent
}

// Member roles.
const (
	RoleViewer = "viewer"
	RoleMember = "member"
	RoleOwner  = "owner"
)

// KindOf is a declared project's kind, with its default.
func KindOf(p Project) string {
	if p.Kind == "" {
		return ProjectTeam
	}
	return p.Kind
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
	Transport string   `json:"transport"`
	Endpoint  string   `json:"endpoint"`
	Args      []string `json:"args,omitempty"`
	// CredentialScope is whose credential the server is reached with:
	// shared, the default, is Credential for every run; person is the
	// credential of the person a run acts for, which each person gives
	// themselves, and nothing for a person who has given none. A
	// person-scoped tool names no Credential, so there is none to fall
	// back to.
	CredentialScope string `json:"credential_scope,omitempty"`
	Credential      *Ref   `json:"credential,omitempty"`
	// CredentialAs is the variable (stdio) or header (http) the credential
	// is sent as. Empty on http is a bearer token. A person-scoped stdio
	// tool needs it.
	CredentialAs string `json:"credential_as,omitempty"`
	// Accept pins the tools taken from the server by schema hash. A tool
	// left out, or changed, stays pending.
	Accept []AcceptTool `json:"accept,omitempty"`
}

// Credential scopes.
const (
	ScopeShared = "shared"
	ScopePerson = "person"
)

// AcceptTool is one pinned tool.
type AcceptTool struct {
	Name       string `json:"name"`
	SchemaHash string `json:"schema_hash"`
	Class      string `json:"class,omitempty"`
	// Description and InputSchema are the tool's whole form as the server
	// offers it. A person-scoped tool's server is not reached at start, as
	// there is no credential to reach it with, so the tool is accepted in
	// this form, which SchemaHash must be the hash of. A shared tool takes
	// its form from the server and leaves these out.
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// Agent is one agent, from a file or written inline in the agent JSON fiber
// reads (docs/agents.md).
type Agent struct {
	File string               `json:"file,omitempty"`
	Spec *agentspec.AgentSpec `json:"spec,omitempty"`
	// Project is where the agent lives: a project in this file. It is
	// available there and below, and nowhere else. Left out, it is a
	// platform agent, available wherever it is enabled.
	Project string `json:"project,omitempty"`
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
	// WarpClient is how the app signs in to warp's API: a machine client of
	// the identity provider, whose tokens it gets and renews with client
	// credentials. Sign-in other than dev needs it or WarpToken.
	WarpClient *ClientCredentials `json:"warp_client,omitempty"`
	// WarpToken is a bearer token the app calls warp's API with, given to it
	// as WARP_TOKEN. It is not renewed, so it suits a token that does not
	// expire; an identity provider's access tokens do, so use WarpClient.
	WarpToken *Ref `json:"warp_token,omitempty"`
}

// ClientCredentials is a machine client of an identity provider: where it
// gets a token, and its id and secret.
type ClientCredentials struct {
	TokenURL     string   `json:"token_url"`
	ClientID     string   `json:"client_id"`
	ClientSecret *Ref     `json:"client_secret"`
	Scopes       []string `json:"scopes,omitempty"`
	Audience     string   `json:"audience,omitempty"`
}

// CheckSignIn reports whether the app can call warp's API under the sign-in
// kind auth, where empty is dev, the default. Dev needs no credential, a
// process cannot sign in through Tailscale Serve, and every other kind needs
// warp_client or warp_token.
func (s Slack) CheckSignIn(auth string) error {
	switch auth {
	case "", "dev":
		return nil
	case "tailscale":
		return errors.New("the Slack app cannot sign in through Tailscale Serve; use oidc, or dev on one machine")
	}
	if s.WarpClient == nil && !s.WarpToken.Set() {
		return fmt.Errorf("under %s sign-in the Slack app needs a credential for warp's API: warp_client (client credentials) or warp_token", auth)
	}
	return nil
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
	// Login is the app's own login, such as "warp[bot]". Once warp is built
	// with a fiber whose GitHub connector reads it, a comment that mentions
	// it and names no agent is answered by the default chain of the client's
	// home. Until then it is kept and changes nothing.
	Login string `json:"login,omitempty"`
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
	if err := checkProjects(sp.Workspace); err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	if err := checkAgents(sp); err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	if err := checkSchedules(sp); err != nil {
		return fmt.Errorf("%s: %w", where, err)
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
			if err := checkScope(b); err != nil {
				return fmt.Errorf("%s: tools[%d] %q: %w", where, i, t.Name, err)
			}
		}
	}
	for i, a := range sp.Agents {
		if (a.File == "") == (a.Spec == nil) {
			return fmt.Errorf("%s: agents[%d]: file or spec, exactly one", where, i)
		}
		if a.Spec != nil && slices.Contains(ReservedNames, agentspec.NormalizeName(a.Spec.Name)) {
			return fmt.Errorf("%s: agents[%d].spec.name: @%s is reserved: it is never an agent's name", where, i, agentspec.NormalizeName(a.Spec.Name))
		}
	}
	if s := sp.Surfaces.Slack; s != nil && s.Enabled {
		if s.Team == "" || !s.BotToken.Set() || !s.AppToken.Set() {
			return fmt.Errorf("%s: surfaces.slack: team, bot_token and app_token are required", where)
		}
		if s.Command != "" && !strings.HasPrefix(s.Command, "/") {
			return fmt.Errorf("%s: surfaces.slack.command starts with /", where)
		}
		if s.WarpClient != nil && s.WarpToken.Set() {
			return fmt.Errorf("%s: surfaces.slack: warp_client or warp_token, not both", where)
		}
		if c := s.WarpClient; c != nil {
			u, err := url.Parse(c.TokenURL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
				return fmt.Errorf("%s: surfaces.slack.warp_client.token_url must be an http or https URL", where)
			}
			if c.ClientID == "" || !c.ClientSecret.Set() {
				return fmt.Errorf("%s: surfaces.slack.warp_client: client_id and client_secret are required", where)
			}
		}
	}
	if g := sp.Surfaces.GitHub; g != nil && g.Enabled {
		if len(g.Owners) == 0 || !g.Token.Set() || !g.WebhookSecret.Set() {
			return fmt.Errorf("%s: surfaces.github: owners, token and webhook_secret are required", where)
		}
	}
	return nil
}

// checkScope holds an mcp tool to its credential scope. A person-scoped one
// names no credential, so a person with none is never given the shared one,
// and declares each tool's whole form, since its server is not reached at
// start.
func checkScope(b MCP) error {
	switch b.CredentialScope {
	case "", ScopeShared:
		return nil
	case ScopePerson:
	default:
		return fmt.Errorf("credential_scope is shared or person")
	}
	if b.Credential.Set() {
		return fmt.Errorf("a person-scoped tool takes each person's own credential, so it names no credential")
	}
	if b.Transport == "stdio" && b.CredentialAs == "" {
		return fmt.Errorf("a person-scoped stdio tool needs credential_as, the variable each person's credential is sent in")
	}
	for j, a := range b.Accept {
		full, err := toolprovider.NewTool(a.Name, a.Description, a.InputSchema)
		if err != nil {
			return fmt.Errorf("accept[%d] %q: input_schema: %w", j, a.Name, err)
		}
		if full.SchemaHash != a.SchemaHash {
			return fmt.Errorf("accept[%d] %q: a person-scoped tool is declared with the description and input_schema its server offers, "+
				"and schema_hash %s is not the hash of this input_schema (%s)", j, a.Name, a.SchemaHash, full.SchemaHash)
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
		if s := p.Spec.Surfaces.Slack; s != nil && s.Enabled {
			if err := s.CheckSignIn(c.System.Spec.Auth.Kind); err != nil {
				return fmt.Errorf("platform %q: surfaces.slack: %w", p.Metadata.Name, err)
			}
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
