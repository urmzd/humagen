// Package projectforks keeps bounded private plans for restartable project copies.
//
// It reads and writes per request: a plan is one record, read by Lookup and
// written by compare-and-set, and a tenant's count of plans is a record kept
// in the same write as each new plan, so the cap holds across processes. It
// keeps nothing in memory, does not need the database's lock, and two
// processes on one database agree.
package projectforks

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/sourcelinks"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
)

const (
	MaxOperations = 500
	MaxProjects   = 20
	MaxAgents     = 20
	MaxNotes      = 100
	MaxBytes      = 512 * 1024
)

var (
	ErrNotFound = errors.New("project forks: not found")
	ErrInvalid  = errors.New("project forks: invalid plan")
	ErrLimit    = errors.New("project forks: capacity reached")
)

// ProjectCopy is configuration only. No conversation or live binding is copied.
type ProjectCopy struct {
	Source   string             `json:"source"`
	Name     string             `json:"name"`
	Parent   string             `json:"parent"`
	Config   projects.Project   `json:"config"`
	Channels []projects.Channel `json:"channels"`
	Rules    []sourcelinks.Rule `json:"rules,omitempty"`
}
type AgentCopy struct {
	Source  string         `json:"source"`
	Name    string         `json:"name"`
	Project string         `json:"project"`
	Spec    wire.AgentSpec `json:"spec"`
}
type ArchiveCopy struct {
	SourceThread string             `json:"source_thread"`
	Project      string             `json:"project"`
	Channel      string             `json:"channel"`
	Title        string             `json:"title"`
	Through      string             `json:"through"`
	Context      []threads.ForkTurn `json:"context"`
}

type NoteCopy struct {
	Project  string         `json:"project"`
	SourceID string         `json:"source_id"`
	Text     string         `json:"text"`
	Details  memory.Details `json:"details,omitempty"`
}

// Plan is captured once from authorized source records. A retry never broadens it.
type Plan struct {
	Source      string        `json:"source"`
	Destination string        `json:"destination"`
	Parent      string        `json:"parent"`
	Projects    []ProjectCopy `json:"projects"`
	Agents      []AgentCopy   `json:"agents"`
	Notes       []NoteCopy    `json:"notes"`
	Archives    []ArchiveCopy `json:"archives,omitempty"`
}

// Record is private to its creator. Ready is determined from the destination's
// durable ForkOrigin gate, rather than a second store's completion marker.
type Record struct {
	ID        string    `json:"id"`
	By        string    `json:"by"`
	At        time.Time `json:"at"`
	UpdatedAt time.Time `json:"updated_at"`
	Error     string    `json:"error,omitempty"`
	Plan      Plan      `json:"plan"`
}

// namespace is where plans are kept: one record per destination, under the
// destination's name, and the tenant's count under countKey.
const namespace = "project-forks"

// countKey holds how many plans a tenant keeps. A project's name never starts
// with "#", so it names no plan.
const countKey = "#count"

// Store keeps every tenant's plans in a state.Store.
type Store struct {
	table *state.Table[json.RawMessage]
}

// Open returns a store that keeps its plans in st. A nil st keeps them in
// memory.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	return &Store{table: state.NewTable[json.RawMessage](st, namespace)}, nil
}

// lookup reads one plan, holding it to what a plan must be.
func (s *Store) lookup(tenant, destination string) (Record, int64, error) {
	if !projects.ValidName(destination) {
		return Record{}, 0, ErrNotFound
	}
	it, err := s.table.Lookup(tenant, destination)
	if errors.Is(err, state.ErrNotFound) {
		return Record{}, 0, ErrNotFound
	}
	if err != nil {
		return Record{}, 0, fmt.Errorf("project forks: read: %w", err)
	}
	var record Record
	if err := json.Unmarshal(it.Value, &record); err != nil || record.Plan.Destination != destination || validate(record) != nil {
		return Record{}, 0, ErrInvalid
	}
	return record, it.Version, nil
}

// count is how many plans a tenant keeps, and the version of its count
// record, state.Missing when there is none yet. A tenant written before the
// count was kept is counted once from its plans, a page at a time; the write
// that creates the count conflicts with any other that counted at once.
func (s *Store) count(tenant string) (int, int64, error) {
	it, err := s.table.Lookup(tenant, countKey)
	if err == nil {
		var n int
		if err := json.Unmarshal(it.Value, &n); err != nil || n < 0 {
			return 0, 0, ErrInvalid
		}
		return n, it.Version, nil
	}
	if !errors.Is(err, state.ErrNotFound) {
		return 0, 0, fmt.Errorf("project forks: read: %w", err)
	}
	n, after := 0, ""
	for {
		page, next, err := s.table.List(tenant, state.Query{After: after, Limit: 20})
		if err != nil {
			return 0, 0, fmt.Errorf("project forks: read: %w", err)
		}
		for _, it := range page {
			if it.Key != countKey {
				n++
			}
		}
		if n > MaxOperations {
			return 0, 0, fmt.Errorf("%w: more than %d plans are kept", ErrLimit, MaxOperations)
		}
		if next == "" {
			return n, state.Missing, nil
		}
		after = next
	}
}

func validate(record Record) error {
	plan := record.Plan
	if record.By == "" || record.At.IsZero() || len(record.ID) != 19 || !projects.ValidName(plan.Source) || !projects.ValidName(plan.Destination) || plan.Source == plan.Destination || len(plan.Projects) == 0 || len(plan.Projects) > MaxProjects || len(plan.Agents) > MaxAgents || len(plan.Notes) > MaxNotes || len(plan.Archives) > 20 || len(record.Error) > 1000 {
		return ErrInvalid
	}
	projectsByName := map[string]bool{}
	for i, copy := range plan.Projects {
		if !projects.ValidName(copy.Source) || !projects.ValidName(copy.Name) || projectsByName[copy.Name] || len(copy.Channels) > projects.MaxChannels {
			return ErrInvalid
		}
		if i == 0 && (copy.Source != plan.Source || copy.Name != plan.Destination || copy.Parent != plan.Parent) {
			return ErrInvalid
		}
		if i > 0 && !projectsByName[copy.Parent] {
			return ErrInvalid
		}
		projectsByName[copy.Name] = true
	}
	agentsByName := map[string]bool{}
	for _, copy := range plan.Agents {
		if !projectsByName[copy.Project] || !projects.ValidAgent(copy.Name) || copy.Spec.Name != copy.Name || agentsByName[copy.Name] {
			return ErrInvalid
		}
		agentsByName[copy.Name] = true
	}
	for _, note := range plan.Notes {
		if !projectsByName[note.Project] || note.SourceID == "" || note.Text == "" {
			return ErrInvalid
		}
	}
	for _, copy := range plan.Archives {
		if !threads.ValidID(copy.SourceThread) || !projectsByName[copy.Project] || len(copy.Context) == 0 || len(copy.Context) > threads.MaxForkTurns {
			return ErrInvalid
		}
		bytes := 0
		for _, turn := range copy.Context {
			bytes += len(turn.Text) + len(turn.By) + len(turn.Name)
			if bytes > threads.MaxForkBytes {
				return ErrInvalid
			}
		}
	}
	raw, err := json.Marshal(record)
	if err != nil || len(raw) > MaxBytes {
		return ErrInvalid
	}
	return nil
}
func clone(record Record) Record {
	raw, _ := json.Marshal(record)
	var out Record
	_ = json.Unmarshal(raw, &out)
	return out
}
func (s *Store) Get(tenant, destination, by string) (Record, error) {
	record, _, err := s.lookup(tenant, destination)
	if err != nil {
		return Record{}, err
	}
	if record.By != by {
		return Record{}, ErrNotFound
	}
	return record, nil
}

// Start keeps a new plan, or returns the one its creator already started for
// the destination. A tenant keeps at most MaxOperations; two processes
// starting plans at once keep the count exact, since each new plan and its
// count are one write that lands only at the count's version it read.
func (s *Store) Start(tenant, by string, plan Plan, at time.Time) (Record, error) {
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Record{}, err
	}
	record := Record{ID: "pf-" + hex.EncodeToString(id[:]), By: by, At: at, UpdatedAt: at, Plan: plan}
	var out Record
	err := state.Retry(func() error {
		existing, _, err := s.lookup(tenant, plan.Destination)
		switch {
		case err == nil:
			if existing.By != by || existing.Plan.Source != plan.Source || existing.Plan.Parent != plan.Parent {
				return ErrNotFound
			}
			out = existing
			return nil
		case !errors.Is(err, ErrNotFound):
			return err
		}
		if err := validate(record); err != nil {
			return err
		}
		n, version, err := s.count(tenant)
		if err != nil {
			return err
		}
		if n >= MaxOperations {
			return ErrLimit
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		next, _ := json.Marshal(n + 1)
		b := s.table.Batch()
		b.Create(tenant, plan.Destination, raw)
		if version == state.Missing {
			b.Create(tenant, countKey, next)
		} else {
			b.PutAt(tenant, countKey, next, version)
		}
		if err := b.Commit(); err != nil {
			return fmt.Errorf("project forks: write: %w", err)
		}
		out = clone(record)
		return nil
	})
	return out, err
}

// Failure records whether a copy stopped, at the plan's version it read.
func (s *Store) Failure(tenant, destination, by string, cause error, at time.Time) error {
	return state.Retry(func() error {
		record, version, err := s.lookup(tenant, destination)
		if err != nil {
			return err
		}
		if record.By != by {
			return ErrNotFound
		}
		record.Error = ""
		if cause != nil {
			record.Error = "Copy stopped before publication. Retry to continue."
		}
		record.UpdatedAt = at
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		b := s.table.Batch()
		b.PutAt(tenant, destination, raw, version)
		if err := b.Commit(); err != nil {
			return fmt.Errorf("project forks: write: %w", err)
		}
		return nil
	})
}
