// Humagen's agent document: the shape its API and files carry. A stored
// agent is a saige definition (pkg/agents); this is converted to and from
// one at the store (agents.go).

package wire

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/runtime"
)

// ErrInvalidAgent reports a definition that will not be accepted. Callers match with
// errors.Is.
var ErrInvalidAgent = errors.New("agentspec: invalid")

// FirstRev is the revision number a new agent's first definition takes.
// Revisions count from one so that a zero Rev is unmistakably "unset" rather
// than an off-by-one that reads as the first revision.
const FirstRev = 1

// MaxNameLen bounds an agent name. A name becomes a topic segment, which
// becomes part of a stream key and of a URL path, so it is bounded here rather
// than discovered as a limit by whichever transport truncates first.
const MaxNameLen = 64

// Providers the built-in runtime can resolve.
//
// The names mirror pkg/agentrt's, and are declared rather than imported so that
// validating a definition costs no LLM client. The set is open in the same way:
// a deployment that registers its own provider validates against it with
// ValidateWith, because a definition that cannot be stored is a worse failure
// than one that fails at build time with a message listing what exists.
const (
	// ProviderAnthropic is the hosted Anthropic API.
	ProviderAnthropic = "anthropic"
	// ProviderOpenAI is the OpenAI API or any server compatible with it.
	ProviderOpenAI = "openai"
	// ProviderOffline replays a canned script instead of calling a model.
	// It is what makes a deployment demonstrable with no API key.
	ProviderOffline = "offline"
)

// MCPGroupPrefix marks an entry of Tools as a tool group: "mcp:urmzd"
// grants the accepted read tools of the MCP provider named urmzd as one name,
// and "mcp:urmzd:write" its read and write tools (see ParseMCPGrant). It
// mirrors pkg/agentrt's. Which tools a group holds is a property of a
// deployment (which tools an admin accepted), so a group is expanded when an
// agent is built, never here.
const MCPGroupPrefix = "mcp:"

// MaxFallbacks bounds how many models an agent may fall back to. Every
// fallback is a client built with the agent and a model that may answer for
// it, so the list is a policy, not a search: a short one an operator can
// reason about. Four alternates is already five models a run may land on.
const MaxFallbacks = 4

// Conditions a spec's FallbackOn may name: the classes of model error that
// move a call on to the next fallback model. Whatever is named, an error no
// other model can fix (a cancelled run, an invalid request or configuration,
// an exhausted budget) never falls through.
const (
	// FallbackOnAny falls through on every error but those. It cannot be
	// combined with another condition, and it is what an empty FallbackOn
	// means.
	FallbackOnAny = "any"
	// FallbackOnUnavailable is an outage: a 5xx, an overloaded provider.
	FallbackOnUnavailable = "unavailable"
	// FallbackOnRateLimit is a rate limit or an exhausted quota.
	FallbackOnRateLimit = "rate_limit"
	// FallbackOnTransient is any error a retry could fix: a timeout, a reset
	// connection, and also a rate limit or an outage.
	FallbackOnTransient = "transient"
	// FallbackOnAuth is a refused or missing credential.
	FallbackOnAuth = "auth"
	// FallbackOnContextLength is a prompt too long for the model.
	FallbackOnContextLength = "context_length"
	// FallbackOnContentFilter is a request or response a provider's safety
	// system blocked.
	FallbackOnContentFilter = "content_filter"
)

// FallbackConditions returns every condition FallbackOn may name, sorted. It
// returns a fresh slice so a caller cannot edit this package's answer.
func FallbackConditions() []string {
	return []string{
		FallbackOnAny, FallbackOnAuth, FallbackOnContentFilter, FallbackOnContextLength,
		FallbackOnRateLimit, FallbackOnTransient, FallbackOnUnavailable,
	}
}

// KnownProviders returns the built-in provider names, sorted. It returns a
// fresh slice so a caller extending it cannot edit this package's answer.
func KnownProviders() []string {
	return []string{ProviderAnthropic, ProviderOffline, ProviderOpenAI}
}

// AgentSpec is what an agent IS.
//
// It mirrors pkg/agentrt.Spec field for field, so adaptation between them is
// assignment.
type AgentSpec struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Model        string   `json:"model"`
	Provider     string   `json:"provider"`
	SystemPrompt string   `json:"system_prompt"`
	Tools        []string `json:"tools,omitempty"`
	Sources      []Source `json:"sources,omitempty"`
	Access       Access   `json:"access,omitzero"`
	// Fallbacks are the models tried, in order, when a model call to the
	// primary (Provider and Model) fails with an error another model could
	// fix: an outage, a rate limit, a refused credential. A request the
	// provider rejected as invalid, a cancelled run, or an exhausted budget
	// does not fall through. Each is built with the agent, so a fallback
	// this deployment cannot construct fails the build like the primary
	// would. Empty means the primary alone, which is what every revision
	// written before fallbacks existed means too.
	Fallbacks []ModelRef `json:"fallbacks,omitempty"`
	// FallbackOn is when a call moves on to the next fallback: the error
	// classes, from FallbackConditions, that fall through. Empty means
	// FallbackOnAny. It is a set, sorted by Normalize, and naming it without
	// any Fallbacks is invalid: there is nothing to fall back to.
	FallbackOn []string `json:"fallback_on,omitempty"`
	// Approvals names the tools whose every call waits for a person to
	// approve it. Each must be a tool the agent is granted: one in Tools, one
	// in a tool group Tools grants (see MCPGroupPrefix), or the delegation
	// tool "tag_<agent>" of an agent in Access.Spawn. A run
	// parks on the call and resumes with the person's decision; see the
	// runtime's "Parking a run on a person".
	Approvals []string `json:"approvals,omitempty"`
	// Approvers, when set, are the only people who may answer an approval:
	// principals as the host names them ("ana", "slack:U1"), or roles as
	// "role:<name>", which the host resolves (the runtime's WithRoles).
	// Empty lets anyone in the tenant approve, except the person whose
	// request raised the call.
	Approvers []string `json:"approvers,omitempty"`
	// SelfApproval lets the person whose request raised a call approve it.
	// Off by default: an approval is a second pair of eyes.
	SelfApproval bool `json:"self_approval,omitempty"`
	// Answerers, when set, are the only people who may answer a choice or
	// a reply the agent asks with ask_person, named as Approvers are. Empty
	// lets anyone in the tenant answer, the requester included.
	Answerers []string `json:"answerers,omitempty"`
	// TaggedOnly keeps the agent out of messages that do not name it: a
	// host that lets agents decide for themselves whether to take an
	// untagged message never asks this one, so it runs only when a person,
	// a default or another agent names it. Off by default.
	TaggedOnly bool `json:"tagged_only,omitempty"`
}

// ModelRef names one model behind one provider: a fallback for the agent's
// primary model. The two fields mean exactly what AgentSpec.Provider and
// AgentSpec.Model mean.
type ModelRef struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// Source is one retrieval corpus the agent may ground answers in. Options are
// connector-specific and this package does not interpret them; it only insists
// that they hash deterministically.
type Source struct {
	Name    string            `json:"name"`
	URI     string            `json:"uri"`
	Options map[string]string `json:"options,omitempty"`
}

// Access is what the agent may do beyond talking, as two closed allowlists:
// everything not listed is denied. Spawn and WorkspaceAreas are separate
// because delegation and storage are separate hazards, and an agent that may
// write to a workspace area has not thereby earned the right to start other
// agents.
type Access struct {
	// Spawn names the agents this agent may delegate to.
	Spawn []string `json:"spawn,omitempty"`
	// WorkspaceAreas are the shared-workspace key prefixes it may use.
	WorkspaceAreas []string `json:"workspace_areas,omitempty"`
}

// Revision is one immutable version of an agent: the definition, which version
// it is, and the evidence that it has not changed since.
type Revision struct {
	Spec      AgentSpec `json:"spec"`
	Rev       int       `json:"rev"`
	Hash      string    `json:"hash"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

// NewRevision canonicalizes spec, validates it, and stamps it as revision rev.
//
// It is the only supported way to build a Revision, because the three steps are
// one step: a Revision whose Spec was not canonicalized would carry a hash of
// content it does not hold, and a store that skipped validation would accept an
// agent name that cannot be addressed on the bus.
func NewRevision(spec AgentSpec, rev int, by string, at time.Time) (Revision, error) {
	return NewRevisionWith(spec, rev, by, at, KnownProviders())
}

// NewRevisionWith is NewRevision for a deployment whose provider table is not
// the built-in one.
func NewRevisionWith(spec AgentSpec, rev int, by string, at time.Time, providers []string) (Revision, error) {
	if rev < FirstRev {
		return Revision{}, fmt.Errorf("%w: revision number %d is below %d; revisions count from %d", ErrInvalidAgent, rev, FirstRev, FirstRev)
	}
	canonical := spec.Normalize()
	if err := canonical.ValidateWith(providers); err != nil {
		return Revision{}, err
	}
	return Revision{
		Spec:      canonical,
		Rev:       rev,
		Hash:      canonical.Hash(),
		CreatedAt: at.UTC(),
		CreatedBy: by,
	}, nil
}

// Validate reports whether r is a well-formed revision of a valid definition,
// including that its recorded hash is the hash of its own content.
//
// The hash check is not redundant with the one pkg/runtime makes. That one
// compares two revisions that claim to be the same revision; this one catches a
// single revision that is internally inconsistent, decoded from a corrupted
// record, or built by a store that mutated the spec after hashing it.
func (r Revision) Validate() error {
	if r.Rev < FirstRev {
		return fmt.Errorf("%w: revision number %d is below %d", ErrInvalidAgent, r.Rev, FirstRev)
	}
	if err := r.Spec.Validate(); err != nil {
		return fmt.Errorf("%w (revision %d)", err, r.Rev)
	}
	if want := r.Spec.Hash(); r.Hash != "" && r.Hash != want {
		return fmt.Errorf("%w: agent %q revision %d records hash %s but its content hashes to %s; an immutable revision changed",
			ErrInvalidAgent, r.Spec.Name, r.Rev, r.Hash, want)
	}
	return nil
}

// Clone returns a deep copy. A Revision carries slices and a map, so handing
// one to a caller without copying would let that caller edit a store's
// immutable history by accident.
func (r Revision) Clone() Revision {
	r.Spec = r.Spec.Clone()
	return r
}

// Clone returns a deep copy of the definition.
func (s AgentSpec) Clone() AgentSpec {
	s.Tools = slices.Clone(s.Tools)
	s.Fallbacks = slices.Clone(s.Fallbacks)
	s.FallbackOn = slices.Clone(s.FallbackOn)
	s.Approvals = slices.Clone(s.Approvals)
	s.Approvers = slices.Clone(s.Approvers)
	s.Answerers = slices.Clone(s.Answerers)
	s.Access.Spawn = slices.Clone(s.Access.Spawn)
	s.Access.WorkspaceAreas = slices.Clone(s.Access.WorkspaceAreas)
	if s.Sources != nil {
		sources := make([]Source, len(s.Sources))
		for i, src := range s.Sources {
			src.Options = maps.Clone(src.Options)
			sources[i] = src
		}
		s.Sources = sources
	}
	return s
}

// NormalizeName is the canonical form of an agent name, and the key an agent is
// looked up by. It is exported because a store must resolve Get("Docs-Bot") to
// the agent it created as "docs-bot", and it must not reimplement the rule to
// do so.
func NormalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// Normalize returns the canonical form of the definition: names lower-cased,
// free text trimmed, set-valued fields sorted, and empty collections collapsed
// to nil so that a spec decoded from JSON with "tools": [] is the same
// definition as one without the field. Fallbacks are a sequence, not a set:
// their order is the order they are tried in, so it is kept.
//
// It never reports an error and never drops content, including content
// Validate will reject. Dropping an empty tool name here would make the
// definition valid by deletion, which is exactly the kind of silent repair that
// leaves an operator wondering where their tool went.
func (s AgentSpec) Normalize() AgentSpec {
	out := AgentSpec{
		Name:         NormalizeName(s.Name),
		Description:  strings.TrimSpace(s.Description),
		Model:        strings.TrimSpace(s.Model),
		Provider:     NormalizeName(s.Provider),
		SystemPrompt: strings.TrimSpace(s.SystemPrompt),
		Tools:        normalizeSet(s.Tools, false),
		Fallbacks:    normalizeFallbacks(s.Fallbacks),
		FallbackOn:   normalizeSet(s.FallbackOn, true),
		Approvals:    normalizeSet(s.Approvals, false),
		Approvers:    normalizeSet(s.Approvers, false),
		SelfApproval: s.SelfApproval,
		Answerers:    normalizeSet(s.Answerers, false),
		TaggedOnly:   s.TaggedOnly,
		Access: Access{
			Spawn:          normalizeSet(s.Access.Spawn, true),
			WorkspaceAreas: normalizeSet(s.Access.WorkspaceAreas, false),
		},
	}
	if len(s.Sources) > 0 {
		sources := make([]Source, 0, len(s.Sources))
		for _, src := range s.Sources {
			sources = append(sources, Source{
				Name:    NormalizeName(src.Name),
				URI:     strings.TrimSpace(src.URI),
				Options: normalizeOptions(src.Options),
			})
		}
		slices.SortStableFunc(sources, func(a, b Source) int {
			if c := strings.Compare(a.Name, b.Name); c != 0 {
				return c
			}
			return strings.Compare(a.URI, b.URI)
		})
		out.Sources = sources
	}
	return out
}

// Validate reports whether the definition is one this deployment will accept,
// using the built-in provider table.
func (s AgentSpec) Validate() error { return s.ValidateWith(KnownProviders()) }

// ValidateWith is Validate against an explicit provider table.
//
// An empty table rejects every definition, which is the fail-closed reading: a
// deployment that can resolve no providers can execute no agents, and accepting
// definitions it will never be able to run only moves the failure to the first
// tag.
//
// Validation runs on the canonical form, so a caller may validate what it was
// given rather than having to remember to Normalize first. It deliberately does
// not resolve tools or sources: whether a tool is loaded is a property of a
// process, not of a definition, and a control plane that rejected a spec
// because one node had not been restarted yet would be enforcing an accident.
func (s AgentSpec) ValidateWith(providers []string) error {
	c := s.Normalize()
	switch {
	case c.Name == "":
		return fmt.Errorf("%w: the definition has no agent name", ErrInvalidAgent)
	case len(c.Name) > MaxNameLen:
		return fmt.Errorf("%w: agent name %q is %d characters, the limit is %d", ErrInvalidAgent, c.Name, len(c.Name), MaxNameLen)
	case c.Model == "":
		return fmt.Errorf("%w: agent %q names no model", ErrInvalidAgent, c.Name)
	case c.Provider == "":
		return fmt.Errorf("%w: agent %q names no provider", ErrInvalidAgent, c.Name)
	case c.SystemPrompt == "":
		return fmt.Errorf("%w: agent %q has an empty system prompt; an agent with no instructions is a definition of nothing", ErrInvalidAgent, c.Name)
	}
	// The charset is pkg/topic's to decide, not this package's. An agent
	// name becomes a topic segment the moment the agent emits anything, so a
	// name that validated here and not there would produce an agent nobody
	// can subscribe to, discovered at the first run rather than at the
	// definition that caused it.
	if err := addressable(c.Name); err != nil {
		return fmt.Errorf("%w: agent name %q is not addressable: %w", ErrInvalidAgent, c.Name, err)
	}
	// One name is the bus's own. Requests addressed to a run travel under it,
	// and an agent by that name would publish its events into them.
	if c.Name == runtime.ReservedName {
		return fmt.Errorf("%w: agent name %q is reserved for control topics", ErrInvalidAgent, c.Name)
	}
	if !slices.ContainsFunc(providers, func(p string) bool { return strings.EqualFold(p, c.Provider) }) {
		return fmt.Errorf("%w: agent %q names provider %q; this deployment knows %s", ErrInvalidAgent, c.Name, c.Provider, list(providers))
	}
	if err := validFallbacks(c, providers); err != nil {
		return err
	}
	if err := uniqueNonEmpty("tool", c.Name, c.Tools); err != nil {
		return err
	}
	groups := false
	for _, name := range c.Tools {
		_, ok, err := ParseMCPGrant(name)
		if !ok {
			continue
		}
		if err != nil {
			return fmt.Errorf("agent %q: %w", c.Name, err)
		}
		groups = true
	}
	if err := uniqueNonEmpty("spawn grant", c.Name, c.Access.Spawn); err != nil {
		return err
	}
	if err := uniqueNonEmpty("approval", c.Name, c.Approvals); err != nil {
		return err
	}
	if err := uniqueNonEmpty("approver", c.Name, c.Approvers); err != nil {
		return err
	}
	if err := uniqueNonEmpty("answerer", c.Name, c.Answerers); err != nil {
		return err
	}
	if len(c.Approvals) == 0 && (len(c.Approvers) > 0 || c.SelfApproval) {
		return fmt.Errorf("%w: agent %q names who may approve but requires no approvals", ErrInvalidAgent, c.Name)
	}
	for _, name := range c.Approvals {
		if strings.HasPrefix(name, MCPGroupPrefix) {
			return fmt.Errorf("%w: agent %q requires approval for %q, which is a tool group; name each tool that needs approval", ErrInvalidAgent, c.Name, name)
		}
		// An approval for a tool the agent cannot call would never be asked,
		// and a definition that reads as guarded and is not is worse than one
		// that is refused.
		granted := slices.Contains(c.Tools, name)
		if target, ok := strings.CutPrefix(name, "tag_"); ok && slices.Contains(c.Access.Spawn, target) {
			granted = true
		}
		// A group's members are known only to a deployment, so a tool that
		// may come from a granted group passes here. Building the agent
		// checks it against the expanded grant and refuses to run an
		// approval it cannot enforce, so the guard fails closed either way.
		if groups && !strings.HasPrefix(name, MCPGroupPrefix) {
			granted = true
		}
		if !granted {
			return fmt.Errorf("%w: agent %q requires approval for %q, which is not a tool it is granted", ErrInvalidAgent, c.Name, name)
		}
	}
	if err := uniqueNonEmpty("workspace area", c.Name, c.Access.WorkspaceAreas); err != nil {
		return err
	}
	// A spawn grant names an agent, so it is bound by the same addressing
	// rule as the agent doing the spawning.
	for _, target := range c.Access.Spawn {
		if err := addressable(target); err != nil {
			return fmt.Errorf("%w: agent %q may spawn %q, which is not an addressable agent name: %w", ErrInvalidAgent, c.Name, target, err)
		}
	}
	seen := make(map[string]struct{}, len(c.Sources))
	for _, src := range c.Sources {
		switch {
		case src.Name == "":
			return fmt.Errorf("%w: agent %q has a source with no name", ErrInvalidAgent, c.Name)
		case src.URI == "":
			return fmt.Errorf("%w: agent %q source %q has no uri", ErrInvalidAgent, c.Name, src.Name)
		}
		if _, dup := seen[src.Name]; dup {
			return fmt.Errorf("%w: agent %q declares source %q twice; a source name is how the agent refers to a corpus and two corpora cannot answer to one name", ErrInvalidAgent, c.Name, src.Name)
		}
		seen[src.Name] = struct{}{}
	}
	return nil
}

// validFallbacks holds every fallback to the rules the primary model is held
// to (a provider this deployment knows, and a model), bounds the list, and
// refuses a repeat: a fallback identical to the primary or to an earlier
// fallback would only send the same failing request again, which is the retry
// decorator's job, not the policy's. c is canonical.
func validFallbacks(c AgentSpec, providers []string) error {
	if len(c.Fallbacks) > MaxFallbacks {
		return fmt.Errorf("%w: agent %q names %d fallback models, the limit is %d", ErrInvalidAgent, c.Name, len(c.Fallbacks), MaxFallbacks)
	}
	if err := uniqueNonEmpty("fallback condition", c.Name, c.FallbackOn); err != nil {
		return err
	}
	if len(c.FallbackOn) > 0 && len(c.Fallbacks) == 0 {
		return fmt.Errorf("%w: agent %q says when to fall back (%s) but names no fallback models", ErrInvalidAgent, c.Name, strings.Join(c.FallbackOn, ", "))
	}
	for _, cond := range c.FallbackOn {
		if !slices.Contains(FallbackConditions(), cond) {
			return fmt.Errorf("%w: agent %q falls back on %q; the conditions are %s", ErrInvalidAgent, c.Name, cond, list(FallbackConditions()))
		}
		if cond == FallbackOnAny && len(c.FallbackOn) > 1 {
			return fmt.Errorf("%w: agent %q falls back on %q together with other conditions; %q already covers them", ErrInvalidAgent, c.Name, FallbackOnAny, FallbackOnAny)
		}
	}
	seen := make(map[ModelRef]int, len(c.Fallbacks)+1)
	seen[ModelRef{Provider: c.Provider, Model: c.Model}] = 0
	for i, fb := range c.Fallbacks {
		n := i + 1
		switch {
		case fb.Provider == "":
			return fmt.Errorf("%w: agent %q fallback %d names no provider", ErrInvalidAgent, c.Name, n)
		case fb.Model == "":
			return fmt.Errorf("%w: agent %q fallback %d (provider %q) names no model", ErrInvalidAgent, c.Name, n, fb.Provider)
		}
		if !slices.ContainsFunc(providers, func(p string) bool { return strings.EqualFold(p, fb.Provider) }) {
			return fmt.Errorf("%w: agent %q fallback %d names provider %q; this deployment knows %s", ErrInvalidAgent, c.Name, n, fb.Provider, list(providers))
		}
		if prev, dup := seen[fb]; dup {
			what := "the primary model"
			if prev > 0 {
				what = fmt.Sprintf("fallback %d", prev)
			}
			return fmt.Errorf("%w: agent %q fallback %d (%s %q) repeats %s", ErrInvalidAgent, c.Name, n, fb.Provider, fb.Model, what)
		}
		seen[fb] = n
	}
	return nil
}

// normalizeFallbacks trims each fallback and lower-cases its provider the way
// the primary's is, keeping the order and collapsing an empty list to nil.
func normalizeFallbacks(fallbacks []ModelRef) []ModelRef {
	if len(fallbacks) == 0 {
		return nil
	}
	out := make([]ModelRef, 0, len(fallbacks))
	for _, fb := range fallbacks {
		out = append(out, ModelRef{Provider: NormalizeName(fb.Provider), Model: strings.TrimSpace(fb.Model)})
	}
	return out
}

// uniqueNonEmpty enforces the two rules every set-valued field shares. The
// values arrive sorted from Normalize, so duplicates are adjacent.
func uniqueNonEmpty(what, agent string, values []string) error {
	for i, v := range values {
		if v == "" {
			return fmt.Errorf("%w: agent %q declares an empty %s name", ErrInvalidAgent, agent, what)
		}
		if i > 0 && v == values[i-1] {
			return fmt.Errorf("%w: agent %q declares %s %q twice", ErrInvalidAgent, agent, what, v)
		}
	}
	return nil
}

// normalizeSet trims, optionally lower-cases, and sorts a set-valued field,
// collapsing an empty one to nil.
func normalizeSet(values []string, lower bool) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if lower {
			out = append(out, NormalizeName(v))
			continue
		}
		out = append(out, strings.TrimSpace(v))
	}
	slices.Sort(out)
	return out
}

// normalizeOptions copies a source's options, collapsing an empty map to nil.
// Keys and values are left exactly as given: they are a connector's vocabulary,
// and trimming or folding them here would change their meaning on someone
// else's behalf.
func normalizeOptions(options map[string]string) map[string]string {
	if len(options) == 0 {
		return nil
	}
	return maps.Clone(options)
}

// list renders a provider table for an error message.
func list(providers []string) string {
	if len(providers) == 0 {
		return "no providers"
	}
	sorted := slices.Clone(providers)
	slices.Sort(sorted)
	return strings.Join(sorted, ", ")
}

// Reserved is the one name no agent may take: the runtime keeps it for
// control requests, and Humagen uses it for a message that names no agent.
const Reserved = runtime.ReservedName

// Addressable reports whether name can name an agent on the bus: a run id
// segment, at most MaxNameLen long.
func Addressable(name string) error { return addressable(name) }

// addressable reports whether name can name an agent on the bus: a run id
// segment, at most MaxNameLen long.
func addressable(name string) error {
	if len(name) > MaxNameLen {
		return fmt.Errorf("longer than %d characters", MaxNameLen)
	}
	return runtime.CheckRun(name, "x")
}
