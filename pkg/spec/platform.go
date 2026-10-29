package spec

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/toolprovider"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/inbox"
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
	// Branding is the workspace's look and name in the console, over the
	// deployment's.
	Branding Branding `json:"branding,omitzero"`
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
	// Skills are named instructions a message posted anywhere in the
	// workspace can tell an agent to use, $<name>, unless a project nearer
	// declares one of the same name. Set here, even as [], the console
	// cannot change them.
	Skills []Skill `json:"skills,omitempty"`
	// MentionKinds are the kinds of @<kind>:<name> the workspace has
	// besides the built-in sme (experts) and skill (skills), each resolving
	// its items to experts, skills, the people holding a role, or a person.
	// Only the file declares them.
	MentionKinds []MentionKind `json:"mention_kinds,omitempty"`
	// PersonCredentials is what people may give for the tools that take
	// their own credential.
	PersonCredentials *PersonCredentials `json:"person_credentials,omitempty"`
	// ReleaseNotes posts what each release of humagen brings in the
	// workspace's own inbox, from humagen: the breaking changes and features
	// the changelog lists for the release this process runs, once a
	// release. Off by default.
	ReleaseNotes bool `json:"release_notes,omitempty"`
	// InboxRetention is how long the workspace's inbox keeps what was
	// settled for everyone, resolved, dismissed or withdrawn, before it is
	// dropped: whole days, such as "90d", or a Go duration, such as
	// "2160h". At least 7 days. Left out, 90 days.
	InboxRetention string `json:"inbox_retention,omitempty"`
}

// Retention is how long the workspace's inbox keeps what was settled
// (InboxRetention), 90 days when the file leaves it out. A document that
// passed its check never sets one it cannot read.
func (w Workspace) Retention() time.Duration {
	d, err := ParseRetention(w.InboxRetention)
	if err != nil {
		return inbox.DefaultRetention
	}
	return d
}

// ParseRetention reads an inbox retention: whole days written like "90d", or
// a Go duration like "2160h". Empty is inbox.DefaultRetention, and less than
// inbox.MinRetention is refused.
func ParseRetention(s string) (time.Duration, error) {
	if s == "" {
		return inbox.DefaultRetention, nil
	}
	bad := fmt.Errorf("%q is whole days, like 90d, or a duration, like 2160h, of at most 100 years", s)
	var d time.Duration
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n < 1 || n > maxRetentionDays || days[0] < '0' || days[0] > '9' {
			return 0, bad
		}
		d = time.Duration(n) * 24 * time.Hour
	} else {
		var err error
		if d, err = time.ParseDuration(s); err != nil || d > maxRetentionDays*24*time.Hour {
			return 0, bad
		}
	}
	if d < inbox.MinRetention {
		return 0, fmt.Errorf("%q is less than the least retention, 7d", s)
	}
	return d, nil
}

// maxRetentionDays bounds a retention, 100 years, so it fits a duration.
const maxRetentionDays = 100 * 365

// PersonCredentials is what a workspace's people may give for a
// person-scoped tool. A reference is always theirs to give, from where the
// operator allows (System person_refs). Values are off by default.
type PersonCredentials struct {
	// Values lets a person store the credential itself with humagen, kept as a
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
// below, and which answers a message that tags none (docs/architecture/projects.md).
type Agents struct {
	// Enable is platform agents available here and below: agents in this
	// file or the platform's own. "*" alone is every platform agent. It
	// adds to what the projects above enable.
	Enable []string `json:"enable,omitempty"`
	// Allow, when set, is the only agents that may run here and below. A
	// project below can only narrow it. Left out, it narrows nothing.
	Allow []string `json:"allow,omitempty"`
	// Default answers a message that tags no agent, here and below: an
	// agent available here, or "*" to let every agent decide. Left out, it inherits.
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
	// Skills are named instructions a message posted here and below can
	// tell an agent to use, $<name>. Set here, even as [], the console
	// cannot change them.
	Skills []Skill `json:"skills,omitempty"`
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

// Skill is named instructions an agent can be told to use: a SKILL.md's
// name and description, what it takes after its name, and its body.
type Skill struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	ArgumentHint string `json:"argument_hint,omitempty"`
	Instructions string `json:"instructions"`
}

// MentionKind is a kind of @<kind>:<name>: its name, what the console calls
// it, and what its items resolve to: experts, skills, role or people.
type MentionKind struct {
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Resolves string `json:"resolves"`
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
// (anthropic, openai, google) with its name. A local server such as
// Ollama or vLLM is openai with its base_url: it serves the OpenAI API.
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
	// Vertex sends google's calls to Vertex AI in a project and location,
	// with Application Default Credentials when no key is given.
	Vertex *Vertex `json:"vertex,omitempty"`
}

// Vertex is where google's calls go on Vertex AI.
type Vertex struct {
	Project  string `json:"project"`
	Location string `json:"location"`
}

// ModelPolicy is a named, ordered list of models and when a call moves on.
type ModelPolicy struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Models      []wire.ModelRef `json:"models"`
	FallbackOn  []string        `json:"fallback_on,omitempty"`
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
	// File is a spec as JSON or YAML, or saige's own format when it ends in
	// .agent.md.
	File string          `json:"file,omitempty"`
	Spec *wire.AgentSpec `json:"spec,omitempty"`
	// Policy is what an .agent.md file is read with. Left out, it grants
	// nothing beyond the file. Only an .agent.md file takes one.
	Policy *wire.DefinitionPolicy `json:"policy,omitempty"`
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
	// Description is the app's short description in Slack. Left out,
	// Humagen's.
	Description string `json:"description,omitempty"`
	// BackgroundColor is the app's background color in Slack, a hex color.
	// Left out, Humagen's.
	BackgroundColor Color `json:"background_color,omitempty"`
	// CommandDescription and CommandUsageHint are what Slack shows for the
	// slash command. Left out, Humagen's.
	CommandDescription string `json:"command_description,omitempty"`
	CommandUsageHint   string `json:"command_usage_hint,omitempty"`
	// HumagenClient is how the app signs in to humagen's API: a machine client of
	// the identity provider, whose tokens it gets and renews with client
	// credentials. Sign-in other than dev needs it or HumagenToken.
	HumagenClient *ClientCredentials `json:"humagen_client,omitempty"`
	// HumagenToken is a bearer token the app calls humagen's API with, given to it
	// as HUMAGEN_TOKEN. It is not renewed, so it suits a token that does not
	// expire; an identity provider's access tokens do, so use HumagenClient.
	HumagenToken *Ref `json:"humagen_token,omitempty"`

	// OldClient and OldToken are humagen_client and humagen_token under the
	// names they had before the rename. A file that uses them still reads:
	// Parse moves each into its new field, clears it and notes it, so
	// nothing else reads or writes them.
	OldClient *ClientCredentials `json:"warp_client,omitempty"`
	OldToken  *Ref               `json:"warp_token,omitempty"`
}

// adoptOld moves the old fields into the new ones, refusing a file that sets
// both names of one field. It returns a note for each old one moved.
func (s *Slack) adoptOld() ([]string, error) {
	if s == nil {
		return nil, nil
	}
	var notes []string
	if s.OldClient != nil {
		if s.HumagenClient != nil {
			return nil, fmt.Errorf("%w: surfaces.slack: humagen_client and %s, not both", ErrInvalid, legacy.SlackClient)
		}
		s.HumagenClient, s.OldClient = s.OldClient, nil
		notes = append(notes, "surfaces.slack."+legacy.SlackClient+" still works; write humagen_client")
	}
	if s.OldToken != nil {
		if s.HumagenToken != nil {
			return nil, fmt.Errorf("%w: surfaces.slack: humagen_token and %s, not both", ErrInvalid, legacy.SlackToken)
		}
		s.HumagenToken, s.OldToken = s.OldToken, nil
		notes = append(notes, "surfaces.slack."+legacy.SlackToken+" still works; write humagen_token")
	}
	return notes, nil
}

// checkCopy checks what the app says about itself in Slack, against
// Slack's own limits for its manifest.
func (s *Slack) checkCopy() error {
	if s == nil {
		return nil
	}
	for _, f := range []struct {
		name  string
		v     string
		limit int
	}{{"description", s.Description, 140}, {"command_description", s.CommandDescription, 2000}, {"command_usage_hint", s.CommandUsageHint, 1000}} {
		if err := checkText(f.v, f.limit, false); err != nil {
			return fmt.Errorf("%s: %w", f.name, err)
		}
	}
	if err := checkColor(s.BackgroundColor); err != nil {
		return fmt.Errorf("background_color: %w", err)
	}
	return nil
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

// CheckSignIn reports whether the app can call humagen's API under the sign-in
// kind auth, where empty is dev, the default. Dev needs no credential, a
// process cannot sign in through Tailscale Serve, and every other kind needs
// humagen_client or humagen_token.
func (s Slack) CheckSignIn(auth string) error {
	switch auth {
	case "", "dev":
		return nil
	case "tailscale":
		return errors.New("the Slack app cannot sign in through Tailscale Serve; use oidc, or dev on one machine")
	}
	if s.HumagenClient == nil && !s.HumagenToken.Set() {
		return fmt.Errorf("under %s sign-in the Slack app needs a credential for humagen's API: humagen_client (client credentials) or humagen_token", auth)
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
	// Login is the app's own login, such as "humagen[bot]". A comment that
	// mentions it and names no agent is answered by the client's default,
	// then by the default chain of the client's home. Left empty, only a
	// comment that names an agent, or an auto label, starts a run.
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
	if _, err := ParseRetention(sp.Workspace.InboxRetention); err != nil {
		return fmt.Errorf("%s: workspace.inbox_retention: %w", where, err)
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
	if err := sp.Branding.check(false); err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	if err := sp.Surfaces.Slack.checkCopy(); err != nil {
		return fmt.Errorf("%s: surfaces.slack.%w", where, err)
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
		if a.Policy != nil && !strings.HasSuffix(a.File, wire.AgentFileSuffix) {
			return fmt.Errorf("%s: agents[%d]: policy goes with an %s file", where, i, wire.AgentFileSuffix)
		}
		if a.Policy != nil && len(a.Policy.Subscriptions) > 0 {
			return fmt.Errorf("%s: agents[%d].policy.subscriptions: the platform decides what wakes an agent", where, i)
		}
		if a.Policy != nil {
			if err := a.Policy.Validate(); err != nil {
				return fmt.Errorf("%s: agents[%d].policy: %w", where, i, err)
			}
		}
		if a.Spec != nil && slices.Contains(ReservedNames, wire.NormalizeName(a.Spec.Name)) {
			return fmt.Errorf("%s: agents[%d].spec.name: @%s is reserved: it is never an agent's name", where, i, wire.NormalizeName(a.Spec.Name))
		}
	}
	if s := sp.Surfaces.Slack; s != nil && s.Enabled {
		if s.Team == "" || !s.BotToken.Set() || !s.AppToken.Set() {
			return fmt.Errorf("%s: surfaces.slack: team, bot_token and app_token are required", where)
		}
		if s.Command != "" && !strings.HasPrefix(s.Command, "/") {
			return fmt.Errorf("%s: surfaces.slack.command starts with /", where)
		}
		if s.HumagenClient != nil && s.HumagenToken.Set() {
			return fmt.Errorf("%s: surfaces.slack: humagen_client or humagen_token, not both", where)
		}
		if c := s.HumagenClient; c != nil {
			u, err := url.Parse(c.TokenURL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
				return fmt.Errorf("%s: surfaces.slack.humagen_client.token_url must be an http or https URL", where)
			}
			if c.ClientID == "" || !c.ClientSecret.Set() {
				return fmt.Errorf("%s: surfaces.slack.humagen_client: client_id and client_secret are required", where)
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
	if b.Vertex != nil {
		switch {
		case kind != "google":
			return fmt.Errorf("vertex is for kind google")
		case b.Vertex.Project == "" || b.Vertex.Location == "":
			return fmt.Errorf("vertex needs project and location")
		}
		return nil
	}
	if b.BaseURL == "" && !b.Key.Set() {
		return fmt.Errorf("a hosted API needs key, as {ref: env:NAME}; a local server such as Ollama is kind openai with its base_url")
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
		if p.Spec.Branding.Palette != nil {
			merged := MergeBranding(c.System.Spec.Branding, p.Spec.Branding)
			if err := merged.Palette.checkContrast(); err != nil {
				return fmt.Errorf("platform %q: branding.palette.%w, over the System document's", p.Metadata.Name, err)
			}
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
