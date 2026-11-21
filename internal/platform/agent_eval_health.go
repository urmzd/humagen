package platform

import (
	"encoding/json"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/operations"
	"slices"
)

// AgentEvaluationHealth is what the checks on an agent's recent runs say, of
// the runs the caller reads in their own right. See
// Platform.AgentEvaluationHealth for what is left out.
type AgentEvaluationHealth struct {
	Agent    string         `json:"agent"`
	Status   string         `json:"status"`
	Samples  int            `json:"samples"`
	Passed   int            `json:"passed"`
	PassRate float64        `json:"pass_rate"`
	Results  []evals.Online `json:"results"`
	// Flagged counts the distinct runs rated down with a reason that wait
	// in the review queue, of those the caller reads in their own right, up
	// to MaxFlagged. It is set, to zero when there are none, for a caller
	// who may read the queue (evals:read), and left out for one who may
	// not, so none and may-not-read are told apart.
	Flagged *int `json:"flagged,omitempty"`
	// FlaggedMore says the caller reads more than MaxFlagged such runs. It
	// is decided from the runs the caller reads only, so it never says
	// anything of candidates the caller cannot see.
	FlaggedMore bool `json:"flagged_more,omitempty"`
}

// MaxFlagged caps the flagged count on agent health; past it the count says
// there are more.
const MaxFlagged = 50

// maxHealthResults is how many of an agent's latest checked runs health
// reads.
const maxHealthResults = 100

// AgentEvaluationHealth answers from the runs the caller reads in their own
// right: an owner's, a participant's, a project member's, a public run's.
//
// A health refresh never uses break-glass. Health is a summary that loads
// with every visit to an agent and on every Refresh, so a run the caller
// could read only by exception (a governor's read of a private conversation
// they take no part in) is left out of the results, the samples and the
// flagged count alike, and the refresh writes no read-audit record. Such a
// run is decided on a holding seer and never disclosed. A governor who
// wants those runs opens the run or the review queue, which shows them and
// records the read.
func (p *Platform) AgentEvaluationHealth(who auth.Principal, name string) (AgentEvaluationHealth, error) {
	out := AgentEvaluationHealth{Agent: name, Status: "untested", Results: []evals.Online{}}
	if !p.Seer(who).Agent(name) {
		return out, operations.ErrNotFound
	}
	results, err := p.Evals.OnlineFor(who.Tenant, name, maxHealthResults)
	if err != nil {
		return out, err
	}
	ids := make([]string, 0, len(results))
	for _, res := range results {
		ids = append(ids, res.Run)
	}
	var flagged []string
	if who.Can(auth.EvalsRead) {
		if flagged, err = p.Evals.CandidateRuns(who.Tenant, name); err != nil {
			return out, err
		}
	}
	own, err := p.runsReadInOwnRight(who, append(ids, flagged...))
	if err != nil {
		return out, err
	}
	for _, res := range results {
		if !own[res.Run] {
			continue
		}
		out.Results = append(out.Results, res)
		out.Samples++
		if res.Passed {
			out.Passed++
		}
	}
	if who.Can(auth.EvalsRead) {
		n := 0
		counted := map[string]bool{}
		for _, id := range flagged {
			if own[id] && !counted[id] {
				counted[id] = true
				n++
			}
		}
		out.FlaggedMore = n > MaxFlagged
		n = min(n, MaxFlagged)
		out.Flagged = &n
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

// runsReadInOwnRight is which of some runs the caller reads without an
// exception. It writes no read-audit record: each run is decided on a
// holding seer, a run granted only by exception (break-glass) is left out,
// and nothing is disclosed. The runs are read in one batched read and their
// threads once; a run the tenant has none of is left out.
func (p *Platform) runsReadInOwnRight(who auth.Principal, runIDs []string) (map[string]bool, error) {
	ids := make([]string, 0, len(runIDs))
	seen := map[string]bool{}
	for _, id := range runIDs {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	own := map[string]bool{}
	if len(ids) == 0 {
		return own, nil
	}
	found, err := p.Runs.Many(who.Tenant, ids)
	if err != nil {
		return nil, err
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
		row := "health:" + id
		if seer.deciding(row, func() bool { return seer.Run(r) }) && !seer.exceptional(row) {
			own[id] = true
		}
	}
	return own, nil
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
