package platform

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/threads"
)

// Bounds on what a session's environment and repository choice may hold.
const (
	// EnvironmentAuto is the deployment's own default environment.
	EnvironmentAuto = "auto"
	// MaxSessionOptionRepos is how many repositories the options list.
	MaxSessionOptionRepos = 100
)

// SessionEnvironment is one place a session's work may run.
type SessionEnvironment struct {
	// Name is what a session stores: "auto" or the sandbox's kind.
	Name  string `json:"name"`
	Label string `json:"label"`
	// Isolated says each run leases an instance of its own.
	Isolated bool `json:"isolated"`
	// Terminal says a person may run their own commands there.
	Terminal bool `json:"terminal"`
}

// SessionOptions is what a person may choose when starting a session: where
// it runs and which repositories it works on.
type SessionOptions struct {
	Environments []SessionEnvironment `json:"environments"`
	Repos        []string             `json:"repos"`
}

// SessionOptions lists this deployment's environments and the repositories
// the caller's tenant's enabled GitHub clients may read. The tenant is the
// principal's.
func (p *Platform) SessionOptions(_ context.Context, who auth.Principal) SessionOptions {
	sb := p.cfg.Sandbox
	name, label := "unisolated", "This server (unisolated)"
	switch kind := sb.String(); {
	case !sb.Isolated():
	case strings.HasPrefix(kind, "docker"):
		name, label = "docker", "Docker sandbox"
	case strings.HasPrefix(kind, "local"):
		name, label = "local", "This server"
	default:
		name, label = kind, kind
	}
	terminal := p.PersonCommands()
	env := []SessionEnvironment{
		{Name: EnvironmentAuto, Label: "Auto", Isolated: sb.Isolated(), Terminal: terminal},
		{Name: name, Label: label, Isolated: sb.Isolated(), Terminal: terminal},
	}
	return SessionOptions{Environments: env, Repos: p.tenantRepos(who.Tenant)}
}

// tenantRepos are the "owner/name" repositories the tenant's enabled GitHub
// clients list for reading or for writes: deduplicated, sorted, capped.
func (p *Platform) tenantRepos(tenant string) []string {
	out := []string{}
	if s := p.surfaces["github"]; s != nil {
		for _, st := range s.all() {
			c := st.client
			if c.Tenant != tenant || !c.Enabled || c.GitHub == nil {
				continue
			}
			for _, r := range slices.Concat(c.GitHub.ReadRepositories, c.GitHub.ActionRepositories) {
				if r = strings.TrimSpace(r); r != "" && !slices.Contains(out, r) {
					out = append(out, r)
				}
			}
		}
	}
	slices.Sort(out)
	return out[:min(len(out), MaxSessionOptionRepos)]
}

// checkSessionChoice refuses an environment or repositories this tenant was
// not offered. It runs before anything is created.
func (p *Platform) checkSessionChoice(ctx context.Context, who auth.Principal, in SessionInput) error {
	if in.Environment == "" && len(in.Repos) == 0 {
		return nil
	}
	if in.Surface != nil {
		return fmt.Errorf("%w: environment and repos are not taken with a surface", threads.ErrInvalid)
	}
	if len(in.Repos) > threads.MaxSessionRepos {
		return fmt.Errorf("%w: at most %d repos", threads.ErrInvalid, threads.MaxSessionRepos)
	}
	opts := p.SessionOptions(ctx, who)
	if in.Environment != "" && !slices.ContainsFunc(opts.Environments, func(e SessionEnvironment) bool { return e.Name == in.Environment }) {
		return fmt.Errorf("%w: environment is not one this deployment offers", threads.ErrInvalid)
	}
	seen := map[string]bool{}
	for _, r := range in.Repos {
		owner, name, ok := strings.Cut(r, "/")
		if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
			return fmt.Errorf("%w: a repo is owner/name", threads.ErrInvalid)
		}
		if !slices.Contains(opts.Repos, r) {
			return fmt.Errorf("%w: %s is not a repository you may choose", threads.ErrInvalid, r)
		}
		if seen[r] {
			return fmt.Errorf("%w: %s is listed twice", threads.ErrInvalid, r)
		}
		seen[r] = true
	}
	return nil
}

// BrowserAvailability says whether a session has a browser a person can look
// at, and if not, why.
type BrowserAvailability struct {
	Available bool     `json:"available"`
	Providers []string `json:"providers"`
	Reason    string   `json:"reason"`
}

// SessionBrowserAvailability reports whether the session's agent holds a
// browser. It reads; it opens no connection to the browser.
func (p *Platform) SessionBrowserAvailability(ctx context.Context, who auth.Principal, id string) (BrowserAvailability, error) {
	th, err := p.SessionRecord(ctx, who, id)
	if err != nil {
		return BrowserAvailability{}, err
	}
	none := BrowserAvailability{Providers: []string{}}
	if th.Session == nil {
		none.Reason = "Only an agent's session has a browser."
		return none, nil
	}
	none.Reason = "This session has no agent with a browser."
	if th.Session.Agent == "" {
		return none, nil
	}
	rev, err := p.Catalog().Get(ctx, who.Tenant, th.Session.Agent, 0)
	if err != nil {
		return none, nil
	}
	providers := p.browserProviders(ctx, who.Tenant, rev.Spec.Tools)
	if len(providers) == 0 {
		return none, nil
	}
	return BrowserAvailability{Available: true, Providers: providers}, nil
}
