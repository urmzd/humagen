// Package evals measures how well an agent or a knowledge source is doing.
//
// A suite is a list of cases against one target. An agent case sends the agent
// an input and checks its answer. A knowledge case asks a source a question and
// checks that the right document comes back. Running a suite produces a run,
// which pins what was tested, so two runs can be compared across revisions.
//
// # Deterministic on purpose
//
// Every check here is a pure function of the output: it contains this, it
// matches that, it answered within a limit, the expected document ranked in the
// top k. Nothing asks a model to grade a model, so the same answer always gets
// the same score, and a score that moved means the agent moved.
//
// The scoring itself is saige's: this package declares cases and stores
// results, and hands the arithmetic to saige/eval and saige/rag/eval.
//
// It reads and writes the database per request (pkg/state, per request): it
// holds nothing in memory, so any number of processes on one database agree,
// and it does not need pkg/state's single-writer lock. A run that was still
// going when the process carrying it out stopped is read as errored once it
// has gone unwritten for a while: nothing is carrying it out any more.
package evals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	saigeeval "github.com/urmzd/saige/eval"
	rageval "github.com/urmzd/saige/rag/eval"
	ragtypes "github.com/urmzd/saige/rag/types"

	"github.com/urmzd/humagen/pkg/state"
)

// Errors.
var (
	ErrInvalid  = errors.New("evals: invalid")
	ErrExists   = errors.New("evals: suite already exists")
	ErrNotFound = errors.New("evals: not found")
	ErrBusy     = errors.New("evals: suite is already running")
)

// Limits. A suite is run by a person waiting on it, so it is kept small.
const (
	MaxCases  = 50
	MaxChecks = 10
	maxRuns   = 50
	DefaultK  = 5
)

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Target is what a suite tests.
type Target string

const (
	// Agent suites send inputs to an agent and check its answers.
	Agent Target = "agent"
	// Knowledge suites send queries to a source and check what is retrieved.
	Knowledge Target = "knowledge"
)

// CheckKind is one deterministic test of an answer.
type CheckKind string

const (
	// Contains passes when the answer includes the value, ignoring case.
	Contains CheckKind = "contains"
	// NotContains passes when the answer does not include the value.
	NotContains CheckKind = "not_contains"
	// Matches passes when the answer matches the value as a regular expression.
	Matches CheckKind = "matches"
	// Equals passes when the answer is the value, ignoring surrounding space.
	Equals CheckKind = "equals"
	// Within passes when the run ended within the value, in milliseconds.
	Within CheckKind = "within_ms"
)

// Check is one test applied to an agent's answer.
type Check struct {
	Kind  CheckKind `json:"kind"`
	Value string    `json:"value"`
}

// Case is one input and what a good result looks like.
type Case struct {
	ID string `json:"id"`
	// Input is the request sent to an agent, or the query sent to a source.
	Input string `json:"input"`
	// Checks apply to an agent's answer. Every one must pass.
	Checks []Check `json:"checks,omitempty"`
	// Expect names the documents, by title, that a knowledge query should
	// retrieve. The case passes when at least one ranks in the top K.
	Expect []string `json:"expect,omitempty"`
}

// Suite is a named list of cases against one target.
type Suite struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Target      Target `json:"target"`
	// Subject is the agent's name or the knowledge source's name.
	Subject string `json:"subject"`
	Cases   []Case `json:"cases"`
	// K is how many results a knowledge query is judged on. Zero means DefaultK.
	K int `json:"k,omitempty"`
	// PassRate is the share of cases, 0 to 100, that must pass for the run to
	// pass. Zero means every case.
	PassRate  float64   `json:"pass_rate,omitempty"`
	CreatedBy string    `json:"created_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Threshold is the pass rate a run must reach.
func (s Suite) Threshold() float64 {
	if s.PassRate <= 0 {
		return 100
	}
	return s.PassRate
}

// Validate checks a suite before it is stored.
func (s Suite) Validate() error {
	switch {
	case !validName.MatchString(s.Name):
		return fmt.Errorf("%w: a suite name is lowercase letters, digits and dashes", ErrInvalid)
	case s.Target != Agent && s.Target != Knowledge:
		return fmt.Errorf("%w: a suite targets an agent or a knowledge source", ErrInvalid)
	case s.Subject == "":
		return fmt.Errorf("%w: a suite needs something to test", ErrInvalid)
	case len(s.Cases) == 0:
		return fmt.Errorf("%w: a suite needs at least one case", ErrInvalid)
	case len(s.Cases) > MaxCases:
		return fmt.Errorf("%w: a suite holds at most %d cases", ErrInvalid, MaxCases)
	case s.PassRate < 0 || s.PassRate > 100:
		return fmt.Errorf("%w: the pass rate is a percentage from 0 to 100", ErrInvalid)
	case s.K < 0 || s.K > 50:
		return fmt.Errorf("%w: k is from 1 to 50", ErrInvalid)
	}
	seen := map[string]bool{}
	for i, c := range s.Cases {
		n := i + 1
		switch {
		case c.ID == "" || seen[c.ID]:
			return fmt.Errorf("%w: case %d needs an id no other case has", ErrInvalid, n)
		case strings.TrimSpace(c.Input) == "":
			return fmt.Errorf("%w: case %d has no input", ErrInvalid, n)
		}
		seen[c.ID] = true
		if s.Target == Knowledge {
			if len(c.Expect) == 0 || slices.Contains(c.Expect, "") {
				return fmt.Errorf("%w: case %d names no document it expects", ErrInvalid, n)
			}
			continue
		}
		if len(c.Checks) == 0 {
			return fmt.Errorf("%w: case %d has no checks, so it could never fail", ErrInvalid, n)
		}
		if len(c.Checks) > MaxChecks {
			return fmt.Errorf("%w: case %d has more than %d checks", ErrInvalid, n, MaxChecks)
		}
		for _, ch := range c.Checks {
			if _, err := scorerOf(ch); err != nil {
				return fmt.Errorf("%w: case %d: %w", ErrInvalid, n, err)
			}
		}
	}
	return nil
}

// Status is where a run stands.
type Status string

const (
	Running Status = "running"
	Passed  Status = "passed"
	Failed  Status = "failed"
	// Errored means the run could not be carried out, which is different
	// from being carried out and failing.
	Errored Status = "errored"
)

// Verdict is one check's result on one case.
type Verdict struct {
	Check  string `json:"check"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason,omitempty"`
}

// Hit is one retrieved document in a knowledge case's result.
type Hit struct {
	Title string  `json:"title"`
	Score float64 `json:"score"`
	// Expected reports whether the case named this document.
	Expected bool `json:"expected"`
}

// Result is how one case went.
type Result struct {
	CaseID string `json:"case_id"`
	Input  string `json:"input"`
	Passed bool   `json:"passed"`
	// Output is the agent's answer.
	Output   string    `json:"output,omitempty"`
	Verdicts []Verdict `json:"verdicts,omitempty"`
	// Hits and Rank describe a knowledge case. Rank is the position of the
	// first expected document, from 1, or 0 when none was retrieved.
	Hits []Hit `json:"hits,omitempty"`
	Rank int   `json:"rank,omitempty"`
	// RunID is the agent run behind the case, for opening its trace.
	RunID      string `json:"run_id,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// Run is one execution of a suite.
type Run struct {
	ID      string `json:"id"`
	Suite   string `json:"suite"`
	Target  Target `json:"target"`
	Subject string `json:"subject"`
	// Rev is the agent revision that was tested.
	Rev    int    `json:"rev,omitempty"`
	Status Status `json:"status"`
	Passed int    `json:"passed"`
	Total  int    `json:"total"`
	// PassRate is the share of cases that passed, 0 to 100.
	PassRate  float64 `json:"pass_rate"`
	Threshold float64 `json:"threshold"`
	// Metrics are retrieval means for a knowledge run: hit_rate, mrr,
	// recall, precision and ndcg, each from 0 to 1.
	Metrics   map[string]float64 `json:"metrics,omitempty"`
	Results   []Result           `json:"results"`
	Error     string             `json:"error,omitempty"`
	StartedBy string             `json:"started_by,omitempty"`
	StartedAt time.Time          `json:"started_at"`
	EndedAt   time.Time          `json:"ended_at,omitzero"`
}

// scorerOf turns a check into the saige scorer that carries it out.
func scorerOf(c Check) (saigeeval.Scorer, error) {
	if c.Value == "" {
		return nil, fmt.Errorf("a %q check needs a value", c.Kind)
	}
	name := string(c.Kind) + ": " + c.Value
	text := func(pred func(out string) (bool, string)) saigeeval.Scorer {
		return saigeeval.NewCheckScorer(name, func(_ context.Context, obs saigeeval.Observation) (bool, string, error) {
			ok, reason := pred(saigeeval.OutputText(obs))
			return ok, reason, nil
		})
	}
	switch c.Kind {
	case Contains:
		want := strings.ToLower(c.Value)
		return text(func(out string) (bool, string) {
			if strings.Contains(strings.ToLower(out), want) {
				return true, ""
			}
			return false, "the answer does not contain it"
		}), nil
	case NotContains:
		bad := strings.ToLower(c.Value)
		return text(func(out string) (bool, string) {
			if strings.Contains(strings.ToLower(out), bad) {
				return false, "the answer contains it"
			}
			return true, ""
		}), nil
	case Equals:
		return text(func(out string) (bool, string) {
			if strings.TrimSpace(out) == strings.TrimSpace(c.Value) {
				return true, ""
			}
			return false, "the answer is different"
		}), nil
	case Matches:
		re, err := regexp.Compile(c.Value)
		if err != nil {
			return nil, fmt.Errorf("%q is not a regular expression: %v", c.Value, err)
		}
		return text(func(out string) (bool, string) {
			if re.MatchString(out) {
				return true, ""
			}
			return false, "the answer does not match"
		}), nil
	case Within:
		var ms int64
		if _, err := fmt.Sscan(c.Value, &ms); err != nil || ms <= 0 {
			return nil, fmt.Errorf("a within_ms check needs a number of milliseconds above zero")
		}
		return saigeeval.Named(saigeeval.RespondsWithinScorer(time.Duration(ms)*time.Millisecond), name), nil
	}
	return nil, fmt.Errorf("unknown check %q", c.Kind)
}

// ScoreAnswer checks an agent's answer against a case.
func ScoreAnswer(ctx context.Context, c Case, output string, took time.Duration) (Result, error) {
	res := Result{CaseID: c.ID, Input: c.Input, Output: output, DurationMs: took.Milliseconds()}
	scorers := make([]saigeeval.Scorer, 0, len(c.Checks))
	for _, ch := range c.Checks {
		s, err := scorerOf(ch)
		if err != nil {
			return res, fmt.Errorf("%w: %w", ErrInvalid, err)
		}
		scorers = append(scorers, s)
	}
	in, _ := json.Marshal(c.Input)
	out, _ := json.Marshal(output)
	suite, err := saigeeval.Run(ctx, c.ID, []saigeeval.Observation{{
		ID: c.ID, Input: in, Output: out,
		// saige declines a time check on an observation with no recorded
		// time, so a run faster than a millisecond is recorded as one.
		Timing: saigeeval.ObservationTiming{TotalMs: max(took.Milliseconds(), 1)},
	}}, scorers)
	if err != nil {
		return res, err
	}
	for _, r := range suite.Results {
		for _, s := range r.Scores {
			// A check scores 1 or 0.
			ok := s.Error == "" && s.Value >= 1
			reason := s.Reason
			if s.Error != "" {
				reason = s.Error
			}
			res.Verdicts = append(res.Verdicts, Verdict{Check: s.Name, Passed: ok, Reason: reason})
		}
	}
	// Every check must have reported and passed. A check that went missing is
	// a failure, never a silent pass.
	res.Passed = len(res.Verdicts) == len(c.Checks)
	for _, v := range res.Verdicts {
		res.Passed = res.Passed && v.Passed
	}
	return res, nil
}

// Retrieved is one document a knowledge search returned, best first.
type Retrieved struct {
	Title string
	Score float64
}

// ScoreRetrieval checks what a source returned for a case. It returns the
// result and the retrieval scores saige computed for it.
func ScoreRetrieval(c Case, got []Retrieved, k int, took time.Duration) (Result, rageval.RetrievalScores) {
	if k <= 0 {
		k = DefaultK
	}
	res := Result{CaseID: c.ID, Input: c.Input, DurationMs: took.Milliseconds()}
	// A document is identified by its title, lowercased, on both sides.
	key := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	want := make([]string, len(c.Expect))
	for i, e := range c.Expect {
		want[i] = key(e)
	}
	hits := make([]ragtypes.SearchHit, 0, len(got))
	seen := map[string]bool{}
	for _, g := range got {
		id := key(g.Title)
		hits = append(hits, ragtypes.SearchHit{Score: g.Score, Provenance: ragtypes.Provenance{DocumentUUID: id}})
		if seen[id] {
			continue
		}
		seen[id] = true
		if len(res.Hits) < k {
			expected := slices.Contains(want, id)
			res.Hits = append(res.Hits, Hit{Title: g.Title, Score: g.Score, Expected: expected})
			if expected && res.Rank == 0 {
				res.Rank = len(res.Hits)
			}
		}
	}
	scores, _ := rageval.ScoreRetrieval(hits, want, rageval.RelevanceDocument, k)
	res.Passed = scores.HitRate > 0
	return res, scores
}

// Finish closes a run from its results.
func (r *Run) Finish(results []Result, metrics map[string]float64, at time.Time) {
	r.Results, r.Metrics, r.EndedAt = results, metrics, at
	r.Total = len(results)
	r.Passed = 0
	for _, res := range results {
		if res.Passed {
			r.Passed++
		}
	}
	if r.Total > 0 {
		r.PassRate = 100 * float64(r.Passed) / float64(r.Total)
	}
	r.Status = Failed
	if r.Total > 0 && r.PassRate >= r.Threshold {
		r.Status = Passed
	}
}

// Store keeps every tenant's suites and runs in a state.Store.
type Store struct {
	kept *state.Table[stored]
	// now is the clock a run's heartbeat is read and written by.
	now func() time.Time
}

// namespace is where suites and runs are kept. The tenant is the shard key;
// within it:
//
//	next                 the number of the tenant's last run
//	suite/<name>         a suite
//	run/<suite>/<n>      a run of a suite, numbered
//
// A suite's name holds no "/". At most maxRuns runs of a suite are kept:
// beginning one drops the oldest past that.
const namespace = "evals"

// stored is one record: a suite, a run, a tenant's run counter, or a suite's
// guard, which holds the number of the suite's newest run in Next.
type stored struct {
	Suite *Suite `json:"suite,omitempty"`
	Run   *Run   `json:"run,omitempty"`
	Next  int    `json:"next,omitempty"`
	// Beat is when a run was last written by what carries it out.
	Beat time.Time `json:"beat,omitzero"`
}

const (
	nextKey     = "next"
	suitePrefix = "suite/"
	runPrefix   = "run/"
	openPrefix  = "open/"
	// maxSuites is the most suites a tenant may create.
	maxSuites = 200
	// staleAfter is how long a running run may go unwritten before it is
	// read as interrupted. What carries a run out writes it after every
	// case, and a case is bounded well under this.
	staleAfter = 10 * time.Minute
)

func suiteKey(name string) string { return suitePrefix + name }

func suiteRuns(name string) string { return runPrefix + name + "/" }

// openKey is a suite's guard: every Begin writes it at the version it read
// before reading the suite's runs, so two Begins of one suite conflict on
// this one key and the one that loses reads again and sees the other's run.
func openKey(name string) string { return openPrefix + name }

// runKey sorts a suite's runs in the order they were numbered.
func runKey(run Run) string {
	n, _ := strconv.ParseInt(strings.TrimPrefix(run.ID, "ev-"), 10, 64)
	return suiteRuns(run.Suite) + state.Seq(n)
}

// Interrupted is the error of a run that was still going when the process
// that ran it stopped.
const Interrupted = "the server stopped while this run was going"

// NewStore returns an empty store in this process's memory.
func NewStore() *Store { s, _ := Open(nil); return s }

// Open returns a store that keeps its suites and runs in st, which may be
// shared with other processes. A nil st keeps them in this process's memory.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	return &Store{kept: state.NewTable[stored](st, namespace), now: time.Now}, nil
}

// stale reports whether a running run has gone unwritten so long that
// nothing is carrying it out any more, as when the process that ran it
// stopped.
func (s *Store) stale(r stored) bool {
	return r.Run.Status == Running && s.now().Sub(r.Beat) > staleAfter
}

// shown is a run as it is read: a stale run is interrupted.
func (s *Store) shown(r stored) Run {
	run := *r.Run
	if s.stale(r) {
		run.Status, run.Error = Errored, Interrupted
	}
	return run
}

func wrap(err error) error {
	if err == nil || errors.Is(err, ErrInvalid) || errors.Is(err, ErrExists) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrBusy) {
		return err
	}
	return fmt.Errorf("evals: write: %w", err)
}

// Put creates a suite, or replaces one when replace is set.
func (s *Store) Put(tenantName string, suite Suite, replace bool) error {
	if err := suite.Validate(); err != nil {
		return err
	}
	if !replace {
		have, _, err := s.kept.List(tenantName, state.Query{Prefix: suitePrefix, Limit: maxSuites})
		if err != nil {
			return fmt.Errorf("evals: read: %w", err)
		}
		if len(have) >= maxSuites && !slices.ContainsFunc(have, func(it state.Item[stored]) bool { return it.Key == suiteKey(suite.Name) }) {
			return fmt.Errorf("%w: a workspace has at most %d suites", ErrInvalid, maxSuites)
		}
	}
	_, err := s.kept.Update(tenantName, suiteKey(suite.Name), func(v stored, exists bool) (stored, error) {
		exists = exists && v.Suite != nil
		switch {
		case exists && !replace:
			return v, fmt.Errorf("%w: %q", ErrExists, suite.Name)
		case !exists && replace:
			return v, fmt.Errorf("%w: suite %q", ErrNotFound, suite.Name)
		case exists && (v.Suite.Target != suite.Target || v.Suite.Subject != suite.Subject):
			// History compares like with like, so a suite keeps its target.
			return v, fmt.Errorf("%w: a suite cannot change what it tests", ErrInvalid)
		}
		return stored{Suite: &suite}, nil
	})
	return wrap(err)
}

// lookup reads a suite and its version.
func (s *Store) lookup(tenantName, name string) (Suite, int64, error) {
	cur, err := s.kept.Lookup(tenantName, suiteKey(name))
	if errors.Is(err, state.ErrNotFound) || err == nil && cur.Value.Suite == nil {
		return Suite{}, 0, fmt.Errorf("%w: suite %q", ErrNotFound, name)
	}
	if err != nil {
		return Suite{}, 0, fmt.Errorf("evals: read: %w", err)
	}
	return *cur.Value.Suite, cur.Version, nil
}

// Get returns one suite.
func (s *Store) Get(tenantName, name string) (Suite, error) {
	suite, _, err := s.lookup(tenantName, name)
	return suite, err
}

// List returns a tenant's suites by name, at most state.MaxList.
func (s *Store) List(tenantName string) ([]Suite, error) {
	items, _, err := s.kept.List(tenantName, state.Query{Prefix: suitePrefix, Limit: state.MaxList})
	if err != nil {
		return nil, fmt.Errorf("evals: read: %w", err)
	}
	out := make([]Suite, 0, len(items))
	for _, it := range items {
		if it.Value.Suite != nil {
			out = append(out, *it.Value.Suite)
		}
	}
	return out, nil
}

// counter adds to b a write of the tenant's run counter at the version read,
// so a Begin and a Delete in one tenant cannot pass each other.
func (s *Store) counter(b *state.Batch[stored], tenantName string, bump int) (int, error) {
	cur, err := s.kept.Lookup(tenantName, nextKey)
	switch {
	case errors.Is(err, state.ErrNotFound):
		b.Create(tenantName, nextKey, stored{Next: bump})
		return bump, nil
	case err != nil:
		return 0, err
	}
	next := cur.Value.Next + bump
	b.PutAt(tenantName, nextKey, stored{Next: next}, cur.Version)
	return next, nil
}

// runsOf reads a suite's runs, newest first, at most limit.
func (s *Store) runsOf(tenantName, name string, limit int) ([]state.Item[stored], error) {
	items, _, err := s.kept.List(tenantName, state.Query{Prefix: suiteRuns(name), Desc: true, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("evals: read: %w", err)
	}
	return slices.DeleteFunc(items, func(it state.Item[stored]) bool { return it.Value.Run == nil }), nil
}

// Delete removes a suite and its runs.
func (s *Store) Delete(tenantName, name string) error {
	err := state.Retry(func() error {
		_, version, err := s.lookup(tenantName, name)
		if err != nil {
			return err
		}
		b := s.kept.Batch()
		if _, err := s.counter(b, tenantName, 0); err != nil {
			return err
		}
		runs, err := s.runsOf(tenantName, name, state.MaxList)
		if err != nil {
			return err
		}
		b.DeleteAt(tenantName, suiteKey(name), version)
		switch guard, err := s.kept.Lookup(tenantName, openKey(name)); {
		case err == nil:
			b.DeleteAt(tenantName, openKey(name), guard.Version)
		case !errors.Is(err, state.ErrNotFound):
			return fmt.Errorf("evals: read: %w", err)
		}
		for _, it := range runs {
			b.DeleteAt(tenantName, it.Key, it.Version)
		}
		return b.Commit()
	})
	return wrap(err)
}

// Begin opens a run of a suite. Only one run of a suite is open at a time, so
// two people pressing Run do not double the load or interleave their results.
// A run left open by a process that stopped is closed as interrupted.
func (s *Store) Begin(tenantName string, suite Suite, rev int, by string, at time.Time) (Run, error) {
	var out Run
	err := state.Retry(func() error {
		if _, _, err := s.lookup(tenantName, suite.Name); err != nil {
			return err
		}
		// The guard is read before the runs: a Begin that commits after this
		// read moves the guard and this one's write of it conflicts, and one
		// that committed before it is seen in the runs read next.
		guard, err := s.kept.Lookup(tenantName, openKey(suite.Name))
		if err != nil && !errors.Is(err, state.ErrNotFound) {
			return fmt.Errorf("evals: read: %w", err)
		}
		b := s.kept.Batch()
		runs, err := s.runsOf(tenantName, suite.Name, state.MaxList)
		if err != nil {
			return err
		}
		if len(runs) > 0 && runs[0].Value.Run.Status == Running {
			if !s.stale(runs[0].Value) {
				return fmt.Errorf("%w: %q", ErrBusy, suite.Name)
			}
			left := s.shown(runs[0].Value)
			b.PutAt(tenantName, runs[0].Key, stored{Run: &left}, runs[0].Version)
		}
		next, err := s.counter(b, tenantName, 1)
		if err != nil {
			return err
		}
		if guard.Version == 0 {
			b.Create(tenantName, openKey(suite.Name), stored{Next: next})
		} else {
			b.PutAt(tenantName, openKey(suite.Name), stored{Next: next}, guard.Version)
		}
		run := Run{
			ID: fmt.Sprintf("ev-%d", next), Suite: suite.Name, Target: suite.Target, Subject: suite.Subject,
			Rev: rev, Status: Running, Total: len(suite.Cases), Threshold: suite.Threshold(),
			Results: []Result{}, StartedBy: by, StartedAt: at,
		}
		b.Create(tenantName, runKey(run), stored{Run: &run, Beat: s.now()})
		if len(runs) >= maxRuns {
			for _, old := range runs[maxRuns-1:] {
				b.DeleteAt(tenantName, old.Key, old.Version)
			}
		}
		if err := b.Commit(); err != nil {
			return err
		}
		out = run
		return nil
	})
	if err != nil {
		return Run{}, wrap(err)
	}
	return out, nil
}

// Save stores a run's latest state. A run no longer kept is not written.
func (s *Store) Save(tenantName string, run Run) error {
	run.Results = slices.Clone(run.Results)
	_, err := s.kept.Update(tenantName, runKey(run), func(v stored, ok bool) (stored, error) {
		if !ok || v.Run == nil || v.Run.ID != run.ID {
			return v, state.ErrUnchanged
		}
		return stored{Run: &run, Beat: s.now()}, nil
	})
	return wrap(err)
}

// Runs returns a suite's runs, newest first.
func (s *Store) Runs(tenantName, name string) ([]Run, error) {
	if _, _, err := s.lookup(tenantName, name); err != nil {
		return nil, err
	}
	runs, err := s.runsOf(tenantName, name, maxRuns)
	if err != nil {
		return nil, err
	}
	out := make([]Run, 0, len(runs))
	for _, it := range runs {
		out = append(out, s.shown(it.Value))
	}
	return out, nil
}
