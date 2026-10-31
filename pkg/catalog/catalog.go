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

// record is one stored document: a connector, an entry, or an entry's
// vector.
type record struct {
	Connector *Connector    `json:"connector,omitempty"`
	Entry     *Entry        `json:"entry,omitempty"`
	Vector    *vectorRecord `json:"vector,omitempty"`
}

func connectorKey(name string) string    { return "c/" + name }
func entryKey(name, path string) string  { return "e/" + name + "/" + path }
func vectorKey(name, path string) string { return "v/" + name + "/" + path }

// splitKey reads a connector name and a path out of an entry or vector key.
func splitKey(key, kind string) (string, string, bool) {
	rest, ok := strings.CutPrefix(key, kind+"/")
	if !ok {
		return "", "", false
	}
	name, path, ok := strings.Cut(rest, "/")
	return name, path, ok && name != "" && path != ""
}

type entry struct {
	Entry
	terms map[string]int
	n     int
	vec   []float32
	model string
}

type connector struct {
	Connector
	entries []*entry
}

// Store is the catalog: connectors and their entries per tenant, in memory,
// written through to a state.Store.
type Store struct {
	mu      sync.RWMutex
	kept    *state.Table[record]
	tenants map[string]map[string]*connector
	dense   *denseIndex
}

// NewStore returns an empty store in memory.
func NewStore() *Store { return &Store{tenants: map[string]map[string]*connector{}} }

// Open returns a store that keeps its records in st, holding what st
// already has. A nil st is NewStore.
func Open(st state.Store) (*Store, error) {
	s := NewStore()
	s.kept = state.NewTable[record](st, "catalog")
	entries := map[[2]string][]*Entry{}
	vectors := map[[3]string]*vectorRecord{}
	err := s.kept.Load(func(tenant, key string, r record) error {
		switch {
		case r.Connector != nil:
			if key != connectorKey(r.Connector.Name) {
				return fmt.Errorf("%w: connector record %q", ErrInvalid, key)
			}
			if s.tenants[tenant] == nil {
				s.tenants[tenant] = map[string]*connector{}
			}
			s.tenants[tenant][r.Connector.Name] = &connector{Connector: *r.Connector}
		case r.Entry != nil:
			if name, _, ok := splitKey(key, "e"); ok {
				e := *r.Entry
				entries[[2]string{tenant, name}] = append(entries[[2]string{tenant, name}], &e)
			}
		case r.Vector != nil:
			if name, path, ok := splitKey(key, "v"); ok {
				vectors[[3]string{tenant, name, path}] = r.Vector
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("catalog: load: %w", err)
	}
	for k, es := range entries {
		c := s.tenants[k[0]][k[1]]
		if c == nil {
			continue
		}
		for _, e := range es {
			in := indexed(*e)
			if v := vectors[[3]string{k[0], k[1], e.Path}]; v != nil && v.Generation == c.Generation {
				in.vec, in.model = v.decode()
			}
			c.entries = append(c.entries, in)
		}
		slices.SortFunc(c.entries, func(a, b *entry) int { return strings.Compare(a.Path, b.Path) })
	}
	return s, nil
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
	s.mu.Lock()
	defer s.mu.Unlock()
	have := s.tenants[tenant][c.Name]
	if have == nil && len(s.tenants[tenant]) >= MaxConnectors {
		return Connector{}, fmt.Errorf("%w: at most %d connectors", ErrInvalid, MaxConnectors)
	}
	next := Connector{Name: c.Name, System: c.System, Owner: c.Owner, Project: c.Project}
	b := s.kept.Batch()
	var entries []*entry
	if have != nil {
		next.Published, next.Withdrawn = have.Published, have.Withdrawn
		next.CrawledAt, next.Entries, next.Generation = have.CrawledAt, have.Entries, have.Generation
		entries = have.entries
		if have.System != c.System || have.Owner != c.Owner {
			for _, e := range have.entries {
				b.Delete(tenant, entryKey(c.Name, e.Path))
				b.Delete(tenant, vectorKey(c.Name, e.Path))
			}
			next.Published, next.CrawledAt, next.Entries, entries = nil, nil, 0, nil
			next.Generation++
		}
		if have.Project != c.Project {
			next.Published = nil // published to one audience is not published to another
		}
		if have.Connector == next {
			return next, nil
		}
	}
	b.Put(tenant, connectorKey(c.Name), record{Connector: &next})
	if err := b.Commit(); err != nil {
		return Connector{}, err
	}
	if s.tenants[tenant] == nil {
		s.tenants[tenant] = map[string]*connector{}
	}
	s.tenants[tenant][c.Name] = &connector{Connector: next, entries: entries}
	return next, nil
}

// Remove deletes a connector and its entries.
func (s *Store) Remove(tenant, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.tenants[tenant][name]
	if !ok {
		return fmt.Errorf("%w: connector %q", ErrNotFound, name)
	}
	b := s.kept.Batch()
	for _, e := range c.entries {
		b.Delete(tenant, entryKey(name, e.Path))
		b.Delete(tenant, vectorKey(name, e.Path))
	}
	b.Delete(tenant, connectorKey(name))
	if err := b.Commit(); err != nil {
		return err
	}
	delete(s.tenants[tenant], name)
	return nil
}

// Connector returns one connector.
func (s *Store) Connector(tenant, name string) (Connector, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.tenants[tenant][name]
	if !ok {
		return Connector{}, fmt.Errorf("%w: connector %q", ErrNotFound, name)
	}
	return c.Connector, nil
}

// Connectors returns a tenant's connectors by name.
func (s *Store) Connectors(tenant string) []Connector {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Connector, 0, len(s.tenants[tenant]))
	for _, c := range s.tenants[tenant] {
		out = append(out, c.Connector)
	}
	slices.SortFunc(out, func(a, b Connector) int { return strings.Compare(a.Name, b.Name) })
	return out
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

	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.tenants[tenant][name]
	if !ok {
		return Connector{}, fmt.Errorf("%w: connector %q", ErrNotFound, name)
	}
	updated := c.Connector
	updated.CrawledAt, updated.Entries = &at, len(next)
	updated.Generation++
	b := s.kept.Batch()
	for _, e := range c.entries {
		b.Delete(tenant, entryKey(name, e.Path))
		b.Delete(tenant, vectorKey(name, e.Path))
	}
	// An entry whose text did not change keeps its vector, under the new
	// generation, so a recrawl does not embed the catalog again.
	before := make(map[string]*entry, len(c.entries))
	for _, e := range c.entries {
		before[e.Path] = e
	}
	for _, e := range next {
		b.Put(tenant, entryKey(name, e.Path), record{Entry: &e.Entry})
		if old := before[e.Path]; old != nil && old.vec != nil && Text(old.Entry) == Text(e.Entry) {
			e.vec, e.model = old.vec, old.model
			b.Put(tenant, vectorKey(name, e.Path), record{Vector: &vectorRecord{Model: old.model, Generation: updated.Generation, Data: encode(old.vec)}})
		}
	}
	b.Put(tenant, connectorKey(name), record{Connector: &updated})
	if err := b.Commit(); err != nil {
		return Connector{}, err
	}
	c.Connector, c.entries = updated, next
	s.dense.nudge()
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
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.tenants[tenant][name]
	if !ok {
		return Connector{}, fmt.Errorf("%w: connector %q", ErrNotFound, name)
	}
	updated := c.Connector
	change(&updated)
	if updated == c.Connector {
		return updated, nil
	}
	if err := s.kept.Put(tenant, connectorKey(name), record{Connector: &updated}); err != nil {
		return Connector{}, err
	}
	c.Connector = updated
	return updated, nil
}

// List returns the entries of the named connectors, ordered by connector
// and path, at most limit of them (1 to MaxEntries). An empty selection
// lists nothing.
func (s *Store) List(tenant string, connectors []string, limit int) []Entry {
	if limit <= 0 || limit > MaxEntries {
		limit = MaxHits
	}
	names := slices.Clone(connectors)
	slices.Sort(names)
	names = slices.Compact(names)
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Entry{}
	for _, n := range names {
		c, ok := s.tenants[tenant][n]
		if !ok {
			continue
		}
		for _, e := range c.entries {
			if len(out) == limit {
				return out
			}
			out = append(out, e.Entry)
		}
	}
	return out
}

// tokens lowercases text and splits it into words of letters and digits,
// so "churn_monthly" is "churn" and "monthly".
func tokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
