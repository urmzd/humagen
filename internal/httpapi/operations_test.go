package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/operations"
)

func TestOperationsAreConfinedAndControlsPersist(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	secretAgent(t, f, owner)
	reader := f.machineAs(t, "reader", "acme", "humagen:agents:read", "humagen:runs:read")
	want(t, f, reader, "GET", "/api/agents/spy/control", "", http.StatusNotFound)
	want(t, f, reader, "GET", "/api/agents/missing/control", "", http.StatusNotFound)
	want(t, f, reader, "PUT", "/api/agent-controls", `{"paused":true,"version":0}`, http.StatusForbidden)
	want(t, f, owner, "PUT", "/api/agent-controls", `{"paused":true,"version":0}`, http.StatusOK)
	want(t, f, owner, "POST", "/api/runs", `{"agent":"helper","text":"Hello"}`, http.StatusConflict)
	want(t, f, owner, "PUT", "/api/agent-controls", `{"paused":false,"version":0}`, http.StatusConflict)
	want(t, f, owner, "PUT", "/api/agent-controls", `{"paused":false,"version":1}`, http.StatusOK)
	now := time.Now().UTC()
	for _, a := range []operations.Assignment{{ID: "public", Agent: "helper", Goal: "Visible", Project: "", Owner: "owner", DoneWhen: "Done", State: operations.Requested}, {ID: "secret", Agent: "spy", Project: "Globex", Owner: "owner", Goal: "Hidden goal", DoneWhen: "Hidden", State: operations.Requested}} {
		if _, err := f.p.Operations.SetAssignment("acme", a, 0, "owner", now); err != nil {
			t.Fatal(err)
		}
	}
	req, _ := http.NewRequest("GET", f.srv.URL+"/api/assignments", nil)
	req.Header.Set("Authorization", "Bearer "+reader)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(data)
	if strings.Contains(raw, "Hidden") || strings.Contains(raw, "spy") {
		t.Fatalf("hidden assignment leaked: %s", raw)
	}
	want(t, f, reader, "GET", "/api/assignments/secret", "", http.StatusNotFound)
	want(t, f, reader, "GET", "/api/assignments/missing", "", http.StatusNotFound)
	want(t, f, owner, "GET", "/api/assignments?limit=201", "", http.StatusBadRequest)
}

func TestAssignmentRunsThroughAdmissionAndLedger(t *testing.T) {
	f := newFixture(t)
	token := f.machineAs(t, "owner", "acme", everything...)
	want(t, f, token, "POST", "/api/agents", helperSpec, http.StatusCreated)
	body := `{"id":"inspect","agent":"helper","project":"","owner":"owner","goal":"Inspect queue","done_when":"Report items","state":"requested","version":0}`
	want(t, f, token, "POST", "/api/assignments", body, http.StatusOK)
	run := want(t, f, token, "POST", "/api/assignments/inspect/run", "", http.StatusOK)
	id := run["id"].(string)
	deadline := time.Now().Add(10 * time.Second)
	for {
		a := want(t, f, token, "GET", "/api/assignments/inspect", "", http.StatusOK)
		if a["state"] == "result" {
			if a["run_id"] != id {
				t.Fatal(a)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("assignment did not settle from ledger: %+v", a)
		}
		time.Sleep(10 * time.Millisecond)
	}
	want(t, f, token, "PUT", "/api/assignments/inspect", fmt.Sprintf(`{"id":"inspect","agent":"helper","project":"","owner":"owner","goal":"Inspect queue","done_when":"Report items","state":"accepted","version":%v}`, 3), http.StatusOK)
}
