package runs

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/fiber/pkg/event"
)

// filed records a run of lead filed in a project, ended as kind.
func filed(t *testing.T, x *Index, tenant, id, project string, at time.Duration, kind string) {
	t.Helper()
	meta := map[string]string{}
	if project != "" {
		meta[MetaProject] = project
	}
	record(t, x, tenant, id, "lead", OriginAPI, at, meta)
	if kind != "" {
		body := wire.Lifecycle{}
		if kind == event.TypeFailed {
			body = wire.Lifecycle{Error: "x", Terminal: true}
		}
		end(t, x, tenant, id, at+time.Second, kind, body)
	}
}

func projectIndex(t *testing.T) *Index {
	t.Helper()
	x := NewIndex()
	filed(t, x, "acme", "f1", "Field", 0, event.TypeCompleted)
	filed(t, x, "acme", "f2", "Field", time.Minute, event.TypeFailed)
	filed(t, x, "acme", "g1", "Globex", 2*time.Minute, event.TypeCompleted)
	filed(t, x, "acme", "o1", "Other", 3*time.Minute, event.TypeCompleted)
	filed(t, x, "acme", "r1", "", 4*time.Minute, "")
	filed(t, x, "globex", "z1", "Field", 0, event.TypeCompleted)
	return x
}

// A summary over some projects is the summary of exactly their runs: the
// same numbers as an index that holds only those runs.
func TestASummaryOverProjectsCountsOnlyTheirRuns(t *testing.T) {
	x := projectIndex(t)
	only := NewIndex()
	filed(t, only, "acme", "f1", "Field", 0, event.TypeCompleted)
	filed(t, only, "acme", "f2", "Field", time.Minute, event.TypeFailed)
	filed(t, only, "acme", "g1", "Globex", 2*time.Minute, event.TypeCompleted)

	since, until := t0, t0.Add(10*time.Minute)
	got, err := x.SummarizeWhere("acme", SummaryQuery{Since: since, Until: until, Bucket: time.Minute, Projects: []string{"Field", "Globex"}})
	if err != nil {
		t.Fatal(err)
	}
	want := only.Summarize("acme", since, until, time.Minute)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered = %+v\nwant %+v", got.Total, want.Total)
	}
	if got.Total.Runs != 3 || got.Total.Failed != 1 {
		t.Fatalf("total = %+v", got.Total)
	}

	// The root's own runs are named by "".
	root, _ := x.SummarizeWhere("acme", SummaryQuery{Since: since, Until: until, Bucket: time.Minute, Projects: []string{""}})
	if root.Total.Runs != 1 || root.Total.Active != 1 {
		t.Fatalf("the root's runs = %+v", root.Total)
	}

	// Every project is Summarize itself.
	all, _ := x.SummarizeWhere("acme", SummaryQuery{Since: since, Until: until, Bucket: time.Minute, AllProjects: true})
	if !reflect.DeepEqual(all, x.Summarize("acme", since, until, time.Minute)) {
		t.Fatalf("all projects = %+v", all.Total)
	}
}

// A query that names no project counts nothing: an empty set is never read
// as every project.
func TestASummaryOverNoProjectsIsEmpty(t *testing.T) {
	x := projectIndex(t)
	for name, q := range map[string]SummaryQuery{
		"nil":   {Since: t0, Until: t0.Add(10 * time.Minute)},
		"empty": {Since: t0, Until: t0.Add(10 * time.Minute), Projects: []string{}},
		"ghost": {Since: t0, Until: t0.Add(10 * time.Minute), Projects: []string{"Ghost"}},
	} {
		s, err := x.SummarizeWhere("acme", q)
		if err != nil {
			t.Fatal(err)
		}
		if s.Total.Runs != 0 || len(s.ByAgent) != 0 || len(s.Slowest) != 0 || s.Delegated != 0 {
			t.Fatalf("%s: %+v", name, s.Total)
		}
	}
}

// Keep leaves out the runs it refuses, and is only asked about runs the rest
// of the query already counts.
func TestASummaryLeavesOutWhatKeepRefuses(t *testing.T) {
	x := projectIndex(t)
	var asked []string
	s, err := x.SummarizeWhere("acme", SummaryQuery{
		Since: t0, Until: t0.Add(10 * time.Minute), Projects: []string{"Field", "Globex"},
		Keep: func(r Run) bool {
			asked = append(asked, r.ID)
			return r.Project != "Globex"
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Total.Runs != 2 || len(s.ByAgent) != 1 || s.ByAgent[0].Runs != 2 {
		t.Fatalf("kept = %+v", s.Total)
	}
	if !reflect.DeepEqual(asked, []string{"f1", "f2", "g1"}) {
		t.Fatalf("keep was asked about %v", asked)
	}
}

func TestASummaryNamesABoundedSetOfProjects(t *testing.T) {
	x := projectIndex(t)
	many := make([]string, MaxSummaryProjects+1)
	for i := range many {
		many[i] = fmt.Sprintf("p%d", i)
	}
	if _, err := x.SummarizeWhere("acme", SummaryQuery{Since: t0, Until: t0.Add(time.Hour), Projects: many}); !errors.Is(err, ErrSummary) {
		t.Fatalf("%d projects: err = %v", len(many), err)
	}
	if _, err := x.SummarizeWhere("acme", SummaryQuery{Since: t0, Until: t0.Add(time.Hour), Projects: many[:MaxSummaryProjects]}); err != nil {
		t.Fatalf("%d projects: err = %v", MaxSummaryProjects, err)
	}
	if _, err := x.SummarizeWhere("acme", SummaryQuery{Since: t0, Until: t0.Add(time.Hour), Projects: []string{"Field"}, AllProjects: true}); !errors.Is(err, ErrSummary) {
		t.Fatalf("projects and all of them: err = %v", err)
	}
}
