// Package catalog is Humagen's index of what exists in connected systems:
// tables, columns, folders and their descriptions (RFC 0005). It holds
// metadata only, never row data or file text.
//
// Each tenant has connectors, and each connector has the entries its last
// crawl listed. A crawl replaces its connector's entries all at once, so a
// table that was dropped disappears and a crawl that fails publishes
// nothing. Search is hybrid: BM25 over each entry's text, fused with vector
// similarity when an embedder is set, the way pkg/knowledge searches.
//
// Who sees an entry is the connector's rule (Connector.VisibleTo): its owner
// by default, and the members of its project once the owner publishes it.
// Seeing an entry never grants reading the object it names.
//
// It reads and writes the database per request (pkg/state, per request): a
// connector's record is the version every change to it and its entries is
// made at, so any number of processes on one database agree, and it does
// not need pkg/state's single-writer lock. Search reads the selected
// connectors' entries, each connector capped at MaxEntries; a process may
// keep what it read, by the version of the connector it read it at.
package catalog

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/state"
)

// Errors a caller can match.
var (
	ErrNotFound = errors.New("catalog: not found")
	ErrInvalid  = errors.New("catalog: invalid")
)

// Bounds on what one connector may hold. A crawl past them is refused whole.
const (
	MaxEntries     = 5000
	MaxColumns     = 1000
	MaxDescription = 1024
	MaxPath        = 1024
	MaxQuery       = 1000
	MaxHits        = 50
	// MaxConnectors bounds one tenant's connectors.
	MaxConnectors = 100
)

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ValidName reports whether a name can name a connector: lower case
// letters, digits and dashes, starting with a letter or digit.
func ValidName(name string) bool { return validName.MatchString(name) }

// Column is one column of a table, with its nested fields for a record.
type Column struct {
	Name        string   `json:"name"`
	Type        string   `json:"type,omitempty"`
	Mode        string   `json:"mode,omitempty"`
	Description string   `json:"description,omitempty"`
	Fields      []Column `json:"fields,omitempty"`
}

// Stats is what the system reports about an object's size and age.
type Stats struct {
	Rows     *int64     `json:"rows,omitempty"`
	Bytes    *int64     `json:"bytes,omitempty"`
	Modified *time.Time `json:"modified,omitempty"`
}

// Entry is one object in a connected system.
type Entry struct {
	System      string   `json:"system"`
	Kind        string   `json:"kind"`
	Path        string   `json:"path"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Schema      []Column `json:"schema,omitempty"`
	Stats       Stats    `json:"stats,omitzero"`
	// Connector and SeenAt are set by the store: which connector listed
	// the entry, and when.
	Connector string    `json:"connector"`
	SeenAt    time.Time `json:"seen_at"`
}

// Mark is who did something, and when.
type Mark struct {
	By string    `json:"by"`
	At time.Time `json:"at"`
}

// Connector is one connection to a system whose objects the catalog lists.
type Connector struct {
	Name   string `json:"name"`
	System string `json:"system"`
	// Owner is the person whose credential crawls it. They see its entries.
	Owner string `json:"owner,omitempty"`
	// Project is whose members see its entries once it is published. Empty
	// is the workspace.
	Project string `json:"project,omitempty"`
	// Published is set while the owner publishes its metadata, and
	// Withdrawn is the last withdrawal.
	Published *Mark `json:"published,omitempty"`
	Withdrawn *Mark `json:"withdrawn,omitempty"`
	// CrawledAt is the last crawl that was kept, and Entries how many
	// entries it listed.
	CrawledAt *time.Time `json:"crawled_at,omitempty"`
	Entries   int        `json:"entries"`
	// Generation counts kept crawls, so a vector computed for one crawl's
	// entry is never attached to the next one's.
	Generation int64 `json:"generation"`
}

// VisibleTo reports whether a person sees the connector's entries: its
// owner always, and anyone member says is a member of its project while it
// is published. An empty subject sees nothing.
func (c Connector) VisibleTo(subject string, member func(project string) bool) bool {
	if subject == "" {
		return false
	}
	if c.Owner != "" && c.Owner == subject {
		return true
	}
	return c.Published != nil && member != nil && member(c.Project)
}

// Hit is one search result.
type Hit struct {
	Entry Entry   `json:"entry"`
	Score float64 `json:"score"`
}

// record is one stored document: a connector, an entry, an entry's vector,
// or the stamp every new connector moves.
type record struct {
	Connector *Connector    `json:"connector,omitempty"`
	Entry     *Entry        `json:"entry,omitempty"`
	Vector    *vectorRecord `json:"vector,omitempty"`
	Stamp     int64         `json:"stamp,omitempty"`
}

// namespace is where the catalog is kept. The tenant is the shard key;
// within it:
//
//	c/<connector>          a connector
//	e/<connector>/<path>   an entry its last kept crawl listed
//	v/<connector>/<path>   that entry's vector
//	h                      a stamp every new connector moves, so two
//	                       processes adding connectors at once cannot both
//	                       count under MaxConnectors
//
// A change to a connector's entries or vectors is written at the version of
// its connector record, and moves it.
const (
	namespace = "catalog"
	headKey   = "h"
)

func connectorKey(name string) string    { return "c/" + name }
func entryPrefix(name string) string     { return "e/" + name + "/" }
func vectorPrefix(name string) string    { return "v/" + name + "/" }
func entryKey(name, path string) string  { return entryPrefix(name) + path }
func vectorKey(name, path string) string { return vectorPrefix(name) + path }

type entry struct {
	Entry
	terms map[string]int
	n     int
	vec   []float32
	model string
}

// Store is the catalog: connectors and their entries per tenant, read and
// written per request on a state.Store.
type Store struct {
	kept *state.Table[record]
	dir  *state.Tenants
	read *readCache
	mu   sync.RWMutex // guards dense
	// dense is the embedder; nil is lexical only.
	dense *denseIndex
}

// NewStore returns a store in this process's memory.
func NewStore() *Store {
	s, err := Open(nil)
	if err != nil {
		panic(err) // a store in memory cannot fail to open
	}
	return s
}

// Open returns a store on st, which may be shared with other processes. A
// nil st keeps the catalog in this process's memory.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	dir, err := state.OpenTenants(st, namespace)
	if err != nil {
		return nil, fmt.Errorf("catalog: tenants: %w", err)
	}
	return &Store{kept: state.NewTable[record](st, namespace), dir: dir, read: newReadCache()}, nil
}

// connector reads one connector and the version of its record.
func (s *Store) connector(tenant, name string) (Connector, int64, error) {
	it, err := s.kept.Lookup(tenant, connectorKey(name))
	if errors.Is(err, state.ErrNotFound) || err == nil && it.Value.Connector == nil {
		return Connector{}, 0, fmt.Errorf("%w: connector %q", ErrNotFound, name)
	}
	if err != nil {
		return Connector{}, 0, err
	}
	return *it.Value.Connector, it.Version, nil
}

// keys reads the keys of a connector's entries and vectors.
func (s *Store) keys(tenant, name string) ([]string, error) {
	var out []string
	for _, prefix := range []string{entryPrefix(name), vectorPrefix(name)} {
		items, err := s.kept.All(tenant, prefix, MaxEntries)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			out = append(out, it.Key)
		}
	}
	return out, nil
}

// indexed is an entry with its terms counted.
func indexed(e Entry) *entry {
	in := &entry{Entry: e, terms: map[string]int{}}
	for _, t := range tokens(Text(e)) {
		in.terms[t]++
		in.n++
	}
	return in
}

// Text is the one short text an entry is searched by: its path, title,
// description, and its columns' names and descriptions. Never data.
func Text(e Entry) string {
	var b strings.Builder
	b.WriteString(e.Path)
	for _, s := range []string{e.Kind, e.Title, e.Description} {
		if s != "" {
			b.WriteString("\n")
			b.WriteString(s)
		}
	}
	var walk func(prefix string, cols []Column)
	walk = func(prefix string, cols []Column) {
		for _, c := range cols {
			b.WriteString("\n")
			b.WriteString(prefix + c.Name)
			if c.Description != "" {
				b.WriteString(": ")
				b.WriteString(c.Description)
			}
			walk(prefix+c.Name+".", c.Fields)
		}
	}
	walk("", e.Schema)
	return b.String()
}

// Declare adds a connector, or updates one that exists, from configuration.
// Its publication and entries are kept, unless its system or owner changed:
// entries crawled with someone else's credential are dropped, and so is the
// publication, which was the old owner's choice.
func (s *Store) Declare(tenant string, c Connector) (Connector, error) {
	if !ValidName(c.Name) || c.System == "" {
		return Connector{}, fmt.Errorf("%w: a connector needs a name of lower case letters, digits and dashes, and a system", ErrInvalid)
	}
	if err := s.dir.Mark(tenant); err != nil {
		return Connector{}, err
	}
	var out Connector
	err := state.Retry(func() error {
		have, version, err := s.connector(tenant, c.Name)
		exists := err == nil
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		next := Connector{Name: c.Name, System: c.System, Owner: c.Owner, Project: c.Project}
		b := s.kept.Batch()
		if exists {
			next.Published, next.Withdrawn = have.Published, have.Withdrawn
			next.CrawledAt, next.Entries, next.Generation = have.CrawledAt, have.Entries, have.Generation
			if have.System != c.System || have.Owner != c.Owner {
				keys, err := s.keys(tenant, c.Name)
				if err != nil {
					return err
				}
				for _, k := range keys {
					b.Delete(tenant, k)
				}
				next.Published, next.CrawledAt, next.Entries = nil, nil, 0
				next.Generation++
			}
			if have.Project != c.Project {
				next.Published = nil // published to one audience is not published to another
			}
			if have == next {
				out = next
				return nil
			}
			b.PutAt(tenant, connectorKey(c.Name), record{Connector: &next}, version)
		} else {
			have, head, err := s.connectors(tenant)
			if err != nil {
				return err
			}
			if len(have) >= MaxConnectors {
				return fmt.Errorf("%w: at most %d connectors", ErrInvalid, MaxConnectors)
			}
			b.Create(tenant, connectorKey(c.Name), record{Connector: &next})
			bump(b, tenant, head)
		}
		if err := b.Commit(); err != nil {
			return err
		}
		out = next
		return nil
	})
	if err != nil {
		return Connector{}, err
	}
	return out, nil
}

// bump moves the head stamp read at version, 0 when there is none.
func bump(b *state.Batch[record], tenant string, version int64) {
	if version == 0 {
		b.Create(tenant, headKey, record{Stamp: 1})
		return
	}
	b.PutAt(tenant, headKey, record{Stamp: version + 1}, version)
}

// Remove deletes a connector and its entries.
func (s *Store) Remove(tenant, name string) error {
	return state.Retry(func() error {
		_, version, err := s.connector(tenant, name)
		if err != nil {
			return err
		}
		keys, err := s.keys(tenant, name)
		if err != nil {
			return err
		}
		b := s.kept.Batch()
		for _, k := range keys {
			b.Delete(tenant, k)
		}
		b.DeleteAt(tenant, connectorKey(name), version)
		return b.Commit()
	})
}

// Connector returns one connector.
func (s *Store) Connector(tenant, name string) (Connector, error) {
	c, _, err := s.connector(tenant, name)
	return c, err
}

// EachTenant calls fn for every tenant that has had a connector, a page of
// the directory at a time. A tenant whose connectors are all removed reads
// as one with none.
func (s *Store) EachTenant(fn func(tenant string) error) error { return s.dir.Each(fn) }

// Connectors returns a tenant's connectors by name.
func (s *Store) Connectors(tenant string) ([]Connector, error) {
	out, _, err := s.connectors(tenant)
	return out, err
}

// connectors reads a tenant's connectors by name, and the version of its
// head stamp.
// The stamp is read first: a connector added after it moves it, so a count
// made under it is written only if nothing was added since.
func (s *Store) connectors(tenant string) ([]Connector, int64, error) {
	head, err := s.kept.Lookup(tenant, headKey)
	if err != nil && !errors.Is(err, state.ErrNotFound) {
		return nil, 0, err
	}
	items, err := s.kept.All(tenant, "c/", MaxConnectors)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Connector, 0, len(items))
	for _, it := range items {
		if it.Value.Connector != nil {
			out = append(out, *it.Value.Connector)
		}
	}
	return out, head.Version, nil
}

// Replace keeps a crawl: the connector's entries become exactly these, in
// one write. A crawl that breaks a bound, or names a path twice, is refused
// and the entries before it stay.
func (s *Store) Replace(tenant, name string, crawled []Entry, at time.Time) (Connector, error) {
	if len(crawled) > MaxEntries {
		return Connector{}, fmt.Errorf("%w: a crawl lists at most %d entries, not %d", ErrInvalid, MaxEntries, len(crawled))
	}
	at = at.UTC()
	seen := make(map[string]bool, len(crawled))
	next := make([]*entry, 0, len(crawled))
	for _, e := range crawled {
		if err := checkEntry(e); err != nil {
			return Connector{}, err
		}
		if seen[e.Path] {
			return Connector{}, fmt.Errorf("%w: path %q listed twice", ErrInvalid, e.Path)
		}
		seen[e.Path] = true
		e.Connector, e.SeenAt = name, at
		e.Description = clip(e.Description)
		e.Schema = clipColumns(e.Schema)
		next = append(next, indexed(e))
	}
	slices.SortFunc(next, func(a, b *entry) int { return strings.Compare(a.Path, b.Path) })

	var updated Connector
	err := state.Retry(func() error {
		c, version, err := s.connector(tenant, name)
		if err != nil {
			return err
		}
		before, err := s.entries(tenant, name, c, version)
		if err != nil {
			return err
		}
		updated = c
		updated.CrawledAt, updated.Entries = &at, len(next)
		updated.Generation++
		b := s.kept.Batch()
		listed := make(map[string]bool, len(next))
		for _, e := range next {
			listed[e.Path] = true
		}
		was := make(map[string]*entry, len(before))
		for _, e := range before {
			was[e.Path] = e
			if !listed[e.Path] {
				b.Delete(tenant, entryKey(name, e.Path))
			}
		}
		// An entry whose text did not change keeps its vector, under the new
		// generation, so a recrawl does not embed the catalog again.
		for _, e := range next {
			b.Put(tenant, entryKey(name, e.Path), record{Entry: &e.Entry})
			if old := was[e.Path]; old != nil && old.vec != nil && Text(old.Entry) == Text(e.Entry) {
				b.Put(tenant, vectorKey(name, e.Path), record{Vector: &vectorRecord{Model: old.model, Generation: updated.Generation, Data: encode(old.vec)}})
			} else if old != nil {
				b.Delete(tenant, vectorKey(name, e.Path))
			}
		}
		for _, e := range before {
			if !listed[e.Path] {
				b.Delete(tenant, vectorKey(name, e.Path))
			}
		}
		b.PutAt(tenant, connectorKey(name), record{Connector: &updated}, version)
		return b.Commit()
	})
	if err != nil {
		return Connector{}, err
	}
	s.nudge()
	return updated, nil
}

func checkEntry(e Entry) error {
	switch {
	case e.Path == "" || len(e.Path) > MaxPath || !utf8.ValidString(e.Path):
		return fmt.Errorf("%w: an entry's path is 1 to %d bytes of UTF-8", ErrInvalid, MaxPath)
	case e.System == "" || e.Kind == "":
		return fmt.Errorf("%w: entry %q needs a system and a kind", ErrInvalid, e.Path)
	case countColumns(e.Schema) > MaxColumns:
		return fmt.Errorf("%w: entry %q has more than %d columns", ErrInvalid, e.Path, MaxColumns)
	}
	return nil
}

func countColumns(cols []Column) int {
	n := len(cols)
	for _, c := range cols {
		n += countColumns(c.Fields)
	}
	return n
}

// clip cuts a description to MaxDescription bytes on a rune boundary.
func clip(s string) string {
	if len(s) <= MaxDescription {
		return s
	}
	cut := MaxDescription
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func clipColumns(cols []Column) []Column {
	if cols == nil {
		return nil
	}
	out := make([]Column, len(cols))
	for i, c := range cols {
		c.Description = clip(c.Description)
		c.Fields = clipColumns(c.Fields)
		out[i] = c
	}
	return out
}

// Publish shows the connector's entries to the members of its project, and
// records who chose to and when.
func (s *Store) Publish(tenant, name, by string, at time.Time) (Connector, error) {
	return s.mark(tenant, name, func(c *Connector) {
		if c.Published == nil {
			c.Published = &Mark{By: by, At: at.UTC()}
		}
	})
}

// Withdraw hides the connector's entries from everyone but its owner at
// once, and records who withdrew them and when.
func (s *Store) Withdraw(tenant, name, by string, at time.Time) (Connector, error) {
	return s.mark(tenant, name, func(c *Connector) {
		if c.Published != nil {
			c.Published, c.Withdrawn = nil, &Mark{By: by, At: at.UTC()}
		}
	})
}

func (s *Store) mark(tenant, name string, change func(*Connector)) (Connector, error) {
	var out Connector
	_, err := s.kept.Update(tenant, connectorKey(name), func(r record, ok bool) (record, error) {
		if !ok || r.Connector == nil {
			return r, fmt.Errorf("%w: connector %q", ErrNotFound, name)
		}
		updated := *r.Connector
		change(&updated)
		out = updated
		if updated == *r.Connector {
			return r, state.ErrUnchanged
		}
		return record{Connector: &updated}, nil
	})
	if err != nil {
		return Connector{}, err
	}
	return out, nil
}

// List returns the entries of the named connectors, ordered by connector
// and path, at most limit of them (1 to MaxEntries). An empty selection
// lists nothing.
func (s *Store) List(tenant string, connectors []string, limit int) ([]Entry, error) {
	if limit <= 0 || limit > MaxEntries {
		limit = MaxHits
	}
	names := slices.Clone(connectors)
	slices.Sort(names)
	names = slices.Compact(names)
	out := []Entry{}
	for _, n := range names {
		if !ValidName(n) {
			continue
		}
		after := ""
		for len(out) < limit {
			page, next, err := s.kept.List(tenant, state.Query{Prefix: entryPrefix(n), After: after, Limit: limit - len(out)})
			if err != nil {
				return nil, err
			}
			for _, it := range page {
				if it.Value.Entry != nil {
					out = append(out, *it.Value.Entry)
				}
			}
			if next == "" {
				break
			}
			after = next
		}
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

// tokens lowercases text and splits it into words of letters and digits,
// so "churn_monthly" is "churn" and "monthly".
func tokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
