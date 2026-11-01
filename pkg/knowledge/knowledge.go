// Package knowledge holds the documents agents may be grounded in.
//
// A source is a named collection in one tenant. A document added to it is cut
// into passages, and Search ranks passages against a query with BM25.
//
// Search is lexical unless an embedder is given with UseEmbedder; then each
// passage also gets a vector, computed in the background, and the two
// rankings are fused (vectors.go).
//
// It reads and writes the database per request (pkg/state, per request): a
// source's record is the version every change to it and its documents is
// made at, so any number of processes on one database agree, and it does not
// need pkg/state's single-writer lock. Search reads the selected sources'
// passages, each source capped at MaxImportDocuments documents and
// MaxImportBytes; a process may keep what it read, by the version of the
// source it read it at.
package knowledge

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/urmzd/humagen/pkg/state"
)

// Errors.
var (
	ErrInvalid      = errors.New("knowledge: invalid")
	ErrExists       = errors.New("knowledge: source already exists")
	ErrNotFound     = errors.New("knowledge: not found")
	ErrUpstreamRead = fmt.Errorf("%w: current upstream file access is required before reading retained external data", ErrInvalid)
)

// passageSize is the target length of a passage in bytes.
const passageSize = 800

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ValidName reports whether a name can name a source: lower case letters,
// digits and dashes, starting with a letter or digit, at most 63.
func ValidName(name string) bool { return validName.MatchString(name) }

// Source is a named collection of documents.
type Source struct {
	ContentVersion uint64     `json:"content_version,omitempty"`
	Lifecycle      *Lifecycle `json:"lifecycle,omitempty"`
	Name           string     `json:"name"`
	Description    string     `json:"description,omitempty"`
	Documents      int        `json:"documents"`
	Passages       int        `json:"passages"`
	// Excluded is how many documents are held but left out of search.
	Excluded int `json:"excluded"`
	// Variants are saved views of the source: the same documents with a
	// different set left out.
	Variants  []Variant `json:"variants"`
	CreatedBy string    `json:"created_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RequiresUpstreamRead distinguishes provider copies from authored local data.
// Collection authority and project membership cannot establish a reader's file access.
func (s Source) RequiresUpstreamRead() bool {
	return s.Lifecycle != nil && s.Lifecycle.Provider != "manual"
}

// MaxVariants bounds how many views a source keeps.
const MaxVariants = 20

// Variant is a named view of a source. It records which documents it leaves
// out, so two ways of curating the same documents can be searched side by side.
type Variant struct {
	Name string `json:"name"`
	// Excluded is the ids of the documents this view leaves out.
	Excluded []string `json:"excluded"`
}

// Document is one thing added to a source. Its text is held as passages.
type Document struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	URI      string `json:"uri,omitempty"`
	Bytes    int    `json:"bytes"`
	Passages int    `json:"passages"`
	// Excluded keeps the document but leaves it out of search.
	Excluded bool      `json:"excluded"`
	AddedBy  string    `json:"added_by,omitempty"`
	AddedAt  time.Time `json:"added_at"`
}

// Hit is one passage that matched a query.
type Hit struct {
	CopyAt        *time.Time `json:"copy_at,omitempty"`
	RefreshFailed bool       `json:"refresh_failed,omitempty"`
	Mode          Mode       `json:"mode,omitempty"`
	Source        string     `json:"source"`
	DocumentID    string     `json:"document_id"`
	Title         string     `json:"title"`
	URI           string     `json:"uri,omitempty"`
	Index         int        `json:"index"`
	Text          string     `json:"text"`
	Score         float64    `json:"score"`
}

type passage struct {
	doc   *Document
	index int
	text  string
	terms map[string]int
	n     int
	// vec is the passage's unit vector from model, or nil. A vector from
	// another model than the store's embedder is ignored and computed again.
	vec   []float32
	model string
}

// source is one source as read at one version of its record, with its
// documents and passages. A source read for search is shared and never
// changed.
type source struct {
	Source
	docs     []*Document
	passages []*passage
	// seqs is each document's number, by id.
	seqs map[string]int64
}

// Store holds every tenant's sources, read and written per request on a
// state.Store. Every method is confined to one tenant.
type Store struct {
	kept *state.Table[stored]
	dir  *state.Tenants
	read *readCache
	mu   sync.RWMutex // guards dense
	// dense is the embedder and its background indexer; nil is lexical only.
	dense *denseIndex
}

// stored is one record: a source, a document, a document's passages or
// vectors, a name's content clock, or the stamp every new source moves. A
// document's passages are written once, when it is added, and its record
// again whenever it changes.
type stored struct {
	Clock    *contentClock `json:"clock,omitempty"`
	Source   *Source       `json:"source,omitempty"`
	Vectors  *vectorRecord `json:"vectors,omitempty"`
	Document *Document     `json:"document,omitempty"`
	Passages []string      `json:"passages,omitempty"`
	Stamp    int64         `json:"stamp,omitempty"`
}

// namespace is where sources are kept. The tenant is the shard key; within
// it:
//
//	source/<name>          a source
//	doc/<name>/<seq>       a document, numbered in the order it was added
//	text/<name>/<seq>      its passages
//	vec/<name>/<seq>       their vectors
//	version/<name>         the name's content clock, kept after a delete
//	head                   a stamp every new source moves, so two processes
//	                       creating sources at once cannot both count under
//	                       the bounds
//
// A change to a source's documents, passages or vectors is written at the
// version of its source record, and moves it.
const (
	namespace = "knowledge"
	headKey   = "head"
	// MaxSources bounds one tenant's sources.
	MaxSources = 200
	// AttemptLease is how long a refresh attempt may run. One older than
	// this, never finished, was cut short by the process running it ending.
	AttemptLease = 15 * time.Minute
)

func sourceKey(name string) string { return "source/" + name }
func docPrefix(name string) string { return "doc/" + name + "/" }
func docKey(name string, seq int64) string {
	return docPrefix(name) + state.Seq(seq)
}
func textPrefix(name string) string { return "text/" + name + "/" }
func textKey(name string, seq int64) string {
	return textPrefix(name) + state.Seq(seq)
}

// seqOf reads a document's number out of its key.
func seqOf(key string) (int64, bool) {
	i := strings.LastIndexByte(key, '/')
	if i < 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(key[i+1:], 10, 64)
	return n, err == nil
}

// NewStore returns a store in this process's memory.
func NewStore() *Store {
	s, err := Open(nil)
	if err != nil {
		panic(err) // a store in memory cannot fail to open
	}
	return s
}

// Open returns a store on st, which may be shared with other processes. A nil
// st keeps sources in this process's memory.
//
// A refresh attempt that has run longer than AttemptLease without finishing
// was cut short when the process running it ended: Open marks it failed, so
// its last good copy is kept and an explicit retry is allowed, without
// inventing successful work. An attempt younger than that may belong to
// another process, and is left alone.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	dir, err := state.OpenTenants(st, namespace)
	if err != nil {
		return nil, fmt.Errorf("knowledge: tenants: %w", err)
	}
	s := &Store{kept: state.NewTable[stored](st, namespace), dir: dir, read: newReadCache()}
	at := time.Now().UTC()
	err = dir.Each(func(tenant string) error {
		srcs, _, err := s.sources(tenant)
		if err != nil {
			return err
		}
		for _, src := range srcs {
			if src.Lifecycle == nil {
				continue
			}
			if id := stale(src.Lifecycle, at); id != "" {
				if _, err := s.FailSync(tenant, src.Name, id, Interrupted, at); err != nil && !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrNotFound) {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge: recover attempts: %w", err)
	}
	return s, nil
}

// passagesOf indexes a document's passages for search.
func passagesOf(doc *Document, texts []string) []*passage {
	out := make([]*passage, 0, len(texts))
	for i, p := range texts {
		terms := map[string]int{}
		n := 0
		for _, t := range tokens(p) {
			terms[t]++
			n++
		}
		out = append(out, &passage{doc: doc, index: i, text: p, terms: terms, n: n})
	}
	return out
}

// meta reads one source's record and its version.
func (s *Store) meta(tenant, name string) (Source, int64, error) {
	it, err := s.kept.Lookup(tenant, sourceKey(name))
	if errors.Is(err, state.ErrNotFound) || err == nil && it.Value.Source == nil {
		return Source{}, 0, fmt.Errorf("%w: source %q", ErrNotFound, name)
	}
	if err != nil {
		return Source{}, 0, err
	}
	src := *it.Value.Source
	if src.Variants == nil {
		src.Variants = []Variant{}
	}
	return src, it.Version, nil
}

// sources reads a tenant's sources by name, and the version of its head
// stamp, read first: a source created after it moves it.
func (s *Store) sources(tenant string) ([]Source, int64, error) {
	head, err := s.kept.Lookup(tenant, headKey)
	if err != nil && !errors.Is(err, state.ErrNotFound) {
		return nil, 0, err
	}
	items, err := s.kept.All(tenant, "source/", MaxSources)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Source, 0, len(items))
	for _, it := range items {
		if it.Value.Source == nil {
			continue
		}
		src := *it.Value.Source
		if src.Variants == nil {
			src.Variants = []Variant{}
		}
		out = append(out, src)
	}
	return out, head.Version, nil
}

// clock reads a name's content clock and its version, 0 when it has none.
func (s *Store) clock(tenant, name string) (contentClock, int64, error) {
	it, err := s.kept.Lookup(tenant, clockKey(name))
	if errors.Is(err, state.ErrNotFound) {
		return contentClock{Name: name}, 0, nil
	}
	if err != nil {
		return contentClock{}, 0, err
	}
	if it.Value.Clock == nil {
		return contentClock{Name: name}, it.Version, nil
	}
	return *it.Value.Clock, it.Version, nil
}

// docRow is one document as stored, with its number and version.
type docRow struct {
	seq     int64
	doc     Document
	version int64
}

// docs reads a source's documents in the order they were added.
func (s *Store) docs(tenant, name string) ([]docRow, error) {
	items, err := s.kept.All(tenant, docPrefix(name), MaxImportDocuments)
	if err != nil {
		return nil, err
	}
	out := make([]docRow, 0, len(items))
	for _, it := range items {
		seq, ok := seqOf(it.Key)
		if !ok || it.Value.Document == nil {
			continue
		}
		out = append(out, docRow{seq, *it.Value.Document, it.Version})
	}
	return out, nil
}

// nextSeq is the number the next document of a name is given: past every
// number its clock or its documents have used.
func nextSeq(clk contentClock, docs []docRow) int64 {
	n := clk.Seq
	for _, d := range docs {
		n = max(n, d.seq)
	}
	return n + 1
}

// putClock writes a name's clock at the version it was read at.
func putClock(b *state.Batch[stored], tenant string, clk contentClock, version int64) {
	if version == 0 {
		b.Create(tenant, clockKey(clk.Name), stored{Clock: &clk})
		return
	}
	b.PutAt(tenant, clockKey(clk.Name), stored{Clock: &clk}, version)
}

// load reads a source with its documents, passages and vectors as of one
// version of its record, from this process's cache when it has it there. A
// read that the source moved under returns state.ErrConflict.
func (s *Store) load(tenant, name string) (*source, int64, error) {
	meta, version, err := s.meta(tenant, name)
	if err != nil {
		return nil, 0, err
	}
	if src, ok := s.read.get(tenant, name, version, meta.ContentVersion); ok {
		return src, version, nil
	}
	docs, err := s.docs(tenant, name)
	if err != nil {
		return nil, 0, err
	}
	texts, err := s.kept.All(tenant, textPrefix(name), MaxImportDocuments)
	if err != nil {
		return nil, 0, err
	}
	vecs, err := s.kept.All(tenant, vecPrefix(name), MaxImportDocuments)
	if err != nil {
		return nil, 0, err
	}
	if _, again, err := s.meta(tenant, name); err != nil {
		return nil, 0, err
	} else if again != version {
		return nil, 0, state.ErrConflict
	}
	bySeq := map[int64][]string{}
	for _, it := range texts {
		if seq, ok := seqOf(it.Key); ok {
			bySeq[seq] = it.Value.Passages
		}
	}
	vecBySeq := map[int64]*vectorRecord{}
	for _, it := range vecs {
		if seq, ok := seqOf(it.Key); ok {
			vecBySeq[seq] = it.Value.Vectors
		}
	}
	src := &source{Source: meta, seqs: map[string]int64{}}
	size := 0
	for _, d := range docs {
		doc := d.doc
		src.docs = append(src.docs, &doc)
		src.seqs[doc.ID] = d.seq
		ps := passagesOf(&doc, bySeq[d.seq])
		vecBySeq[d.seq].attach(ps)
		src.passages = append(src.passages, ps...)
		size += doc.Bytes
	}
	s.read.put(tenant, name, version, meta.ContentVersion, src, size)
	return src, version, nil
}

// current loads a source, reading again while writers move it.
func (s *Store) current(tenant, name string) (*source, error) {
	for range state.MaxTries {
		src, _, err := s.load(tenant, name)
		if errors.Is(err, state.ErrConflict) {
			continue
		}
		return src, err
	}
	return nil, state.ErrBusy
}

// Create adds an empty source.
func (s *Store) Create(tenant, name, description, by string, at time.Time) (Source, error) {
	return s.CreateWithConfig(tenant, name, description, by, at, nil)
}

// CreateWithConfig publishes a scoped source in one durable write. No unscoped
// intermediate name appears in inventory when configuration validation fails.
func (s *Store) CreateWithConfig(tenant, name, description, by string, at time.Time, cfg *SourceConfig) (Source, error) {
	var configured *Lifecycle
	if cfg != nil {
		checked, err := normalizeSourceConfig(*cfg)
		if err != nil {
			return Source{}, err
		}
		if checked.Snapshot {
			return Source{}, fmt.Errorf("%w: a new source has no last good copy to freeze", ErrInvalid)
		}
		configured = &Lifecycle{ConnectedAt: at, SourceConfig: checked, Status: Connected, Attempts: []Attempt{}}
	}
	if len(description) > 4096 {
		return Source{}, fmt.Errorf("%w: source description is at most 4096 bytes", ErrInvalid)
	}
	if !validName.MatchString(name) {
		return Source{}, fmt.Errorf("%w: a source name is lowercase letters, digits and dashes", ErrInvalid)
	}
	if err := s.dir.Mark(tenant); err != nil {
		return Source{}, err
	}
	var out Source
	err := state.Retry(func() error {
		all, head, err := s.sources(tenant)
		if err != nil {
			return err
		}
		if len(all) >= MaxSources {
			return fmt.Errorf("%w: at most %d sources", ErrInvalid, MaxSources)
		}
		if slices.ContainsFunc(all, func(x Source) bool { return x.Name == name }) {
			return fmt.Errorf("%w: %q", ErrExists, name)
		}
		clk, clkAt, err := s.clock(tenant, name)
		if err != nil {
			return err
		}
		b := s.kept.Batch()
		if clkAt == 0 {
			// Every name a tenant has used keeps a clock, and the clocks are
			// what MaxSourceNames bounds.
			clocks, err := s.kept.All(tenant, "version/", MaxSourceNames)
			if errors.Is(err, state.ErrTooMany) || err == nil && len(clocks) >= MaxSourceNames {
				return fmt.Errorf("%w: source name history capacity reached", ErrInvalid)
			}
			if err != nil {
				return err
			}
			putClock(b, tenant, clk, 0)
		}
		src := Source{Name: name, Description: description, Variants: []Variant{}, CreatedBy: by, UpdatedAt: at}
		src.Lifecycle = configured
		src.ContentVersion = clk.Value
		b.Create(tenant, sourceKey(name), stored{Source: &src})
		bumpHead(b, tenant, head)
		if err := b.Commit(); err != nil {
			return err
		}
		out = src
		return nil
	})
	if err != nil {
		return Source{}, writeErr(err)
	}
	return cloneSource(out), nil
}

// bumpHead moves the head stamp read at version, 0 when there is none.
func bumpHead(b *state.Batch[stored], tenant string, version int64) {
	if version == 0 {
		b.Create(tenant, headKey, stored{Stamp: 1})
		return
	}
	b.PutAt(tenant, headKey, stored{Stamp: version + 1}, version)
}

// Delete removes a source and everything in it.
func (s *Store) Delete(tenant, name string) error { return s.deleteWithScope(tenant, name, nil, false) }

// DeleteForScope rejects a source whose authorized audience changed before the mutation.
func (s *Store) DeleteForScope(tenant, name string, expected *SourceConfig) error {
	return s.deleteWithScope(tenant, name, expected, true)
}
func (s *Store) deleteWithScope(tenant, name string, expected *SourceConfig, guard bool) error {
	return writeErr(state.Retry(func() error {
		src, at, err := s.meta(tenant, name)
		if err != nil {
			return err
		}
		if guard && !sameConfiguration(src, expected) {
			return ErrNotFound
		}
		clk, clkAt, err := s.clock(tenant, name)
		if err != nil {
			return err
		}
		version, err := nextVersion(src, clk)
		if err != nil {
			return err
		}
		docs, err := s.docs(tenant, name)
		if err != nil {
			return err
		}
		b := s.kept.Batch()
		for _, d := range docs {
			b.Delete(tenant, docKey(name, d.seq))
			b.Delete(tenant, textKey(name, d.seq))
			b.Delete(tenant, vecKey(name, d.seq))
		}
		b.DeleteAt(tenant, sourceKey(name), at)
		putClock(b, tenant, contentClock{Name: name, Value: version, Seq: nextSeq(clk, docs) - 1}, clkAt)
		return b.Commit()
	}))
}

// writeErr marks an error from the state store as a failed write, leaving
// the store's own errors as they are.
func writeErr(err error) error {
	if err == nil || errors.Is(err, ErrInvalid) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrExists) {
		return err
	}
	return fmt.Errorf("knowledge: write: %w", err)
}

// List returns a tenant's sources by name.
func (s *Store) List(tenant string) ([]Source, error) {
	srcs, _, err := s.sources(tenant)
	if err != nil {
		return nil, err
	}
	for i := range srcs {
		srcs[i] = cloneSource(srcs[i])
	}
	return srcs, nil
}

// Documents returns a source's documents, newest first.
func (s *Store) Documents(tenant, name string) ([]Document, error) {
	return s.documents(tenant, name, nil, false)
}

// DocumentsForScope binds a public read to the exact authorized source scope.
func (s *Store) DocumentsForScope(tenant, name string, expected *SourceConfig) ([]Document, error) {
	return s.documents(tenant, name, expected, true)
}
func (s *Store) documents(tenant, name string, expected *SourceConfig, guard bool) ([]Document, error) {
	for range state.MaxTries {
		src, at, err := s.meta(tenant, name)
		if err != nil {
			return nil, err
		}
		if guard && !sameConfiguration(src, expected) {
			return nil, ErrNotFound
		}
		if guard && src.RequiresUpstreamRead() {
			return nil, ErrUpstreamRead
		}
		docs, err := s.docs(tenant, name)
		if err != nil {
			return nil, err
		}
		// The documents are the source's at the version its scope was
		// checked at, or they are read again.
		if _, again, err := s.meta(tenant, name); err != nil {
			return nil, err
		} else if again != at {
			continue
		}
		out := make([]Document, 0, len(docs))
		for _, d := range slices.Backward(docs) {
			out = append(out, d.doc)
		}
		return out, nil
	}
	return nil, state.ErrBusy
}

// Add cuts a document into passages and stores it.
func (s *Store) Add(tenant, name, title, uri, text, by string, at time.Time) (Document, error) {
	return s.add(tenant, name, title, uri, text, by, at, nil, false)
}

// AddForScope inserts only while the source retains the exact expected public
// configuration. A nil expectation requires a legacy manual workspace source.
// The guard and insertion are one write at the version of the source read,
// so a concurrent Configure cannot broaden the audience or replace the source
// authority between them.
func (s *Store) AddForScope(tenant, name, title, uri, text, by string, at time.Time, expected *SourceConfig) (Document, error) {
	return s.add(tenant, name, title, uri, text, by, at, expected, true)
}

func (s *Store) add(tenant, name, title, uri, text, by string, at time.Time, expected *SourceConfig, guard bool) (Document, error) {
	title, text = strings.TrimSpace(title), strings.TrimSpace(text)
	switch {
	case title == "":
		return Document{}, fmt.Errorf("%w: a document needs a title", ErrInvalid)
	case text == "":
		return Document{}, fmt.Errorf("%w: a document needs text", ErrInvalid)
	}
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return Document{}, err
	}
	doc := &Document{ID: hex.EncodeToString(id), Title: title, URI: uri, Bytes: len(text), AddedBy: by, AddedAt: at}
	texts := split(text)
	doc.Passages = len(texts)

	err := state.Retry(func() error {
		src, srcAt, err := s.meta(tenant, name)
		if err != nil {
			return err
		}
		docs, err := s.docs(tenant, name)
		if err != nil {
			return err
		}
		totalBytes := doc.Bytes
		passages := doc.Passages
		for _, prior := range docs {
			totalBytes += prior.doc.Bytes
			passages += prior.doc.Passages
		}
		if len(docs) >= MaxImportDocuments || totalBytes > MaxImportBytes {
			return fmt.Errorf("%w: source document or byte limit reached", ErrInvalid)
		}
		if guard {
			if expected == nil {
				if src.Lifecycle != nil {
					return fmt.Errorf("%w: the source configuration changed; review its audience again", ErrExists)
				}
			} else if src.Lifecycle == nil || !src.Lifecycle.Equal(*expected) || src.Lifecycle.Mode != Indexed || src.Lifecycle.Provider != "manual" {
				return fmt.Errorf("%w: the source configuration changed; review its audience again", ErrExists)
			}
		}
		clk, clkAt, err := s.clock(tenant, name)
		if err != nil {
			return err
		}
		seq := nextSeq(clk, docs)
		meta := cloneSource(src)
		if meta.Lifecycle != nil && meta.Lifecycle.Provider == "manual" && meta.Lifecycle.Mode == Indexed {
			stamp := at
			meta.Lifecycle.Status = Available
			meta.Lifecycle.LastGoodAt = &stamp
			meta.Lifecycle.Cause = ""
		}
		version, err := nextVersion(src, clk)
		if err != nil {
			return err
		}
		meta.ContentVersion = version
		meta.Documents, meta.Passages, meta.UpdatedAt = len(docs)+1, passages, at
		b := s.kept.Batch()
		b.Put(tenant, textKey(name, seq), stored{Passages: texts})
		b.Create(tenant, docKey(name, seq), stored{Document: doc})
		b.PutAt(tenant, sourceKey(name), stored{Source: &meta}, srcAt)
		putClock(b, tenant, contentClock{Name: name, Value: version, Seq: seq}, clkAt)
		return b.Commit()
	})
	if err != nil {
		return Document{}, writeErr(err)
	}
	s.nudge()
	return *doc, nil
}

// Remove deletes one document from a source.
func (s *Store) Remove(tenant, name, id string, at time.Time) error {
	return s.removeWithScope(tenant, name, id, at, nil, false)
}

// RemoveForScope rejects a source whose authorized audience changed before the mutation.
func (s *Store) RemoveForScope(tenant, name, id string, at time.Time, expected *SourceConfig) error {
	return s.removeWithScope(tenant, name, id, at, expected, true)
}
func (s *Store) removeWithScope(tenant, name, id string, at time.Time, expected *SourceConfig, guard bool) error {
	return writeErr(state.Retry(func() error {
		src, srcAt, err := s.meta(tenant, name)
		if err != nil {
			return err
		}
		if guard && !sameConfiguration(src, expected) {
			return ErrNotFound
		}
		docs, err := s.docs(tenant, name)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(docs, func(d docRow) bool { return d.doc.ID == id })
		if i < 0 {
			return fmt.Errorf("%w: document %q", ErrNotFound, id)
		}
		gone := docs[i]
		docs = slices.Delete(docs, i, i+1)
		next := cloneSource(src)
		for i := range next.Variants {
			next.Variants[i].Excluded = slices.DeleteFunc(next.Variants[i].Excluded, func(x string) bool { return x == id })
		}
		recount(&next, docs)
		next.UpdatedAt = at
		clk, clkAt, err := s.clock(tenant, name)
		if err != nil {
			return err
		}
		version, err := nextVersion(src, clk)
		if err != nil {
			return err
		}
		next.ContentVersion = version
		b := s.kept.Batch()
		b.DeleteAt(tenant, docKey(name, gone.seq), gone.version)
		b.Delete(tenant, textKey(name, gone.seq))
		b.Delete(tenant, vecKey(name, gone.seq))
		b.PutAt(tenant, sourceKey(name), stored{Source: &next}, srcAt)
		putClock(b, tenant, contentClock{Name: name, Value: version, Seq: max(clk.Seq, nextSeq(clk, docs)-1, gone.seq)}, clkAt)
		return b.Commit()
	}))
}

// recount sets a source's counts from its documents.
func recount(src *Source, docs []docRow) {
	src.Documents, src.Passages, src.Excluded = len(docs), 0, 0
	for _, d := range docs {
		src.Passages += d.doc.Passages
		if d.doc.Excluded {
			src.Excluded++
		}
	}
}

// SetExcluded leaves a document out of search, or puts it back. The document
// and its text are kept either way.
func (s *Store) SetExcluded(tenant, name, id string, excluded bool, at time.Time) (Document, error) {
	return s.setExcludedWithScope(tenant, name, id, excluded, at, nil, false)
}

// SetExcludedForScope rejects a source whose authorized audience changed before the mutation.
func (s *Store) SetExcludedForScope(tenant, name, id string, excluded bool, at time.Time, expected *SourceConfig) (Document, error) {
	return s.setExcludedWithScope(tenant, name, id, excluded, at, expected, true)
}
func (s *Store) setExcludedWithScope(tenant, name, id string, excluded bool, at time.Time, expected *SourceConfig, guard bool) (Document, error) {
	var out Document
	err := state.Retry(func() error {
		src, srcAt, err := s.meta(tenant, name)
		if err != nil {
			return err
		}
		if guard && !sameConfiguration(src, expected) {
			return ErrNotFound
		}
		if guard && src.RequiresUpstreamRead() {
			return ErrUpstreamRead
		}
		docs, err := s.docs(tenant, name)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(docs, func(d docRow) bool { return d.doc.ID == id })
		if i < 0 {
			return fmt.Errorf("%w: document %q", ErrNotFound, id)
		}
		row := docs[i]
		changed := row.doc.Excluded != excluded
		docs[i].doc.Excluded = excluded
		meta := cloneSource(src)
		meta.UpdatedAt = at
		recount(&meta, docs)
		clk, clkAt, err := s.clock(tenant, name)
		if err != nil {
			return err
		}
		if changed {
			version, err := nextVersion(src, clk)
			if err != nil {
				return err
			}
			meta.ContentVersion = version
		}
		clk.Value = max(clk.Value, meta.ContentVersion)
		doc := docs[i].doc
		b := s.kept.Batch()
		b.PutAt(tenant, docKey(name, row.seq), stored{Document: &doc}, row.version)
		b.PutAt(tenant, sourceKey(name), stored{Source: &meta}, srcAt)
		putClock(b, tenant, clk, clkAt)
		if err := b.Commit(); err != nil {
			return err
		}
		out = doc
		return nil
	})
	if err != nil {
		return Document{}, writeErr(err)
	}
	return out, nil
}

// SaveVariant records the documents currently left out as a named view. Saving
// a name that exists replaces it.
func (s *Store) SaveVariant(tenant, name, variant string, at time.Time) (Variant, error) {
	return s.saveVariantWithScope(tenant, name, variant, at, nil, false)
}

// SaveVariantForScope rejects a source whose authorized audience changed before the mutation.
func (s *Store) SaveVariantForScope(tenant, name, variant string, at time.Time, expected *SourceConfig) (Variant, error) {
	return s.saveVariantWithScope(tenant, name, variant, at, expected, true)
}
func (s *Store) saveVariantWithScope(tenant, name, variant string, at time.Time, expected *SourceConfig, guard bool) (Variant, error) {
	if !validName.MatchString(variant) {
		return Variant{}, fmt.Errorf("%w: a variant name is lowercase letters, digits and dashes", ErrInvalid)
	}
	var out Variant
	err := state.Retry(func() error {
		src, srcAt, err := s.meta(tenant, name)
		if err != nil {
			return err
		}
		if guard && !sameConfiguration(src, expected) {
			return ErrNotFound
		}
		if guard && src.RequiresUpstreamRead() {
			return ErrUpstreamRead
		}
		docs, err := s.docs(tenant, name)
		if err != nil {
			return err
		}
		v := Variant{Name: variant, Excluded: []string{}}
		for _, d := range docs {
			if d.doc.Excluded {
				v.Excluded = append(v.Excluded, d.doc.ID)
			}
		}
		i := slices.IndexFunc(src.Variants, func(x Variant) bool { return x.Name == variant })
		meta := cloneSource(src)
		switch {
		case i >= 0:
			meta.Variants[i] = v
		case len(src.Variants) >= MaxVariants:
			return fmt.Errorf("%w: a source keeps at most %d variants", ErrInvalid, MaxVariants)
		default:
			meta.Variants = append(meta.Variants, v)
		}
		meta.UpdatedAt = at
		b := s.kept.Batch()
		b.PutAt(tenant, sourceKey(name), stored{Source: &meta}, srcAt)
		if err := b.Commit(); err != nil {
			return err
		}
		out = v
		return nil
	})
	if err != nil {
		return Variant{}, writeErr(err)
	}
	return out, nil
}

// DeleteVariant removes a saved view. The documents are untouched.
func (s *Store) DeleteVariant(tenant, name, variant string) error {
	return s.deleteVariantWithScope(tenant, name, variant, nil, false)
}

// DeleteVariantForScope rejects a source whose authorized audience changed before the mutation.
func (s *Store) DeleteVariantForScope(tenant, name, variant string, expected *SourceConfig) error {
	return s.deleteVariantWithScope(tenant, name, variant, expected, true)
}
func (s *Store) deleteVariantWithScope(tenant, name, variant string, expected *SourceConfig, guard bool) error {
	_, err := s.update(tenant, name, func(src *Source) error {
		if guard && !sameConfiguration(*src, expected) {
			return ErrNotFound
		}
		i := slices.IndexFunc(src.Variants, func(x Variant) bool { return x.Name == variant })
		if i < 0 {
			return fmt.Errorf("%w: variant %q", ErrNotFound, variant)
		}
		src.Variants = slices.Delete(src.Variants, i, i+1)
		return nil
	})
	return writeErr(err)
}

// update changes one source's record at the version read, reading again
// when another writer moved it.
func (s *Store) update(tenant, name string, change func(*Source) error) (Source, error) {
	var out Source
	_, err := s.kept.Update(tenant, sourceKey(name), func(r stored, ok bool) (stored, error) {
		if !ok || r.Source == nil {
			return r, fmt.Errorf("%w: source %q", ErrNotFound, name)
		}
		next := cloneSource(*r.Source)
		if next.Variants == nil {
			next.Variants = []Variant{}
		}
		if err := change(&next); err != nil {
			return r, err
		}
		out = next
		return stored{Source: &next}, nil
	})
	if err != nil {
		return Source{}, err
	}
	return cloneSource(out), nil
}

// Search ranks passages in the named sources against a query. No names means
// every source in the tenant. A name that does not exist is an error, so a
// caller cannot mistake "no such source" for "no matches".
//
// Documents marked excluded are left out. variant, when set, searches one
// source through a saved view instead: what that view leaves out, rather than
// what is left out now.
func (s *Store) Search(tenant string, names []string, query string, limit int, variant string) ([]Hit, error) {
	return s.search(tenant, names, query, limit, variant, nil, false)
}

// SearchForScopes checks every authorized source snapshot it searches.
// An empty selection searches nothing, never the entire tenant.
func (s *Store) SearchForScopes(tenant string, names []string, query string, limit int, variant string, expected map[string]*SourceConfig) ([]Hit, error) {
	if len(names) == 0 {
		return []Hit{}, nil
	}
	return s.search(tenant, names, query, limit, variant, expected, true)
}
func (s *Store) search(tenant string, names []string, query string, limit int, variant string, expected map[string]*SourceConfig, guard bool) ([]Hit, error) {
	if len(query) > 1000 {
		return nil, fmt.Errorf("%w: search query is at most 1000 bytes", ErrInvalid)
	}
	q := tokens(query)
	slices.Sort(q)
	q = slices.Compact(q)
	if len(q) == 0 {
		return nil, fmt.Errorf("%w: a search needs words to look for", ErrInvalid)
	}
	if limit <= 0 || limit > 50 {
		limit = 8
	}
	// Embedded first: it may call a model. Nil is lexical only.
	qv, model := s.embedder().query(query)

	named := len(names)
	if named == 0 {
		srcs, _, err := s.sources(tenant)
		if err != nil {
			return nil, err
		}
		for _, src := range srcs {
			names = append(names, src.Name)
		}
	}
	var in []*source
	for _, n := range names {
		src, err := s.current(tenant, n)
		if err != nil {
			return nil, err
		}
		if guard {
			cfg, ok := expected[n]
			if !ok || !sameConfiguration(src.Source, cfg) {
				return nil, ErrNotFound
			}
		}
		in = append(in, src)
	}

	for _, src := range in {
		if src.RequiresUpstreamRead() {
			return nil, ErrUpstreamRead
		}
	}
	skip := func(p *passage) bool { return p.doc.Excluded }
	if variant != "" {
		// The source must be named. Guessing it from "the tenant happens to
		// have one source" would change meaning when a second is added.
		if named != 1 {
			return nil, fmt.Errorf("%w: a variant belongs to one source", ErrInvalid)
		}
		i := slices.IndexFunc(in[0].Variants, func(x Variant) bool { return x.Name == variant })
		if i < 0 {
			return nil, fmt.Errorf("%w: variant %q", ErrNotFound, variant)
		}
		out := in[0].Variants[i].Excluded
		skip = func(p *passage) bool { return slices.Contains(out, p.doc.ID) }
	}

	// BM25 over the selected passages.
	const k1, b = 1.2, 0.75
	var total, count int
	df := map[string]int{}
	for _, src := range in {
		for _, p := range src.passages {
			if skip(p) {
				continue
			}
			total += p.n
			count++
			for _, t := range q {
				if p.terms[t] > 0 {
					df[t]++
				}
			}
		}
	}
	if count == 0 {
		return []Hit{}, nil
	}
	avg := float64(total) / float64(count)
	var lexical []scored
	for _, src := range in {
		for _, p := range src.passages {
			if skip(p) {
				continue
			}
			var score float64
			for _, t := range q {
				tf := float64(p.terms[t])
				if tf == 0 {
					continue
				}
				idf := math.Log(1 + (float64(count)-float64(df[t])+0.5)/(float64(df[t])+0.5))
				score += idf * tf * (k1 + 1) / (tf + k1*(1-b+b*float64(p.n)/avg))
			}
			if score > 0 {
				lexical = append(lexical, scored{src: src, p: p, score: score})
			}
		}
	}
	byScore(lexical)

	ranked := lexical
	if qv != nil {
		// The dense arm sees exactly the passages the lexical arm may see.
		var dense []scored
		for _, src := range in {
			for _, p := range src.passages {
				if skip(p) || p.vec == nil || p.model != model {
					continue
				}
				dense = append(dense, scored{src: src, p: p, score: dot(qv, p.vec)})
			}
		}
		byScore(dense)
		ranked = fuse(lexical, dense)
	}
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	hits := make([]Hit, 0, len(ranked))
	for _, c := range ranked {
		hits = append(hits, hitOf(c.src, c.p, c.score))
	}
	return hits, nil
}

// readCache keeps the sources this process read, by name, the version of
// the source's record and its content version. Every change to a source's
// documents, passages or vectors moves the record's version, and a source
// deleted and created again starts its versions over at a greater content
// version (the name's clock outlives it), so a source here is what the
// database holds at both, and dropping one changes no answer.
type readCache struct {
	mu    sync.Mutex
	by    map[[2]string]cachedSource
	bytes int
}

type cachedSource struct {
	version, content uint64
	src              *source
	bytes            int
}

// maxCacheBytes bounds how much document text a process keeps.
const maxCacheBytes = 256 << 20

func newReadCache() *readCache { return &readCache{by: map[[2]string]cachedSource{}} }

func (r *readCache) get(tenant, name string, version int64, content uint64) (*source, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.by[[2]string{tenant, name}]
	if !ok || c.version != uint64(version) || c.content != content {
		return nil, false
	}
	return c.src, true
}

func (r *readCache) put(tenant, name string, version int64, content uint64, src *source, bytes int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := [2]string{tenant, name}
	if old, ok := r.by[k]; ok {
		r.bytes -= old.bytes
		delete(r.by, k)
	}
	for other, c := range r.by {
		if r.bytes+bytes <= maxCacheBytes {
			break
		}
		r.bytes -= c.bytes
		delete(r.by, other)
	}
	if bytes > maxCacheBytes {
		return
	}
	r.by[k] = cachedSource{uint64(version), content, src, bytes}
	r.bytes += bytes
}

// hitOf is a passage as a search result.
func hitOf(src *source, p *passage, score float64) Hit {
	hit := Hit{Source: src.Name, DocumentID: p.doc.ID, Title: p.doc.Title, URI: p.doc.URI, Index: p.index, Text: p.text, Score: score, Mode: Indexed}
	if src.Lifecycle != nil {
		if len(src.Lifecycle.Attempts) > 0 {
			hit.RefreshFailed = src.Lifecycle.Attempts[len(src.Lifecycle.Attempts)-1].Status == "failed"
		}
		if src.Lifecycle.LastGoodAt != nil {
			at := *src.Lifecycle.LastGoodAt
			hit.CopyAt = &at
		}
	}
	if hit.CopyAt == nil && (src.Lifecycle == nil || src.Lifecycle.Provider == "manual") && !p.doc.AddedAt.IsZero() {
		at := p.doc.AddedAt
		hit.CopyAt = &at
	}
	return hit
}

// split cuts text into passages on blank lines, joining short paragraphs and
// breaking long ones at whitespace.
func split(text string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	for _, para := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
		para = strings.TrimSpace(para)
		for len(para) > passageSize {
			cut := strings.LastIndexFunc(para[:passageSize], unicode.IsSpace)
			if cut <= 0 {
				cut = passageSize
				for cut > 0 && para[cut]&0xC0 == 0x80 {
					cut--
				}
			}
			flush()
			out = append(out, strings.TrimSpace(para[:cut]))
			para = strings.TrimSpace(para[cut:])
		}
		if para == "" {
			continue
		}
		if cur.Len() > 0 && cur.Len()+len(para) > passageSize {
			flush()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(para)
	}
	flush()
	return out
}

// tokens lowercases text and splits it into words of letters and digits.
func tokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
