// Package projectforks keeps bounded private plans for restartable project copies.
package projectforks

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/urmzd/fiber/pkg/agentspec"
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
	Source  string              `json:"source"`
	Name    string              `json:"name"`
	Project string              `json:"project"`
	Spec    agentspec.AgentSpec `json:"spec"`
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

type Store struct {
	mu      sync.Mutex
	records map[string]map[string]Record
	table   *state.Table[Record]
}

func Open(st state.Store) (*Store, error) {
	s := &Store{records: map[string]map[string]Record{}, table: state.NewTable[Record](st, "project-forks")}
	err := s.table.Load(func(tenant, key string, record Record) error {
		if key != record.Plan.Destination || validate(record) != nil {
			return ErrInvalid
		}
		if s.records[tenant] == nil {
			s.records[tenant] = map[string]Record{}
		}
		if len(s.records[tenant]) >= MaxOperations {
			return ErrLimit
		}
		s.records[tenant][key] = record
		return nil
	})
	return s, err
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
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[tenant][destination]
	if !ok || record.By != by {
		return Record{}, ErrNotFound
	}
	return clone(record), nil
}
func (s *Store) Start(tenant, by string, plan Plan, at time.Time) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.records[tenant][plan.Destination]; ok {
		if existing.By != by || existing.Plan.Source != plan.Source || existing.Plan.Parent != plan.Parent {
			return Record{}, ErrNotFound
		}
		return clone(existing), nil
	}
	if len(s.records[tenant]) >= MaxOperations {
		return Record{}, ErrLimit
	}
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Record{}, err
	}
	record := Record{ID: "pf-" + hex.EncodeToString(id[:]), By: by, At: at, UpdatedAt: at, Plan: plan}
	if err := validate(record); err != nil {
		return Record{}, err
	}
	if err := s.table.Put(tenant, plan.Destination, record); err != nil {
		return Record{}, fmt.Errorf("project forks: write: %w", err)
	}
	if s.records[tenant] == nil {
		s.records[tenant] = map[string]Record{}
	}
	s.records[tenant][plan.Destination] = clone(record)
	return clone(record), nil
}
func (s *Store) Failure(tenant, destination, by string, cause error, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[tenant][destination]
	if !ok || record.By != by {
		return ErrNotFound
	}
	record.Error = ""
	if cause != nil {
		record.Error = "Copy stopped before publication. Retry to continue."
	}
	record.UpdatedAt = at
	if err := s.table.Put(tenant, destination, record); err != nil {
		return err
	}
	s.records[tenant][destination] = record
	return nil
}
