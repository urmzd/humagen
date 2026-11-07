package platform

import (
	"fmt"
	"slices"

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
	records, err := p.Evals.History(who.Tenant, name)
	if err != nil {
		return nil, err
	}
	out := make([]evals.Run, 0, len(records))
	for _, record := range records {
		if p.evalRunVisible(who, record) {
			out = append(out, record.Run)
		}
	}
	return out, nil
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
