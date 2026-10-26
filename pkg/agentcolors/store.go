// Package agentcolors keeps a workspace's agent hues stable across readers and restarts.
package agentcolors

import (
	"fmt"
	"math"
	"sync"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/pkg/state"
)

// Limit bounds permanently reserved names per workspace, including deleted agents.
const Limit = 10000

// Store reserves a hue for each name. Reservations are never reused.
type Store struct {
	mu      sync.Mutex
	tenants map[string]map[string]float64
	kept    *state.Table[float64]
}

// Open reads the assignments kept in st. A nil st keeps them in memory.
func Open(st state.Store) (*Store, error) {
	s := &Store{tenants: map[string]map[string]float64{}, kept: state.NewTable[float64](st, "agent-colors")}
	err := s.kept.Load(func(tenant, name string, hue float64) error {
		if name == "" || math.IsNaN(hue) || math.IsInf(hue, 0) || hue < 0 || hue >= 360 {
			return fmt.Errorf("agentcolors: invalid assignment")
		}
		hues := s.tenant(tenant)
		if len(hues) >= Limit {
			return fmt.Errorf("agentcolors: reservation limit reached")
		}
		for _, used := range hues {
			if used == hue {
				return fmt.Errorf("agentcolors: duplicate hue")
			}
		}
		hues[name] = hue
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) tenant(tenant string) map[string]float64 {
	hues := s.tenants[tenant]
	if hues == nil {
		hues = map[string]float64{}
		s.tenants[tenant] = hues
	}
	return hues
}

// Preferred preserves the console's original twelve-hue mapping when available.
func Preferred(name string) float64 {
	h := 186
	for _, c := range legacy.BuiltinAgentKey(name) {
		h = (h*31 + int(c)) % 1000003
	}
	return float64(h%12) * 30
}

// candidate fills the largest remaining gaps: twelve hues, then their midpoints.
func candidate(i int) float64 {
	if i < 12 {
		return float64(i) * 30
	}
	width, count := 30.0, 12
	for i -= 12; i >= count; count *= 2 {
		i -= count
		width /= 2
	}
	return (float64(i) + 0.5) * width
}

// Assign returns only the requested names. It writes every new reservation
// atomically before returning it and leaves existing assignments unchanged.
func (s *Store) Assign(tenant string, names []string) (map[string]float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hues := s.tenant(tenant)
	used := make(map[float64]bool, len(hues))
	for _, hue := range hues {
		used[hue] = true
	}
	added, out := map[string]float64{}, map[string]float64{}
	batch := s.kept.Batch()
	next := 0
	for _, requested := range names {
		name := legacy.BuiltinAgentKey(requested)
		if name == "" {
			return nil, fmt.Errorf("agentcolors: empty name")
		}
		if hue, ok := hues[name]; ok {
			out[requested] = hue
			continue
		}
		if hue, ok := added[name]; ok {
			out[requested] = hue
			continue
		}
		if len(hues)+len(added) >= Limit {
			return nil, fmt.Errorf("agentcolors: reservation limit reached")
		}
		hue := Preferred(name)
		if used[hue] {
			for used[candidate(next)] {
				next++
			}
			hue = candidate(next)
		}
		added[name], out[requested], used[hue] = hue, hue, true
		batch.Put(tenant, name, hue)
	}
	if err := batch.Commit(); err != nil {
		return nil, err
	}
	for name, hue := range added {
		hues[name] = hue
	}
	return out, nil
}
