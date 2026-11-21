package platform

import (
	"encoding/json"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/state"
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

// maxFlaggedPages bounds what one count reads: this many pages of
// state.MaxList candidates. Past it the count is a lower bound and says so.
const maxFlaggedPages = 10

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

// countFlagged counts the distinct runs the caller may read that have an
// open candidate for an agent, as the review queue shows them. It pages
// through the queue with its cursor, reads each page's new runs in one
// batched read and their threads once, and stops when more than MaxFlagged
// visible runs are counted or the queue ends. Both the count and whether
// there are more come from visible runs only, except past maxFlaggedPages,
// where the count is what was found so far and more says the rest is
// unread.
func (p *Platform) countFlagged(who auth.Principal, agent string) (n int, more bool, err error) {
	seer := p.Seer(who).Snapshot()
	decided := map[string]bool{}
	visible := map[string]bool{}
	after := ""
	for range maxFlaggedPages {
		page, next, err := p.Evals.CandidatePage(who.Tenant, agent, after, state.MaxList)
		if err != nil {
			return 0, false, err
		}
		ids := make([]string, 0, len(page))
		for _, c := range page {
			if !decided[c.Run] && !slices.Contains(ids, c.Run) {
				ids = append(ids, c.Run)
			}
		}
		if len(ids) > 0 {
			found, err := p.Runs.Many(who.Tenant, ids)
			if err != nil {
				return 0, false, err
			}
			threads := make([]string, 0, len(found))
			for _, r := range found {
				threads = append(threads, placeOfRun(r).Thread)
			}
			seer.Read(threads...)
			for _, id := range ids {
				r, ok := found[id]
				decided[id] = true
				if ok && seer.Run(r) {
					visible[id] = true
				}
			}
		}
		if len(visible) > MaxFlagged {
			return MaxFlagged, true, nil
		}
		if next == "" {
			return len(visible), false, nil
		}
		after = next
	}
	return len(visible), true, nil
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
