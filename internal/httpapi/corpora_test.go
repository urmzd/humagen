package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/catalog"
)

// A corpus is read by its owner and the readers it names, and by nobody
// else; only its owner changes it. A member the reader may not see shows as
// hidden, without its name, and the owner's changes keep it.
func TestACorpusIsReadByItsReadersAndChangedByItsOwner(t *testing.T) {
	f := newFixture(t)
	ana := f.person(t, "acme", "00u-ana", "Ana", "humagen-admins")
	bo := f.person(t, "acme", "00u-bo", "Bo", "humagen-operators")
	cy := f.person(t, "acme", "00u-cy", "Cy", "humagen-admins")
	dee := f.person(t, "acme", "00u-dee", "Dee", "humagen-admins")
	want(t, f, ana, "POST", "/api/knowledge", `{"name":"handbook"}`, http.StatusCreated)
	// Ana's own warehouse, unpublished: only Ana sees it.
	if _, err := f.p.CatalogStore.Declare("acme", catalog.Connector{Name: "warehouse", System: "bigquery", Owner: "00u-ana"}); err != nil {
		t.Fatal(err)
	}

	if code, _ := status(t, f, bo, "POST", "/api/corpora", `{"name":"mine"}`); code != http.StatusForbidden {
		t.Fatalf("an operator, who may not write knowledge, made a corpus: %d", code)
	}
	if code, out := status(t, f, cy, "POST", "/api/corpora", `{"name":"theirs","members":[{"kind":"catalog","name":"warehouse"}]}`); code != http.StatusBadRequest {
		t.Fatalf("a corpus holding a warehouse its maker cannot see = %d %v", code, out)
	}
	made := want(t, f, ana, "POST", "/api/corpora", `{"name":"hr","title":"HR policies","members":[{"kind":"source","name":"handbook"},{"kind":"catalog","name":"warehouse"}],"readers":[{"kind":"person","name":"00u-bo"},{"kind":"person","name":"00u-dee"}]}`, http.StatusCreated)
	if made["owner"] != "00u-ana" || made["mine"] != true || made["links"] != "isolated" || made["title"] != "HR policies" {
		t.Fatalf("made %v", made)
	}
	if got := fmt.Sprint(made["members"]); got != "[map[holds:files items:0 kind:source name:handbook provider:manual status:ready] map[holds:records items:0 kind:catalog name:warehouse provider:bigquery status:syncing]]" {
		t.Fatalf("the owner sees members %s", got)
	}

	// Bo, a reader, sees it, but not the warehouse he cannot see.
	read := want(t, f, bo, "GET", "/api/corpora/hr", "", http.StatusOK)
	if got := fmt.Sprint(read["members"]); read["mine"] != false || got != "[map[holds:files items:0 kind:source name:handbook provider:manual status:ready] map[items:0 kind:catalog name: status:hidden]]" {
		t.Fatalf("a reader sees %v and members %s", read["mine"], got)
	}
	if list := want(t, f, bo, "GET", "/api/corpora", "", http.StatusOK); len(sliceOf(list["corpora"])) != 1 {
		t.Fatalf("a reader lists %v", list)
	}

	// Cy is not a reader: the corpus is not there for him.
	if list := want(t, f, cy, "GET", "/api/corpora", "", http.StatusOK); len(sliceOf(list["corpora"])) != 0 {
		t.Fatalf("a stranger lists %v", list)
	}
	for _, c := range []struct{ method, path, body string }{
		{"GET", "/api/corpora/hr", ""},
		{"PATCH", "/api/corpora/hr", `{"links":"shared"}`},
		{"POST", "/api/corpora/hr/members", `{"kind":"source","name":"handbook"}`},
		{"DELETE", "/api/corpora/hr/members/source/handbook", ""},
		{"DELETE", "/api/corpora/hr", ""},
	} {
		if code, _ := status(t, f, cy, c.method, c.path, c.body); code != http.StatusNotFound {
			t.Errorf("a stranger's %s %s = %d, want 404", c.method, c.path, code)
		}
	}

	// Only agents that exist are granted it.
	want(t, f, ana, "POST", "/api/agents", helperSpec, http.StatusCreated)
	if code, _ := status(t, f, ana, "PATCH", "/api/corpora/hr", `{"agents":["nobody"]}`); code != http.StatusBadRequest {
		t.Fatalf("granted to an agent that does not exist: %d", code)
	}
	if granted := want(t, f, ana, "PATCH", "/api/corpora/hr", `{"agents":["helper"]}`, http.StatusOK); fmt.Sprint(granted["agents"]) != "[helper]" {
		t.Fatalf("granted %v", granted["agents"])
	}

	// Dee reads it and may write knowledge, but is not its owner.
	if code, _ := status(t, f, dee, "PATCH", "/api/corpora/hr", `{"links":"shared"}`); code != http.StatusForbidden {
		t.Fatalf("a reader who is not the owner changed it: %d", code)
	}

	// The owner opens it to the workspace and drops the handbook; the
	// warehouse she can see stays because she sent it.
	made = want(t, f, ana, "GET", "/api/corpora/hr", "", http.StatusOK)
	changed := want(t, f, ana, "PATCH", "/api/corpora/hr", fmt.Sprintf(`{"version":%v,"readers":[{"kind":"workspace"}],"members":[{"kind":"catalog","name":"warehouse"}]}`, made["version"]), http.StatusOK)
	if len(sliceOf(changed["members"])) != 1 {
		t.Fatalf("changed %v", changed)
	}
	if list := want(t, f, cy, "GET", "/api/corpora", "", http.StatusOK); len(sliceOf(list["corpora"])) != 1 {
		t.Fatalf("a workspace corpus is not listed for everyone: %v", list)
	}

	// Handed to Dee, Ana no longer changes it, and Dee does. It cannot be
	// handed to someone the workspace has never seen.
	if code, _ := status(t, f, ana, "PATCH", "/api/corpora/hr", `{"owner":"00u-nobody"}`); code != http.StatusBadRequest {
		t.Fatalf("handed to a stranger: %d", code)
	}
	want(t, f, ana, "PATCH", "/api/corpora/hr", `{"owner":"00u-dee"}`, http.StatusOK)
	if code, _ := status(t, f, ana, "DELETE", "/api/corpora/hr", ""); code != http.StatusForbidden {
		t.Fatalf("the former owner deleted it: %d", code)
	}
	// Dee cannot see Ana's warehouse, and keeps it when she sets members.
	kept := want(t, f, dee, "PATCH", "/api/corpora/hr", `{"members":[{"kind":"source","name":"handbook"}]}`, http.StatusOK)
	if got := fmt.Sprint(kept["members"]); got != "[map[holds:files items:0 kind:source name:handbook provider:manual status:ready] map[items:0 kind:catalog name:warehouse status:hidden]]" {
		t.Fatalf("a member the owner cannot see was dropped, or not named to its owner: %s", got)
	}
	want(t, f, dee, "DELETE", "/api/corpora/hr", "", http.StatusNoContent)
}

// A corpus's retrieval is evaluated like an agent: a suite of queries, each
// naming the documents it should find, scored on the same search agents
// run.
func TestACorpusRetrievalIsEvaluated(t *testing.T) {
	f := newFixture(t)
	ana := f.person(t, "acme", "00u-ana", "Ana", "humagen-admins")
	cy := f.person(t, "acme", "00u-cy", "Cy", "humagen-admins")
	want(t, f, ana, "POST", "/api/knowledge", `{"name":"handbook"}`, http.StatusCreated)
	want(t, f, ana, "POST", "/api/knowledge/handbook/documents", `{"title":"Parental leave","text":"Parental leave is sixteen weeks, paid in full."}`, http.StatusCreated)
	want(t, f, ana, "POST", "/api/knowledge/handbook/documents", `{"title":"Travel","text":"Book travel two weeks ahead."}`, http.StatusCreated)
	want(t, f, ana, "POST", "/api/corpora", `{"name":"hr","members":[{"kind":"source","name":"handbook"}]}`, http.StatusCreated)
	suite := `{"name":"hr-retrieval","target":"corpus","subject":"hr","k":1,"cases":[{"id":"leave","input":"how long is parental leave","expect":["Parental leave"]},{"id":"miss","input":"travel booking","expect":["Parental leave"]}]}`
	if code, _ := status(t, f, cy, "POST", "/api/evals", strings.Replace(suite, "hr-retrieval", "cy-retrieval", 1)); code != http.StatusNotFound {
		t.Fatalf("a suite over a corpus its maker cannot read = %d", code)
	}
	want(t, f, ana, "POST", "/api/evals", suite, http.StatusCreated)
	want(t, f, ana, "POST", "/api/evals/hr-retrieval/run", "", http.StatusAccepted)
	var run map[string]any
	for range 400 {
		runs := sliceOf(want(t, f, ana, "GET", "/api/evals/hr-retrieval/runs", "", http.StatusOK)["runs"])
		if len(runs) > 0 && obj(runs[0])["status"] != "running" {
			run = obj(runs[0])
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if run == nil {
		t.Fatal("the corpus eval did not finish")
	}
	results := sliceOf(run["results"])
	if len(results) != 2 || obj(results[0])["passed"] != true || obj(results[1])["passed"] == true {
		t.Fatalf("results %v", results)
	}
	if metrics := obj(run["metrics"]); metrics["hit_rate"] != 0.5 {
		t.Fatalf("metrics %v", metrics)
	}
}
