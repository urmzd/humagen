package platform

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
)

// The agent store holds the platform's optional agents as packages, each a
// versioned base definition (builtinSpecs). The entry agent and the approval
// agent are in every workspace; a workspace adds the rest from the store.
//
// Whether a package is installed is never stored: it is installed while any
// of its agents is in the workspace, not retired. An agent records the
// version it started from (wire.PackageBase), a newer version shows as an
// update to take or leave, and Reset returns it to its base. The store adds
// and retires whole packages; nothing else adds one, and EnsureBuiltins only
// keeps an installed, platform-written agent at the current version.

// ErrStoreConflict reports a store change the workspace's agents stand in
// the way of: a name one of its own agents holds, or an agent changed in the
// workspace that removing would retire. The message names them.
var ErrStoreConflict = errors.New("platform: store conflict")

// storeListing is what a person reads about one of the store's packages.
type storeListing struct {
	name, title, description string
	// gives completes "install the X package from the Store to ...".
	gives string
}

// agentStore lists the store's packages, by the name their agents record.
var agentStore = []storeListing{{
	name:        "humagen/core",
	title:       "Core",
	description: "A writer that publishes documents and an analyst that reports on how your agents are doing.",
	gives:       "to use the writer and the analyst",
}, {
	name:        "humagen/review",
	title:       "Review",
	description: "Pull request reviews, with security, architecture and test specialists and one verdict.",
	gives:       "to get pull request reviews",
}}

// listing is the store's listing for a package name, or false when the
// package is not in the store (humagen/system, the entry agent's).
func listing(name string) (storeListing, bool) {
	i := slices.IndexFunc(agentStore, func(l storeListing) bool { return l.name == name })
	if i < 0 {
		return storeListing{}, false
	}
	return agentStore[i], true
}

// optional reports whether a platform spec is one of the store's: a
// workspace has it only once its package is installed.
func optional(spec wire.AgentSpec) bool {
	if spec.Package == nil {
		return false
	}
	_, ok := listing(spec.Package.Name)
	return ok
}

// StorePackage is one of the store's packages as a workspace has it.
type StorePackage struct {
	// Name is the package's name, the one its agents record (humagen/core).
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Version is the package's current version.
	Version string `json:"version"`
	// Installed is true while any of its agents is in the workspace.
	Installed bool         `json:"installed"`
	Agents    []StoreAgent `json:"agents"`
	// Missing names the agents installing would add: those not in the
	// workspace, or retired. A name one of the workspace's own agents
	// holds is never missing; it is taken.
	Missing []string `json:"missing"`
}

// StoreAgent is one agent of a package, as the workspace has it.
type StoreAgent struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Installed: the workspace has it, from this package.
	Installed bool `json:"installed"`
	// Customized: someone in the workspace changed it since the platform
	// last wrote it. Removing the package retires it with its changes,
	// which stay in its history.
	Customized bool `json:"customized"`
	// UpdateAvailable: a customized agent based on an older version.
	UpdateAvailable bool `json:"update_available"`
	// Taken: one of the workspace's own agents, not based on this package,
	// holds the name. Installing is refused until it is renamed.
	Taken bool `json:"taken"`
}

// StoreInstall is what installing a package did.
type StoreInstall struct {
	Package StorePackage `json:"package"`
	// Added names the agents installing created or restored: none when the
	// package was installed already.
	Added []string `json:"added"`
}

// StoreRemoval is what removing a package did.
type StoreRemoval struct {
	Package StorePackage `json:"package"`
	// Retired names the agents removing retired. Their history and pinned
	// revisions stay readable.
	Retired []string `json:"retired"`
	// Customized names those of them that had changes made in the
	// workspace, retired because the removal was forced.
	Customized []string `json:"customized"`
}

// storeSpecs are the current specs of a package's agents, in the order the
// platform ships them.
func (p *Platform) storeSpecs(pkg string) []wire.AgentSpec {
	var out []wire.AgentSpec
	for _, spec := range p.builtinSpecs() {
		if spec.Package != nil && spec.Package.Name == pkg {
			out = append(out, spec)
		}
	}
	return out
}

// storePackage resolves a package by its name, or by its last part
// ("review" for humagen/review), to its listing and agents. A package this
// server ships no agents for (no built-in agents here) is not found.
func (p *Platform) storePackage(name string) (storeListing, []wire.AgentSpec, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, l := range agentStore {
		if l.name != name && strings.TrimPrefix(l.name, "humagen/") != name {
			continue
		}
		if specs := p.storeSpecs(l.name); len(specs) > 0 {
			return l, specs, nil
		}
	}
	return storeListing{}, nil, fmt.Errorf("%w: the store has no package %q", wire.ErrNotFound, name)
}

// storeAgent is one package agent as tenant has it, and the revision it is
// at when the workspace has it.
func (p *Platform) storeAgent(ctx context.Context, tenant string, spec wire.AgentSpec) (StoreAgent, wire.Revision, error) {
	a := StoreAgent{Name: spec.Name, Description: spec.Description}
	have, err := p.Catalog().Get(ctx, tenant, spec.Name, 0)
	switch {
	case errors.Is(err, wire.ErrNotFound):
		return a, wire.Revision{}, nil
	case err != nil:
		return a, wire.Revision{}, err
	}
	if p.checkBuiltinOwner(ctx, tenant, have) != nil {
		a.Taken = true
		return a, have, nil
	}
	r := p.PackageRevision(have)
	a.Installed, a.Customized, a.UpdateAvailable = true, r.Customized, r.UpdateAvailable
	return a, have, nil
}

// storeView is a package as tenant has it now.
func (p *Platform) storeView(ctx context.Context, tenant string, l storeListing, specs []wire.AgentSpec) (StorePackage, error) {
	out := StorePackage{Name: l.name, Title: l.title, Description: l.description, Version: specs[0].Package.Version, Agents: []StoreAgent{}, Missing: []string{}}
	for _, spec := range specs {
		a, _, err := p.storeAgent(ctx, tenant, spec)
		if err != nil {
			return StorePackage{}, err
		}
		out.Installed = out.Installed || a.Installed
		if !a.Installed && !a.Taken {
			out.Missing = append(out.Missing, a.Name)
		}
		out.Agents = append(out.Agents, a)
	}
	return out, nil
}

// StorePackages lists the store's packages and what tenant has of each.
// A server that ships no built-in agents has an empty store.
func (p *Platform) StorePackages(ctx context.Context, tenant string) ([]StorePackage, error) {
	out := []StorePackage{}
	for _, l := range agentStore {
		specs := p.storeSpecs(l.name)
		if len(specs) == 0 {
			continue
		}
		v, err := p.storeView(ctx, tenant, l, specs)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// InstallPackage adds a package's missing agents to who's workspace, each a
// platform revision at the package's current version. A name the store
// retired, or anyone did, is revived: its versions continue and every
// pinned revision stays as it was. Installing an installed package adds
// nothing. A name one of the workspace's own agents holds refuses the whole
// install, before anything is added.
func (p *Platform) InstallPackage(ctx context.Context, who auth.Principal, name string) (StoreInstall, error) {
	if !who.Can(auth.AgentsWrite) {
		return StoreInstall{}, auth.ErrForbidden
	}
	l, specs, err := p.storePackage(name)
	if err != nil {
		return StoreInstall{}, err
	}
	p.storeMu.Lock()
	defer p.storeMu.Unlock()
	var add []wire.AgentSpec
	var taken []string
	for _, spec := range specs {
		a, _, err := p.storeAgent(ctx, who.Tenant, spec)
		if err != nil {
			return StoreInstall{}, err
		}
		switch {
		case a.Taken:
			taken = append(taken, "@"+a.Name)
		case !a.Installed:
			add = append(add, spec)
		}
	}
	if len(taken) > 0 {
		return StoreInstall{}, fmt.Errorf("%w: %s %s one of this workspace's own agents, not the %s package's; rename it to install %s", ErrStoreConflict, strings.Join(taken, " and "), plural(len(taken), "is", "are"), l.title, l.title)
	}
	out := StoreInstall{Added: []string{}}
	if len(add) > 0 {
		for _, spec := range add {
			out.Added = append(out.Added, spec.Name)
		}
		attempt, err := p.BeginAudit(who, audit.Change{
			Route: "POST /api/store/packages/{name}/install", Rule: string(auth.AgentsWrite),
			Action: "agent.package.install", Resource: "agent_package", ID: l.name, Scope: "workspace",
			After: map[string]string{"version": specs[0].Package.Version, "added": strings.Join(out.Added, ","), "count": strconv.Itoa(len(out.Added))},
		})
		if err != nil {
			return StoreInstall{}, err
		}
		err = p.addPackageAgents(ctx, who.Tenant, add)
		if completion := attempt.Finish(err); completion != nil {
			p.LogAuditCompletionFailure(completion)
		}
		if err != nil {
			return StoreInstall{}, err
		}
		p.log.Info("platform: store package installed", "tenant", who.Tenant, "package", l.name, "version", specs[0].Package.Version, "added", out.Added, "by", who.Subject)
	}
	// The entry agent may call what was added, and every agent has a home.
	if err := p.EnsureBuiltins(ctx, who.Tenant); err != nil {
		return StoreInstall{}, err
	}
	if out.Package, err = p.storeView(ctx, who.Tenant, l, specs); err != nil {
		return StoreInstall{}, err
	}
	return out, nil
}

// addPackageAgents creates each spec as a platform revision, with its old
// handles (internal/legacy) beside it as EnsureBuiltins always kept them, so
// saved runs, schedules and grants that name one answer again.
func (p *Platform) addPackageAgents(ctx context.Context, tenant string, specs []wire.AgentSpec) error {
	for _, spec := range specs {
		for _, name := range append([]string{spec.Name}, aliasesOf(spec.Name)...) {
			want := spec
			want.Name = name
			if name != spec.Name {
				// An old handle the workspace has, or one of its own agents holds, stays.
				if _, err := p.Catalog().Get(ctx, tenant, name, 0); !errors.Is(err, wire.ErrNotFound) {
					continue
				}
			}
			if _, err := p.Agents.Create(ctx, tenant, want, "platform"); err != nil && !errors.Is(err, wire.ErrExists) {
				return fmt.Errorf("platform: add %s: %w", name, err)
			}
		}
	}
	return nil
}

// aliasesOf are the old handles of a current agent name, sorted.
func aliasesOf(name string) []string {
	var out []string
	for old, current := range legacy.BuiltinAgents() {
		if current == name {
			out = append(out, old)
		}
	}
	slices.Sort(out)
	return out
}

// RemovePackage retires a package's agents in who's workspace, and the old
// handles of each. Retiring keeps every revision readable: history, pinned
// revisions and the runs they answered stay. An agent someone in the
// workspace changed is retired only when force is set; without it nothing is
// retired and the conflict names them, so changes are never lost by
// surprise. A name one of the workspace's own agents holds is never
// retired. Removing a package that is not installed retires nothing.
func (p *Platform) RemovePackage(ctx context.Context, who auth.Principal, name string, force bool) (StoreRemoval, error) {
	if !who.Can(auth.AgentsWrite) {
		return StoreRemoval{}, auth.ErrForbidden
	}
	l, specs, err := p.storePackage(name)
	if err != nil {
		return StoreRemoval{}, err
	}
	p.storeMu.Lock()
	defer p.storeMu.Unlock()
	type held struct {
		name string
		rev  int
	}
	var retire []held
	out := StoreRemoval{Retired: []string{}, Customized: []string{}}
	for _, spec := range specs {
		a, have, err := p.storeAgent(ctx, who.Tenant, spec)
		if err != nil {
			return StoreRemoval{}, err
		}
		if !a.Installed {
			continue
		}
		retire = append(retire, held{a.Name, have.Rev})
		out.Retired = append(out.Retired, a.Name)
		if a.Customized {
			out.Customized = append(out.Customized, a.Name)
		}
	}
	if len(out.Customized) > 0 && !force {
		names := make([]string, len(out.Customized))
		for i, n := range out.Customized {
			names[i] = "@" + n
		}
		return StoreRemoval{}, fmt.Errorf("%w: %s %s changes made in this workspace; remove with force to retire %s too (%s history stays readable)", ErrStoreConflict, strings.Join(names, " and "), plural(len(names), "has", "have"), plural(len(names), "it", "them"), plural(len(names), "its", "their"))
	}
	if len(retire) > 0 {
		attempt, err := p.BeginAudit(who, audit.Change{
			Route: "DELETE /api/store/packages/{name}", Rule: string(auth.AgentsWrite),
			Action: "agent.package.remove", Resource: "agent_package", ID: l.name, Scope: "workspace",
			After: map[string]string{"retired": strings.Join(out.Retired, ","), "customized": strings.Join(out.Customized, ","), "forced": strconv.FormatBool(force)},
		})
		if err != nil {
			return StoreRemoval{}, err
		}
		err = func() error {
			for _, h := range retire {
				if err := p.retireAgent(ctx, who.Tenant, h.name, h.rev); err != nil {
					return err
				}
				for _, old := range aliasesOf(h.name) {
					have, err := p.Catalog().Get(ctx, who.Tenant, old, 0)
					if err != nil || p.checkBuiltinOwner(ctx, who.Tenant, have) != nil {
						continue
					}
					if err := p.retireAgent(ctx, who.Tenant, old, have.Rev); err != nil {
						return err
					}
				}
			}
			return nil
		}()
		if completion := attempt.Finish(err); completion != nil {
			p.LogAuditCompletionFailure(completion)
		}
		if err != nil {
			return StoreRemoval{}, err
		}
		p.log.Info("platform: store package removed", "tenant", who.Tenant, "package", l.name, "retired", out.Retired, "customized", out.Customized, "by", who.Subject)
	}
	// The entry agent no longer lists what was retired.
	if err := p.EnsureBuiltins(ctx, who.Tenant); err != nil {
		return StoreRemoval{}, err
	}
	if out.Package, err = p.storeView(ctx, who.Tenant, l, specs); err != nil {
		return StoreRemoval{}, err
	}
	return out, nil
}

// retireAgent retires name at rev, the revision removal read: a revision
// written since is a conflict, so a change made meanwhile is never retired
// unseen. One already retired is left as it is.
func (p *Platform) retireAgent(ctx context.Context, tenant, name string, rev int) error {
	unlock, err := p.lockSetupRevision(tenant, name)
	if err != nil {
		return err
	}
	defer unlock()
	if err := p.Agents.Delete(ctx, tenant, name, rev); err != nil && !errors.Is(err, wire.ErrNotFound) {
		return fmt.Errorf("platform: retire %s: %w", name, err)
	}
	return nil
}

// NotInstalled says which package to install for an agent of the store that
// tenant does not have: "@code-reviewer is not installed: install the Review
// package from the Store to get pull request reviews". It is "" for any other
// agent, and for one the workspace has. Admission asks it of every run, so
// any other agent is told apart without a read.
func (p *Platform) NotInstalled(ctx context.Context, tenant, agent string) string {
	name := wire.NormalizeName(agent)
	l, ok := p.storeMember(legacy.BuiltinAgent(name))
	if !ok {
		return ""
	}
	if _, err := p.Catalog().Get(ctx, tenant, name, 0); !errors.Is(err, wire.ErrNotFound) {
		return ""
	}
	return fmt.Sprintf("@%s is not installed: install the %s package from the Store %s", name, l.title, l.gives)
}

// storeMember is the store package an agent name belongs to. The store's
// agents are this platform's for its life, so they are worked out once.
func (p *Platform) storeMember(name string) (storeListing, bool) {
	p.storeOnce.Do(func() {
		p.storeNames = map[string]storeListing{}
		for _, spec := range p.builtinSpecs() {
			if optional(spec) {
				p.storeNames[spec.Name], _ = listing(spec.Package.Name)
			}
		}
	})
	l, ok := p.storeNames[name]
	return l, ok
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
