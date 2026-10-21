package projects

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/urmzd/warp/pkg/state"
)

// namespace is where every tenant's projects, history and tombstones are
// kept, so a record and its history entry are one state.Write.
const namespace = "projects"

// Key prefixes within the namespace.
const (
	keyProject   = "p:"
	keyHistory   = "h:"
	keyTombstone = "x:"
)

// stored is a project as it is written: the record, and the fields the file
// owns.
type stored struct {
	Project
	Owns []string `json:"file_owns,omitempty"`
}

func (s stored) project() Project {
	p := s.Project
	p.FileOwns = s.Owns
	return p
}

// tenant is one tenant's records, as they are written. A write builds a new
// one from a copy, so a failed write leaves the old one as it was.
type tenant struct {
	projects map[string]Project
	// tombs is every deleted name, by its lower case.
	tombs map[string]Tombstone
	// history is the tenant's changes, oldest first.
	history []Change
	seq     int64
}

func newTenant() *tenant {
	return &tenant{projects: map[string]Project{}, tombs: map[string]Tombstone{}}
}

func (t *tenant) copy() *tenant {
	return &tenant{projects: maps.Clone(t.projects), tombs: maps.Clone(t.tombs), history: slices.Clone(t.history), seq: t.seq}
}

// stored reports whether the tenant has written anything.
func (t *tenant) stored() bool {
	return len(t.projects) > 0 || len(t.tombs) > 0 || len(t.history) > 0
}

// lookup returns the project whose name equals name ignoring case.
func (t *tenant) lookup(name string) (Project, bool) {
	for n, p := range t.projects {
		if n != Root && strings.EqualFold(n, name) {
			return p, true
		}
	}
	return Project{}, false
}

// Store keeps every tenant's tree. It is in memory, and built with Open it
// writes every change through a state.Store and reads them back when it is
// built again. It is one process's: it reads the store once and then trusts
// its memory.
type Store struct {
	// mu serializes writes, which check the tree and change it in one step.
	// Hold takes it to read, so a project cannot be deleted while a caller
	// files something under it.
	mu      sync.RWMutex
	tenants map[string]*tenant
	// trees is each tenant's current Tree, replaced on every write and read
	// without the lock.
	trees sync.Map
	caps  Caps
	kept  *state.Table[json.RawMessage]
}

// Option configures a Store.
type Option func(*Store)

// WithCaps sets the bounds of every tenant's tree. A zero field keeps the
// default.
func WithCaps(c Caps) Option {
	return func(s *Store) {
		d := DefaultCaps()
		s.caps = Caps{
			Projects: cmp.Or(c.Projects, d.Projects), Depth: cmp.Or(c.Depth, d.Depth),
			Children: cmp.Or(c.Children, d.Children), Company: cmp.Or(c.Company, d.Company),
		}
	}
}

// NewStore returns an empty store in memory.
func NewStore(opts ...Option) *Store {
	s := &Store{tenants: map[string]*tenant{}, caps: DefaultCaps()}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Open returns a store that keeps its records in st, holding what st already
// has. A nil st is NewStore. It refuses records that do not make a tree: a
// project whose parent is missing, or a cycle.
func Open(st state.Store, opts ...Option) (*Store, error) {
	s := NewStore(opts...)
	s.kept = state.NewTable[json.RawMessage](st, namespace)
	err := s.kept.Load(func(tenantName, key string, raw json.RawMessage) error {
		t, ok := s.tenants[tenantName]
		if !ok {
			t = newTenant()
			s.tenants[tenantName] = t
		}
		return t.load(key, raw)
	})
	if err != nil {
		return nil, fmt.Errorf("projects: load: %w", err)
	}
	for name, t := range s.tenants {
		slices.SortFunc(t.history, func(a, b Change) int { return cmp.Compare(a.Seq, b.Seq) })
		if err := t.check(); err != nil {
			return nil, fmt.Errorf("projects: load %q: %w", name, err)
		}
		s.trees.Store(name, build(t.projects))
	}
	return s, nil
}

func (t *tenant) load(key string, raw json.RawMessage) error {
	switch {
	case strings.HasPrefix(key, keyProject):
		var r stored
		if err := json.Unmarshal(raw, &r); err != nil {
			return fmt.Errorf("project %q: %w", key, err)
		}
		p := r.project()
		if p.Name != strings.TrimPrefix(key, keyProject) {
			return fmt.Errorf("%w: the project %q is kept under %q", ErrInvalid, p.Name, key)
		}
		t.projects[p.Name] = p
	case strings.HasPrefix(key, keyHistory):
		var c Change
		if err := json.Unmarshal(raw, &c); err != nil {
			return fmt.Errorf("change %q: %w", key, err)
		}
		n, err := strconv.ParseInt(strings.TrimPrefix(key, keyHistory), 10, 64)
		if err != nil || n != c.Seq {
			return fmt.Errorf("%w: a change is kept under %q", ErrInvalid, key)
		}
		t.history = append(t.history, c)
		t.seq = max(t.seq, c.Seq)
	case strings.HasPrefix(key, keyTombstone):
		var x Tombstone
		if err := json.Unmarshal(raw, &x); err != nil {
			return fmt.Errorf("tombstone %q: %w", key, err)
		}
		if strings.ToLower(x.Name) != strings.TrimPrefix(key, keyTombstone) {
			return fmt.Errorf("%w: the tombstone of %q is kept under %q", ErrInvalid, x.Name, key)
		}
		t.tombs[strings.ToLower(x.Name)] = x
	default:
		return fmt.Errorf("%w: nothing is kept under %q", ErrInvalid, key)
	}
	return nil
}

// check holds loaded records to what a tree must be: valid records, unique
// names, every parent present, and no cycle. Caps are not checked, so a
// tighter cap never stops a tree from loading.
func (t *tenant) check() error {
	seen := map[string]string{}
	for name, p := range t.projects {
		if err := p.validate(); err != nil {
			return fmt.Errorf("%q: %w", name, err)
		}
		if other, dup := seen[strings.ToLower(name)]; dup {
			return fmt.Errorf("%w: %q and %q are one name", ErrInvalid, other, name)
		}
		seen[strings.ToLower(name)] = name
	}
	for name, p := range t.projects {
		if name == Root {
			continue
		}
		visited := map[string]bool{name: true}
		for at := p; at.Parent != Root; {
			parent, ok := t.projects[at.Parent]
			if !ok {
				return fmt.Errorf("%w: the parent %q of %q is missing", ErrInvalid, at.Parent, at.Name)
			}
			if visited[parent.Name] {
				return fmt.Errorf("%w: %q is its own ancestor", ErrInvalid, parent.Name)
			}
			visited[parent.Name] = true
			at = parent
		}
	}
	return nil
}

// Tree returns a tenant's tree. A tenant that has written nothing has the
// root alone.
func (s *Store) Tree(tenantName string) *Tree {
	if v, ok := s.trees.Load(tenantName); ok {
		return v.(*Tree)
	}
	return bare
}

// bare is the tree of a tenant that has written nothing. A Tree is never
// changed, so one serves them all.
var bare = build(nil)

// Get returns a project by its exact name. The root is "".
func (s *Store) Get(tenantName, name string) (Project, error) {
	p, ok := s.Tree(tenantName).Get(name)
	if !ok {
		return Project{}, fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	return p, nil
}

// List returns a tenant's projects, the root aside, in tree order.
func (s *Store) List(tenantName string) []Project { return s.Tree(tenantName).Projects() }

// Stored reports whether a tenant has written anything: a project, its root,
// a change or a tombstone.
func (s *Store) Stored(tenantName string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tenants[tenantName]
	return ok && t.stored()
}

// Hold runs fn while the project exists, and keeps it from being deleted until
// fn returns. A caller filing something under a project (a schedule, an alert
// rule, a policy) does it in fn, so a delete that checks for such things
// cannot miss it. fn may read Tree and Get, which take no lock, and must call
// nothing else on this store: a write waiting for the lock would wait for fn.
func (s *Store) Hold(tenantName, name string, fn func() error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.Tree(tenantName).Has(name) {
		return fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	return fn()
}

// write is one change to a tenant: the batch to write and the tenant as it
// is after it.
type write struct {
	s     *Store
	name  string
	t     *tenant
	batch *state.Batch[json.RawMessage]
	err   error
}

// begin starts a change to a tenant. The caller holds the lock.
func (s *Store) begin(tenantName string) *write {
	t, ok := s.tenants[tenantName]
	if !ok {
		t = newTenant()
	}
	return &write{s: s, name: tenantName, t: t.copy(), batch: s.kept.Batch()}
}

func (w *write) put(key string, v any) {
	if w.err != nil {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil {
		w.err = fmt.Errorf("projects: %q: %w", key, err)
		return
	}
	w.batch.Put(w.name, key, raw)
}

func (w *write) delete(key string) { w.batch.Delete(w.name, key) }

// project puts a project's record.
func (w *write) project(p Project) {
	w.t.projects[p.Name] = p
	w.put(keyProject+p.Name, stored{Project: p, Owns: p.FileOwns})
}

// record appends a change, dropping the oldest past MaxHistory.
func (w *write) record(c Change) {
	w.t.seq++
	c.Seq = w.t.seq
	w.t.history = append(w.t.history, c)
	w.put(keyHistory+state.Seq(c.Seq), c)
	for len(w.t.history) > MaxHistory {
		w.delete(keyHistory + state.Seq(w.t.history[0].Seq))
		w.t.history = w.t.history[1:]
	}
}

// bury leaves a tombstone for a deleted name, dropping the oldest past
// MaxTombstones.
func (w *write) bury(x Tombstone) {
	low := strings.ToLower(x.Name)
	w.t.tombs[low] = x
	w.put(keyTombstone+low, x)
	for len(w.t.tombs) > MaxTombstones {
		var oldest string
		var at time.Time
		for k, t := range w.t.tombs {
			if oldest == "" || t.At.Before(at) || (t.At.Equal(at) && k < oldest) {
				oldest, at = k, t.At
			}
		}
		delete(w.t.tombs, oldest)
		w.delete(keyTombstone + oldest)
	}
}

// root writes the root the first time a tenant writes anything.
func (w *write) root(by string, at time.Time) {
	if _, ok := w.t.projects[Root]; ok {
		return
	}
	r := virtualRoot()
	r.Version, r.CreatedBy, r.CreatedAt, r.UpdatedBy, r.UpdatedAt = 1, by, at, by, at
	w.project(r)
	w.record(Change{At: at, By: by, Project: Root, What: Created, After: ptr(r)})
}

// commit writes the change and, only once it is written, makes it the
// tenant's.
func (w *write) commit() error {
	if w.err != nil {
		return w.err
	}
	if err := w.batch.Commit(); err != nil {
		return fmt.Errorf("projects: write: %w", err)
	}
	w.s.tenants[w.name] = w.t
	w.s.trees.Store(w.name, build(w.t.projects))
	return nil
}

func ptr(p Project) *Project {
	c := p.clone()
	c.FileOwns = nil
	return &c
}

// place checks where a new project would sit: its parent, depth, kind and the
// counts the caps bound. The caller holds the lock.
func (s *Store) place(t *tenant, p Project, w Writer) error {
	tree := build(t.projects)
	parent, ok := tree.Get(p.Parent)
	if !ok {
		return fmt.Errorf("%w: there is no project %q to put it under", ErrInvalid, p.Parent)
	}
	if tree.Len() >= s.caps.Projects {
		return fmt.Errorf("%w: a workspace has at most %d projects", ErrLimit, s.caps.Projects)
	}
	switch {
	case tree.Depth(p.Parent)+1 > s.caps.Depth:
		return fmt.Errorf("%w: a project sits at most %d levels below the workspace", ErrInvalid, s.caps.Depth)
	case len(tree.Children(p.Parent)) >= s.caps.Children:
		return fmt.Errorf("%w: a project has at most %d sub-projects", ErrInvalid, s.caps.Children)
	case p.Kind == Company && parent.Kind != Company:
		return fmt.Errorf("%w: a company project sits under the workspace or another company project", ErrInvalid)
	case accountLike(p.Kind) && accountLike(parent.Kind):
		return fmt.Errorf("%w: an account cannot sit under another account", ErrInvalid)
	case p.Kind == Company && !w.Govern:
		return fmt.Errorf("%w: company projects need projects:govern", ErrForbidden)
	}
	if p.Kind == Company {
		n := 0
		for _, q := range tree.Projects() {
			if q.Kind == Company {
				n++
			}
		}
		if n >= s.caps.Company {
			return fmt.Errorf("%w: a workspace has at most %d company projects", ErrLimit, s.caps.Company)
		}
	}
	return nil
}

// Create adds a project. Its name must be free ignoring case, deleted names
// included unless the writer holds projects:govern, and its place must keep
// the tree within its caps and kind rules. The checks and the insert are one
// step, so two creates cannot both take one name or the last place.
func (s *Store) Create(tenantName string, p Project, w Writer, at time.Time) (Project, error) {
	p = p.clone()
	p.Kind = cmp.Or(p.Kind, Team)
	p.Managed, p.FileOwns = "", nil
	if p.IsRoot() {
		return Project{}, fmt.Errorf("%w: the workspace is the root, and is not created", ErrInvalid)
	}
	if err := p.validate(); err != nil {
		return Project{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.create(tenantName, p, w, at)
}

// create is Create, and Upsert's way in for a new project. The caller holds
// the lock and has validated p.
func (s *Store) create(tenantName string, p Project, w Writer, at time.Time) (Project, error) {
	tx := s.begin(tenantName)
	if have, ok := tx.t.lookup(p.Name); ok {
		return Project{}, fmt.Errorf("%w: %q", ErrExists, have.Name)
	}
	note := ""
	tomb, deleted := tx.t.tombs[strings.ToLower(p.Name)]
	if deleted {
		if !w.Govern {
			return Project{}, fmt.Errorf("%w: %q was deleted; reusing its name adopts its old conversations, which needs projects:govern", ErrForbidden, tomb.Name)
		}
		note = AdoptsNote
	}
	if err := s.place(tx.t, p, w); err != nil {
		return Project{}, err
	}
	p.Version, p.CreatedBy, p.CreatedAt, p.UpdatedBy, p.UpdatedAt = 1, w.By, at, w.By, at
	tx.root(w.By, at)
	tx.project(p)
	if deleted {
		delete(tx.t.tombs, strings.ToLower(p.Name))
		tx.delete(keyTombstone + strings.ToLower(p.Name))
	}
	tx.record(Change{At: at, By: w.By, Project: p.Name, What: Created, Note: note, After: ptr(p)})
	if err := tx.commit(); err != nil {
		return Project{}, err
	}
	return p.clone(), nil
}

// current is a project as a write starts from: the root reads as the virtual
// root until it is written.
func current(t *tenant, name string) (Project, bool) {
	if p, ok := t.projects[name]; ok {
		return p, true
	}
	if name == Root {
		return virtualRoot(), true
	}
	return Project{}, false
}

// Update changes a project through the API. fn edits a copy; the name, kind,
// parent, management and record fields cannot change, a field the file owns
// cannot change (ErrManaged), and version must be the current one
// (ErrVersion). The root and company projects need projects:govern. A change
// that changes nothing writes nothing and keeps the version.
func (s *Store) Update(tenantName, name string, version int, w Writer, at time.Time, fn func(*Project) error) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := s.begin(tenantName)
	cur, ok := current(tx.t, name)
	if !ok {
		return Project{}, fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	if (cur.IsRoot() || cur.Kind == Company) && !w.Govern {
		return Project{}, fmt.Errorf("%w: the workspace and company projects are changed with projects:govern", ErrForbidden)
	}
	if version != cur.Version {
		return Project{}, fmt.Errorf("%w: %q is at version %d, not %d", ErrVersion, display(name), cur.Version, version)
	}
	next := cur.clone()
	if err := fn(&next); err != nil {
		return Project{}, err
	}
	switch {
	case next.Name != cur.Name || next.Kind != cur.Kind || next.Parent != cur.Parent:
		return Project{}, fmt.Errorf("%w: a project's name, kind and parent are fixed when it is created", ErrInvalid)
	case next.Managed != cur.Managed || !slices.Equal(next.FileOwns, cur.FileOwns):
		return Project{}, fmt.Errorf("%w: only the platform file manages a project", ErrInvalid)
	}
	next.Version, next.CreatedBy, next.CreatedAt, next.UpdatedBy, next.UpdatedAt = cur.Version, cur.CreatedBy, cur.CreatedAt, cur.UpdatedBy, cur.UpdatedAt
	if err := next.validate(); err != nil {
		return Project{}, err
	}
	for _, f := range cur.FileOwns {
		if !sameField(cur, next, f) {
			return Project{}, fmt.Errorf("%w: the platform file sets %s on %q", ErrManaged, f, display(name))
		}
	}
	if same(cur, next) {
		return cur.clone(), nil
	}
	return s.replace(tx, cur, next, w.By, at, Updated)
}

// replace writes next over cur as a new version.
func (s *Store) replace(tx *write, cur, next Project, by string, at time.Time, what string) (Project, error) {
	next.Version, next.UpdatedBy, next.UpdatedAt = cur.Version+1, by, at
	if next.CreatedAt.IsZero() {
		next.CreatedBy, next.CreatedAt = by, at
	}
	tx.project(next)
	tx.record(Change{At: at, By: by, Project: next.Name, What: what, Before: ptr(cur), After: ptr(next)})
	if err := tx.commit(); err != nil {
		return Project{}, err
	}
	return next.clone(), nil
}

func display(name string) string {
	if name == Root {
		return "the workspace"
	}
	return name
}

// same reports whether two versions of a project hold the same fields.
func same(a, b Project) bool {
	for f := range validField {
		if !sameField(a, b, f) {
			return false
		}
	}
	return a.Managed == b.Managed && slices.Equal(a.FileOwns, b.FileOwns)
}

func sameField(a, b Project, field string) bool {
	switch field {
	case FieldTitle:
		return a.Title == b.Title
	case FieldDescription:
		return a.Description == b.Description
	case FieldKind:
		return a.Kind == b.Kind
	case FieldParent:
		return a.Parent == b.Parent
	case FieldAccount:
		return (a.Account == nil) == (b.Account == nil) && (a.Account == nil || *a.Account == *b.Account)
	case FieldInstructions:
		return a.Instructions == b.Instructions
	}
	return false
}

// Delete removes a project and leaves a tombstone, so its name is reused only
// with projects:govern. It is refused while the project has children, or
// while inUse, called under the lock, names anything that points at it. The
// root cannot be deleted, a file-managed project cannot be deleted here, and
// a company project needs projects:govern.
func (s *Store) Delete(tenantName, name string, w Writer, at time.Time, inUse func(Project) []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := s.begin(tenantName)
	if name == Root {
		return fmt.Errorf("%w: the workspace cannot be deleted", ErrInvalid)
	}
	cur, ok := tx.t.projects[name]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	if cur.Managed == ManagedFile {
		return fmt.Errorf("%w: %q is declared in the platform file; remove it there", ErrManaged, name)
	}
	if cur.Kind == Company && !w.Govern {
		return fmt.Errorf("%w: company projects are deleted with projects:govern", ErrForbidden)
	}
	var users []string
	for _, c := range build(tx.t.projects).Children(name) {
		users = append(users, "the sub-project "+c.Name)
	}
	if inUse != nil {
		users = append(users, inUse(cur.clone())...)
	}
	if len(users) > 0 {
		return fmt.Errorf("%w: %q is still used by %s", ErrInUse, name, strings.Join(users, ", "))
	}
	delete(tx.t.projects, name)
	tx.delete(keyProject + name)
	tx.bury(Tombstone{Name: name, By: w.By, At: at})
	tx.record(Change{At: at, By: w.By, Project: name, What: Deleted, Before: ptr(cur)})
	return tx.commit()
}

// Tombstone returns the record of a deleted name, ignoring case.
func (s *Store) Tombstone(tenantName, name string) (Tombstone, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tenants[tenantName]
	if !ok {
		return Tombstone{}, false
	}
	x, ok := t.tombs[strings.ToLower(name)]
	return x, ok
}

// Upsert applies the platform file's declaration of a project: it creates the
// project, or takes over the one by that name, with the fields owns lists set
// from p and the rest left as they are. The file is the server's own
// configuration, so it writes as projects:govern. A declaration that changes
// nothing writes nothing and keeps the version. Parents come first: a project
// whose parent is not there yet is refused.
func (s *Store) Upsert(tenantName string, p Project, owns []string, by string, at time.Time) (Project, error) {
	p = p.clone()
	owns = slices.Clone(owns)
	slices.Sort(owns)
	owns = slices.Compact(owns)
	for _, f := range owns {
		if !validField[f] {
			return Project{}, fmt.Errorf("%w: the file cannot own %q", ErrInvalid, f)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := s.begin(tenantName)
	cur, ok := current(tx.t, p.Name)
	if !ok {
		if have, taken := tx.t.lookup(p.Name); taken {
			return Project{}, fmt.Errorf("%w: %q", ErrExists, have.Name)
		}
		p.Kind = cmp.Or(p.Kind, Team)
		p.Managed, p.FileOwns = ManagedFile, owns
		if err := p.validate(); err != nil {
			return Project{}, err
		}
		return s.create(tenantName, p, Writer{By: by, Govern: true}, at)
	}
	next := cur.clone()
	for _, f := range owns {
		switch f {
		case FieldTitle:
			next.Title = p.Title
		case FieldDescription:
			next.Description = p.Description
		case FieldKind:
			next.Kind = cmp.Or(p.Kind, Team)
		case FieldParent:
			next.Parent = p.Parent
		case FieldAccount:
			next.Account = p.Account
		case FieldInstructions:
			next.Instructions = p.Instructions
		}
	}
	if next.IsRoot() {
		next.Kind = Company
	}
	if next.Kind != cur.Kind || next.Parent != cur.Parent {
		return Project{}, fmt.Errorf("%w: %q is a %s under %s; a project's kind and parent are fixed when it is created", ErrInvalid, display(cur.Name), cur.Kind, display(cur.Parent))
	}
	next.Managed, next.FileOwns = ManagedFile, owns
	if err := next.validate(); err != nil {
		return Project{}, err
	}
	if same(cur, next) {
		return cur.clone(), nil
	}
	what := Updated
	if cur.Managed != ManagedFile {
		what = Managed
	}
	return s.replace(tx, cur, next, by, at, what)
}

// Release lets the platform file go of a project that left it. The project
// stays, unmanaged, and every field is the API's again.
func (s *Store) Release(tenantName, name, by string, at time.Time) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := s.begin(tenantName)
	cur, ok := current(tx.t, name)
	if !ok {
		return Project{}, fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	if cur.Managed == "" && len(cur.FileOwns) == 0 {
		return cur.clone(), nil
	}
	next := cur.clone()
	next.Managed, next.FileOwns = "", nil
	return s.replace(tx, cur, next, by, at, Managed)
}

// Import writes a tenant's first records in one write: the root, and every
// project given as a team at the top level, each with its history entry. It
// is how the projects a workspace kept in its settings move here, and it is
// refused (ErrExists) once the tenant has written anything, so running it
// again changes nothing.
func (s *Store) Import(tenantName string, ps []Project, by string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := s.begin(tenantName)
	if tx.t.stored() {
		return fmt.Errorf("%w: %q already has projects", ErrExists, tenantName)
	}
	if len(ps) > s.caps.Projects {
		return fmt.Errorf("%w: a workspace has at most %d projects", ErrLimit, s.caps.Projects)
	}
	tx.root(by, at)
	for _, p := range ps {
		p = Project{Name: p.Name, Description: legacyText(p.Description, MaxDescription), Kind: Team}
		if err := p.validate(); err != nil {
			return fmt.Errorf("%q: %w", p.Name, err)
		}
		if have, ok := tx.t.lookup(p.Name); ok {
			return fmt.Errorf("%w: %q and %q are one name", ErrExists, have.Name, p.Name)
		}
		p.Version, p.CreatedBy, p.CreatedAt, p.UpdatedBy, p.UpdatedAt = 1, by, at, by, at
		tx.project(p)
		tx.record(Change{At: at, By: by, Project: p.Name, What: Created, After: ptr(p)})
	}
	return tx.commit()
}

// History returns a project's changes, newest first: at most limit (at most
// MaxPage, DefaultPage when zero), older than the change before when it is
// not zero. next is what to pass as before for the page after this one, or 0
// when there is none. A deleted project's changes stay as long as the
// tenant's history keeps them.
func (s *Store) History(tenantName, name string, limit int, before int64) (changes []Change, next int64) {
	if limit <= 0 {
		limit = DefaultPage
	}
	limit = min(limit, MaxPage)
	s.mu.RLock()
	defer s.mu.RUnlock()
	changes = []Change{}
	t, ok := s.tenants[tenantName]
	if !ok {
		return changes, 0
	}
	for i := len(t.history) - 1; i >= 0; i-- {
		c := t.history[i]
		if c.Project != name || (before > 0 && c.Seq >= before) {
			continue
		}
		if len(changes) == limit {
			return changes, changes[len(changes)-1].Seq
		}
		changes = append(changes, c)
	}
	return changes, 0
}

// legacyText is text kept before it was bounded: control characters other
// than line breaks and tabs dropped, and clipped to limit characters.
func legacyText(s string, limit int) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' {
			return -1
		}
		return r
	}, s)
	if r := []rune(s); len(r) > limit {
		s = string(r[:limit])
	}
	return s
}
