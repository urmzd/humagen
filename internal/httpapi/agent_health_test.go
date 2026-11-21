package httpapi

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/evals"
)

// An agent's health counts the runs rated down that wait for review, so the
// console can link to the queue, and a reader without the runs sees none.
func TestAgentHealthCountsTheRunsFlaggedForReview(t *testing.T) {
	f := newFixture(t)
	admin := f.machine(t, "acme", everything...)
	want(t, f, admin, "POST", "/api/agents", helperSpec, http.StatusCreated)
	if got := want(t, f, admin, "GET", "/api/agents/helper/health", "", http.StatusOK); got["flagged"] != float64(0) || got["flagged_more"] != nil {
		t.Fatalf("health before any rating = %v; want flagged 0, present for a caller with evals:read", got)
	}
	first := f.ask(t, admin, `{"agent":"helper","text":"Where is the first report?"}`)
	second := f.ask(t, admin, `{"agent":"helper","text":"Where is the second report?"}`)
	want(t, f, admin, "PUT", "/api/runs/"+first["id"].(string)+"/feedback", `{"rating":"down","reason":"wrong"}`, http.StatusOK)
	want(t, f, admin, "PUT", "/api/runs/"+second["id"].(string)+"/feedback", `{"rating":"up"}`, http.StatusOK)
	if got := want(t, f, admin, "GET", "/api/agents/helper/health", "", http.StatusOK); got["flagged"] != float64(1) || got["flagged_more"] != nil {
		t.Fatalf("health with one run rated down = %v", got)
	}
	// Health needs only agents:read; the queue's count is for those who may
	// read the queue. It is absent for one who may not, and only for them,
	// so a client tells "none" (0) from "may not read" (absent).
	viewer := f.machineAs(t, "viewer", "acme", "humagen:agents:read")
	got := want(t, f, viewer, "GET", "/api/agents/helper/health", "", http.StatusOK)
	if _, has := got["flagged"]; has || got["flagged_more"] != nil {
		t.Fatalf("health for a caller without evals:read = %v", got)
	}
	page := want(t, f, admin, "GET", "/api/eval-candidates?agent=helper", "", http.StatusOK)
	want(t, f, admin, "DELETE", "/api/eval-candidates/"+candidateIDs(page)[0], "", http.StatusNoContent)
	if got := want(t, f, admin, "GET", "/api/agents/helper/health", "", http.StatusOK); got["flagged"] != float64(0) {
		t.Fatalf("health after dismissing = %v; want flagged 0", got)
	}

	// Many ratings of one run are one flagged run.
	for i := range platform.MaxFlagged + 5 {
		if err := f.p.Evals.SaveCandidate("acme", fmt.Sprintf("rater-%d", i), evals.Candidate{Run: first["id"].(string), Agent: "helper", Input: "q", Reason: "wrong", At: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	if got := want(t, f, admin, "GET", "/api/agents/helper/health", "", http.StatusOK); got["flagged"] != float64(1) || got["flagged_more"] != nil {
		t.Fatalf("health with one run rated down by many = %v", got)
	}
}

// Over the real route, a governor who takes no part in private threads is
// shown none of their checked or flagged runs on health, and refreshing
// writes no read-audit record; the owner is shown all of them. The review
// queue is where a governor reads such runs, and it records the read.
func TestAgentHealthNeverReadsByBreakGlass(t *testing.T) {
	f := newFixture(t)
	admin := f.machine(t, "acme", everything...)
	want(t, f, admin, "POST", "/api/agents", helperSpec, http.StatusCreated)
	ana := f.person(t, "acme", "00u-ana", "Ana", "humagen-admins")
	gov := f.person(t, "acme", "00u-gov", "Gov", "humagen-admins")
	const n = 3
	for range n {
		started := want(t, f, ana, "POST", "/api/threads", `{"visibility":"private","text":"@helper where is the plan?"}`, http.StatusCreated)
		run := started["runs"].([]any)[0].(map[string]any)["id"].(string)
		ended(t, f, ana, run)
		want(t, f, ana, "PUT", "/api/runs/"+run+"/feedback", `{"rating":"down","reason":"wrong"}`, http.StatusOK)
	}
	audited := func() int {
		rows, err := f.p.ReadAudit.List("acme", time.Now().Add(-time.Hour), time.Now().Add(time.Minute), 500)
		if err != nil {
			t.Fatal(err)
		}
		return len(rows)
	}
	// The checks on the runs are written after each run ends.
	var own map[string]any
	for range 400 {
		own = want(t, f, ana, "GET", "/api/agents/helper/health", "", http.StatusOK)
		if own["samples"] == float64(n) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if own["samples"] != float64(n) || own["flagged"] != float64(n) {
		t.Fatalf("the owner's health = %v; want %d samples and %d flagged", own, n, n)
	}
	for range 3 {
		got := want(t, f, gov, "GET", "/api/agents/helper/health", "", http.StatusOK)
		if got["samples"] != float64(0) || len(got["results"].([]any)) != 0 || got["flagged"] != float64(0) || got["flagged_more"] != nil || got["status"] != "untested" {
			t.Fatalf("a governor's health = %v; want none of the private runs", got)
		}
	}
	if got := audited(); got != 0 {
		t.Fatalf("health refreshes wrote %d read-audit records", got)
	}
	// The queue shows the governor the same runs, and records that it did.
	page := want(t, f, gov, "GET", "/api/eval-candidates?agent=helper", "", http.StatusOK)
	if len(page["candidates"].([]any)) != n || audited() != n {
		t.Fatalf("the governor's queue = %d rows, %d records; want %d and %d", len(page["candidates"].([]any)), audited(), n, n)
	}
}

// A suite listing says who a schedule acts for and when it next runs, as the
// schedule loop decides it; a suite without a schedule has no next run.
func TestASuiteListingSaysWhenItsScheduleNextRunsAndForWhom(t *testing.T) {
	f := newFixture(t)
	admin := f.machine(t, "acme", everything...)
	want(t, f, admin, "POST", "/api/agents", helperSpec, http.StatusCreated)
	suite := func(name, extra string) string {
		return fmt.Sprintf(`{"name":%q,"target":"agent","subject":"helper",%s"cases":[{"id":"a","input":"hello","checks":[{"kind":"contains","value":"x"}]}]}`, name, extra)
	}
	want(t, f, admin, "POST", "/api/evals", suite("plain", ""), http.StatusCreated)
	want(t, f, admin, "POST", "/api/evals", suite("timed", `"every_seconds":21600,`), http.StatusCreated)
	want(t, f, admin, "POST", "/api/evals", suite("too-often", `"every_seconds":600,`), http.StatusBadRequest)

	before := time.Now().Add(-time.Second)
	listed := want(t, f, admin, "GET", "/api/evals", "", http.StatusOK)["suites"].([]any)
	byName := map[string]map[string]any{}
	for _, s := range listed {
		byName[s.(map[string]any)["name"].(string)] = s.(map[string]any)
	}
	if _, has := byName["plain"]["next_run_at"]; has {
		t.Fatalf("a suite without a schedule has a next run: %v", byName["plain"])
	}
	timed := byName["timed"]
	next, err := time.Parse(time.RFC3339Nano, timed["next_run_at"].(string))
	// Never run, it is due at once: no earlier than now.
	if err != nil || next.Before(before) || next.After(time.Now().Add(time.Minute)) {
		t.Fatalf("next run of a suite never run = %v (%v)", timed["next_run_at"], err)
	}
	if timed["created_by_name"] == "" || timed["created_by_name"] == nil {
		t.Fatalf("a scheduled suite does not say who it runs as: %v", timed)
	}
	// What the listing adds is not taken back on a save.
	want(t, f, admin, "PUT", "/api/evals/timed", suite("timed", `"every_seconds":21600,"created_by_name":"x",`), http.StatusBadRequest)
	want(t, f, admin, "PUT", "/api/evals/timed", suite("timed", `"every_seconds":0,`), http.StatusOK)
	for _, s := range want(t, f, admin, "GET", "/api/evals", "", http.StatusOK)["suites"].([]any) {
		if s := s.(map[string]any); s["name"] == "timed" {
			if _, has := s["next_run_at"]; has {
				t.Fatalf("a schedule turned off still has a next run: %v", s)
			}
		}
	}

	// While a run is going the loop skips the suite, and the listing says so.
	want(t, f, admin, "PUT", "/api/evals/timed", suite("timed", `"every_seconds":21600,`), http.StatusOK)
	kept, err := f.p.Evals.Get("acme", "timed")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Evals.Begin("acme", kept, 0, "someone", time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, s := range want(t, f, admin, "GET", "/api/evals", "", http.StatusOK)["suites"].([]any) {
		if s := s.(map[string]any); s["name"] == "timed" && s["schedule_busy"] != true {
			t.Fatalf("a scheduled suite with a run going is not busy: %v", s)
		}
		if s := s.(map[string]any); s["name"] == "plain" && s["schedule_busy"] != nil {
			t.Fatalf("a suite without a schedule is busy: %v", s)
		}
	}
}
