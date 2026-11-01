// Package artifacts is the record of published files: what each one is, where
// it came from, and which version of it this is.
//
// The bytes live in a blob store. This index holds everything else, and is
// what the console's storage page and the download route read.
//
// It reads and writes the database per request (pkg/state, per request): it
// holds nothing in memory, so any number of processes on one database agree,
// and it does not need pkg/state's single-writer lock.
package artifacts

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/urmzd/humagen/internal/textindex"
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
	SearchText string `json:"-"`
	ID         string `json:"id"`
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
	order  int
	record string
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

// Index holds every tenant's artifacts. It reads and writes them per
// request and holds none in memory.
type Index struct {
	st state.Store
}

// The namespace and its keys. The tenant is the shard key; within it:
//
//	next                   the order counter and the legacy cursor boundary
//	artifact/<order>       an artifact, by private publication order, or the
//	                       tombstone a deleted one leaves for cursors
//	id/<id>                the order of an artifact's id
//	file/<hash>            the live versions of one file: a title in a
//	                       conversation
//	usage                  what the tenant holds, by place and by agent
//
// A publish or a delete writes every key it changes in one write, each at
// the version it read, so two processes agree.
const (
	namespace = "artifacts"
	nextKey   = "next"
	usageKey  = "usage"
	// indexed marks, in the tenant "", that records kept before the id,
	// file and usage keys existed have them.
	indexed = "indexed:v1"
)

// Bounds.
const (
	// MaxVersions is the most live versions one file holds.
	MaxVersions = 200
	// maxScan is the most records one Page reads looking for a full page.
	// A page that reaches it is returned short, with a cursor to go on.
	maxScan = 5 * state.MaxList
)

// stored is one record: an artifact, or a tenant's id counter.
type stored struct {
	Artifact  *storedArtifact `json:"artifact,omitempty"`
	Next      int             `json:"next,omitempty"`
	Opaque    bool            `json:"opaque,omitempty"`
	LegacyMax int             `json:"legacy_max,omitempty"`
	Order     int             `json:"order,omitempty"`
	CursorID  string          `json:"cursor_id,omitempty"`
	// Superseded marks an artifact a newer live version of its file hides.
	Superseded bool `json:"superseded,omitempty"`
}

// storedArtifact is an artifact with what its wire form leaves out.
type storedArtifact struct {
	Artifact
	Key        string   `json:"key"`
	Within     []string `json:"within,omitempty"`
	SearchText string   `json:"search_text,omitempty"`
}

// idEntry is where an id is in publication order.
type idEntry struct {
	Order int `json:"order"`
}

// fileRecord is a file's live versions, oldest first.
type fileRecord struct {
	Versions []fileVersion `json:"versions"`
}

type fileVersion struct {
	Order   int `json:"order"`
	Version int `json:"version"`
}

// usageRecord is what a tenant holds: by place, a project and the projects
// it was within when published, and by agent.
type usageRecord struct {
	Bytes  int64             `json:"bytes"`
	Count  int               `json:"count"`
	Places map[string]*place `json:"places,omitempty"`
	Agents map[string]*Share `json:"agents,omitempty"`
}

type place struct {
	Project string   `json:"project,omitempty"`
	Within  []string `json:"within,omitempty"`
	Bytes   int64    `json:"bytes"`
	Count   int      `json:"count"`
}

func placeKey(project string, within []string) string {
	raw, _ := json.Marshal(append([]string{project}, within...))
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:12])
}

// add counts an artifact in, or out with sign -1.
func (u *usageRecord) add(a Artifact, sign int) {
	u.Bytes += int64(sign) * a.Size
	u.Count += sign
	if u.Places == nil {
		u.Places = map[string]*place{}
	}
	k := placeKey(a.Project, a.Within)
	p := u.Places[k]
	if p == nil {
		p = &place{Project: a.Project, Within: slices.Clone(a.Within)}
		u.Places[k] = p
	}
	p.Bytes += int64(sign) * a.Size
	p.Count += sign
	if p.Count <= 0 {
		delete(u.Places, k)
	}
	if a.Agent != "" {
		if u.Agents == nil {
			u.Agents = map[string]*Share{}
		}
		s := u.Agents[a.Agent]
		if s == nil {
			s = &Share{Name: a.Agent}
			u.Agents[a.Agent] = s
		}
		s.Bytes += int64(sign) * a.Size
		s.Count += sign
		if s.Count <= 0 {
			delete(u.Agents, a.Agent)
		}
	}
}

func seqOf(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "art-"))
	return n
}

// recordKey sorts records by private durable publication order.
func recordKey(order int) string { return "artifact/" + state.Seq(int64(order)) }

const recordPrefix = "artifact/"

func idKey(id string) string { return "id/" + id }

func fileKey(thread, title string) string {
	h := sha256.Sum256([]byte(thread + "\x00" + title))
	return "file/" + hex.EncodeToString(h[:])
}

// NewIndex returns an empty index in memory.
func NewIndex() *Index {
	x, err := Open(nil)
	if err != nil {
		panic(err)
	}
	return x
}

// Open returns an index that keeps its records in st. A nil st is a
// state.Store in memory.
//
// New identities are random. Private publication order and cursor tombstones
// are durable; legacy identifiers and blob paths remain unchanged. The first
// time any process opens the index, records kept before ids, files and usage
// had their own keys are given them, once; a process that opens it at the
// same time writes the same, or finds them written.
func Open(st state.Store) (*Index, error) {
	if st == nil {
		st = state.NewMemory()
	}
	x := &Index{st: st}
	if err := x.index(); err != nil {
		return nil, fmt.Errorf("artifacts: load: %w", err)
	}
	return x, nil
}

func ctxTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), state.Timeout)
}

// read decodes one record into v and returns its version, or
// state.Missing when there is none.
func (x *Index) read(ctx context.Context, tenant, key string, v any) (int64, error) {
	r, err := x.st.Lookup(ctx, namespace, tenant, key)
	if errors.Is(err, state.ErrNotFound) {
		return state.Missing, nil
	}
	if err != nil {
		return 0, err
	}
	if err := json.Unmarshal(r.Value, v); err != nil {
		return 0, fmt.Errorf("%q: %w", key, err)
	}
	return r.Version, nil
}

// put returns the op that writes v under key at version, created when it
// was missing.
func put(tenant, key string, v any, version int64) state.Op {
	raw, _ := json.Marshal(v)
	if version == state.Missing {
		return state.Create(tenant, key, raw)
	}
	return state.PutAt(tenant, key, raw, version)
}

// artifactOf is the artifact a record holds, with what its wire form leaves
// out.
func artifactOf(key string, r stored) Artifact {
	a := r.Artifact.Artifact
	a.Key, a.Within, a.SearchText = r.Artifact.Key, r.Artifact.Within, r.Artifact.SearchText
	a.order, a.record = r.Order, key
	if r.Order == 0 {
		a.order = seqOf(a.ID)
	}
	return a
}

// index gives records kept before the id, file and usage keys existed those
// keys, once, tenant by tenant. Each tenant's keys are written in one write
// at the versions read, so a tenant another process indexed meanwhile is
// left as it found it.
func (x *Index) index() error {
	ctx, cancel := ctxTimeout()
	defer cancel()
	if _, err := x.st.Lookup(ctx, namespace, "", indexed); err == nil {
		return nil
	} else if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	recs, err := x.st.Load(ctx, namespace)
	if err != nil {
		return err
	}
	type tenantRecs struct {
		next    *state.Record
		records []state.Record
		other   bool
	}
	byTenant := map[string]*tenantRecs{}
	var names []string
	for _, r := range recs {
		if r.Tenant == "" {
			continue
		}
		t := byTenant[r.Tenant]
		if t == nil {
			t = &tenantRecs{}
			byTenant[r.Tenant] = t
			names = append(names, r.Tenant)
		}
		switch {
		case r.Key == nextKey:
			t.next = &r
		case strings.HasPrefix(r.Key, recordPrefix):
			t.records = append(t.records, r)
		default:
			// An id, file or usage key: the tenant is indexed already.
			t.other = true
		}
	}
	for _, name := range names {
		t := byTenant[name]
		if t.other {
			continue
		}
		var ops []state.Op
		next := stored{}
		nextVersion := state.Missing
		if t.next != nil {
			if err := json.Unmarshal(t.next.Value, &next); err != nil {
				return fmt.Errorf("%s %q: %w", name, nextKey, err)
			}
			nextVersion = t.next.Version
		}
		legacyMax := next.Next
		if next.Opaque {
			legacyMax = next.LegacyMax
		}
		type live struct {
			a       Artifact
			rec     stored
			version int64
		}
		var items []live
		for _, r := range t.records {
			var s stored
			if err := json.Unmarshal(r.Value, &s); err != nil {
				return fmt.Errorf("%s %q: %w", name, r.Key, err)
			}
			if s.CursorID != "" {
				ops = append(ops, put(name, idKey(s.CursorID), idEntry{Order: s.Order}, state.Missing))
				next.Next = max(next.Next, s.Order)
				continue
			}
			if s.Artifact == nil {
				continue
			}
			a := artifactOf(r.Key, s)
			if n, ok := seq(a.ID); ok {
				legacyMax = max(legacyMax, n)
			}
			next.Next = max(next.Next, a.order)
			items = append(items, live{a, s, r.Version})
		}
		slices.SortFunc(items, func(a, b live) int { return cmp.Compare(a.a.order, b.a.order) })
		files := map[string]*fileRecord{}
		var fileKeys []string
		latest := map[string]int{}
		u := usageRecord{}
		for i, it := range items {
			fk := fileKey(it.a.Thread, it.a.Title)
			f := files[fk]
			if f == nil {
				f = &fileRecord{}
				files[fk] = f
				fileKeys = append(fileKeys, fk)
			}
			f.Versions = append(f.Versions, fileVersion{Order: it.a.order, Version: it.a.Version})
			latest[fk] = i
			ops = append(ops, put(name, idKey(it.a.ID), idEntry{Order: it.a.order}, state.Missing))
			u.add(it.a, 1)
		}
		for i, it := range items {
			if latest[fileKey(it.a.Thread, it.a.Title)] != i && !it.rec.Superseded {
				s := it.rec
				s.Superseded = true
				ops = append(ops, put(name, it.a.record, s, it.version))
			}
		}
		for _, fk := range fileKeys {
			ops = append(ops, put(name, fk, files[fk], state.Missing))
		}
		ops = append(ops, put(name, usageKey, u, state.Missing))
		next.Opaque, next.LegacyMax = true, legacyMax
		ops = append(ops, put(name, nextKey, next, nextVersion))
		if err := x.st.Write(ctx, namespace, ops...); err != nil && !errors.Is(err, state.ErrConflict) {
			return err
		}
	}
	return x.st.Write(ctx, namespace, state.Put("", indexed, []byte{}))
}

// NextID draws the id the next artifact will have, so its bytes can be
// stored under it before it is recorded. Publish refuses an id already
// published.
func (x *Index) NextID(string) string {
	// crypto/rand.Text uses the runtime's secure source and fails closed if
	// entropy cannot be obtained. Never fall back to a counter or weak random.
	return "art-" + rand.Text()
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
	if tenantName == "" || strings.ContainsRune(a.ID, 0) {
		return Artifact{}, fmt.Errorf("%w: an artifact needs a tenant and an id", ErrInvalid)
	}
	a.Within = slices.Clone(a.Within)
	ctx, cancel := ctxTimeout()
	defer cancel()
	var out Artifact
	err := state.Retry(func() error {
		var used idEntry
		if v, err := x.read(ctx, tenantName, idKey(a.ID), &used); err != nil {
			return err
		} else if v != state.Missing {
			return fmt.Errorf("%w: %s is already published", ErrInvalid, a.ID)
		}
		var next stored
		nv, err := x.read(ctx, tenantName, nextKey, &next)
		if err != nil {
			return err
		}
		fk := fileKey(a.Thread, a.Title)
		var file fileRecord
		fv, err := x.read(ctx, tenantName, fk, &file)
		if err != nil {
			return err
		}
		if len(file.Versions) >= MaxVersions {
			return fmt.Errorf("%w: a file keeps at most %d versions", ErrInvalid, MaxVersions)
		}
		var u usageRecord
		uv, err := x.read(ctx, tenantName, usageKey, &u)
		if err != nil {
			return err
		}
		b := a
		b.Version = 1
		for _, v := range file.Versions {
			b.Version = max(b.Version, v.Version+1)
		}
		b.order = max(next.Next+1, seqOf(b.ID))
		b.record = recordKey(b.order)
		legacyMax := next.LegacyMax
		if !next.Opaque {
			legacyMax = next.Next
		}
		if n, ok := seq(b.ID); ok {
			legacyMax = max(legacyMax, n)
		}
		var ops []state.Op
		if len(file.Versions) > 0 {
			// The newest live version is hidden by this one.
			prevKey := recordKey(file.Versions[len(file.Versions)-1].Order)
			var prev stored
			pv, err := x.read(ctx, tenantName, prevKey, &prev)
			if err != nil {
				return err
			}
			if pv != state.Missing && prev.Artifact != nil && !prev.Superseded {
				prev.Superseded = true
				ops = append(ops, put(tenantName, prevKey, prev, pv))
			}
		}
		file.Versions = append(file.Versions, fileVersion{Order: b.order, Version: b.Version})
		u.add(b, 1)
		ops = append(ops,
			put(tenantName, nextKey, stored{Next: b.order, Opaque: true, LegacyMax: legacyMax}, nv),
			put(tenantName, b.record, stored{Order: b.order, Artifact: &storedArtifact{Artifact: b, Key: b.Key, Within: b.Within, SearchText: b.SearchText}}, state.Missing),
			put(tenantName, idKey(b.ID), idEntry{Order: b.order}, state.Missing),
			put(tenantName, fk, file, fv),
			put(tenantName, usageKey, u, uv),
		)
		if err := x.st.Write(ctx, namespace, ops...); err != nil {
			return err
		}
		out = b
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			return Artifact{}, err
		}
		return Artifact{}, fmt.Errorf("artifacts: write: %w", err)
	}
	return out, nil
}

// lookup reads the live artifact an id names, and its record's version.
func (x *Index) lookup(ctx context.Context, tenantName, id string) (Artifact, stored, int64, error) {
	var e idEntry
	v, err := x.read(ctx, tenantName, idKey(id), &e)
	if err != nil {
		return Artifact{}, stored{}, 0, fmt.Errorf("artifacts: read: %w", err)
	}
	if v == state.Missing {
		return Artifact{}, stored{}, 0, fmt.Errorf("%w: artifact %q", ErrNotFound, id)
	}
	var r stored
	rv, err := x.read(ctx, tenantName, recordKey(e.Order), &r)
	if err != nil {
		return Artifact{}, stored{}, 0, fmt.Errorf("artifacts: read: %w", err)
	}
	if rv == state.Missing || r.Artifact == nil || r.Artifact.ID != id {
		return Artifact{}, stored{}, 0, fmt.Errorf("%w: artifact %q", ErrNotFound, id)
	}
	return artifactOf(recordKey(e.Order), r), r, rv, nil
}

// Get returns one artifact.
func (x *Index) Get(tenantName, id string) (Artifact, error) {
	ctx, cancel := ctxTimeout()
	defer cancel()
	a, _, _, err := x.lookup(ctx, tenantName, id)
	return a, err
}

// Versions returns every live version of the file an artifact is a version
// of, newest first: at most MaxVersions.
func (x *Index) Versions(tenantName, id string) ([]Artifact, error) {
	ctx, cancel := ctxTimeout()
	defer cancel()
	a, _, _, err := x.lookup(ctx, tenantName, id)
	if err != nil {
		return nil, err
	}
	var file fileRecord
	if _, err := x.read(ctx, tenantName, fileKey(a.Thread, a.Title), &file); err != nil {
		return nil, fmt.Errorf("artifacts: read: %w", err)
	}
	var out []Artifact
	for i := len(file.Versions) - 1; i >= 0; i-- {
		key := recordKey(file.Versions[i].Order)
		var r stored
		v, err := x.read(ctx, tenantName, key, &r)
		if err != nil {
			return nil, fmt.Errorf("artifacts: read: %w", err)
		}
		if v != state.Missing && r.Artifact != nil {
			out = append(out, artifactOf(key, r))
		}
	}
	return out, nil
}

// Delete removes one version and returns it, so its bytes can be removed too.
func (x *Index) Delete(tenantName, id string) (Artifact, error) {
	ctx, cancel := ctxTimeout()
	defer cancel()
	var out Artifact
	err := state.Retry(func() error {
		a, r, rv, err := x.lookup(ctx, tenantName, id)
		if err != nil {
			return err
		}
		fk := fileKey(a.Thread, a.Title)
		var file fileRecord
		fv, err := x.read(ctx, tenantName, fk, &file)
		if err != nil {
			return err
		}
		var u usageRecord
		uv, err := x.read(ctx, tenantName, usageKey, &u)
		if err != nil {
			return err
		}
		var ops []state.Op
		// Keep the private boundary for opaque cursors after deletion. Legacy
		// cursors resolve numerically within the pre-opaque watermark as before.
		if _, legacy := seq(id); legacy {
			ops = append(ops, state.DeleteAt(tenantName, a.record, rv), state.Delete(tenantName, idKey(id)))
		} else {
			ops = append(ops, put(tenantName, a.record, stored{Order: a.order, CursorID: id}, rv))
		}
		file.Versions = slices.DeleteFunc(file.Versions, func(v fileVersion) bool { return v.Order == a.order })
		if fv != state.Missing {
			if len(file.Versions) == 0 {
				ops = append(ops, state.DeleteAt(tenantName, fk, fv))
			} else {
				ops = append(ops, put(tenantName, fk, file, fv))
			}
		}
		if !r.Superseded && len(file.Versions) > 0 {
			// The version before it is the newest again.
			prevKey := recordKey(file.Versions[len(file.Versions)-1].Order)
			var prev stored
			pv, err := x.read(ctx, tenantName, prevKey, &prev)
			if err != nil {
				return err
			}
			if pv != state.Missing && prev.Superseded {
				prev.Superseded = false
				ops = append(ops, put(tenantName, prevKey, prev, pv))
			}
		}
		u.add(a, -1)
		ops = append(ops, put(tenantName, usageKey, u, uv))
		if err := x.st.Write(ctx, namespace, ops...); err != nil {
			return err
		}
		out = a
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalid) {
			return Artifact{}, err
		}
		return Artifact{}, fmt.Errorf("artifacts: write: %w", err)
	}
	return out, nil
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
	Search string
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
// resolve to private durable order, so a page neither repeats nor skips
// when something is published or deleted between pages. A page reads at most
// maxScan records: one that finds fewer than its limit by then is returned
// short, with a cursor to go on from.
func (x *Index) Page(tenantName string, q Query) ([]Artifact, string, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultPage
	}
	limit = min(limit, MaxPage)
	var beforeID string
	if q.Before != "" {
		raw, err := base64.RawURLEncoding.DecodeString(q.Before)
		if err != nil || len(raw) == 0 || len(raw) > 200 {
			return nil, "", ErrCursor
		}
		beforeID = string(raw)
	}
	if tenantName == "" {
		if beforeID != "" {
			return nil, "", ErrCursor
		}
		return []Artifact{}, "", nil
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	query := state.Query{Prefix: recordPrefix, Desc: true, Limit: state.MaxList}
	if beforeID != "" {
		before, err := x.cursor(ctx, tenantName, beforeID)
		if err != nil {
			return nil, "", err
		}
		query.After = recordKey(before)
	}
	out := []Artifact{}
	var last string
	for scanned := 0; scanned < maxScan; {
		query.Limit = min(state.MaxList, maxScan-scanned)
		recs, next, err := x.st.List(ctx, namespace, tenantName, query)
		if err != nil {
			return nil, "", fmt.Errorf("artifacts: read: %w", err)
		}
		for _, rec := range recs {
			scanned++
			var r stored
			if err := json.Unmarshal(rec.Value, &r); err != nil {
				return nil, "", fmt.Errorf("artifacts: read %q: %w", rec.Key, err)
			}
			// A newer version of the same file hides the older ones.
			if r.Artifact == nil || r.Superseded {
				if r.CursorID != "" {
					last = r.CursorID
				} else if r.Artifact != nil {
					last = r.Artifact.ID
				}
				continue
			}
			a := artifactOf(rec.Key, r)
			last = a.ID
			if q.Search != "" && !textindex.Matches(a.Title+"\n"+a.Description+"\n"+a.Preview+"\n"+a.SearchText, q.Search) || !q.match(&a) {
				continue
			}
			if len(out) == limit {
				return out, encodeCursor(&out[len(out)-1]), nil
			}
			out = append(out, a)
		}
		if next == "" {
			return out, "", nil
		}
		query.After = next
	}
	if last == "" {
		return out, "", nil
	}
	// The scan stopped before the end: the next page goes on from the last
	// record read, returned or not.
	return out, base64.RawURLEncoding.EncodeToString([]byte(last)), nil
}

// cursor resolves the id a page goes on before to its order: a legacy id
// numerically, within the boundary before ids were opaque, and any other
// through its id key, which a deleted artifact keeps.
func (x *Index) cursor(ctx context.Context, tenantName, id string) (int, error) {
	if n, legacy := seq(id); legacy {
		var next stored
		if _, err := x.read(ctx, tenantName, nextKey, &next); err != nil {
			return 0, fmt.Errorf("artifacts: read: %w", err)
		}
		boundary := next.LegacyMax
		if !next.Opaque {
			boundary = next.Next
		}
		if n <= 0 || n > boundary {
			return 0, ErrCursor
		}
		return n, nil
	}
	var e idEntry
	v, err := x.read(ctx, tenantName, idKey(id), &e)
	if err != nil {
		return 0, fmt.Errorf("artifacts: read: %w", err)
	}
	if v == state.Missing {
		return 0, ErrCursor
	}
	return e.Order, nil
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

func (x *Index) usage(tenantName string) (usageRecord, error) {
	var u usageRecord
	if tenantName == "" {
		return u, nil
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	if _, err := x.read(ctx, tenantName, usageKey, &u); err != nil {
		return usageRecord{}, fmt.Errorf("artifacts: read: %w", err)
	}
	return u, nil
}

// Usage totals a tenant's storage, from the totals every publish and delete
// keeps.
func (x *Index) Usage(tenantName string) (Usage, error) {
	r, err := x.usage(tenantName)
	if err != nil {
		return Usage{}, err
	}
	u := Usage{Bytes: r.Bytes, Count: r.Count}
	projects := map[string]*Share{}
	for _, p := range r.Places {
		if p.Project == "" {
			continue
		}
		if projects[p.Project] == nil {
			projects[p.Project] = &Share{Name: p.Project}
		}
		projects[p.Project].Bytes += p.Bytes
		projects[p.Project].Count += p.Count
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
	u.ByProject, u.ByAgent = flat(projects), flat(r.Agents)
	return u, nil
}

// UsedUnder returns the bytes filed in a project or below it: in one of
// names, which is the project's subtree as the tree holds it now, or in any
// project that was below it when the file was published, deleted since or
// not.
func (x *Index) UsedUnder(tenantName, project string, names []string) (int64, error) {
	r, err := x.usage(tenantName)
	if err != nil {
		return 0, err
	}
	var n int64
	for _, p := range r.Places {
		if slices.Contains(names, p.Project) || slices.Contains(p.Within, project) {
			n += p.Bytes
		}
	}
	return n, nil
}

// Used returns the bytes a tenant holds, or one of its projects when named.
func (x *Index) Used(tenantName, project string) (int64, error) {
	r, err := x.usage(tenantName)
	if err != nil {
		return 0, err
	}
	if project == "" {
		return r.Bytes, nil
	}
	var n int64
	for _, p := range r.Places {
		if p.Project == project {
			n += p.Bytes
		}
	}
	return n, nil
}
