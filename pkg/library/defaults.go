package library

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

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

// defaultsNamespace is where each agent's default skills are kept. The
// tenant is the shard key; within it:
//
//	<agent>                       an agent's defaults
//	#count                        how many agents have defaults
//	u:<skill>:<version>:<agent>   a copy of an agent's defaults, for each
//	                              revision they bind, the skill's id in
//	                              base64url
//
// An agent's name holds no ":" and does not start with "#".
const defaultsNamespace = "library.agent_defaults"

const countKey = "#count"

func usingPrefix(ref SkillRef) string {
	return "u:" + base64.RawURLEncoding.EncodeToString([]byte(ref.ID)) + ":" + strconv.Itoa(ref.Version) + ":"
}
func usingKey(ref SkillRef, agent string) string { return usingPrefix(ref) + agent }

func validAgent(agent string) bool {
	return agent != "" && len(agent) <= 160 && !strings.Contains(agent, ":") && agent[0] != '#'
}

func validDefaults(key string, v AgentDefaults) bool {
	return key == v.Agent && validAgent(v.Agent) && v.AgentRevision >= 1 && v.Version >= 1 && ValidRefs(v.Skills)
}

// DefaultsStore keeps each agent's default skills. It reads and writes the
// database per request.
type DefaultsStore struct {
	kv    kv
	index *state.Table[AgentDefaults]
}

// OpenDefaults returns a store on st, which may be shared with other
// processes. A nil st keeps defaults in this process's memory.
//
// Defaults kept by a release before the count and index records are counted
// and indexed the first time any process opens the store; a process that
// opens it at the same time writes the same records.
func OpenDefaults(st state.Store) (*DefaultsStore, error) {
	if st == nil {
		st = state.NewMemory()
	}
	s := &DefaultsStore{kv: kv{st: st, namespace: defaultsNamespace}, index: state.NewTable[AgentDefaults](st, defaultsNamespace)}
	if err := s.kv.convertOnce(s.convert); err != nil {
		return nil, fmt.Errorf("library: convert agent defaults: %w", err)
	}
	return s, nil
}

// convert counts each tenant's defaults and indexes each row. A row is
// indexed in one write with itself at the version read, so a row another
// process changed since is left to the index that process wrote.
func (s *DefaultsStore) convert(recs []state.Record) error {
	counts := map[string]int{}
	var tenants []string
	for _, r := range recs {
		if strings.HasPrefix(r.Key, "#") || strings.HasPrefix(r.Key, "u:") {
			continue
		}
		var v AgentDefaults
		if err := json.Unmarshal(r.Value, &v); err != nil {
			return fmt.Errorf("%q/%q: %w", r.Tenant, r.Key, err)
		}
		if !validDefaults(r.Key, v) {
			return fmt.Errorf("%q/%q: %w", r.Tenant, r.Key, ErrInvalid)
		}
		if counts[r.Tenant] == 0 {
			tenants = append(tenants, r.Tenant)
		}
		counts[r.Tenant]++
		o := &ops{}
		o.list = append(o.list, state.PutAt(r.Tenant, r.Key, r.Value, r.Version))
		for _, ref := range v.Skills {
			o.set(r.Tenant, usingKey(ref, v.Agent), v)
		}
		if err := o.commit(s.kv); err != nil && !errors.Is(err, state.ErrConflict) {
			return err
		}
	}
	var list []state.Op
	for _, t := range tenants {
		if counts[t] > MaxAgentDefaults {
			return fmt.Errorf("%q: %w", t, ErrInvalid)
		}
		o := &ops{}
		o.put(t, countKey, counts[t], 0)
		list = append(list, o.list...)
	}
	return s.kv.createAll(list)
}

// Get returns an agent's defaults, version 0 with no skills when it has none.
func (s *DefaultsStore) Get(tenant, agent string) (AgentDefaults, error) {
	var v AgentDefaults
	if validAgent(agent) {
		if _, err := s.kv.lookupMaybe(tenant, agent, &v); err != nil {
			return AgentDefaults{}, err
		}
	}
	v.Agent = agent
	if v.Skills == nil {
		v.Skills = []SkillRef{}
	}
	return v, nil
}

// Set replaces an agent's defaults when they are at version expected.
func (s *DefaultsStore) Set(tenant, agent string, revision, expected int, refs []SkillRef) (AgentDefaults, error) {
	if tenant == "" || !validAgent(agent) || revision < 1 || !ValidRefs(refs) {
		return AgentDefaults{}, ErrInvalid
	}
	var out AgentDefaults
	err := state.Retry(func() error {
		var current AgentDefaults
		ver, err := s.kv.lookupMaybe(tenant, agent, &current)
		if err != nil {
			return err
		}
		if current.Version != expected {
			return ErrConflict
		}
		next := AgentDefaults{Agent: agent, AgentRevision: revision, Version: current.Version + 1, Skills: slices.Clone(refs)}
		o := &ops{}
		o.put(tenant, agent, next, ver)
		if ver == 0 {
			var count int
			cv, err := s.kv.lookupMaybe(tenant, countKey, &count)
			if err != nil {
				return err
			}
			if count >= MaxAgentDefaults {
				return ErrInvalid
			}
			o.put(tenant, countKey, count+1, cv)
		}
		for _, ref := range current.Skills {
			if !slices.Contains(next.Skills, ref) {
				o.del(tenant, usingKey(ref, agent))
			}
		}
		for _, ref := range next.Skills {
			o.set(tenant, usingKey(ref, agent), next)
		}
		if err := o.commit(s.kv); err != nil {
			return err
		}
		out = next
		return nil
	})
	if err != nil {
		return AgentDefaults{}, err
	}
	out.Skills = slices.Clone(out.Skills)
	return out, nil
}

// Using returns the defaults that bind exactly one revision, by agent. The
// tenant's cap on defaults bounds the read.
func (s *DefaultsStore) Using(tenant string, ref SkillRef) ([]AgentDefaults, error) {
	items, err := s.index.All(tenant, usingPrefix(ref), MaxAgentDefaults)
	if err != nil {
		return nil, err
	}
	out := []AgentDefaults{}
	for _, it := range items {
		if slices.Contains(it.Value.Skills, ref) {
			out = append(out, it.Value)
		}
	}
	slices.SortFunc(out, func(a, b AgentDefaults) int { return strings.Compare(a.Agent, b.Agent) })
	return out, nil
}
