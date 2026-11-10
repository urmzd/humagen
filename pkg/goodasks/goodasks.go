// Package goodasks keeps the asks each agent answered well: the text of a
// run a person rated up, filed under the agent that answered it. Routing
// searches them beside the agent's prompt (pkg/join), so an agent is found
// for the work people were glad it did, not only for what its prompt says.
//
// It keeps the ask alone, never the answer, and at most MaxPerAgent asks per
// agent, the newest. A rating taken back or turned down drops the ask.
package goodasks

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/state"
)

// Bounds.
const (
	// MaxPerAgent is how many asks are kept for one agent.
	MaxPerAgent = 25
	// MaxText bounds one ask's text, in bytes.
	MaxText = 500
	// maxPages bounds how many pages All reads.
	maxPages = 4
)

// Ask is one ask an agent answered well.
type Ask struct {
	Agent string    `json:"agent"`
	Run   string    `json:"run"`
	Text  string    `json:"text"`
	At    time.Time `json:"at"`
}

// Store keeps asks per tenant on a state.Store.
type Store struct {
	t *state.Table[Ask]
}

// Open returns a store on st. A nil st keeps asks in this process's memory.
func Open(st state.Store) *Store {
	if st == nil {
		st = state.NewMemory()
	}
	return &Store{t: state.NewTable[Ask](st, "good-asks")}
}

func prefix(agent string) string { return "a:" + agent + ":" }

func key(agent, run string) string {
	sum := sha256.Sum256([]byte(run))
	return prefix(agent) + hex.EncodeToString(sum[:12])
}

// Keep files a's text under its agent, replacing an earlier ask from the
// same run, and drops the oldest when the agent has more than MaxPerAgent.
func (s *Store) Keep(tenant string, a Ask) error {
	a.Text = clip(strings.TrimSpace(a.Text), MaxText)
	if a.Agent == "" || a.Run == "" || a.Text == "" {
		return fmt.Errorf("goodasks: an ask needs an agent, a run and text")
	}
	k := key(a.Agent, a.Run)
	if err := s.t.Put(tenant, k, a); err != nil {
		return err
	}
	items, _, err := s.t.List(tenant, state.Query{Prefix: prefix(a.Agent), Limit: state.MaxList})
	if err != nil || len(items) <= MaxPerAgent {
		return err
	}
	slices.SortFunc(items, func(x, y state.Item[Ask]) int { return y.Value.At.Compare(x.Value.At) })
	b := s.t.Batch()
	for _, it := range items[MaxPerAgent:] {
		b.Delete(tenant, it.Key)
	}
	return b.Commit()
}

// Forget drops the ask a run filed under agent, if any.
func (s *Store) Forget(tenant, agent, run string) error {
	if agent == "" || run == "" {
		return nil
	}
	return s.t.Delete(tenant, key(agent, run))
}

// All returns each agent's asks, newest first, reading a bounded number of
// pages: a workspace with more praise than that is searched on what was read.
func (s *Store) All(tenant string) (map[string][]string, error) {
	byAgent := map[string][]Ask{}
	q := state.Query{Prefix: "a:", Limit: state.MaxList}
	for range maxPages {
		items, next, err := s.t.List(tenant, q)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			byAgent[it.Value.Agent] = append(byAgent[it.Value.Agent], it.Value)
		}
		if next == "" {
			break
		}
		q.After = next
	}
	out := make(map[string][]string, len(byAgent))
	for agent, asks := range byAgent {
		slices.SortStableFunc(asks, func(x, y Ask) int { return y.At.Compare(x.At) })
		if len(asks) > MaxPerAgent {
			asks = asks[:MaxPerAgent]
		}
		texts := make([]string, len(asks))
		for i, a := range asks {
			texts[i] = a.Text
		}
		out[agent] = texts
	}
	return out, nil
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
