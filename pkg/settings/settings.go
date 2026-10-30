// Package settings declares what a run may be told, and how the levels that
// tell it combine.
//
// Every setting is one of two kinds, declared once (Declared):
//
//   - A Ceiling bounds what a run may do. The strictest value on the chain
//     wins: the workspace, each project from the root down, then the person
//     the run acts for. A level below can only tighten what is above it.
//   - A Preference chooses among what the ceilings allow. The nearest value
//     wins: the person, then the project, then its ancestors, then the
//     workspace, then the default. A preference the ceilings refuse is passed
//     over for the next one out.
//
// A Layer is what one level says. Resolve folds a chain of them into what a
// run gets. Nothing here reads a store or knows who anyone is: the platform
// collects the layers, and pkg/runctx pins the result to the run.
package settings

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// ErrInvalid reports a layer that cannot be kept.
var ErrInvalid = errors.New("settings: invalid")

// Kind is how a setting resolves.
type Kind string

const (
	// Ceiling: the strictest value on the chain wins.
	Ceiling Kind = "ceiling"
	// Preference: the nearest value the ceilings allow wins.
	Preference Kind = "preference"
)

// Setting is one declared setting: its name, as the spec and the API write
// it, and its kind.
type Setting struct {
	Name string
	Kind Kind
}

// Declared is every setting, each with its kind. A setting is added here, to
// Ceilings or Preferences, and to Resolve together.
var Declared = []Setting{
	{"tools", Ceiling},
	{"models", Ceiling},
	{"call_depth", Ceiling},
	{"tool_servers", Ceiling},
	{"model", Preference},
	{"compaction_tokens", Preference},
}

// Bounds.
const (
	// MaxList bounds every list a layer holds.
	MaxList = 64
	// MaxName bounds one entry of a list, in bytes.
	MaxName = 256
	// MaxCallDepth is the deepest call_depth a layer may name.
	MaxCallDepth = 16
	// MinCompactionTokens and MaxCompactionTokens bound compaction_tokens.
	MinCompactionTokens = 1024
	MaxCompactionTokens = 2_000_000
)

// Ceilings bound a run. A list left out (nil) narrows nothing at its level;
// an empty list allows nothing. Each is held by the strictest level.
type Ceilings struct {
	// Tools are the tool names an agent's tools may keep, as an agent's
	// tools list writes them ("mcp:<name>" for a provider's group). A tool
	// the agent grants and this does not list is taken away for the run.
	Tools []string `json:"tools,omitzero"`
	// Models are the models a run may call, each "provider/model", or
	// "provider/*" for every model of a provider. A run whose agent's model
	// and fallbacks are all outside it is refused.
	Models []string `json:"models,omitzero"`
	// CallDepth is the deepest a run's calls may go: 0 lets it call no
	// agent. It never deepens the deployment's own limit.
	CallDepth *int `json:"call_depth,omitempty"`
	// ToolServers are the hosts a person's own tool servers may be on.
	// Unlike the others it is default deny: the workspace's list opens it,
	// and left out there, it allows none. A level below narrows it.
	ToolServers []string `json:"tool_servers,omitzero"`
}

// Preferences choose within the ceilings. A zero field says nothing at its
// level.
type Preferences struct {
	// Model is the model a run uses in place of its agent's, "provider/model"
	// or "policy/<name>" for one of the workspace's model policies. It is
	// passed over when the ceilings do not allow it.
	Model string `json:"model,omitempty"`
	// CompactionTokens is the input size, in tokens, past which a run's
	// history is summarized before the next model call. Left out
	// everywhere, a run never compacts.
	CompactionTokens int `json:"compaction_tokens,omitempty"`
}

// Layer is what one level says.
type Layer struct {
	Ceilings    Ceilings    `json:"ceilings,omitzero"`
	Preferences Preferences `json:"preferences,omitzero"`
}

// IsZero reports whether the layer says nothing.
func (l Layer) IsZero() bool {
	c := l.Ceilings
	return c.Tools == nil && c.Models == nil && c.CallDepth == nil && c.ToolServers == nil && l.Preferences == (Preferences{})
}

// Resolved is what a chain of layers gives a run. Its ceilings are the
// strictest of the chain's, and its preferences the nearest each allows.
type Resolved struct {
	Ceilings    Ceilings    `json:"ceilings,omitzero"`
	Preferences Preferences `json:"preferences,omitzero"`
}

// Resolve folds the workspace's layer and the layers below it (the projects
// from the root down, then the person) into what a run gets.
func Resolve(workspace Layer, below ...Layer) Resolved {
	chain := append([]Layer{workspace}, below...)
	// Tool servers are default deny: only the workspace opens them.
	out := Resolved{Ceilings: Ceilings{ToolServers: []string{}}}
	if workspace.Ceilings.ToolServers != nil {
		out.Ceilings.ToolServers = nil
	}
	for _, l := range chain {
		out.Ceilings = Tighten(out.Ceilings, l.Ceilings)
	}
	// The nearest first: a level's preference stands only where the
	// ceilings allow it, and otherwise the next one out is asked.
	for i := len(chain) - 1; i >= 0; i-- {
		p := chain[i].Preferences
		if out.Preferences.Model == "" && p.Model != "" && out.Ceilings.AllowsModel(p.Model) {
			out.Preferences.Model = p.Model
		}
		if out.Preferences.CompactionTokens == 0 && p.CompactionTokens > 0 {
			out.Preferences.CompactionTokens = p.CompactionTokens
		}
	}
	return out
}

// Tighten is the strictest of two ceilings: each list the entries both
// allow, in a's order, and the lower call depth. Neither is changed.
func Tighten(a, b Ceilings) Ceilings {
	return Ceilings{
		Tools:       intersect(a.Tools, b.Tools),
		Models:      intersectModels(a.Models, b.Models),
		CallDepth:   lower(a.CallDepth, b.CallDepth),
		ToolServers: intersect(a.ToolServers, b.ToolServers),
	}
}

// Narrow is r held to the ceilings c as well: a called run's context, which
// keeps its caller's preferences only where they still fit.
func (r Resolved) Narrow(c Ceilings) Resolved {
	out := Resolved{Ceilings: Tighten(r.Ceilings, c), Preferences: r.Preferences}
	if out.Preferences.Model != "" && !out.Ceilings.AllowsModel(out.Preferences.Model) {
		out.Preferences.Model = ""
	}
	return out
}

// AllowsTool reports whether a tool name is within the ceilings.
func (c Ceilings) AllowsTool(name string) bool {
	return c.Tools == nil || slices.Contains(c.Tools, name)
}

// AllowsModel reports whether a model, "provider/model", is within the
// ceilings. A preference naming a model policy ("policy/<name>") is judged
// by the models the policy resolves to, when the run starts, so it is
// allowed here unless the ceilings allow no model at all.
func (c Ceilings) AllowsModel(ref string) bool {
	if c.Models == nil {
		return true
	}
	provider, model, ok := SplitModel(ref)
	if !ok {
		return false
	}
	if provider == PolicyProvider {
		return len(c.Models) > 0
	}
	return slices.ContainsFunc(c.Models, func(m string) bool { return m == ref || m == provider+"/*" && model != "" })
}

// AllowsToolServer reports whether a host may serve a person's own tools:
// listed by the ceilings, which are default deny.
func (c Ceilings) AllowsToolServer(host string) bool {
	return host != "" && slices.Contains(c.ToolServers, strings.ToLower(host))
}

// Depth is the call depth the ceilings allow under a deployment's own limit.
func (c Ceilings) Depth(limit int) int {
	if c.CallDepth != nil && *c.CallDepth < limit {
		return *c.CallDepth
	}
	return limit
}

// PolicyProvider is the provider a model preference names to choose one of
// the workspace's model policies.
const PolicyProvider = "policy"

// SplitModel reads "provider/model". The model may hold more slashes.
func SplitModel(ref string) (provider, model string, ok bool) {
	provider, model, ok = strings.Cut(ref, "/")
	return provider, model, ok && provider != "" && model != ""
}

func intersect(a, b []string) []string {
	switch {
	case a == nil:
		return slices.Clone(b)
	case b == nil:
		return slices.Clone(a)
	}
	out := []string{}
	for _, x := range a {
		if slices.Contains(b, x) && !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

// intersectModels keeps what both lists allow: an exact model either names
// that the other allows, and a provider's wildcard only where both have it.
func intersectModels(a, b []string) []string {
	switch {
	case a == nil:
		return slices.Clone(b)
	case b == nil:
		return slices.Clone(a)
	}
	out := []string{}
	add := func(m string) {
		if !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	for _, m := range a {
		if (Ceilings{Models: b}).allowsPattern(m) {
			add(m)
		}
	}
	for _, m := range b {
		if !strings.HasSuffix(m, "/*") && (Ceilings{Models: a}).allowsPattern(m) {
			add(m)
		}
	}
	return out
}

// allowsPattern reports whether an entry of another list is within this one:
// an exact model this allows, or a wildcard this has too.
func (c Ceilings) allowsPattern(m string) bool {
	if strings.HasSuffix(m, "/*") {
		return slices.Contains(c.Models, m)
	}
	return c.AllowsModel(m)
}

func lower(a, b *int) *int {
	switch {
	case a == nil && b == nil:
		return nil
	case a == nil:
		v := *b
		return &v
	case b == nil || *a <= *b:
		v := *a
		return &v
	}
	v := *b
	return &v
}

// Check reports whether a layer can be kept: bounded lists of well-formed,
// distinct entries and values in range.
func (l Layer) Check() error {
	c, p := l.Ceilings, l.Preferences
	if err := checkList("tools", c.Tools, func(string) error { return nil }); err != nil {
		return err
	}
	if err := checkList("models", c.Models, func(m string) error {
		if provider, _, ok := SplitModel(m); !ok || provider == PolicyProvider {
			return fmt.Errorf("%q is not provider/model or provider/*", m)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := checkList("tool_servers", c.ToolServers, func(h string) error {
		if h != strings.ToLower(h) || strings.ContainsAny(h, "/:@ ") {
			return fmt.Errorf("%q is not a host name in lower case", h)
		}
		return nil
	}); err != nil {
		return err
	}
	if c.CallDepth != nil && (*c.CallDepth < 0 || *c.CallDepth > MaxCallDepth) {
		return fmt.Errorf("%w: call_depth is from 0 to %d", ErrInvalid, MaxCallDepth)
	}
	if p.Model != "" {
		if _, _, ok := SplitModel(p.Model); !ok || len(p.Model) > MaxName {
			return fmt.Errorf("%w: model %q is provider/model, or policy/<name>", ErrInvalid, p.Model)
		}
	}
	if p.CompactionTokens != 0 && (p.CompactionTokens < MinCompactionTokens || p.CompactionTokens > MaxCompactionTokens) {
		return fmt.Errorf("%w: compaction_tokens is from %d to %d", ErrInvalid, MinCompactionTokens, MaxCompactionTokens)
	}
	return nil
}

func checkList(name string, list []string, each func(string) error) error {
	if len(list) > MaxList {
		return fmt.Errorf("%w: %s holds at most %d entries", ErrInvalid, name, MaxList)
	}
	for i, x := range list {
		if x == "" || len(x) > MaxName || strings.ContainsAny(x, "\x00\r\n") {
			return fmt.Errorf("%w: %s[%d] is empty or too long", ErrInvalid, name, i)
		}
		if slices.Contains(list[:i], x) {
			return fmt.Errorf("%w: %s names %q twice", ErrInvalid, name, x)
		}
		if err := each(x); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrInvalid, name, err)
		}
	}
	return nil
}

// HostOf is the host of an https URL, in lower case, as tool_servers lists
// it. "" for anything else.
func HostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}
