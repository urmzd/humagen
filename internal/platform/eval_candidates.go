package platform

import (
	"encoding/base64"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/evals"
)

// MaxEvalCandidatePage is the most candidates one page of the review queue
// holds.
const MaxEvalCandidatePage = 200

// EvalCandidatePage is one page of the review queue and the cursor of the
// page that follows, empty on the last.
type EvalCandidatePage struct {
	Candidates []evals.Candidate `json:"candidates"`
	Next       string            `json:"next"`
}

// openEvalCandidates is the undecided candidates who may read, newest first
// by when the rating last changed and then by id. A candidate is shown only
// to a reader who sees its agent and the run it came from, as the run is
// filed now; one whose run is gone is shown to nobody.
func (p *Platform) openEvalCandidates(who auth.Principal, agent string) ([]evals.Candidate, error) {
	all, err := p.Evals.OpenCandidates(who.Tenant, agent)
	if err != nil {
		return nil, err
	}
	seer := p.Seer(who)
	out := []evals.Candidate{}
	for _, c := range all {
		if !seer.Agent(c.Agent) {
			continue
		}
		if run, err := p.Runs.Get(who.Tenant, c.Run); err == nil && seer.Run(run) {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b evals.Candidate) int {
		if n := b.At.Compare(a.At); n != 0 {
			return n
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

// ListEvalCandidates is a page of the review queue: at most limit (1 to
// MaxEvalCandidatePage) candidates after the place the previous page's
// cursor names. A cursor is the place of the last candidate on a page, when
// its rating last changed and its id, so the next page is every candidate
// strictly after that place whatever was decided or added meanwhile.
func (p *Platform) ListEvalCandidates(who auth.Principal, agent string, limit int, after string) (EvalCandidatePage, error) {
	if limit < 1 || limit > MaxEvalCandidatePage {
		return EvalCandidatePage{}, fmt.Errorf("%w: limit is 1 to %d", evals.ErrInvalid, MaxEvalCandidatePage)
	}
	start := 0
	var at int64
	var id string
	if after != "" {
		var err error
		if at, id, err = decodeThreadCursor(after); err != nil {
			return EvalCandidatePage{}, fmt.Errorf("%w: after is not the next of a page of candidates", evals.ErrInvalid)
		}
	}
	all, err := p.openEvalCandidates(who, agent)
	if err != nil {
		return EvalCandidatePage{}, err
	}
	if after != "" {
		start = len(all)
		if i := slices.IndexFunc(all, func(c evals.Candidate) bool {
			changed := c.At.UnixNano()
			return changed < at || (changed == at && c.ID > id)
		}); i >= 0 {
			start = i
		}
	}
	end := min(start+limit, len(all))
	out := EvalCandidatePage{Candidates: all[start:end]}
	if end < len(all) {
		last := all[end-1]
		out.Next = base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(last.At.UnixNano(), 10) + ":" + last.ID))
	}
	return out, nil
}

// EvalCandidate is one undecided candidate who may read. One that does not
// exist, one already decided and one the reader may not see all answer
// evals.ErrNotFound in the same words.
func (p *Platform) EvalCandidate(who auth.Principal, id string) (evals.Candidate, error) {
	all, err := p.openEvalCandidates(who, "")
	if err != nil {
		return evals.Candidate{}, err
	}
	for _, c := range all {
		if c.ID == id {
			return c, nil
		}
	}
	return evals.Candidate{}, fmt.Errorf("%w: candidate", evals.ErrNotFound)
}

// evalCandidateCase is the id of the case a candidate becomes.
func evalCandidateCase(c evals.Candidate) string { return "feedback-" + c.ID[:min(12, len(c.ID))] }

// EvalCandidateSuite is the suite named name with c added as a new case
// holding the question that was asked and the reviewer's checks. Nothing
// else of the run is copied: not its answer, not the rating's reason or
// comment. The suite must be one the reviewer sees that tests the
// candidate's agent, and changing it takes what replacing it does (MayTry,
// MergeEvalSuiteReplay). added is false when the suite already holds the
// case, as after an accept whose decision was not recorded.
func (p *Platform) EvalCandidateSuite(who auth.Principal, c evals.Candidate, name string, checks []evals.Check) (suite evals.Suite, added bool, err error) {
	if len(checks) == 0 {
		return evals.Suite{}, false, fmt.Errorf("%w: a case from feedback needs at least one check", evals.ErrInvalid)
	}
	suite, err = p.Evals.Get(who.Tenant, name)
	if err != nil || !p.Seer(who).Suite(suite) {
		return evals.Suite{}, false, fmt.Errorf("%w: suite %q", evals.ErrNotFound, name)
	}
	if suite.Target != evals.Agent || suite.Subject != c.Agent {
		return evals.Suite{}, false, fmt.Errorf("%w: the suite does not test the agent that answered", evals.ErrInvalid)
	}
	if err := p.MayTry(who, suite.Subject); err != nil {
		return evals.Suite{}, false, err
	}
	id := evalCandidateCase(c)
	if slices.ContainsFunc(suite.Cases, func(have evals.Case) bool { return have.ID == id }) {
		return suite, false, nil
	}
	suite.Cases = append(slices.Clone(suite.Cases), evals.Case{ID: id, Input: c.Input, Checks: checks})
	suite, err = p.MergeEvalSuiteReplay(who, suite, true)
	if err != nil {
		return evals.Suite{}, false, err
	}
	// The reviewer answers for the suite now, as anyone who replaces it does.
	suite.CreatedBy, suite.UpdatedAt = who.Subject, p.now()
	return suite, true, nil
}

// AcceptEvalCandidate saves the suite EvalCandidateSuite built and takes the
// candidate out of the queue.
func (p *Platform) AcceptEvalCandidate(who auth.Principal, c evals.Candidate, suite evals.Suite, added bool) error {
	if added {
		if err := p.Evals.Put(who.Tenant, suite, true); err != nil {
			return err
		}
	}
	return p.Evals.ResolveCandidate(who.Tenant, c, evals.Resolution{Decision: "accepted", Suite: suite.Name, By: who.Subject, At: p.now()})
}

// DismissEvalCandidate takes a candidate out of the queue without a case.
func (p *Platform) DismissEvalCandidate(who auth.Principal, c evals.Candidate) error {
	return p.Evals.ResolveCandidate(who.Tenant, c, evals.Resolution{Decision: "dismissed", By: who.Subject, At: p.now()})
}
