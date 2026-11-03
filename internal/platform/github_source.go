package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/urmzd/humagen/pkg/integrations/github"
)

// The bounds of a source excerpt: how many lines one shows and how many lines
// of context it may add around them, and how many files are kept read.
const (
	SourceMaxLines   = 200
	SourceMaxContext = 20
	sourceCacheFiles = 64
)

var (
	// ErrSourceInvalid is a source request that names no file a commit holds.
	ErrSourceInvalid = errors.New("source: invalid request")
	// ErrSourceNotFound is a file no GitHub client of the workspace may read:
	// the same answer whether it is not granted or does not exist.
	ErrSourceNotFound = errors.New("source: not found")
	// ErrSourceUnavailable is a file GitHub would not give as text.
	ErrSourceUnavailable = errors.New("source: unavailable")
)

// SourceRequest names lines of a file at a commit, as an evidence permalink
// does: https://github.com/<repo>/blob/<sha>/<path>#L<start>-L<end>.
type SourceRequest struct {
	Repo  string // owner/name
	SHA   string // full commit SHA
	Path  string
	Start int
	End   int
	// Context is how many lines to add before and after.
	Context int
}

// SourceExcerpt is lines of one file at one commit, numbered from First.
type SourceExcerpt struct {
	Repo  string
	SHA   string
	Path  string
	First int
	Lines []string
	// Start and End are the lines asked for, inside the excerpt.
	Start, End int
	// Total is how many lines the file has.
	Total int
	URL   string
}

// sourceReader reads a file at a commit: a GitHub client's API.
type sourceReader = github.FileAPI

// sourceFiles keeps files read at a commit. A file at a commit never changes,
// so an entry is never stale; the oldest goes when the cache is full. It is
// read only after the caller's access is checked.
type sourceFiles struct {
	mu    sync.Mutex
	order []string
	files map[string]string
}

func (c *sourceFiles) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f, ok := c.files[key]
	return f, ok
}

func (c *sourceFiles) put(key, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.files == nil {
		c.files = map[string]string{}
	}
	if _, ok := c.files[key]; ok {
		return
	}
	if len(c.order) >= sourceCacheFiles {
		delete(c.files, c.order[0])
		c.order = c.order[1:]
	}
	c.order = append(c.order, key)
	c.files[key] = content
}

// GitHubSource reads lines of a file at a commit through a GitHub client of
// the tenant, so the client's token never reaches the browser. A client may
// read a repository its owners include or its read_repositories name; with no
// such client the file is not found.
func (p *Platform) GitHubSource(ctx context.Context, tenant string, req SourceRequest) (SourceExcerpt, error) {
	owner, repo, ok := strings.Cut(req.Repo, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return SourceExcerpt{}, fmt.Errorf("%w: repo must be owner/name", ErrSourceInvalid)
	}
	if !fullSHA(req.SHA) {
		return SourceExcerpt{}, fmt.Errorf("%w: sha must be a full commit SHA", ErrSourceInvalid)
	}
	if req.Path == "" || strings.HasPrefix(req.Path, "/") || len(req.Path) > 1024 {
		return SourceExcerpt{}, fmt.Errorf("%w: path", ErrSourceInvalid)
	}
	if req.Start < 1 || req.End < req.Start || req.End-req.Start+1 > SourceMaxLines {
		return SourceExcerpt{}, fmt.Errorf("%w: lines must be 1 or more, at most %d of them", ErrSourceInvalid, SourceMaxLines)
	}
	if req.Context < 0 || req.Context > SourceMaxContext {
		return SourceExcerpt{}, fmt.Errorf("%w: context is 0 to %d lines", ErrSourceInvalid, SourceMaxContext)
	}
	reader := p.sourceReader(tenant, owner, repo)
	if reader == nil {
		return SourceExcerpt{}, ErrSourceNotFound
	}
	key := strings.ToLower(req.Repo) + "@" + strings.ToLower(req.SHA) + ":" + req.Path
	content, ok := p.surfaces["github"].sources.get(key)
	if !ok {
		// Ref.Number only satisfies the read's validation: a file at a commit
		// belongs to no issue or pull request.
		f, err := reader.ReadFile(ctx, github.Ref{Owner: owner, Repo: repo, Number: 1}, req.SHA, req.Path)
		if err != nil {
			// GitHub answers a file the installation may not read as missing.
			var missing *github.NotFoundError
			why := strings.ToLower(err.Error())
			if errors.As(err, &missing) || strings.Contains(why, "not found") || strings.Contains(why, "forbidden") {
				return SourceExcerpt{}, ErrSourceNotFound
			}
			return SourceExcerpt{}, fmt.Errorf("%w: %v", ErrSourceUnavailable, err)
		}
		content = f.Content
		p.surfaces["github"].sources.put(key, content)
	}
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	if req.Start > len(lines) {
		return SourceExcerpt{}, fmt.Errorf("%w: the file has %d lines", ErrSourceInvalid, len(lines))
	}
	end := min(req.End, len(lines))
	first := max(1, req.Start-req.Context)
	last := min(len(lines), end+req.Context)
	return SourceExcerpt{
		Repo: owner + "/" + repo, SHA: strings.ToLower(req.SHA), Path: req.Path,
		First: first, Lines: lines[first-1 : last], Start: req.Start, End: end, Total: len(lines),
		URL: fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s#L%d-L%d", owner, repo, strings.ToLower(req.SHA), req.Path, req.Start, end),
	}, nil
}

// sourceReader is the file reader of the tenant's first enabled GitHub client
// that may read owner/repo, or nil.
func (p *Platform) sourceReader(tenant, owner, repo string) sourceReader {
	s := p.surfaces["github"]
	if s == nil {
		return nil
	}
	for _, st := range s.all() {
		c := st.client
		if c.Tenant != tenant || !c.Enabled || st.files == nil {
			continue
		}
		granted := c.Serves(owner)
		if g := c.GitHub; !granted && g != nil {
			for _, r := range g.ReadRepositories {
				if strings.EqualFold(r, owner+"/"+repo) {
					granted = true
					break
				}
			}
		}
		if granted {
			return st.files
		}
	}
	return nil
}

func fullSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}
