package runs

import "time"

// ProjectCount is what a project holds in the index: the conversations filed
// in it, how many of their runs are in flight, and when any of them last
// changed. A conversation is filed where its first run was, as its
// ThreadSummary says, so a follow-up never moves it.
//
// The index keeps one per project as runs are recorded and change, so reading
// them is a lookup and not a scan of every run.
type ProjectCount struct {
	// Threads is how many conversations are filed in the project.
	Threads int
	// Active is how many of their runs have not ended, calls included.
	Active int
	// ActiveAt is when a run in one of them last changed.
	ActiveAt time.Time
}

// counters are the index's per-project counts.
type counters struct {
	// by is each tenant's counts by project. "" is conversations filed at
	// the root, in no project.
	by map[string]map[string]*ProjectCount
	// filed is the project each conversation is filed in, by tenant and
	// thread.
	filed map[string]string
}

func newCounters() counters {
	return counters{by: map[string]map[string]*ProjectCount{}, filed: map[string]string{}}
}

func (c counters) of(tenant, project string) *ProjectCount {
	t, ok := c.by[tenant]
	if !ok {
		t = map[string]*ProjectCount{}
		c.by[tenant] = t
	}
	n, ok := t[project]
	if !ok {
		n = &ProjectCount{}
		t[project] = n
	}
	return n
}

// count adds a run that was just recorded, or read back, to its project. The
// caller holds the lock.
func (x *Index) count(r *Run) {
	key := r.Tenant + "\x00" + r.Thread
	project, known := x.counts.filed[key]
	if !known {
		project = r.Project
		x.counts.filed[key] = project
	}
	n := x.counts.of(r.Tenant, project)
	if !known {
		n.Threads++
	}
	if !r.Status.Terminal() {
		n.Active++
	}
	if r.UpdatedAt.After(n.ActiveAt) {
		n.ActiveAt = r.UpdatedAt
	}
}

// recount follows a run that was in flight and may have changed: it leaves
// the count of runs in flight once it has ended, and its change is the
// project's latest activity. The caller holds the lock.
func (x *Index) recount(r *Run) {
	n := x.counts.of(r.Tenant, x.counts.filed[r.Tenant+"\x00"+r.Thread])
	if r.Status.Terminal() {
		n.Active--
	}
	if r.UpdatedAt.After(n.ActiveAt) {
		n.ActiveAt = r.UpdatedAt
	}
}

// ProjectCounts returns what each of a tenant's projects holds, by name. ""
// is the conversations filed at the root. A project with nothing filed in it
// is absent. A deleted project's conversations keep its name, so they are
// counted under it until it is created again.
func (x *Index) ProjectCounts(tenant string) map[string]ProjectCount {
	x.mu.RLock()
	defer x.mu.RUnlock()
	out := make(map[string]ProjectCount, len(x.counts.by[tenant]))
	for name, n := range x.counts.by[tenant] {
		out[name] = *n
	}
	return out
}
