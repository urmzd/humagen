package runs

import (
	"errors"
	"fmt"
	"time"
)

// MaxSummaryProjects bounds the projects one summary may name: at most a
// tenant's whole tree.
const MaxSummaryProjects = 500

// ErrSummary reports a summary the index will not compute.
var ErrSummary = errors.New("runs: invalid summary")

// SummaryQuery says which of a tenant's runs a summary counts.
type SummaryQuery struct {
	// Since, Until and Bucket are Summarize's: runs that started in
	// [Since, Until), counted in buckets widened to at most MaxBuckets.
	Since, Until time.Time
	Bucket       time.Duration
	// Projects are the projects whose runs count, "" being the root. A run
	// counts under the project it is filed in, and a run another run called
	// is filed with its root. A query that names none counts nothing, unless
	// AllProjects is set.
	Projects []string
	// AllProjects counts runs wherever they are filed. It cannot be set with
	// Projects.
	AllProjects bool
	// Keep, when set, is asked about each run the rest of the query counts,
	// and the runs it refuses are left out: those a reader may not see. It
	// is given a copy, under the index's read lock, so it must not call the
	// index.
	Keep func(Run) bool
}

// SummarizeWhere is Summarize over the runs a query keeps.
func (x *Index) SummarizeWhere(tenant string, q SummaryQuery) (Summary, error) {
	switch {
	case len(q.Projects) > MaxSummaryProjects:
		return Summary{}, fmt.Errorf("%w: a summary names at most %d projects", ErrSummary, MaxSummaryProjects)
	case q.AllProjects && len(q.Projects) > 0:
		return Summary{}, fmt.Errorf("%w: name projects or count all of them, not both", ErrSummary)
	}
	in := make(map[string]bool, len(q.Projects))
	for _, name := range q.Projects {
		in[name] = true
	}
	keep := func(r *Run) bool {
		if !q.AllProjects && !in[r.Project] {
			return false
		}
		return q.Keep == nil || q.Keep(*r)
	}
	return x.summarize(tenant, q.Since, q.Until, q.Bucket, keep), nil
}
