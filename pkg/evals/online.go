package evals

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/signals"
	"github.com/urmzd/humagen/pkg/state"
)

// Online is a deterministic assessment, never a model's judgment.
type Online struct {
	Signals    signals.Record `json:"signals"`
	Run        string         `json:"run"`
	Agent      string         `json:"agent"`
	Version    int            `json:"version"`
	Passed     bool           `json:"passed"`
	Words      int            `json:"words"`
	WordBudget int            `json:"word_budget"`
	Verdicts   []Verdict      `json:"verdicts"`
	At         time.Time      `json:"at"`
}

// CheckStyle counts Unicode-separated words and detects introductory filler.
// Detailed requests may exceed the word budget; filler remains a failure.
func CheckStyle(text string, budget int, detail bool) Online {
	words := len(strings.Fields(text))
	length := detail || budget <= 0 || words <= budget
	start := strings.ToLower(strings.TrimSpace(text))
	filler := false
	for _, prefix := range []string{"great question", "certainly!", "absolutely!", "of course!", "happy to help", "i'd be happy to", "sure, here's", "sure!"} {
		filler = filler || strings.HasPrefix(start, prefix)
	}
	return Online{Words: words, WordBudget: budget, Passed: length && !filler, Verdicts: []Verdict{
		{Check: "word_budget", Passed: length, Reason: chooseReason(length, fmt.Sprintf("%d words exceeds the %d-word budget", words, budget))},
		{Check: "answer_first", Passed: !filler, Reason: chooseReason(!filler, "The reply starts with filler")},
	}}
}

func chooseReason(passed bool, reason string) string {
	if passed {
		return ""
	}
	return reason
}

func identity(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:])
}

func (s *Store) SaveOnline(tenant string, result Online) error {
	if tenant == "" || result.Run == "" || result.Agent == "" || result.At.IsZero() {
		return ErrInvalid
	}
	if err := s.online.Put(tenant, identity(result.Run), result); err != nil {
		return err
	}
	return s.onlineAgent.Put(tenant, result.Agent+"/"+state.Seq(result.At.UnixNano())+"/"+identity(result.Run), result)
}

func (s *Store) Online(tenant, run string) (Online, error) {
	r, err := s.online.Lookup(tenant, identity(run))
	return r.Value, err
}

// Candidate is a feedback-derived input awaiting explicit expected checks.
// A complaint is not automatically converted into a passing assertion.
type Candidate struct {
	ID      string    `json:"id"`
	Run     string    `json:"run"`
	Agent   string    `json:"agent"`
	Project string    `json:"project"`
	Input   string    `json:"input"`
	Reason  string    `json:"reason"`
	Comment string    `json:"comment,omitempty"`
	At      time.Time `json:"at"`
}

func (s *Store) SaveCandidate(tenant, subject string, c Candidate) error {
	if tenant == "" || subject == "" || c.Run == "" || c.Agent == "" || strings.TrimSpace(c.Reason) == "" || len(c.Input) > 64000 || len(c.Reason) > 80 || len(c.Comment) > 4000 {
		return ErrInvalid
	}
	c.ID = identity(c.Run, subject)
	return s.candidates.Put(tenant, c.Agent+"/"+c.ID, c)
}

func (s *Store) Candidates(tenant, agent string, limit int) ([]Candidate, error) {
	if tenant == "" || agent == "" || limit < 1 || limit > 200 {
		return nil, ErrInvalid
	}
	rows, _, err := s.candidates.List(tenant, state.Query{Prefix: agent + "/", Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Value)
	}
	return out, nil
}

// OnlineFor returns a bounded newest-first window for agent health.
func (s *Store) OnlineFor(tenant, agent string, limit int) ([]Online, error) {
	if limit < 1 || limit > 200 {
		return nil, ErrInvalid
	}
	rows, _, err := s.onlineAgent.List(tenant, state.Query{Prefix: agent + "/", Limit: limit, Desc: true})
	if err != nil {
		return nil, err
	}
	out := make([]Online, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Value)
	}
	return out, nil
}
