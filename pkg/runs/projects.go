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
	// each is each tenant's count of each conversation on its own, by
	// thread, so a reader's counts can leave out those they may not read.
	each map[string]map[string]*ProjectCount
}

func newCounters() counters {
	return counters{by: map[string]map[string]*ProjectCount{}, filed: map[string]string{}, each: map[string]map[string]*ProjectCount{}}
}

// one is a conversation's own count.
func (c counters) one(tenant, thread string) *ProjectCount {
	t, ok := c.each[tenant]
	if !ok {
		t = map[string]*ProjectCount{}
		c.each[tenant] = t
	}
	n, ok := t[thread]
	if !ok {
		n = &ProjectCount{Threads: 1}
		t[thread] = n
	}
	return n
}

// add counts a run in a count: one more in flight unless it has ended, and
// its change as the latest activity.
func (n *ProjectCount) add(r *Run) {
	if !r.Status.Terminal() {
		n.Active++
	}
	if r.UpdatedAt.After(n.ActiveAt) {
		n.ActiveAt = r.UpdatedAt
	}
}

// settle follows a run that was in flight: one fewer once it has ended, and
// its change as the latest activity.
func (n *ProjectCount) settle(r *Run) {
	if r.Status.Terminal() {
		n.Active--
	}
	if r.UpdatedAt.After(n.ActiveAt) {
		n.ActiveAt = r.UpdatedAt
	}
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
	n.add(r)
	x.counts.one(r.Tenant, r.Thread).add(r)
}

// recount follows a run that was in flight and may have changed: it leaves
// the count of runs in flight once it has ended, and its change is the
// project's latest activity. The caller holds the lock.
func (x *Index) recount(r *Run) {
	x.counts.of(r.Tenant, x.counts.filed[r.Tenant+"\x00"+r.Thread]).settle(r)
	x.counts.one(r.Tenant, r.Thread).settle(r)
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

// ProjectCountsWhere is ProjectCounts over the conversations keep holds,
// by thread id: those a reader may read. It reads every conversation of
// the tenant once, so a caller with nothing to leave out asks ProjectCounts.
// keep is called under the index's read lock, so it must not call the
// index.
func (x *Index) ProjectCountsWhere(tenant string, keep func(thread string) bool) map[string]ProjectCount {
	x.mu.RLock()
	defer x.mu.RUnlock()
	out := map[string]ProjectCount{}
	for thread, n := range x.counts.each[tenant] {
		if !keep(thread) {
			continue
		}
		project := x.counts.filed[tenant+"\x00"+thread]
		sum := out[project]
		sum.Threads += n.Threads
		sum.Active += n.Active
		if n.ActiveAt.After(sum.ActiveAt) {
			sum.ActiveAt = n.ActiveAt
		}
		out[project] = sum
	}
	return out
}
