package runs

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/urmzd/humagen/pkg/state"
)

// ErrRefile refuses a move Refile cannot make.
var ErrRefile = errors.New("runs: refile")

// EachTenant calls fn with every tenant the index holds a run of, in order,
// until fn returns an error.
func (x *Index) EachTenant(fn func(tenant string) error) error { return x.tenants.Each(fn) }

// Each calls fn with a tenant's runs from the newest, until fn returns false
// or MaxScan runs have been read.
func (x *Index) Each(tenant string, fn func(Run) bool) error {
	return x.scan(tenant, func(r *Run) bool { return fn(citationSnapshot(r)) })
}

// Refile moves conversation from, with every run in it, into conversation
// to, as if each run had been recorded there: each run says it is in to,
// its turns are listed under to, and to's summary counts them. from is left
// with no run and no longer listed. It is how a run from before sessions is
// filed in the session a run asked at the same place now opens (humagen
// migrate sessions).
//
// Both are in one project: a move into a conversation filed elsewhere is
// refused, and so is a housekeeping run's. A conversation already moved, or
// moved into itself, changes nothing. It returns how many runs it moved.
//
// title, when given, is to's title from now on: what the place it is about
// is called, such as a pull request's title. Without one, to keeps its own,
// or takes from's when from was asked first.
func (x *Index) Refile(tenant, from, to, title string) (int, error) {
	if tenant == "" || from == "" || to == "" {
		return 0, fmt.Errorf("%w: a tenant and two conversations", ErrRefile)
	}
	if from == to {
		return 0, nil
	}
	moved := 0
	err := x.do(tenant, func(t *txn) error {
		moved = 0
		src, err := t.thread(from)
		if err != nil {
			return err
		}
		srcVersion := t.threads[from].version
		if srcVersion == 0 || src.Runs == 0 {
			return errSkip
		}
		if src.Origin == OriginSystem {
			return fmt.Errorf("%w: %s is a housekeeping run's", ErrRefile, from)
		}
		dst, err := t.thread(to)
		if err != nil {
			return err
		}
		opened := dst.Runs == 0
		if !opened && dst.Project != src.Project {
			return fmt.Errorf("%w: %s is filed in %q and %s in %q", ErrRefile, from, src.Project, to, dst.Project)
		}
		// Every run in from: each turn and the runs its call tree holds.
		turns, err := x.keys(tenant, turnPrefix(from), MaxTree)
		if err != nil {
			return err
		}
		roots := map[string]bool{}
		var members []string
		for _, k := range turns {
			r, _, err := x.at(tenant, seqOf(k))
			if err != nil {
				return err
			}
			if r == nil || roots[r.Root] {
				continue
			}
			roots[r.Root] = true
			tree, err := x.under(tenant, treePrefix(r.Root), MaxTree)
			if err != nil {
				return err
			}
			for _, c := range tree {
				if c.Thread == from && !slices.Contains(members, c.ID) {
					members = append(members, c.ID)
				}
			}
		}
		for _, id := range members {
			r, err := t.run(id)
			if err != nil {
				return err
			}
			if r == nil || r.Thread != from {
				continue
			}
			if conversationTurn(r) {
				t.del(turnKey(from, r.seq))
				t.put(turnKey(to, r.seq), "", always)
			}
			r.Thread = to
			t.touch(r)
			moved++
		}

		before := src.UpdatedAt
		first := opened || src.CreatedAt.Before(dst.CreatedAt)
		if opened {
			dst.Origin, dst.Project, dst.Channel, dst.Opened = src.Origin, src.Project, src.Channel, src.Opened
		}
		if title != "" {
			dst.Title = title
		}
		if first {
			dst.Title, dst.CreatedAt = cmp.Or(title, src.Title), src.CreatedAt
			dst.Agents, dst.People = union(src.Agents, dst.Agents), union(src.People, dst.People)
		} else {
			dst.Agents, dst.People = union(dst.Agents, src.Agents), union(dst.People, src.People)
		}
		dst.Runs += src.Runs
		dst.Turns += src.Turns
		dst.Active += src.Active
		dst.Waiting += src.Waiting
		dst.Failed += src.Failed
		dst.Branches += src.Branches
		for prev, n := range src.Answers {
			if dst.Answers == nil {
				dst.Answers = map[string]int{}
			}
			dst.Answers[prev] += n
		}
		if src.UpdatedAt.After(dst.UpdatedAt) {
			dst.UpdatedAt = src.UpdatedAt
			if src.Last != "" {
				dst.Last, dst.LastBy, dst.LastAgent = src.Last, src.LastBy, src.LastAgent
			}
		}
		t.threadChanged(to)

		// from holds nothing now: its summary and its place in the activity
		// order go, at the version read.
		t.threads[from].changed = false
		t.ops = append(t.ops, state.DeleteAt(tenant, threadKey(from), srcVersion), state.Delete(tenant, activityKey(before, from)))
		if !opened {
			// Two conversations are one now.
			n, err := t.project(src.Project)
			if err != nil {
				return err
			}
			n.Threads = max(0, n.Threads-1)
			t.projectChanged(src.Project)
		}
		return nil
	})
	return moved, err
}

// union is a, then what b adds to it, in order.
func union(a, b []string) []string {
	out := slices.Clone(a)
	for _, v := range b {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}
