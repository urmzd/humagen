package platform

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/integrations/github"
)

// get_context for a conversation that came from a GitHub issue or pull
// request: the place is the issue or pull request it was asked on, read
// through the GitHub client that raised the run, and only when that client
// is granted the repository (its owners, the repositories it names, or its
// App installation). A person asking from the console reads through
// GitHubIssue, under the same grants and the repository's own setting.
// What is read is untrusted: it goes in one data block whose markers carry
// a nonce made for this read, as the reads before a run do.

// Bounds on what get_context reads from GitHub.
const (
	// placeReadBudget keeps the block, with its lead, inside maxContextText,
	// so the closing marker is never cut.
	placeReadBudget = maxContextText - 1500
	// placeReadBody is the description.
	placeReadBody = 3000
	// placeReadComments is how many of the latest comments are read.
	placeReadComments = 5
	// placeReadComment is one comment.
	placeReadComment = 800
	// placeReadPatch is one changed file's patch.
	placeReadPatch = 1500
	// placeReadTimeout bounds every read for one get_context.
	placeReadTimeout = 10 * time.Second
)

// githubContext is get_context's resolver for github:// places.
func (p *Platform) githubContext(ctx context.Context, req ContextRequest) (ContextResult, error) {
	repo, kind, number, ok := channelRepository(req.Origin)
	if !ok || number <= 0 {
		return ContextResult{Note: "This conversation came from a GitHub repository, not one of its issues or pull requests, so there is nothing to read here."}, nil
	}
	owner, name, _ := strings.Cut(repo, "/")
	ctx, cancel := context.WithTimeout(ctx, placeReadTimeout)
	defer cancel()
	var read github.Context
	var err error
	if req.Run != "" {
		api, granted := p.runPlaceReader(ctx, req.Tenant, req.Run, owner, name)
		if !granted {
			return ContextResult{Note: fmt.Sprintf("%s may not be read here: this run was not raised by a GitHub client granted it.", repo)}, nil
		}
		read, err = api.ReadContext(ctx, github.Ref{Owner: owner, Repo: name, Number: number}, 1)
	} else {
		var issue GitHubIssue
		issue, err = p.GitHubIssue(ctx, req.Tenant, repo, number, 1)
		read = issue.Context
	}
	if errors.Is(err, ErrSourceNotFound) || errors.Is(err, ErrSourceInvalid) {
		return ContextResult{Note: fmt.Sprintf("%s #%d may not be read here: no GitHub client of this workspace is granted %s, or it does not exist.", placeNoun(kind), number, repo)}, nil
	}
	if err != nil {
		p.log.Warn("platform: a GitHub place could not be read for get_context", "tenant", req.Tenant, "repository", repo, "number", number, "error", err)
		return ContextResult{Note: fmt.Sprintf("%s #%d could not be read here.", placeNoun(kind), number)}, nil
	}
	link := read.Subject.URL
	if !strings.HasPrefix(link, "https://github.com/") {
		link = fmt.Sprintf("https://github.com/%s/%s/%d", repo, kind, number)
	}
	return ContextResult{
		Text:    placeRead(read, repo, kind, number, readNonce()),
		Sources: []ContextSource{{Title: fmt.Sprintf("%s#%d", repo, number), URL: link}},
	}, nil
}

// runPlaceReader is the issue and pull request reader of the GitHub client
// that raised run, when that client is enabled and granted owner/name.
func (p *Platform) runPlaceReader(ctx context.Context, tenant, runID, owner, name string) (github.ContextAPI, bool) {
	run, err := p.runRecordOf(tenant, runID)
	if err != nil || run.Client == "" {
		return nil, false
	}
	s := p.surfaces["github"]
	if s == nil {
		return nil, false
	}
	st := s.seat(tenant, run.Client)
	if st == nil || st.context == nil {
		return nil, false
	}
	c, enabled := p.currentInstallation(st.client)
	if !enabled {
		return nil, false
	}
	granted := c.Serves(owner)
	if g := c.GitHub; !granted && g != nil {
		granted = slices.ContainsFunc(g.ReadRepositories, func(r string) bool { return strings.EqualFold(r, owner+"/"+name) })
	}
	if !granted {
		granted = st.installed.Covers(ctx, owner, name)
	}
	return st.context, granted
}

// placeRead is an issue or pull request as get_context gives it: its title
// and state, its description, its latest comments, and for a pull request
// its changed files and their patches, bounded, in one data block.
func placeRead(read github.Context, repo, kind string, number int, nonce string) string {
	b := newReadBlock(placeReadBudget)
	what := placeNoun(kind)
	b.write(fmt.Sprintf("%s %s#%d: %s (%s)\n", what, repo, number, read.Subject.Title, read.Subject.State))
	if kind == "pull" && read.Subject.Head.Ref != "" {
		b.write(fmt.Sprintf("Head: %s at %s\n", read.Subject.Head.Ref, read.Subject.Head.SHA))
	}
	if body, more := clipRead(read.Subject.Body, placeReadBody); body != "" {
		b.write("\nDescription:\n")
		b.fenced("text", body)
		if more {
			b.write("(description shortened)\n")
		}
	}
	// Comments by apps, Humagen's own answers among them, are left out.
	var people []github.ContextComment
	for _, c := range read.Comments {
		if c.User.Login != "" && !strings.HasSuffix(c.User.Login, "[bot]") {
			people = append(people, c)
		}
	}
	comments := people[max(0, len(people)-placeReadComments):]
	if len(comments) > 0 {
		b.write("\nLatest comments:\n")
		for _, c := range comments {
			body, more := clipRead(c.Body, placeReadComment)
			b.write(fmt.Sprintf("\n%s wrote:\n", c.User.Login))
			b.fenced("text", body)
			if more {
				b.write("(comment shortened)\n")
			}
		}
	}
	if kind == "pull" && len(read.Files) > 0 {
		b.write("\nChanged files:\n")
		for _, f := range read.Files {
			b.write(fmt.Sprintf("- %s (%s)\n", f.Filename, f.Status))
		}
		if read.NextPage > 0 {
			b.write("More files changed than one page holds; read the rest with your GitHub tools.\n")
		}
		b.write("\nPatches:\n")
		for _, f := range read.Files {
			patch, more := clipRead(f.Patch, placeReadPatch)
			if patch == "" {
				continue
			}
			b.write(fmt.Sprintf("\n%s\n", f.Filename))
			b.fenced("diff", patch)
			if more {
				b.write("(patch shortened)\n")
			}
		}
	}
	return b.wrap(fmt.Sprintf("Humagen read %s#%d, the %s this conversation was asked on.", repo, number, strings.ToLower(what)), nonce)
}

// placeLine names the GitHub issue or pull request a run was asked on, for
// the briefs it gives the agents it calls. Empty for any other place.
func placeLine(channel string) string {
	repo, kind, number, ok := channelRepository(channel)
	if !ok || number <= 0 {
		return ""
	}
	return fmt.Sprintf("Asked on GitHub %s %s#%d (https://github.com/%s/%s/%d).", strings.ToLower(placeNoun(kind)), repo, number, repo, kind, number)
}

// runSessionURL is the console's address of the session run answers in,
// for a link under its GitHub comment. Empty without a canonical console
// address, for a run of another tenant, or one in no session.
func (p *Platform) runSessionURL(tenant, viewTenant, runID string) string {
	if p.cfg.PublicURL == "" || viewTenant != tenant || runID == "" {
		return ""
	}
	run, err := p.Runs.Get(tenant, runID)
	if err != nil || run.Thread == "" {
		return ""
	}
	base, err := url.Parse(p.cfg.PublicURL)
	if err != nil || (base.Scheme != "https" && base.Scheme != "http") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return ""
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/sessions/" + url.PathEscape(run.Thread)
	return base.String()
}
