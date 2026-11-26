package platform

import (
	"fmt"
	"slices"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/evals"
)

// EvalSuiteView keeps retained retrieval bytes in the execution store. A suite
// reader is not automatically authorized to read its source corpus or bindings.
// Clone every modified level so projection never damages the replay snapshot.
func (p *Platform) EvalSuiteView(suite evals.Suite) evals.Suite {
	suite.Cases = slices.Clone(suite.Cases)
	for i, c := range suite.Cases {
		if c.Trace == nil {
			continue
		}
		trace := *c.Trace
		trace.Retrieval = nil
		suite.Cases[i].Trace = &trace
	}
	return suite
}

// EvalSuiteSourcesVisible guards the current case definitions. Historical
// answers use their own retained dependencies in EvalRunsView.
func (p *Platform) EvalSuiteSourcesVisible(who auth.Principal, suite evals.Suite) bool {
	for _, c := range suite.Cases {
		if c.Trace == nil {
			continue
		}
		source := evals.ReplaySource{Run: c.Trace.Run}
		if r := c.Trace.Retrieval; r != nil {
			source.Retrieval, source.Sources, source.RequiresEvidence = true, r.Sources, len(r.Blocks) > 0
		}
		if !p.evalReplaySourceVisible(who, source) {
			return false
		}
	}
	return true
}

func (p *Platform) evalReplaySourceVisible(who auth.Principal, source evals.ReplaySource) bool {
	if source.Run == "" || !p.Seer(who).RunID(source.Run) {
		return false
	}
	if !source.Retrieval {
		return true
	}
	if !who.Can(auth.KnowledgeRead) || len(source.Sources) == 0 {
		return false
	}
	for _, name := range source.Sources {
		if !p.SourceVisible(who, name) {
			return false
		}
	}
	proofs, err := p.SourceEvidence.ForRun(who.Tenant, source.Run)
	if err != nil || source.RequiresEvidence && len(proofs) == 0 {
		return false
	}
	for _, proof := range proofs {
		if !p.citationEvidenceVisible(who, proof) {
			return false
		}
	}
	return true
}

func (p *Platform) evalRunVisible(who auth.Principal, record evals.RunRecord) bool {
	if record.Access == nil || !p.Seer(who).Suite(evals.Suite{Target: record.Run.Target, Subject: record.Run.Subject}) {
		return false
	}
	for _, source := range record.Access.Replay {
		if !p.evalReplaySourceVisible(who, source) {
			return false
		}
	}
	return true
}

// EvalRunsView authorizes each result against its original execution sources,
// even after the current suite removes or replaces a recorded case. Old rows
// without retained provenance remain hidden because it cannot be reconstructed
// safely from a mutable suite. Newly recorded plain runs have known empty access.
func (p *Platform) EvalRunsView(who auth.Principal, name string) ([]evals.Run, error) {
	suite, err := p.Evals.Get(who.Tenant, name)
	if err != nil {
		return nil, err
	}
	if !p.Seer(who).Suite(suite) {
		return nil, auth.ErrForbidden
	}
	runs, _, err := p.EvalSuiteListing(who, suite)
	return runs, err
}

// EvalSchedule is when a suite's schedule next starts it, as runDueEvals
// decides it. Busy means a run of the suite is going: the loop skips the
// suite until it ends, so the next run comes after it.
type EvalSchedule struct {
	Scheduled bool
	Next      time.Time
	Busy      bool
}

// evalSchedule reads a schedule from the runs given. A due suite starts
// within a minute, so its next run is no earlier than now.
func (p *Platform) evalSchedule(suite evals.Suite, runs []evals.Run) EvalSchedule {
	next, scheduled := evals.NextRun(suite, runs)
	if !scheduled {
		return EvalSchedule{}
	}
	if now := p.now(); next.Before(now) {
		next = now
	}
	busy := slices.ContainsFunc(runs, func(r evals.Run) bool { return r.Status == evals.Running })
	return EvalSchedule{Scheduled: true, Next: next, Busy: busy}
}

// EvalSuiteListing is a suite's runs the caller may read and its schedule,
// from one read of its history. The caller has already checked the suite is
// theirs to see.
//
// The schedule a reader is shown comes from the runs they may read only:
// a run they may not see (a replay of a private run, say) moves neither the
// next run nor busy for them, so neither says such a run exists. For a reader
// who sees every run it is what runDueEvals decides; for another it may say
// the next run is due when the loop is waiting on a run they cannot see.
func (p *Platform) EvalSuiteListing(who auth.Principal, suite evals.Suite) ([]evals.Run, EvalSchedule, error) {
	records, err := p.Evals.History(who.Tenant, suite.Name)
	if err != nil {
		return nil, EvalSchedule{}, err
	}
	out := make([]evals.Run, 0, len(records))
	for _, record := range records {
		if p.evalRunVisible(who, record) {
			out = append(out, record.Run)
		}
	}
	return out, p.evalSchedule(suite, out), nil
}

// MergeEvalSuiteReplay preserves server-owned snapshots on a public editing
// roundtrip. A client cannot manufacture source evidence or bind it to a new
// question. A changed recorded case must be captured again by the builder.
func (p *Platform) MergeEvalSuiteReplay(who auth.Principal, suite evals.Suite, replace bool) (evals.Suite, error) {
	for _, c := range suite.Cases {
		if c.Trace != nil && !p.Seer(who).RunID(c.Trace.Run) {
			return suite, auth.ErrForbidden
		}
		if c.Trace != nil && c.Trace.Retrieval != nil {
			return suite, auth.ErrForbidden
		}
	}
	if !replace {
		return suite, nil
	}
	old, err := p.Evals.Get(who.Tenant, suite.Name)
	if err != nil {
		return suite, err
	}
	if !p.EvalSuiteSourcesVisible(who, old) {
		return suite, auth.ErrForbidden
	}
	suite.Cases = slices.Clone(suite.Cases)
	for i, c := range suite.Cases {
		if c.Trace == nil {
			continue
		}
		before := slices.IndexFunc(old.Cases, func(x evals.Case) bool { return x.ID == c.ID })
		if before < 0 || old.Cases[before].Trace == nil || old.Cases[before].Trace.Retrieval == nil {
			continue
		}
		previous := old.Cases[before]
		if c.Input != previous.Input || c.Trace.Run != previous.Trace.Run {
			return suite, fmt.Errorf("%w: capture changed recorded input again", evals.ErrInvalid)
		}
		trace := *c.Trace
		snapshot := previous.Trace.Retrieval.Seal()
		trace.Retrieval = &snapshot
		suite.Cases[i].Trace = &trace
	}
	return suite, nil
}
