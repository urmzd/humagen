package runs

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

// ProjectCount is what a project holds in the index: the conversations filed
// in it, how many of their runs are in flight, and when any of them last
// changed. A conversation is filed where its first run was, as its
// ThreadSummary says, so a follow-up never moves it.
//
// The index keeps one per project as runs are recorded and change, in the
// same write, so reading them is a lookup and not a scan of every run.
type ProjectCount struct {
	// Threads is how many conversations are filed in the project.
	Threads int
	// Active is how many of their runs have not ended, calls included.
	Active int
	// ActiveAt is when a run in one of them last changed.
	ActiveAt time.Time
}

// ProjectCounts returns what each of a tenant's projects holds, by name. ""
// is the conversations filed at the root. A project with nothing filed in it
// is absent. A deleted project's conversations keep its name, so they are
// counted under it until it is created again.
func (x *Index) ProjectCounts(tenant string) map[string]ProjectCount {
	out, err := x.projectCounts(tenant)
	if err != nil {
		x.log.Error("runs: could not read project counts", "tenant", tenant, "error", err)
	}
	return out
}

// maxProjects bounds the projects one tenant's counts are read for. The
// project tree's own caps keep a tenant far below it.
const maxProjects = 100000

// projectCounts reads every project's count.
func (x *Index) projectCounts(tenant string) (map[string]ProjectCount, error) {
	out := map[string]ProjectCount{}
	after := ""
	for len(out) < maxProjects {
		recs, next, err := x.page(tenant, state.Query{Prefix: "p:", After: after, Limit: state.MaxList})
		if err != nil {
			return nil, err
		}
		for _, rec := range recs {
			var n storedCount
			if err := json.Unmarshal(rec.Value, &n); err != nil {
				return nil, fmt.Errorf("runs: %q: %w", rec.Key, err)
			}
			out[n.Name] = n.ProjectCount
		}
		if next == "" {
			break
		}
		after = next
	}
	return out, nil
}

// eachThreadRecord calls fn for a tenant's conversations with a run in them,
// in no useful order, until fn returns false or MaxThreadScan have been read.
func (x *Index) eachThreadRecord(tenant string, fn func(s storedThread) bool) error {
	after, seen := "", 0
	for seen < MaxThreadScan {
		recs, next, err := x.page(tenant, state.Query{Prefix: "s:", After: after, Limit: min(state.MaxList, MaxThreadScan-seen)})
		if err != nil {
			return err
		}
		for _, rec := range recs {
			seen++
			var s storedThread
			if err := json.Unmarshal(rec.Value, &s); err != nil {
				return fmt.Errorf("runs: %q: %w", rec.Key, err)
			}
			if s.Runs > 0 && !fn(s) {
				return nil
			}
		}
		if next == "" {
			return nil
		}
		after = next
	}
	return nil
}

// Opened is how many conversations a tenant's runs have opened. It marks a
// moment for ProjectCountsWhere.
func (x *Index) Opened(tenant string) int {
	var h head
	if _, err := x.lookup(tenant, headKey, &h); err != nil {
		x.log.Error("runs: could not read the head", "tenant", tenant, "error", err)
	}
	return int(h.Opened)
}

// ProjectCountsWhere is ProjectCounts for one reader, as of a mark from
// Opened: over the conversations opened before it that keep holds, by
// thread id, which are those the reader may read. The caller decides that
// after taking the mark, so a conversation opened since is one it has not
// decided, and it is left out until a later read. A nil keep holds every
// conversation, and with none opened since the mark that is ProjectCounts.
func (x *Index) ProjectCountsWhere(tenant string, mark int, keep func(thread string) bool) map[string]ProjectCount {
	return x.ProjectCountsPlaced(tenant, mark, keep, nil)
}

// ProjectCountsPlaced groups operational counts by the authoritative current
// placement. The resolver must not call the run index. Accounting stays filed
// under its original immutable attribution. It reads at most MaxThreadScan
// conversations.
func (x *Index) ProjectCountsPlaced(tenant string, mark int, keep func(string) bool, place func(thread, original string) string) map[string]ProjectCount {
	if place == nil && keep == nil && x.Opened(tenant) <= mark {
		return x.ProjectCounts(tenant)
	}
	out := map[string]ProjectCount{}
	err := x.eachThreadRecord(tenant, func(s storedThread) bool {
		if s.Opened > int64(max(mark, 0)) || keep != nil && !keep(s.ID) {
			return true
		}
		project := s.Project
		if place != nil {
			project = place(s.ID, project)
		}
		sum := out[project]
		sum.Threads++
		sum.Active += s.Active
		if s.UpdatedAt.After(sum.ActiveAt) {
			sum.ActiveAt = s.UpdatedAt
		}
		out[project] = sum
		return true
	})
	if err != nil {
		x.log.Error("runs: could not read conversations to count", "tenant", tenant, "error", err)
	}
	return out
}

// SystemThreads returns the conversations a run of origin system opened in a
// tenant, in the order they were recorded, at most MaxThreadScan. The
// platform reads such a conversation where the turn its run serves is, not
// where it is filed, so a reader's counts can leave out the ones they may not
// read.
func (x *Index) SystemThreads(tenant string) []string {
	var out []string
	after := ""
	for len(out) < MaxThreadScan {
		recs, next, err := x.page(tenant, state.Query{Prefix: "y:", After: after, Limit: min(state.MaxList, MaxThreadScan-len(out))})
		if err != nil {
			x.log.Error("runs: could not read housekeeping conversations", "tenant", tenant, "error", err)
			return out
		}
		for _, rec := range recs {
			var id string
			if json.Unmarshal(rec.Value, &id) == nil {
				out = append(out, id)
			}
		}
		if next == "" {
			break
		}
		after = next
	}
	return out
}
