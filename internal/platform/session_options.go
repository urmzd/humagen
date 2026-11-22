package platform

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/images"
	"github.com/urmzd/humagen/pkg/threads"
)

// MaxSessionOptionRepos is how many repositories the options list.
const MaxSessionOptionRepos = 100

// SessionOptions is what a person may choose when starting a session: the
// image its tools run in and the repositories it works on.
type SessionOptions struct {
	// Images are the deployment's default first, then the workspace's own;
	// only a ready one may be chosen.
	Images []SessionImage `json:"images"`
	// ImageRegistries are where the workspace may add images from; empty
	// when it may add none.
	ImageRegistries []string `json:"imageRegistries"`
	// Sandbox says where the images run, as the deployment names its
	// sandbox ("docker/runsc"), and Isolated whether each run has an
	// instance of its own. Terminal says a person may run commands there.
	Sandbox  string   `json:"sandbox"`
	Isolated bool     `json:"isolated"`
	Terminal bool     `json:"terminal"`
	Repos    []string `json:"repos"`
}

// SessionOptions lists the images the caller's workspace may run a session
// in and the repositories its enabled GitHub clients may read: the first
// MaxSessionOptionRepos of them. The tenant is the principal's.
func (p *Platform) SessionOptions(ctx context.Context, who auth.Principal) (SessionOptions, error) {
	imgs, err := p.SessionImages(who)
	if err != nil {
		return SessionOptions{}, err
	}
	repos := p.tenantRepos(ctx, who.Tenant)
	return SessionOptions{
		Images:          imgs,
		ImageRegistries: p.ImageRegistries(),
		Sandbox:         p.cfg.Sandbox.String(),
		Isolated:        p.cfg.Sandbox.Isolated(),
		Terminal:        p.PersonCommands(),
		Repos:           repos[:min(len(repos), MaxSessionOptionRepos)],
	}, nil
}

// tenantRepos are the "owner/name" repositories the tenant's enabled GitHub
// clients cover: those each client's installation covers, and those its
// read_repositories or action_repositories name. They are deduplicated in
// any case and sorted. An installation's list is capped at
// github.MaxInstallationRepos, and a client's named ones at what
// clients.Validate takes, so the whole is bounded. A client whose list is
// not read before ctx is done adds the list it last read, or none.
func (p *Platform) tenantRepos(ctx context.Context, tenant string) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(r string) {
		if r = strings.TrimSpace(r); r != "" && !seen[strings.ToLower(r)] {
			seen[strings.ToLower(r)] = true
			out = append(out, r)
		}
	}
	var seats []*seat
	if s := p.surfaces["github"]; s != nil {
		for _, st := range s.all() {
			if c := st.client; c.Tenant == tenant && c.Enabled && (c.GitHub != nil || st.installed != nil) {
				seats = append(seats, st)
			}
		}
	}
	// Each installation's list is read at once, side by side, so the first
	// use waits for the slowest client, not for all of them in turn.
	installed := make([][]string, len(seats))
	var wg sync.WaitGroup
	for i, st := range seats {
		if st.installed == nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			installed[i], _ = st.installed.Repos(ctx)
		}()
	}
	wg.Wait()
	// A repository the workspace keeps to its own runs is not one a session
	// elsewhere may read, whichever client covers it.
	kept, ok := p.keptToOwnRuns(tenant)
	if !ok {
		return out
	}
	for i, st := range seats {
		offer := func(r string) {
			if !kept[strings.ToLower(strings.TrimSpace(r))] {
				add(r)
			}
		}
		for _, r := range installed[i] {
			offer(r)
		}
		if g := st.client.GitHub; g != nil {
			for _, r := range slices.Concat(g.ReadRepositories, g.ActionRepositories) {
				offer(r)
			}
		}
	}
	slices.SortFunc(out, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return out
}

// checkSessionChoice refuses an image or repositories this tenant was not
// offered, or an image that is not ready. It runs before anything is
// created.
func (p *Platform) checkSessionChoice(ctx context.Context, who auth.Principal, in SessionInput) error {
	if in.Image == "" && len(in.Repos) == 0 {
		return nil
	}
	if in.Surface != nil {
		return fmt.Errorf("%w: image and repos are not taken with a surface", threads.ErrInvalid)
	}
	if len(in.Repos) > threads.MaxSessionRepos {
		return fmt.Errorf("%w: at most %d repos", threads.ErrInvalid, threads.MaxSessionRepos)
	}
	opts, err := p.SessionOptions(ctx, who)
	if err != nil {
		return err
	}
	// A repository is checked against every one the tenant covers, not only
	// the ones the options list, so an installation with more than the list
	// shows can still start a session on any of them.
	repos := p.tenantRepos(ctx, who.Tenant)
	if in.Image != "" {
		i := slices.IndexFunc(opts.Images, func(img SessionImage) bool { return img.ID == in.Image })
		if i < 0 {
			return fmt.Errorf("%w: image is not one this workspace has", threads.ErrInvalid)
		}
		if opts.Images[i].Status != images.Ready {
			return fmt.Errorf("%w: image %s is %s, not ready", threads.ErrInvalid, opts.Images[i].Name, opts.Images[i].Status)
		}
	}
	seen := map[string]bool{}
	for _, r := range in.Repos {
		owner, name, ok := strings.Cut(r, "/")
		if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
			return fmt.Errorf("%w: a repo is owner/name", threads.ErrInvalid)
		}
		if !slices.Contains(repos, r) {
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
