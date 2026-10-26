// Package artifacts is the record of published files: what each one is, where
// it came from, and which version of it this is.
//
// The bytes live in a blob store. This index holds everything else, and is
// what the console's storage page and the download route read. Built with
// Open, it writes every change through a state.Store and reads the records
// back when it is built again.
package artifacts

import (
	"cmp"
	"encoding/base64"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/urmzd/humagen/pkg/state"
)

// Errors a caller can match.
var (
	ErrNotFound = errors.New("artifacts: not found")
	ErrInvalid  = errors.New("artifacts: invalid")
	ErrCursor   = errors.New("artifacts: invalid cursor")
)

// Kind says what sort of file an artifact is, for how it is shown.
type Kind string

// The kinds the console knows how to show.
const (
	Document     Kind = "document"
	Spreadsheet  Kind = "spreadsheet"
	Presentation Kind = "presentation"
	Image        Kind = "image"
	PDF          Kind = "pdf"
	HTML         Kind = "html"
	Code         Kind = "code"
	Data         Kind = "data"
	Archive      Kind = "archive"
)

// Kinds lists every kind.
func Kinds() []Kind {
	return []Kind{Document, Spreadsheet, Presentation, Image, PDF, HTML, Code, Data, Archive}
}

// MaxTitle bounds a title, which is also the name a download is saved under.
const MaxTitle = 120

// Artifact is one published version of a file.
type Artifact struct {
	ID string `json:"id"`
	// Title is the file's name, as a person will save it.
	Title string `json:"title"`
	Kind  Kind   `json:"kind"`
	// Mime is what the bytes were found to be, not what anyone said they were.
	Mime    string `json:"mime"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	Version int    `json:"version"`
	Run     string `json:"run"`
	Thread  string `json:"thread"`
	Agent   string `json:"agent"`
	Project string `json:"project,omitempty"`
	// Description is the publisher's line about what is in it.
	Description string `json:"description,omitempty"`
	// Preview is the start of a text file.
	Preview   string    `json:"preview,omitempty"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	// Key is where the bytes are in the blob store.
	Key string `json:"-"`
	// Within is the project's ancestors, nearest first and the workspace
	// left out, pinned when it was published: whose storage it counts
	// toward besides its project's, even once its project is deleted.
	Within []string `json:"-"`
}

// CleanTitle turns what a model called a file into a name that is safe to
// save a download under: the last path element, no control characters, bounded.
func CleanTitle(title string) (string, error) {
	title = path.Base(strings.ReplaceAll(strings.TrimSpace(title), `\`, "/"))
	title = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`"<>:|?*`, r) {
			return -1
		}
		return r
	}, title)
	title = strings.TrimLeft(strings.TrimSpace(title), ".")
	if title == "" || title == "/" {
		return "", fmt.Errorf("%w: an artifact needs a file name for a title", ErrInvalid)
	}
	if len(title) > MaxTitle {
		ext := path.Ext(title)
		if len(ext) > 12 {
			ext = ""
		}
		keep := MaxTitle - len(ext)
		for keep > 0 && title[keep]&0xC0 == 0x80 {
			keep--
		}
		title = title[:keep] + ext
	}
	return title, nil
}

type tenant struct {
	items []*Artifact // oldest first
	byID  map[string]*Artifact
	next  int
}

// Index holds every tenant's artifacts.
type Index struct {
	mu      sync.RWMutex
	tenants map[string]*tenant
	// kept is where records are written. Nil keeps them only in memory.
	kept *state.Table[stored]
}

// stored is one record: an artifact, or a tenant's id counter.
type stored struct {
	Artifact *storedArtifact `json:"artifact,omitempty"`
	Next     int             `json:"next,omitempty"`
}

// storedArtifact is an artifact with what its wire form leaves out.
type storedArtifact struct {
	Artifact
	Key    string   `json:"key"`
	Within []string `json:"within,omitempty"`
}

const nextKey = "next"

func seqOf(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "art-"))
	return n
}

// recordKey sorts a tenant's artifacts in the order their ids were issued.
func recordKey(id string) string { return "artifact/" + state.Seq(int64(seqOf(id))) }

// NewIndex returns an empty index in memory.
func NewIndex() *Index { return &Index{tenants: map[string]*tenant{}} }

// Open returns an index that keeps its records in st, holding what st
// already has. A nil st is NewIndex.
//
// Ids are issued once. NextID reserves one in memory, and Publish writes the
// counter with the record, so an id reserved and never published may be
// issued again after a restart: nothing was kept under it.
func Open(st state.Store) (*Index, error) {
	x := NewIndex()
	x.kept = state.NewTable[stored](st, "artifacts")
	err := x.kept.Load(func(name, key string, r stored) error {
		t := x.of(name)
		if key == nextKey {
			t.next = max(t.next, r.Next)
			return nil
		}
		if r.Artifact == nil {
			return nil
		}
		a := r.Artifact.Artifact
		a.Key, a.Within = r.Artifact.Key, r.Artifact.Within
		t.items = append(t.items, &a)
		t.byID[a.ID] = &a
		t.next = max(t.next, seqOf(a.ID))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("artifacts: load: %w", err)
	}
	return x, nil
}

// empty stands in for a tenant with nothing published, for readers. It is
// never written to.
var empty = &tenant{byID: map[string]*Artifact{}}

// peek returns a tenant for reading. It does not add one, so it is safe under
// the read lock.
func (x *Index) peek(name string) *tenant {
	if t, ok := x.tenants[name]; ok {
		return t
	}
	return empty
}

// of returns a tenant for writing, adding it if needed. It needs the write lock.
func (x *Index) of(name string) *tenant {
	t, ok := x.tenants[name]
	if !ok {
		t = &tenant{byID: map[string]*Artifact{}}
		x.tenants[name] = t
	}
	return t
}

// NextID reserves the id the next artifact will have, so its bytes can be
// stored under it before it is recorded.
func (x *Index) NextID(tenantName string) string {
	x.mu.Lock()
	defer x.mu.Unlock()
	t := x.of(tenantName)
	t.next++
	return "art-" + strconv.Itoa(t.next)
}

// Publish records an artifact under an id from NextID. A file with a title
// already published in the same conversation is the next version of it.
func (x *Index) Publish(tenantName string, a Artifact) (Artifact, error) {
	if a.ID == "" || a.Key == "" || a.Thread == "" || a.Title == "" {
		return Artifact{}, fmt.Errorf("%w: an artifact needs an id, a key, a conversation and a title", ErrInvalid)
	}
	if !slices.Contains(Kinds(), a.Kind) {
		return Artifact{}, fmt.Errorf("%w: kind %q is not one of %v", ErrInvalid, a.Kind, Kinds())
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	t := x.of(tenantName)
	if t.byID[a.ID] != nil {
		return Artifact{}, fmt.Errorf("%w: %s is already published", ErrInvalid, a.ID)
	}
	a.Version = 1
	for _, o := range t.items {
		if o.Thread == a.Thread && o.Title == a.Title && o.Version >= a.Version {
			a.Version = o.Version + 1
		}
	}
	b := x.kept.Batch()
	b.Put(tenantName, nextKey, stored{Next: max(t.next, seqOf(a.ID))})
	a.Within = slices.Clone(a.Within)
	b.Put(tenantName, recordKey(a.ID), stored{Artifact: &storedArtifact{Artifact: a, Key: a.Key, Within: a.Within}})
	if err := b.Commit(); err != nil {
		return Artifact{}, fmt.Errorf("artifacts: write: %w", err)
	}
	t.items = append(t.items, &a)
	t.byID[a.ID] = &a
	return a, nil
}

// Get returns one artifact.
func (x *Index) Get(tenantName, id string) (Artifact, error) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	a := x.peek(tenantName).byID[id]
	if a == nil {
		return Artifact{}, fmt.Errorf("%w: artifact %q", ErrNotFound, id)
	}
	return *a, nil
}

// Versions returns every version of the file an artifact is a version of,
// newest first.
func (x *Index) Versions(tenantName, id string) ([]Artifact, error) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	t := x.peek(tenantName)
	a := t.byID[id]
	if a == nil {
		return nil, fmt.Errorf("%w: artifact %q", ErrNotFound, id)
	}
	var out []Artifact
	for i := len(t.items) - 1; i >= 0; i-- {
		if o := t.items[i]; o.Thread == a.Thread && o.Title == a.Title {
			out = append(out, *o)
		}
	}
	return out, nil
}

// Delete removes one version and returns it, so its bytes can be removed too.
func (x *Index) Delete(tenantName, id string) (Artifact, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	t := x.peek(tenantName)
	a := t.byID[id]
	if a == nil {
		return Artifact{}, fmt.Errorf("%w: artifact %q", ErrNotFound, id)
	}
	if err := x.kept.Delete(tenantName, recordKey(id)); err != nil {
		return Artifact{}, fmt.Errorf("artifacts: write: %w", err)
	}
	delete(t.byID, id)
	t.items = slices.DeleteFunc(t.items, func(o *Artifact) bool { return o.ID == id })
	return *a, nil
}

// Paging bounds for Page.
const (
	DefaultPage = 50
	MaxPage     = 200
)

// Query narrows a page of artifacts. Empty fields match everything.
type Query struct {
	Project string
	Agent   string
	Run     string
	Thread  string
	Kind    Kind
	// Title keeps files whose title contains this, whatever the case.
	Title  string
	Limit  int
	Before string
	// Keep, when set, keeps only the files it accepts: those the reader may
	// read. A page is full of those.
	Keep func(Artifact) bool
	// ProjectOf resolves operational placement without rewriting storage attribution.
	ProjectOf func(Artifact) string
}

func (q Query) match(a *Artifact) bool {
	project := a.Project
	if q.ProjectOf != nil {
		project = q.ProjectOf(*a)
	}
	return (q.Keep == nil || q.Keep(*a)) && (q.Project == "" || project == q.Project) && (q.Agent == "" || a.Agent == q.Agent) &&
		(q.Run == "" || a.Run == q.Run) && (q.Thread == "" || a.Thread == q.Thread) && (q.Kind == "" || a.Kind == q.Kind) &&
		(q.Title == "" || strings.Contains(strings.ToLower(a.Title), strings.ToLower(q.Title)))
}

func encodeCursor(a *Artifact) string {
	return base64.RawURLEncoding.EncodeToString([]byte(a.ID))
}

func seq(id string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimPrefix(id, "art-"))
	return n, err == nil && strings.HasPrefix(id, "art-")
}

// Page returns one page of a tenant's artifacts, newest first, and the cursor
// for the page after it. Only the latest version of each file is listed. Ids
// are issued in order and never reused, so a page neither repeats nor skips
// when something is published or deleted between pages.
func (x *Index) Page(tenantName string, q Query) ([]Artifact, string, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultPage
	}
	limit = min(limit, MaxPage)
	before := 0
	if q.Before != "" {
		raw, err := base64.RawURLEncoding.DecodeString(q.Before)
		n, ok := seq(string(raw))
		if err != nil || !ok {
			return nil, "", ErrCursor
		}
		before = n
	}
	x.mu.RLock()
	defer x.mu.RUnlock()
	t := x.peek(tenantName)
	// A newer version of the same file hides the older ones.
	latest := map[string]bool{}
	out := []Artifact{}
	var last *Artifact
	for i := len(t.items) - 1; i >= 0; i-- {
		a := t.items[i]
		file := a.Thread + "\x00" + a.Title
		if latest[file] {
			continue
		}
		latest[file] = true
		if n, _ := seq(a.ID); before > 0 && n >= before {
			continue
		}
		if !q.match(a) {
			continue
		}
		if len(out) == limit {
			return out, encodeCursor(last), nil
		}
		out = append(out, *a)
		last = a
	}
	return out, "", nil
}

// Share is how much of a tenant's storage one project or agent holds.
type Share struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
	Count int    `json:"count"`
}

// Usage is what a tenant has stored, every version counted.
type Usage struct {
	Bytes     int64   `json:"used_bytes"`
	Count     int     `json:"count"`
	ByProject []Share `json:"by_project"`
	ByAgent   []Share `json:"by_agent"`
}

// Usage totals a tenant's storage.
func (x *Index) Usage(tenantName string) Usage {
	x.mu.RLock()
	defer x.mu.RUnlock()
	u := Usage{}
	projects, agents := map[string]*Share{}, map[string]*Share{}
	add := func(m map[string]*Share, name string, size int64) {
		if name == "" {
			return
		}
		if m[name] == nil {
			m[name] = &Share{Name: name}
		}
		m[name].Bytes += size
		m[name].Count++
	}
	for _, a := range x.peek(tenantName).items {
		u.Bytes += a.Size
		u.Count++
		add(projects, a.Project, a.Size)
		add(agents, a.Agent, a.Size)
	}
	flat := func(m map[string]*Share) []Share {
		out := make([]Share, 0, len(m))
		for _, s := range m {
			out = append(out, *s)
		}
		slices.SortFunc(out, func(a, b Share) int {
			if c := cmp.Compare(b.Bytes, a.Bytes); c != 0 {
				return c
			}
			return strings.Compare(a.Name, b.Name)
		})
		return out
	}
	u.ByProject, u.ByAgent = flat(projects), flat(agents)
	return u
}

// UsedUnder returns the bytes filed in a project or below it: in one of
// names, which is the project's subtree as the tree holds it now, or in any
// project that was below it when the file was published, deleted since or
// not.
func (x *Index) UsedUnder(tenantName, project string, names []string) int64 {
	x.mu.RLock()
	defer x.mu.RUnlock()
	var n int64
	for _, a := range x.peek(tenantName).items {
		if slices.Contains(names, a.Project) || slices.Contains(a.Within, project) {
			n += a.Size
		}
	}
	return n
}

// Used returns the bytes a tenant holds, or one of its projects when named.
func (x *Index) Used(tenantName, project string) int64 {
	x.mu.RLock()
	defer x.mu.RUnlock()
	var n int64
	for _, a := range x.peek(tenantName).items {
		if project == "" || a.Project == project {
			n += a.Size
		}
	}
	return n
}
