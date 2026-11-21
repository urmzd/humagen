package platform

import (
	"encoding/json"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/operations"
	"slices"
)

// AgentEvaluationHealth includes only runs readable by the caller.
type AgentEvaluationHealth struct {
	Agent    string         `json:"agent"`
	Status   string         `json:"status"`
	Samples  int            `json:"samples"`
	Passed   int            `json:"passed"`
	PassRate float64        `json:"pass_rate"`
	Results  []evals.Online `json:"results"`
	// Flagged counts the distinct runs rated down with a reason that wait
	// in the review queue, of those the caller may read, up to MaxFlagged;
	// FlaggedMore says there are more, or that the count stopped reading. Both are left out for a caller
	// who may not read the queue (evals:read).
	Flagged     int  `json:"flagged,omitempty"`
	FlaggedMore bool `json:"flagged_more,omitempty"`
}

// MaxFlagged caps the flagged count on agent health; past it the count says
// there are more.
const MaxFlagged = 50

func (p *Platform) AgentEvaluationHealth(who auth.Principal, name string) (AgentEvaluationHealth, error) {
	out := AgentEvaluationHealth{Agent: name, Status: "untested", Results: []evals.Online{}}
	seer := p.Seer(who)
	if !seer.Agent(name) {
		return out, operations.ErrNotFound
	}
	results, err := p.Evals.OnlineFor(who.Tenant, name, 100)
	if err != nil {
		return out, err
	}
	for _, res := range results {
		run, err := p.Runs.Get(who.Tenant, res.Run)
		if err != nil || !seer.Run(run) {
			continue
		}
		out.Results = append(out.Results, res)
		out.Samples++
		if res.Passed {
			out.Passed++
		}
	}
	if who.Can(auth.EvalsRead) {
		if out.Flagged, out.FlaggedMore, err = p.countFlagged(who, name); err != nil {
			return out, err
		}
	}
	if out.Samples > 0 {
		out.PassRate = 100 * float64(out.Passed) / float64(out.Samples)
		out.Status = "healthy"
		if out.Passed < out.Samples {
			out.Status = "needs_review"
		}
	}
	return out, nil
}

// countFlagged counts the distinct runs with an open candidate for an agent
// that the caller reads in their own right, from the window of candidates
// the review queue reads (evals.Store.CandidateRuns), so the two agree.
//
// A count is not a read of what it counts, so it writes no read-audit
// record: a run the caller sees only by exception (a governor's break-glass
// read of a private conversation) is decided on a holding seer, left out of
// the count and never disclosed. The review queue, which shows such a row's
// question, records the read when it shows it.
//
// It decodes only each candidate's run, reads the distinct runs in one
// batched read and their threads once. more says there are more than
// MaxFlagged, or that the queue holds candidates past the window, so the
// count is a lower bound.
func (p *Platform) countFlagged(who auth.Principal, agent string) (n int, more bool, err error) {
	runIDs, past, err := p.Evals.CandidateRuns(who.Tenant, agent)
	if err != nil {
		return 0, false, err
	}
	ids := make([]string, 0, len(runIDs))
	seen := map[string]bool{}
	for _, id := range runIDs {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	found, err := p.Runs.Many(who.Tenant, ids)
	if err != nil {
		return 0, false, err
	}
	seer := p.Seer(who).Snapshot().Holding()
	threads := make([]string, 0, len(found))
	for _, r := range found {
		threads = append(threads, placeOfRun(r).Thread)
	}
	seer.Read(threads...)
	for _, id := range ids {
		r, ok := found[id]
		if !ok {
			continue
		}
		row := "flagged:" + id
		if seer.deciding(row, func() bool { return seer.Run(r) }) && !seer.exceptional(row) {
			n++
		}
	}
	if n > MaxFlagged {
		return MaxFlagged, true, nil
	}
	return n, past, nil
}

func (p *Platform) AgentChangeScores(who auth.Principal, name string) ([]evals.Change, error) {
	if !p.Seer(who).Agent(name) {
		return nil, operations.ErrNotFound
	}
	rows, err := p.Evals.Changes(who.Tenant, name)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []evals.Change{}
	}
	// Configuration and case text can carry private source details. This public
	// scorecard keeps counts/verdicts; the authorized builder owns the full record.
	for i := range rows {
		if rows[i].Kind == "workspace_prompt" {
			rows[i] = p.sharedPromptCardView(who.Tenant, rows[i], p.Seer(who).Agent)
		}
		if rows[i].OverrideBy != "" {
			rows[i].OverrideBy = p.Names(who.Tenant).Key(rows[i].OverrideBy)
		}
		rows[i].Candidate = json.RawMessage(`{}`)
		for j := range rows[i].Scores {
			rows[i].Scores[j].Results = []evals.Result{}
		}
	}
	slices.SortFunc(rows, func(a, b evals.Change) int { return b.At.Compare(a.At) })
	return rows, nil
}
