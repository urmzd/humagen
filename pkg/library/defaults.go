package library

import (
	"slices"
	"sync"

	"github.com/urmzd/humagen/pkg/state"
)

const MaxAgentDefaults = 10000
const MaxDefaultSkills = 4

// SkillRef identifies one exact immutable published revision.
type SkillRef struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

type AgentDefaults struct {
	Agent         string     `json:"agent"`
	AgentRevision int        `json:"agent_revision"`
	Version       int        `json:"version"`
	Skills        []SkillRef `json:"skills"`
}

func ValidRefs(refs []SkillRef) bool {
	if refs == nil || len(refs) > MaxDefaultSkills {
		return false
	}
	for i, ref := range refs {
		if ref.ID == "" || len(ref.ID) > 160 || ref.Version < 1 {
			return false
		}
		for _, previous := range refs[:i] {
			if previous.ID == ref.ID {
				return false
			}
		}
	}
	return true
}

type DefaultsStore struct {
	mu   sync.Mutex
	by   map[string]map[string]AgentDefaults
	kept *state.Table[AgentDefaults]
}

func OpenDefaults(st state.Store) (*DefaultsStore, error) {
	s := &DefaultsStore{by: map[string]map[string]AgentDefaults{}, kept: state.NewTable[AgentDefaults](st, "library.agent_defaults")}
	err := s.kept.Load(func(tenant, key string, v AgentDefaults) error {
		if tenant == "" || key != v.Agent || v.Agent == "" || len(v.Agent) > 160 || v.AgentRevision < 1 || v.Version < 1 || !ValidRefs(v.Skills) {
			return ErrInvalid
		}
		if s.by[tenant] == nil {
			s.by[tenant] = map[string]AgentDefaults{}
		}
		if len(s.by[tenant]) >= MaxAgentDefaults {
			return ErrInvalid
		}
		s.by[tenant][key] = v
		return nil
	})
	return s, err
}
func (s *DefaultsStore) Get(tenant, agent string) AgentDefaults {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.by[tenant][agent]
	v.Agent = agent
	v.Skills = slices.Clone(v.Skills)
	if v.Skills == nil {
		v.Skills = []SkillRef{}
	}
	return v
}
func (s *DefaultsStore) Set(tenant, agent string, revision, expected int, refs []SkillRef) (AgentDefaults, error) {
	if tenant == "" || agent == "" || len(agent) > 160 || revision < 1 || !ValidRefs(refs) {
		return AgentDefaults{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.by[tenant][agent]
	if current.Version != expected {
		return AgentDefaults{}, ErrConflict
	}
	if current.Version == 0 && len(s.by[tenant]) >= MaxAgentDefaults {
		return AgentDefaults{}, ErrInvalid
	}
	next := AgentDefaults{Agent: agent, AgentRevision: revision, Version: current.Version + 1, Skills: slices.Clone(refs)}
	if err := s.kept.Put(tenant, agent, next); err != nil {
		return AgentDefaults{}, err
	}
	if s.by[tenant] == nil {
		s.by[tenant] = map[string]AgentDefaults{}
	}
	s.by[tenant][agent] = next
	next.Skills = slices.Clone(next.Skills)
	return next, nil
}
