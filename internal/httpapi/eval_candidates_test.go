package httpapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/audit"
)

// candidateIDs is the ids on a page of the review queue, in order.
func candidateIDs(page map[string]any) []string {
	out := []string{}
	for _, c := range page["candidates"].([]any) {
		out = append(out, c.(map[string]any)["id"].(string))
	}
	return out
}

func TestAFeedbackCandidateIsReviewedIntoASuiteWithoutItsAnswer(t *testing.T) {
	f := newFixture(t)
	admin := f.machine(t, "acme", everything...)
	want(t, f, admin, "POST", "/api/agents", helperSpec, http.StatusCreated)
	want(t, f, admin, "POST", "/api/agents", strings.Replace(helperSpec, `"helper"`, `"other"`, 1), http.StatusCreated)
	want(t, f, admin, "POST", "/api/evals", `{"name":"smoke","target":"agent","subject":"helper","cases":[{"id":"a","input":"hello","checks":[{"kind":"contains","value":"x"}]}]}`, http.StatusCreated)
	want(t, f, admin, "POST", "/api/evals", `{"name":"elsewhere","target":"agent","subject":"other","cases":[{"id":"a","input":"hello","checks":[{"kind":"contains","value":"x"}]}]}`, http.StatusCreated)

	first := f.ask(t, admin, `{"agent":"helper","text":"Where is the first report?"}`)
	second := f.ask(t, admin, `{"agent":"helper","text":"Where is the second report?"}`)
	for _, run := range []map[string]any{first, second} {
		want(t, f, admin, "PUT", "/api/runs/"+run["id"].(string)+"/feedback", `{"rating":"down","reason":"wrong","comment":"It named the old report"}`, http.StatusOK)
	}
	// A rating without a reason, and a good one, open nothing.
	third := f.ask(t, admin, `{"agent":"helper","text":"Where is the third report?"}`)
	want(t, f, admin, "PUT", "/api/runs/"+third["id"].(string)+"/feedback", `{"rating":"up"}`, http.StatusOK)

	// The queue is newest first and pages by place, never repeating.
	page := want(t, f, admin, "GET", "/api/eval-candidates?limit=1", "", http.StatusOK)
	ids := candidateIDs(page)
	if len(ids) != 1 || page["next"] == "" {
		t.Fatalf("first page = %v", page)
	}
	newest := page["candidates"].([]any)[0].(map[string]any)
	if newest["input"] != "Where is the second report?" || newest["reason"] != "wrong" || newest["comment"] != "It named the old report" || newest["agent"] != "helper" || newest["run"] != second["id"] {
		t.Fatalf("newest candidate = %v", newest)
	}
	if _, has := newest["answer"]; has {
		t.Fatal("a candidate carries the answer")
	}
	rest := want(t, f, admin, "GET", "/api/eval-candidates?limit=1&after="+page["next"].(string), "", http.StatusOK)
	if got := candidateIDs(rest); len(got) != 1 || got[0] == ids[0] || rest["next"] != "" {
		t.Fatalf("second page = %v", rest)
	}
	older := candidateIDs(rest)[0]
	if got := candidateIDs(want(t, f, admin, "GET", "/api/eval-candidates?agent=other", "", http.StatusOK)); len(got) != 0 {
		t.Fatalf("another agent's queue = %v", got)
	}
	for _, q := range []string{"limit=0", "limit=201", "after=nonsense"} {
		want(t, f, admin, "GET", "/api/eval-candidates?"+q, "", http.StatusBadRequest)
	}

	// Reading the queue does not let a caller change it.
	reader := f.machine(t, "acme", "humagen:evals:read")
	if got := candidateIDs(want(t, f, reader, "GET", "/api/eval-candidates", "", http.StatusOK)); len(got) != 2 {
		t.Fatalf("a reader's queue = %v", got)
	}
	want(t, f, reader, "POST", "/api/eval-candidates/"+ids[0]+"/accept", `{"suite":"smoke","checks":[{"kind":"contains","value":"second"}]}`, http.StatusForbidden)
	want(t, f, reader, "DELETE", "/api/eval-candidates/"+ids[0], "", http.StatusForbidden)

	// A case needs the reviewer's checks and a suite that tests the agent.
	accept := "/api/eval-candidates/" + ids[0] + "/accept"
	want(t, f, admin, "POST", accept, `{"suite":"smoke","checks":[]}`, http.StatusBadRequest)
	want(t, f, admin, "POST", accept, `{"suite":"elsewhere","checks":[{"kind":"contains","value":"second"}]}`, http.StatusBadRequest)
	want(t, f, admin, "POST", accept, `{"suite":"nope","checks":[{"kind":"contains","value":"second"}]}`, http.StatusNotFound)
	want(t, f, admin, "POST", accept, `{"suite":"smoke","checks":[{"kind":"contains","value":"second"}],"answer":"forged"}`, http.StatusBadRequest)

	suite := want(t, f, admin, "POST", accept, `{"suite":"smoke","checks":[{"kind":"contains","value":"second"}]}`, http.StatusOK)
	cases := suite["cases"].([]any)
	added := cases[len(cases)-1].(map[string]any)
	if len(cases) != 2 || added["input"] != "Where is the second report?" || added["checks"].([]any)[0].(map[string]any)["value"] != "second" {
		t.Fatalf("suite after accepting = %v", suite)
	}
	if _, traced := added["trace"]; traced {
		t.Fatal("an accepted case carries a recorded run")
	}
	// Only the question and the checks are copied: not the answer, and not
	// what the person who rated it wrote.
	kept, err := f.p.Evals.Get("acme", "smoke")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(kept)
	answer, _ := second["answer"].(string)
	if answer == "" {
		t.Fatal("the rated run has no answer to look for")
	}
	for _, private := range []string{answer, "It named the old report", "wrong"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("the suite holds %q: %s", private, raw)
		}
	}

	// Accepted, it leaves the queue, and a changed rating does not reopen it.
	want(t, f, admin, "POST", accept, `{"suite":"smoke","checks":[{"kind":"contains","value":"second"}]}`, http.StatusNotFound)
	want(t, f, admin, "PUT", "/api/runs/"+second["id"].(string)+"/feedback", `{"rating":"down","reason":"wrong","comment":"Still the old report"}`, http.StatusOK)
	if got := candidateIDs(want(t, f, admin, "GET", "/api/eval-candidates", "", http.StatusOK)); len(got) != 1 || got[0] != older {
		t.Fatalf("queue after accepting = %v", got)
	}

	// Dismissed, the other leaves too, with no case.
	want(t, f, admin, "DELETE", "/api/eval-candidates/"+older, "", http.StatusNoContent)
	want(t, f, admin, "DELETE", "/api/eval-candidates/"+older, "", http.StatusNotFound)
	if got := candidateIDs(want(t, f, admin, "GET", "/api/eval-candidates", "", http.StatusOK)); len(got) != 0 {
		t.Fatalf("queue after dismissing = %v", got)
	}
	if kept, _ := f.p.Evals.Get("acme", "smoke"); len(kept.Cases) != 2 {
		t.Fatalf("dismissing changed the suite: %+v", kept.Cases)
	}
	// Both decisions are in the audit log.
	log, err := f.p.Audit.List("acme", audit.Query{From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour), Limit: 50}, func(audit.Record) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	text, _ := json.Marshal(log)
	for _, action := range []string{"eval.candidate.accept", "eval.candidate.dismiss"} {
		if !strings.Contains(string(text), action) {
			t.Errorf("the audit log has no %s", action)
		}
	}
	if strings.Contains(string(text), answer) || strings.Contains(string(text), "second report") {
		t.Errorf("the audit log holds text from the run: %s", text)
	}
}

func TestACandidateFromAPrivateRunIsItsReadersAlone(t *testing.T) {
	f := newFixture(t)
	admin := f.machine(t, "acme", everything...)
	want(t, f, admin, "POST", "/api/agents", helperSpec, http.StatusCreated)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Field"}`, http.StatusCreated)
	want(t, f, admin, "POST", "/api/evals", `{"name":"smoke","target":"agent","subject":"helper","cases":[{"id":"a","input":"hello","checks":[{"kind":"contains","value":"x"}]}]}`, http.StatusCreated)
	ana := f.person(t, "acme", "00u-ana", "Ana", "humagen-admins")
	// A member of Field who takes no part in the thread and does not govern
	// the workspace, so nothing but taking part would show them the run.
	member := f.iss.Sign(t, "svc-member", audience, map[string]any{
		"org": "acme", "scp": slices.DeleteFunc(slices.Clone(everything), func(s string) bool { return s == "humagen:projects:govern" }),
	})
	if code, out := get(t, f, member, "/api/projects/Field"); code != http.StatusOK {
		t.Fatalf("the member does not read Field: %d %s", code, out)
	}
	started := want(t, f, ana, "POST", "/api/threads", `{"project":"Field","visibility":"private","text":"@helper the plan is canary-7."}`, http.StatusCreated)
	run := started["runs"].([]any)[0].(map[string]any)["id"].(string)
	ended(t, f, ana, run)
	want(t, f, ana, "PUT", "/api/runs/"+run+"/feedback", `{"rating":"down","reason":"wrong","comment":"Not the plan"}`, http.StatusOK)

	mine := want(t, f, ana, "GET", "/api/eval-candidates", "", http.StatusOK)
	ids := candidateIDs(mine)
	if len(ids) != 1 {
		t.Fatalf("the person who rated sees %v", mine)
	}
	// Someone who may not read the run is shown nothing of it, and naming it
	// answers exactly as naming a candidate that does not exist.
	for _, other := range []string{member, f.machine(t, "globex", everything...)} {
		theirs := want(t, f, other, "GET", "/api/eval-candidates", "", http.StatusOK)
		if raw, _ := json.Marshal(theirs); len(candidateIDs(theirs)) != 0 || strings.Contains(string(raw), "canary-7") || theirs["next"] != "" {
			t.Fatalf("a caller who may not read the run sees %s", raw)
		}
		body := `{"suite":"smoke","checks":[{"kind":"contains","value":"plan"}]}`
		for _, call := range [][2]string{{"POST", "/accept"}, {"DELETE", ""}} {
			hidden := want(t, f, other, call[0], "/api/eval-candidates/"+ids[0]+call[1], body, http.StatusNotFound)
			absent := want(t, f, other, call[0], "/api/eval-candidates/no-such-candidate"+call[1], body, http.StatusNotFound)
			if hidden["error"] != absent["error"] {
				t.Fatalf("%s tells a hidden candidate (%v) from an absent one (%v)", call[0], hidden, absent)
			}
		}
	}
	// It is still there for the person who may read it.
	if got := candidateIDs(want(t, f, ana, "GET", "/api/eval-candidates", "", http.StatusOK)); len(got) != 1 {
		t.Fatalf("the candidate was changed by a caller who may not read it: %v", got)
	}
	if kept, _ := f.p.Evals.Get("acme", "smoke"); len(kept.Cases) != 1 {
		t.Fatalf("the suite gained a case from a hidden candidate: %+v", kept.Cases)
	}
}
