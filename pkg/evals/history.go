package evals

import "slices"

// ReplaySource is the access dependency of one recorded case. It deliberately
// retains no tool output or retrieved text. Current authority is checked by the
// host each time a historical result is read.
type ReplaySource struct {
	Run              string   `json:"run"`
	Retrieval        bool     `json:"retrieval,omitempty"`
	Sources          []string `json:"sources,omitempty"`
	RequiresEvidence bool     `json:"requires_evidence,omitempty"`
}

// RunAccess belongs to the run's original execution snapshot. A non-nil value
// with no replay sources identifies a plain run; nil means unknown provenance.
type RunAccess struct {
	Replay []ReplaySource `json:"replay"`
}

// RunRecord combines a result and its retained access dependencies in one read.
// It is an authorization input for hosts, not a public response type.
type RunRecord struct {
	Run    Run
	Access *RunAccess
}

func accessFor(suite Suite) *RunAccess {
	access := &RunAccess{Replay: []ReplaySource{}}
	for _, c := range suite.Cases {
		if c.Trace == nil {
			continue
		}
		source := ReplaySource{Run: c.Trace.Run}
		if r := c.Trace.Retrieval; r != nil {
			source.Retrieval = true
			source.Sources = slices.Clone(r.Sources)
			source.RequiresEvidence = len(r.Blocks) > 0
		}
		access.Replay = append(access.Replay, source)
	}
	return access
}

// History returns at most maxRuns records, newest first, with the access
// dependencies captured atomically by Begin from the actual execution suite.
// Hosts must authorize these dependencies before returning results to a reader.
func (s *Store) History(tenantName, name string) ([]RunRecord, error) {
	if _, _, err := s.lookup(tenantName, name); err != nil {
		return nil, err
	}
	runs, err := s.runsOf(tenantName, name, maxRuns)
	if err != nil {
		return nil, err
	}
	out := make([]RunRecord, 0, len(runs))
	for _, it := range runs {
		out = append(out, RunRecord{Run: s.shown(it.Value), Access: it.Value.Access})
	}
	return out, nil
}
