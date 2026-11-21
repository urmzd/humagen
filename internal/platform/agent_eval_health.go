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
	// Flagged counts the runs rated down with a reason that wait in the
	// review queue, of those the caller may read, up to MaxFlagged;
	// FlaggedMore says there may be more. Both are left out for a caller
	// who may not read the queue (evals:read).
	Flagged     int  `json:"flagged,omitempty"`
	FlaggedMore bool `json:"flagged_more,omitempty"`
}

// MaxFlagged bounds the flagged count on agent health: it reads at most one
// candidate past it, and each candidate's run once, to keep the queue's
// read rule without listing the whole queue on every refresh.
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

// countFlagged counts an agent's open candidates the caller may read, as the
// review queue shows them, reading at most MaxFlagged+1 candidates. The count
// is of those read; more says the queue holds candidates past them.
func (p *Platform) countFlagged(who auth.Principal, agent string) (n int, more bool, err error) {
	candidates, err := p.Evals.Candidates(who.Tenant, agent, MaxFlagged+1)
	if err != nil {
		return 0, false, err
	}
	if len(candidates) > MaxFlagged {
		candidates, more = candidates[:MaxFlagged], true
	}
	seer := p.Seer(who)
	for _, c := range candidates {
		if run, err := p.Runs.Get(who.Tenant, c.Run); err == nil && seer.Run(run) {
			n++
		}
	}
	return n, more, nil
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
