// Package library keeps bounded immutable skill revisions. Published revisions
// are resolved by the platform into its existing pinned skill composition.
//
// It reads and writes the database per request (pkg/state, per request): it
// holds nothing in memory, so any number of processes on one database agree,
// and it does not need pkg/state's single-writer lock.
package library

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
)

// MaxSkills is how many skills one scope holds. A person's own skills, kept
// in one store for every assistant they use, number in the hundreds, and a
// scope must take them. Listing a scope reads at most this many.
const MaxSkills = 256
const MaxVersions = 32
const MaxScopes = 1000

var (
	ErrInvalid  = errors.New("library: invalid")
	ErrNotFound = errors.New("library: not found")
	ErrConflict = errors.New("library: version conflict")
)

type Scope struct {
	Kind    string `json:"kind"`
	Project string `json:"project,omitempty"`
	Subject string `json:"subject,omitempty"`
}

func (s Scope) Key() string {
	value := s.Project
	if s.Kind == "personal" {
		value = s.Subject
	}
	return s.Kind + ":" + base64.RawURLEncoding.EncodeToString([]byte(value))
}
func (s Scope) Valid() bool {
	switch s.Kind {
	case "workspace":
		return s.Project == "" && s.Subject == ""
	case "project":
		return s.Project != "" && len(s.Project) <= 512 && s.Subject == ""
	case "personal":
		return s.Subject != "" && len(s.Subject) <= 512 && s.Project == ""
	}
	return false
}

type Definition struct {
	projects.Skill
	Inputs  string   `json:"inputs,omitempty"`
	Outputs string   `json:"outputs,omitempty"`
	Tools   []string `json:"tools"`
	Files   []File   `json:"files,omitempty"`
}

func (d Definition) Valid() bool {
	if !projects.ValidSkill(d.Name) || !d.validPackage() || strings.TrimSpace(d.Description) == "" || strings.TrimSpace(d.Instructions) == "" || utf8.RuneCountInString(d.Description) > projects.MaxSkillDescription || len(d.ArgumentHint) > projects.MaxArgumentHint || len(d.Inputs) > 4000 || len(d.Outputs) > 4000 || len(d.Tools) > 32 {
		return false
	}
	for _, value := range []string{d.Description, d.Instructions, d.ArgumentHint, d.Inputs, d.Outputs} {
		if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return false
		}
	}
	seen := map[string]bool{}
	for _, tool := range d.Tools {
		if tool == "" || len(tool) > 128 || strings.ContainsAny(tool, "\x00\r\n") || seen[tool] {
			return false
		}
		seen[tool] = true
	}
	return true
}

type Provenance struct {
	Scope   Scope  `json:"scope"`
	Name    string `json:"name"`
	Version int    `json:"version"`
}

// ViaImport marks a revision drafted from a skill folder on someone's
// machine (humagen import skills), as distinct from one saved in the editor.
const ViaImport = "import"

type Revision struct {
	OriginRun string `json:"origin_run,omitempty"`
	ID        string `json:"id"`
	// Via is how the revision was written when not in the editor: ViaImport,
	// or empty. An import reads it to tell a revision it wrote, which a
	// changed folder may follow, from one a person edited, which it may not
	// write over unasked.
	Via string `json:"via,omitempty"`
	Definition
	Scope       Scope       `json:"scope"`
	Version     int         `json:"version"`
	By          string      `json:"by"`
	At          time.Time   `json:"at"`
	PublishedAt time.Time   `json:"published_at,omitzero"`
	PublishedBy string      `json:"published_by,omitempty"`
	Tests       []string    `json:"tests"`
	From        *Provenance `json:"from,omitempty"`
}

func clone(r Revision) Revision {
	r.Tools = slices.Clone(r.Tools)
	r.Files = cloneFiles(r.Files)
	r.Tests = slices.Clone(r.Tests)
	if r.From != nil {
		from := *r.From
		r.From = &from
	}
	return r
}

// skillsNamespace is where skill revisions are kept. The tenant is the shard
// key; within it:
//
//	<scope>/<name>   every revision of one skill, oldest first
//	#id:<id>         the key of the skill with that id
//	#scope:<scope>   how many skills a scope holds
//	#scopes          how many scopes the tenant holds
//
// A scope's key starts with its kind, so no skill's key starts with "#".
const skillsNamespace = "library.skills"

func skillKey(scope Scope, name string) string { return scope.Key() + "/" + name }
func idKey(id string) string                   { return "#id:" + id }
func scopeCountKey(scope Scope) string         { return "#scope:" + scope.Key() }

const scopesKey = "#scopes"

// Store keeps skill revisions. It reads and writes the database per request.
type Store struct {
	kv kv
	// raw reads skill records undecoded, so a listing decodes each on its
	// own (scan).
	raw *state.Table[json.RawMessage]
}

// Open returns a store on st, which may be shared with other processes. A nil
// st keeps skills in this process's memory.
//
// Skills kept by a release before the index and counter records are counted
// and indexed the first time any process opens the store; a process that
// opens it at the same time writes the same records.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	s := &Store{kv: kv{st: st, namespace: skillsNamespace}, raw: state.NewTable[json.RawMessage](st, skillsNamespace)}
	if err := s.kv.convertOnce(s.convert); err != nil {
		return nil, fmt.Errorf("library: convert skills: %w", err)
	}
	return s, nil
}

// checkRows validates a skill's revisions as read, giving each its id.
func checkRows(key string, revisions []Revision) ([]Revision, error) {
	if len(revisions) == 0 || len(revisions) > MaxVersions {
		return nil, ErrInvalid
	}
	first := revisions[0]
	if key != skillKey(first.Scope, first.Name) || !first.Scope.Valid() {
		return nil, ErrInvalid
	}
	for i, r := range revisions {
		if r.ID == "" {
			revisions[i].ID = ID(r.Scope, r.Name)
		} else if r.ID != ID(r.Scope, r.Name) {
			return nil, ErrInvalid
		}
		if r.Via != "" && r.Via != ViaImport {
			return nil, ErrInvalid
		}
		if !r.Valid() || r.Scope != first.Scope || r.Name != first.Name || r.Version != i+1 || r.By == "" || r.At.IsZero() || len(r.Tests) > 10 {
			return nil, ErrInvalid
		}
	}
	return revisions, nil
}

func (s *Store) convert(recs []state.Record) error {
	var list []state.Op
	tenant := ""
	scopes := map[string]int{}
	flush := func() {
		if tenant == "" {
			return
		}
		o := &ops{}
		for key, n := range scopes {
			o.put(tenant, "#scope:"+key, n, 0)
		}
		o.put(tenant, scopesKey, len(scopes), 0)
		list = append(list, o.list...)
		scopes = map[string]int{}
	}
	for _, r := range recs {
		if r.Tenant != tenant {
			flush()
			tenant = r.Tenant
		}
		if len(r.Key) > 0 && r.Key[0] == '#' {
			continue
		}
		var revisions []Revision
		if err := json.Unmarshal(r.Value, &revisions); err != nil {
			return fmt.Errorf("%q/%q: %w", r.Tenant, r.Key, err)
		}
		revisions, err := checkRows(r.Key, revisions)
		if err != nil {
			return fmt.Errorf("%q/%q: %w", r.Tenant, r.Key, err)
		}
		first := revisions[0]
		scopes[first.Scope.Key()]++
		o := &ops{}
		o.put(r.Tenant, idKey(first.ID), r.Key, 0)
		list = append(list, o.list...)
	}
	flush()
	return s.kv.createAll(list)
}

// read returns a skill's revisions and their version, none when it has no
// record.
func (s *Store) read(tenant string, scope Scope, name string) ([]Revision, int64, error) {
	key := skillKey(scope, name)
	var rows []Revision
	ver, err := s.kv.lookupMaybe(tenant, key, &rows)
	if err != nil || ver == 0 {
		return nil, 0, err
	}
	rows, err = checkRows(key, rows)
	if err != nil {
		return nil, 0, fmt.Errorf("library: %q/%q: %w", tenant, key, err)
	}
	return rows, ver, nil
}

// fullSkill is the way on from a skill that holds as much as it may.
const fullSkill = "a skill's revisions are never removed, so save this one as another skill, under another name or in another scope"

// Draft appends an immutable revision; expected is the latest version read.
// Provenance is validated by the platform before entering this store.
func (s *Store) Draft(tenant, by string, scope Scope, d Definition, expected int, from *Provenance, at time.Time, trustedOrigin ...string) (Revision, error) {
	return s.draft(tenant, by, scope, d, expected, from, at, "", trustedOrigin)
}

// Import is Draft for a revision read from a skill folder: the same
// immutable, unpublished revision, marked ViaImport. It publishes nothing
// and changes no revision already there.
func (s *Store) Import(tenant, by string, scope Scope, d Definition, expected int, at time.Time) (Revision, error) {
	return s.draft(tenant, by, scope, d, expected, nil, at, ViaImport, nil)
}

func (s *Store) draft(tenant, by string, scope Scope, d Definition, expected int, from *Provenance, at time.Time, via string, trustedOrigin []string) (Revision, error) {
	var err error
	if d, err = d.Normalize(); err != nil {
		return Revision{}, err
	}
	if tenant == "" || by == "" || !scope.Valid() || !d.Valid() || at.IsZero() || len(trustedOrigin) > 1 || len(trustedOrigin) == 1 && len(trustedOrigin[0]) > 128 {
		return Revision{}, ErrInvalid
	}
	if d.Tools == nil {
		d.Tools = []string{}
	}
	var out Revision
	err = state.Retry(func() error {
		previous, ver, err := s.read(tenant, scope, d.Name)
		if err != nil {
			return err
		}
		if len(previous) != expected {
			return ErrConflict
		}
		if len(previous) >= MaxVersions {
			return fmt.Errorf("%w: %s holds as many revisions as a skill may (%d); %s", ErrInvalid, d.Name, MaxVersions, fullSkill)
		}
		held := PackageBytes(d.Files)
		for _, r := range previous {
			held += PackageBytes(r.Files)
		}
		if held > MaxSkillBytes {
			return fmt.Errorf("%w: the revisions of %s would hold more than %d bytes of files together; %s", ErrInvalid, d.Name, MaxSkillBytes, fullSkill)
		}
		r := clone(Revision{OriginRun: outcomeOrigin(previous, from, trustedOrigin), ID: ID(scope, d.Name), Via: via, Definition: d, Scope: scope, Version: len(previous) + 1, By: by, At: at, Tests: []string{}, From: from})
		next := append(slices.Clone(previous), r)
		o := &ops{}
		o.put(tenant, skillKey(scope, d.Name), next, ver)
		if ver == 0 {
			// A new skill is counted in its scope, and a new scope in the
			// tenant, in the same write.
			var count int
			cv, err := s.kv.lookupMaybe(tenant, scopeCountKey(scope), &count)
			if err != nil {
				return err
			}
			if count >= MaxSkills {
				return ErrInvalid
			}
			if cv == 0 {
				var scopes int
				sv, err := s.kv.lookupMaybe(tenant, scopesKey, &scopes)
				if err != nil {
					return err
				}
				if scopes >= MaxScopes {
					return ErrInvalid
				}
				o.put(tenant, scopesKey, scopes+1, sv)
			}
			o.put(tenant, scopeCountKey(scope), count+1, cv)
			o.set(tenant, idKey(r.ID), skillKey(scope, d.Name))
		}
		if err := o.commit(s.kv); err != nil {
			return err
		}
		out = r
		return nil
	})
	if err != nil {
		return Revision{}, err
	}
	return clone(out), nil
}

// change rewrites one revision of a skill by compare-and-set. fn returns
// state.ErrUnchanged to write nothing.
func (s *Store) change(tenant string, scope Scope, name string, version int, fn func(r *Revision) error) (Revision, error) {
	var out Revision
	err := state.Retry(func() error {
		rows, ver, err := s.read(tenant, scope, name)
		if err != nil {
			return err
		}
		if version < 1 || version > len(rows) {
			return ErrNotFound
		}
		r := clone(rows[version-1])
		if err := fn(&r); errors.Is(err, state.ErrUnchanged) {
			out = r
			return nil
		} else if err != nil {
			return err
		}
		next := slices.Clone(rows)
		next[version-1] = r
		o := &ops{}
		o.put(tenant, skillKey(scope, name), next, ver)
		if err := o.commit(s.kv); err != nil {
			return err
		}
		out = r
		return nil
	})
	return out, err
}

func (s *Store) Get(tenant string, scope Scope, name string, version int) (Revision, error) {
	rows, _, err := s.read(tenant, scope, name)
	if err != nil {
		return Revision{}, err
	}
	if version == 0 {
		version = len(rows)
	}
	if version < 1 || version > len(rows) {
		return Revision{}, ErrNotFound
	}
	return clone(rows[version-1]), nil
}

// scan reads every skill record of a scope in one bounded read and gives fn
// the revisions of each this release can read. A record it cannot decode or
// validate, such as one a later release wrote past this one's bounds, fails
// only itself: it is left out, and its name is among the unreadable this
// returns, sorted. Every read of that one skill (read, Find) still refuses
// it, so nothing of it is used.
func (s *Store) scan(tenant string, scope Scope, fn func([]Revision)) ([]string, error) {
	prefix := scope.Key() + "/"
	items, err := s.raw.All(tenant, prefix, MaxSkills)
	if err != nil {
		return nil, err
	}
	unreadable := []string{}
	for _, it := range items {
		var rows []Revision
		if json.Unmarshal(it.Value, &rows) != nil {
			unreadable = append(unreadable, strings.TrimPrefix(it.Key, prefix))
			continue
		}
		rows, err := checkRows(it.Key, rows)
		if err != nil {
			unreadable = append(unreadable, strings.TrimPrefix(it.Key, prefix))
			continue
		}
		fn(rows)
	}
	slices.Sort(unreadable)
	return unreadable, nil
}

// List returns the latest revision of every skill in a scope, by name, and
// the names of the skills there this release cannot read (scan).
func (s *Store) List(tenant string, scope Scope) ([]Revision, []string, error) {
	out := []Revision{}
	unreadable, err := s.scan(tenant, scope, func(rows []Revision) {
		out = append(out, clone(rows[len(rows)-1]))
	})
	if err != nil {
		return nil, nil, err
	}
	slices.SortFunc(out, func(a, b Revision) int { return strings.Compare(a.Name, b.Name) })
	return out, unreadable, nil
}

// ListPublished returns the latest published revision of every skill in a
// scope that has one, by name, in one bounded read, and the names of the
// skills there this release cannot read (scan).
func (s *Store) ListPublished(tenant string, scope Scope) ([]Revision, []string, error) {
	out := []Revision{}
	unreadable, err := s.scan(tenant, scope, func(rows []Revision) {
		for i := len(rows) - 1; i >= 0; i-- {
			if !rows[i].PublishedAt.IsZero() {
				out = append(out, clone(rows[i]))
				break
			}
		}
	})
	if err != nil {
		return nil, nil, err
	}
	slices.SortFunc(out, func(a, b Revision) int { return strings.Compare(a.Name, b.Name) })
	return out, unreadable, nil
}

func (s *Store) History(tenant string, scope Scope, name string) ([]Revision, error) {
	rows, _, err := s.read(tenant, scope, name)
	if err != nil {
		return nil, err
	}
	out := []Revision{}
	for _, r := range rows {
		out = append(out, clone(r))
	}
	return out, nil
}

// Published returns the latest published revision of a skill, and whether
// it has one.
func (s *Store) Published(tenant string, scope Scope, name string) (Revision, bool, error) {
	rows, _, err := s.read(tenant, scope, name)
	if err != nil {
		return Revision{}, false, err
	}
	for i := len(rows) - 1; i >= 0; i-- {
		if !rows[i].PublishedAt.IsZero() {
			return clone(rows[i]), true, nil
		}
	}
	return Revision{}, false, nil
}

// Tested records the durable run reference, never a client-supplied success flag.
func (s *Store) Tested(tenant string, scope Scope, name string, version int, run string) error {
	if run == "" || len(run) > 128 {
		return ErrInvalid
	}
	_, err := s.change(tenant, scope, name, version, func(r *Revision) error {
		if slices.Contains(r.Tests, run) {
			return state.ErrUnchanged
		}
		if len(r.Tests) >= 10 {
			return ErrInvalid
		}
		r.Tests = append(r.Tests, run)
		return nil
	})
	return err
}

func (s *Store) Publish(tenant, by string, scope Scope, name string, version int, at time.Time) (Revision, error) {
	r, err := s.change(tenant, scope, name, version, func(r *Revision) error {
		if !r.PublishedAt.IsZero() {
			return state.ErrUnchanged
		}
		// The platform checks that a recorded test actually completed successfully.
		if len(r.Tests) == 0 || by == "" || at.IsZero() {
			return fmt.Errorf("%w: test the revision before publishing", ErrConflict)
		}
		r.PublishedAt = at
		r.PublishedBy = by
		return nil
	})
	if err != nil {
		return Revision{}, err
	}
	return clone(r), nil
}

// ID is stable across revisions without exposing a private scope in the path.
func ID(scope Scope, name string) string {
	sum := sha256.Sum256([]byte(scope.Key() + "/" + name))
	return fmt.Sprintf("sk-%x", sum[:20])
}

// Find returns the latest revision of the skill with an id.
func (s *Store) Find(tenant, id string) (Revision, error) {
	if id == "" {
		return Revision{}, ErrNotFound
	}
	var key string
	if _, err := s.kv.lookup(tenant, idKey(id), &key); errors.Is(err, state.ErrNotFound) {
		return Revision{}, ErrNotFound
	} else if err != nil {
		return Revision{}, err
	}
	var rows []Revision
	if _, err := s.kv.lookup(tenant, key, &rows); errors.Is(err, state.ErrNotFound) {
		return Revision{}, ErrNotFound
	} else if err != nil {
		return Revision{}, err
	}
	rows, err := checkRows(key, rows)
	if err != nil {
		return Revision{}, fmt.Errorf("library: %q/%q: %w", tenant, key, err)
	}
	if last := rows[len(rows)-1]; last.ID == id {
		return clone(last), nil
	}
	return Revision{}, ErrNotFound
}
