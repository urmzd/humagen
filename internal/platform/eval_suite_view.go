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

// EvalSuiteSourcesVisible also guards evaluation results: an answer may quote
// recorded passages even though EvalSuiteView omits the snapshot itself.
func (p *Platform) EvalSuiteSourcesVisible(who auth.Principal, suite evals.Suite) bool {
	for _, c := range suite.Cases {
		if c.Trace == nil {
			continue
		}
		if !p.Seer(who).RunID(c.Trace.Run) {
			return false
		}
		if c.Trace.Retrieval == nil {
			continue
		}
		if !who.Can(auth.KnowledgeRead) {
			return false
		}
		for _, source := range c.Trace.Retrieval.Sources {
			if !p.SourceVisible(who, source) {
				return false
			}
		}
		proofs, err := p.SourceEvidence.ForRun(who.Tenant, c.Trace.Run)
		if err != nil || len(c.Trace.Retrieval.Blocks) > 0 && len(proofs) == 0 {
			return false
		}
		for _, proof := range proofs {
			if !p.citationEvidenceVisible(who, proof) {
				return false
			}
		}
	}
	return true
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
