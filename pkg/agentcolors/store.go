// Package agentcolors keeps a workspace's agent hues stable across readers and restarts.
//
// It reads and writes the database per request (pkg/state, per request): it
// holds nothing in memory, so any number of processes on one database agree
// on every hue, and it does not need pkg/state's single-writer lock.
package agentcolors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/pkg/state"
)

// Limit bounds permanently reserved names per workspace, including deleted agents.
const Limit = 10000

// namespace is where hues are kept. Within a tenant:
//
//	<name>  the hue reserved for an agent's name
//	#hues   every reservation of the tenant, by name
//
// The "#hues" record is what an assignment reads and changes at the version
// it read, so two processes never reserve one hue for two names. An agent's
// name never starts with "#". A tenant kept before the "#hues" record
// existed gets one from its name records on its first new reservation.
const namespace = "agent-colors"

// all names the record of every reservation in a tenant.
const all = "#hues"

// Store reserves a hue for each name. Reservations are never reused.
type Store struct {
	st state.Store
}

// Open returns a store on st, which may be shared with other processes. A
// nil st keeps assignments in this process's memory.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	return &Store{st: st}, nil
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

func validHue(hue float64) bool {
	return !math.IsNaN(hue) && !math.IsInf(hue, 0) && hue >= 0 && hue < 360
}

// reservations reads a tenant's reservations and the version of the record
// they are kept in, 0 when it has none yet.
func (s *Store) reservations(ctx context.Context, tenant string) (map[string]float64, int64, error) {
	r, err := s.st.Lookup(ctx, namespace, tenant, all)
	if err == nil {
		hues := map[string]float64{}
		if err := json.Unmarshal(r.Value, &hues); err != nil {
			return nil, 0, fmt.Errorf("agentcolors: %q: %w", tenant, err)
		}
		return hues, r.Version, nil
	}
	if !errors.Is(err, state.ErrNotFound) {
		return nil, 0, err
	}
	// Kept before the record of every reservation: read the names, which a
	// tenant holds at most Limit of.
	hues := map[string]float64{}
	used := map[float64]bool{}
	after := ""
	for {
		page, next, err := s.st.List(ctx, namespace, tenant, state.Query{After: after, Limit: state.MaxList})
		if err != nil {
			return nil, 0, err
		}
		for _, rec := range page {
			if strings.HasPrefix(rec.Key, "#") {
				continue
			}
			var hue float64
			if err := json.Unmarshal(rec.Value, &hue); err != nil || !validHue(hue) {
				return nil, 0, fmt.Errorf("agentcolors: invalid assignment")
			}
			if used[hue] {
				return nil, 0, fmt.Errorf("agentcolors: duplicate hue")
			}
			hues[rec.Key], used[hue] = hue, true
		}
		if len(hues) > Limit {
			return nil, 0, fmt.Errorf("agentcolors: reservation limit reached")
		}
		if next == "" {
			return hues, 0, nil
		}
		after = next
	}
}

// Assign returns only the requested names. It writes every new reservation
// atomically before returning it and leaves existing assignments unchanged.
func (s *Store) Assign(tenant string, names []string) (map[string]float64, error) {
	for _, requested := range names {
		name := legacy.BuiltinAgentKey(requested)
		if name == "" {
			return nil, fmt.Errorf("agentcolors: empty name")
		}
		if strings.HasPrefix(name, "#") {
			return nil, fmt.Errorf("agentcolors: %q is not an agent's name", requested)
		}
	}
	var out map[string]float64
	err := state.Retry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
		defer cancel()
		hues, version, err := s.reservations(ctx, tenant)
		if err != nil {
			return err
		}
		used := make(map[float64]bool, len(hues))
		for _, hue := range hues {
			used[hue] = true
		}
		out = map[string]float64{}
		var ops []state.Op
		next := 0
		for _, requested := range names {
			name := legacy.BuiltinAgentKey(requested)
			if hue, ok := hues[name]; ok {
				out[requested] = hue
				continue
			}
			if len(hues) >= Limit {
				return fmt.Errorf("agentcolors: reservation limit reached")
			}
			hue := Preferred(name)
			if used[hue] {
				for used[candidate(next)] {
					next++
				}
				hue = candidate(next)
			}
			hues[name], out[requested], used[hue] = hue, hue, true
			raw, _ := json.Marshal(hue)
			ops = append(ops, state.Put(tenant, name, raw))
		}
		if len(ops) == 0 {
			return nil
		}
		raw, err := json.Marshal(hues)
		if err != nil {
			return err
		}
		if version == 0 {
			ops = append(ops, state.Create(tenant, all, raw))
		} else {
			ops = append(ops, state.PutAt(tenant, all, raw, version))
		}
		return s.st.Write(ctx, namespace, ops...)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
