// Package knowledge holds the documents agents may be grounded in.
//
// A source is a named collection in one tenant. A document added to it is cut
// into passages, and Search ranks passages against a query with BM25.
//
// The store is in memory and lexical only: no embeddings. Built with Open, it
// writes every change through a state.Store, passages included, and reads
// them back when it is built again. It is the smallest thing that makes a
// source real enough to fill, search and assign to an agent.
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
	ErrInvalid  = errors.New("knowledge: invalid")
	ErrExists   = errors.New("knowledge: source already exists")
	ErrNotFound = errors.New("knowledge: not found")
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
}

type source struct {
	Source
	docs     []*Document
	passages []*passage
	// seqs is each document's place in docs as it was written, by id.
	seqs map[string]int64
}

// Store holds every tenant's sources. Every method is confined to one tenant.
type Store struct {
	mu       sync.RWMutex
	tenants  map[string]map[string]*source
	versions map[string]map[string]uint64
	seq      int64
	// kept is where sources and documents are written. Nil keeps them only
	// in memory.
	kept *state.Table[stored]
}

// stored is one record: a source, a document, or a document's passages. A
// document's passages are written once, when it is added, and its record
// again whenever it changes.
type stored struct {
	Clock    *contentClock `json:"clock,omitempty"`
	Source   *Source       `json:"source,omitempty"`
	Document *Document     `json:"document,omitempty"`
	Passages []string      `json:"passages,omitempty"`
}

func sourceKey(name string) string { return "source/" + name }
func docKey(name string, seq int64) string {
	return "doc/" + name + "/" + state.Seq(seq)
}
func textKey(name string, seq int64) string {
	return "text/" + name + "/" + state.Seq(seq)
}

// NewStore returns an empty store in memory.
func NewStore() *Store {
	return &Store{tenants: map[string]map[string]*source{}, versions: map[string]map[string]uint64{}}
}

// Open returns a store that keeps its sources in st, holding what st already
// has. A nil st is NewStore.
func Open(st state.Store) (*Store, error) {
	s := NewStore()
	s.kept = state.NewTable[stored](st, "knowledge")
	type doc struct {
		d        *Document
		passages []string
	}
	docs := map[[2]string]map[int64]*doc{} // (tenant, source) -> seq -> doc
	at := func(tenant, key, kind string) (string, int64, bool) {
		rest, ok := strings.CutPrefix(key, kind+"/")
		if !ok {
			return "", 0, false
		}
		i := strings.LastIndexByte(rest, '/')
		if i < 0 {
			return "", 0, false
		}
		n, err := strconv.ParseInt(rest[i+1:], 10, 64)
		if err != nil {
			return "", 0, false
		}
		return rest[:i], n, true
	}
	of := func(tenant, name string, seq int64) *doc {
		k := [2]string{tenant, name}
		if docs[k] == nil {
			docs[k] = map[int64]*doc{}
		}
		if docs[k][seq] == nil {
			docs[k][seq] = &doc{}
		}
		return docs[k][seq]
	}
	err := s.kept.Load(func(tenant, key string, r stored) error {
		switch {
		case r.Clock != nil:
			if !ValidName(r.Clock.Name) || key != clockKey(r.Clock.Name) {
				return ErrInvalid
			}
			s.rememberVersion(tenant, r.Clock.Name, r.Clock.Value)
		case r.Source != nil:
			if s.tenants[tenant] == nil {
				s.tenants[tenant] = map[string]*source{}
			}
			src := &source{Source: *r.Source, seqs: map[string]int64{}}
			if src.Variants == nil {
				src.Variants = []Variant{}
			}
			s.tenants[tenant][src.Name] = src
			s.rememberVersion(tenant, src.Name, src.ContentVersion)
		case r.Document != nil:
			if name, seq, ok := at(tenant, key, "doc"); ok {
				d := *r.Document
				of(tenant, name, seq).d = &d
				s.seq = max(s.seq, seq)
			}
		case r.Passages != nil:
			if name, seq, ok := at(tenant, key, "text"); ok {
				of(tenant, name, seq).passages = r.Passages
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge: load: %w", err)
	}
	for k, bySeq := range docs {
		src := s.tenants[k[0]][k[1]]
		if src == nil {
			continue
		}
		seqs := make([]int64, 0, len(bySeq))
		for seq := range bySeq {
			seqs = append(seqs, seq)
		}
		slices.Sort(seqs)
		for _, seq := range seqs {
			d := bySeq[seq]
			if d.d == nil {
				continue
			}
			src.docs = append(src.docs, d.d)
			src.seqs[d.d.ID] = seq
			src.passages = append(src.passages, passagesOf(d.d, d.passages)...)
		}
		src.Documents, src.Passages = len(src.docs), len(src.passages)
		src.recount()
	}
	// An attempt that outlived this process did not complete. Keep its last good
	// copy and permit an explicit retry, without inventing successful work.
	for tenant, sources := range s.tenants {
		for name, src := range sources {
			if src.Lifecycle != nil {
				if id := active(src.Lifecycle); id != "" {
					if _, err := s.FailSync(tenant, name, id, Interrupted, time.Now().UTC()); err != nil {
						return nil, fmt.Errorf("knowledge: recover attempt: %w", err)
					}
				}
			}
		}
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

func (s *Store) get(tenant, name string) (*source, error) {
	src, ok := s.tenants[tenant][name]
	if !ok {
		return nil, fmt.Errorf("%w: source %q", ErrNotFound, name)
	}
	return src, nil
}

// Create adds an empty source.
func (s *Store) Create(tenant, name, description, by string, at time.Time) (Source, error) {
	if !validName.MatchString(name) {
		return Source{}, fmt.Errorf("%w: a source name is lowercase letters, digits and dashes", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.tenants[tenant]) >= 200 {
		return Source{}, fmt.Errorf("%w: at most 200 sources", ErrInvalid)
	}
	if _, ok := s.tenants[tenant][name]; ok {
		return Source{}, fmt.Errorf("%w: %q", ErrExists, name)
	}
	if _, known := s.versions[tenant][name]; !known && len(s.versions[tenant]) >= MaxSourceNames {
		return Source{}, fmt.Errorf("%w: source name history capacity reached", ErrInvalid)
	}
	src := &source{Source: Source{Name: name, Description: description, Variants: []Variant{}, CreatedBy: by, UpdatedAt: at}, seqs: map[string]int64{}}
	src.ContentVersion = s.versions[tenant][name]
	if err := s.kept.Put(tenant, sourceKey(name), stored{Source: &src.Source}); err != nil {
		return Source{}, fmt.Errorf("knowledge: write: %w", err)
	}
	if s.tenants[tenant] == nil {
		s.tenants[tenant] = map[string]*source{}
	}
	s.tenants[tenant][name] = src
	s.rememberVersion(tenant, name, src.ContentVersion)
	return src.Source, nil
}

// Delete removes a source and everything in it.
func (s *Store) Delete(tenant, name string) error { return s.deleteWithScope(tenant, name, nil, false) }

// DeleteForScope rejects a source whose authorized audience changed before the mutation.
func (s *Store) DeleteForScope(tenant, name string, expected *SourceConfig) error {
	return s.deleteWithScope(tenant, name, expected, true)
}
func (s *Store) deleteWithScope(tenant, name string, expected *SourceConfig, guard bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	src, err := s.get(tenant, name)
	if err != nil {
		return err
	}
	if guard && !sameConfiguration(src.Source, expected) {
		return ErrNotFound
	}
	version, versionErr := s.nextVersion(tenant, name)
	if versionErr != nil {
		return versionErr
	}
	b := s.kept.Batch()
	b.Delete(tenant, sourceKey(name))
	for _, seq := range src.seqs {
		b.Delete(tenant, docKey(name, seq))
		b.Delete(tenant, textKey(name, seq))
	}
	b.Put(tenant, clockKey(name), stored{Clock: &contentClock{Name: name, Value: version}})
	if err := b.Commit(); err != nil {
		return fmt.Errorf("knowledge: write: %w", err)
	}
	s.rememberVersion(tenant, name, version)
	delete(s.tenants[tenant], name)
	return nil
}

// List returns a tenant's sources by name.
func (s *Store) List(tenant string) []Source {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Source, 0, len(s.tenants[tenant]))
	for _, src := range s.tenants[tenant] {
		view := cloneSource(src.Source)
		out = append(out, view)
	}
	slices.SortFunc(out, func(a, b Source) int { return strings.Compare(a.Name, b.Name) })
	return out
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
	s.mu.RLock()
	defer s.mu.RUnlock()
	src, err := s.get(tenant, name)
	if err != nil {
		return nil, err
	}
	if guard && !sameConfiguration(src.Source, expected) {
		return nil, ErrNotFound
	}
	out := make([]Document, 0, len(src.docs))
	for _, d := range slices.Backward(src.docs) {
		out = append(out, *d)
	}
	return out, nil
}

// Add cuts a document into passages and stores it.
func (s *Store) Add(tenant, name, title, uri, text, by string, at time.Time) (Document, error) {
	return s.add(tenant, name, title, uri, text, by, at, nil, false)
}

// AddForScope inserts only while the source retains the exact expected public
// configuration. A nil expectation requires a legacy manual workspace source.
// The guard and insertion hold one lock, so a concurrent Configure cannot
// broaden the audience or replace the source authority between them.
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

	s.mu.Lock()
	defer s.mu.Unlock()
	src, err := s.get(tenant, name)
	if err != nil {
		return Document{}, err
	}
	totalBytes := doc.Bytes
	for _, prior := range src.docs {
		totalBytes += prior.Bytes
	}
	if len(src.docs) >= MaxImportDocuments || totalBytes > MaxImportBytes {
		return Document{}, fmt.Errorf("%w: source document or byte limit reached", ErrInvalid)
	}

	if guard {
		if expected == nil {
			if src.Lifecycle != nil {
				return Document{}, fmt.Errorf("%w: the source configuration changed; review its audience again", ErrExists)
			}
		} else if src.Lifecycle == nil || src.Lifecycle.SourceConfig != *expected || src.Lifecycle.Mode != Indexed || src.Lifecycle.Provider != "manual" {
			return Document{}, fmt.Errorf("%w: the source configuration changed; review its audience again", ErrExists)
		}
	}
	texts := split(text)
	added := passagesOf(doc, texts)
	doc.Passages = len(added)
	seq := s.seq + 1
	meta := cloneSource(src.Source)
	if meta.Lifecycle != nil && meta.Lifecycle.Provider == "manual" && meta.Lifecycle.Mode == Indexed {
		stamp := at
		meta.Lifecycle.Status = Available
		meta.Lifecycle.LastGoodAt = &stamp
		meta.Lifecycle.Cause = ""
	}
	version, versionErr := s.nextVersion(tenant, name)
	if versionErr != nil {
		return Document{}, versionErr
	}
	meta.ContentVersion = version
	meta.Documents, meta.Passages, meta.UpdatedAt = len(src.docs)+1, len(src.passages)+len(added), at
	b := s.kept.Batch()
	b.Put(tenant, textKey(name, seq), stored{Passages: texts})
	b.Put(tenant, docKey(name, seq), stored{Document: doc})
	b.Put(tenant, sourceKey(name), stored{Source: &meta})
	b.Put(tenant, clockKey(name), stored{Clock: &contentClock{Name: name, Value: meta.ContentVersion}})
	if err := b.Commit(); err != nil {
		return Document{}, fmt.Errorf("knowledge: write: %w", err)
	}
	s.rememberVersion(tenant, name, meta.ContentVersion)
	s.seq = seq
	src.seqs[doc.ID] = seq
	src.passages = append(src.passages, added...)
	src.docs = append(src.docs, doc)
	src.Source = meta
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
	s.mu.Lock()
	defer s.mu.Unlock()
	src, err := s.get(tenant, name)
	if err != nil {
		return err
	}
	if guard && !sameConfiguration(src.Source, expected) {
		return ErrNotFound
	}
	i := slices.IndexFunc(src.docs, func(d *Document) bool { return d.ID == id })
	if i < 0 {
		return fmt.Errorf("%w: document %q", ErrNotFound, id)
	}
	docs := slices.Delete(slices.Clone(src.docs), i, i+1)
	passages := slices.DeleteFunc(slices.Clone(src.passages), func(p *passage) bool { return p.doc.ID == id })
	variants := slices.Clone(src.Variants)
	for i := range variants {
		variants[i].Excluded = slices.DeleteFunc(slices.Clone(variants[i].Excluded), func(x string) bool { return x == id })
	}
	next := *src
	next.docs, next.passages, next.Variants = docs, passages, variants
	next.Documents, next.Passages, next.UpdatedAt = len(docs), len(passages), at
	version, versionErr := s.nextVersion(tenant, name)
	if versionErr != nil {
		return versionErr
	}
	next.ContentVersion = version
	next.recount()
	seq := src.seqs[id]
	b := s.kept.Batch()
	b.Delete(tenant, docKey(name, seq))
	b.Delete(tenant, textKey(name, seq))
	b.Put(tenant, sourceKey(name), stored{Source: &next.Source})
	b.Put(tenant, clockKey(name), stored{Clock: &contentClock{Name: name, Value: next.ContentVersion}})
	if err := b.Commit(); err != nil {
		return fmt.Errorf("knowledge: write: %w", err)
	}
	s.rememberVersion(tenant, name, next.ContentVersion)
	delete(next.seqs, id)
	*src = next
	return nil
}

func (src *source) recount() {
	src.Excluded = 0
	for _, d := range src.docs {
		if d.Excluded {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	src, err := s.get(tenant, name)
	if err != nil {
		return Document{}, err
	}
	if guard && !sameConfiguration(src.Source, expected) {
		return Document{}, ErrNotFound
	}
	i := slices.IndexFunc(src.docs, func(d *Document) bool { return d.ID == id })
	if i < 0 {
		return Document{}, fmt.Errorf("%w: document %q", ErrNotFound, id)
	}
	doc := *src.docs[i]
	doc.Excluded = excluded
	meta := src.Source
	meta.UpdatedAt = at
	meta.Excluded = src.Excluded
	if doc.Excluded != src.docs[i].Excluded {
		version, versionErr := s.nextVersion(tenant, name)
		if versionErr != nil {
			return Document{}, versionErr
		}
		meta.ContentVersion = version
		if excluded {
			meta.Excluded++
		} else {
			meta.Excluded--
		}
	}
	b := s.kept.Batch()
	b.Put(tenant, docKey(name, src.seqs[id]), stored{Document: &doc})
	b.Put(tenant, sourceKey(name), stored{Source: &meta})
	b.Put(tenant, clockKey(name), stored{Clock: &contentClock{Name: name, Value: meta.ContentVersion}})
	if err := b.Commit(); err != nil {
		return Document{}, fmt.Errorf("knowledge: write: %w", err)
	}
	s.rememberVersion(tenant, name, meta.ContentVersion)
	src.docs[i].Excluded = excluded
	src.Source = meta
	src.recount()
	return *src.docs[i], nil
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
	s.mu.Lock()
	defer s.mu.Unlock()
	src, err := s.get(tenant, name)
	if err != nil {
		return Variant{}, err
	}
	if guard && !sameConfiguration(src.Source, expected) {
		return Variant{}, ErrNotFound
	}
	v := Variant{Name: variant, Excluded: []string{}}
	for _, d := range src.docs {
		if d.Excluded {
			v.Excluded = append(v.Excluded, d.ID)
		}
	}
	i := slices.IndexFunc(src.Variants, func(x Variant) bool { return x.Name == variant })
	variants := slices.Clone(src.Variants)
	switch {
	case i >= 0:
		variants[i] = v
	case len(src.Variants) >= MaxVariants:
		return Variant{}, fmt.Errorf("%w: a source keeps at most %d variants", ErrInvalid, MaxVariants)
	default:
		variants = append(variants, v)
	}
	meta := src.Source
	meta.Variants, meta.UpdatedAt = variants, at
	if err := s.kept.Put(tenant, sourceKey(name), stored{Source: &meta}); err != nil {
		return Variant{}, fmt.Errorf("knowledge: write: %w", err)
	}
	src.Variants, src.UpdatedAt = variants, at
	return v, nil
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
	s.mu.Lock()
	defer s.mu.Unlock()
	src, err := s.get(tenant, name)
	if err != nil {
		return err
	}
	if guard && !sameConfiguration(src.Source, expected) {
		return ErrNotFound
	}
	i := slices.IndexFunc(src.Variants, func(x Variant) bool { return x.Name == variant })
	if i < 0 {
		return fmt.Errorf("%w: variant %q", ErrNotFound, variant)
	}
	meta := src.Source
	meta.Variants = slices.Delete(slices.Clone(src.Variants), i, i+1)
	if err := s.kept.Put(tenant, sourceKey(name), stored{Source: &meta}); err != nil {
		return fmt.Errorf("knowledge: write: %w", err)
	}
	src.Variants = meta.Variants
	return nil
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

// SearchForScopes checks every authorized source snapshot under the read lock.
// An empty selection searches nothing, never the entire tenant.
func (s *Store) SearchForScopes(tenant string, names []string, query string, limit int, variant string, expected map[string]*SourceConfig) ([]Hit, error) {
	if len(names) == 0 {
		return []Hit{}, nil
	}
	return s.search(tenant, names, query, limit, variant, expected, true)
}
func (s *Store) search(tenant string, names []string, query string, limit int, variant string, expected map[string]*SourceConfig, guard bool) ([]Hit, error) {
	q := tokens(query)
	if len(q) == 0 {
		return nil, fmt.Errorf("%w: a search needs words to look for", ErrInvalid)
	}
	if limit <= 0 || limit > 50 {
		limit = 8
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	var in []*source
	if len(names) == 0 {
		for _, src := range s.tenants[tenant] {
			in = append(in, src)
		}
	}
	for _, n := range names {
		src, err := s.get(tenant, n)
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

	skip := func(p *passage) bool { return p.doc.Excluded }
	if variant != "" {
		// The source must be named. Guessing it from "the tenant happens to
		// have one source" would change meaning when a second is added.
		if len(names) != 1 {
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
	hits := []Hit{}
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
				hit := Hit{Source: src.Name, DocumentID: p.doc.ID, Title: p.doc.Title, URI: p.doc.URI, Index: p.index, Text: p.text, Score: score}
				if src.Lifecycle != nil {
					hit.Mode = Indexed
					hit.RefreshFailed = src.Lifecycle.Status == NeedsAttention
					if src.Lifecycle.LastGoodAt != nil {
						at := *src.Lifecycle.LastGoodAt
						hit.CopyAt = &at
					}
				}
				hits = append(hits, hit)
			}
		}
	}
	slices.SortStableFunc(hits, func(a, b Hit) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		}
		return 0
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
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
