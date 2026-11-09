package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// openKnowledgeAlerts are the titles of a person's open knowledge alerts.
func openKnowledgeAlerts(t *testing.T, f *fixture, tok string) []string {
	t.Helper()
	out := []string{}
	for _, a := range sliceOf(want(t, f, tok, "GET", "/api/alerts", "", http.StatusOK)["alerts"]) {
		al := obj(a)
		if al["kind"] == "knowledge" && al["acked_by"] == nil && al["state"] != "resolved" {
			out = append(out, fmt.Sprint(al["title"]))
		}
	}
	return out
}

// Maintenance tells each document's owner when theirs are stale, and the
// corpus's owner when documents have no owner, in their own sessions; it
// resolves each alert once its cause is gone.
func TestKnowledgeMaintenanceRaisesAndResolvesAlerts(t *testing.T) {
	f := newFixture(t)
	ana := f.person(t, "acme", "00u-ana", "Ana", "humagen-admins")
	want(t, f, ana, "POST", "/api/knowledge", `{"name":"handbook"}`, http.StatusCreated)
	old := time.Now().Add(-3 * 365 * 24 * time.Hour)
	stale, err := f.p.Knowledge.Add("acme", "handbook", "Leave policy 2023", "", "Parental leave was twelve weeks.", "00u-ana", old)
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := f.p.Knowledge.Add("acme", "handbook", "Travel policy", "", "Travel is booked ahead.", "00u-gone", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	want(t, f, ana, "POST", "/api/corpora", `{"name":"hr","title":"HR policies","members":[{"kind":"source","name":"handbook"}]}`, http.StatusCreated)

	if err := f.p.MaintainKnowledge(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(openKnowledgeAlerts(t, f, ana), " | ")
	if !strings.Contains(got, "1 document you own in HR policies is stale") || !strings.Contains(got, "1 document in HR policies has no owner") {
		t.Fatalf("ana's knowledge alerts = %q", got)
	}
	// A second pass raises nothing twice.
	if err := f.p.MaintainKnowledge(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := len(openKnowledgeAlerts(t, f, ana)); n != 2 {
		t.Fatalf("after a second pass ana has %d open knowledge alerts, want 2", n)
	}

	// Ana removes the stale document and claims the orphan: both resolve.
	want(t, f, ana, "DELETE", "/api/knowledge/handbook/documents/"+stale.ID, "", http.StatusNoContent)
	want(t, f, ana, "POST", "/api/corpora/hr/graph/build", "", http.StatusOK)
	for _, n := range sliceOf(want(t, f, ana, "GET", "/api/corpora/hr/graph", "", http.StatusOK)["nodes"]) {
		if node := obj(n); node["name"] == orphan.Title {
			want(t, f, ana, "PUT", fmt.Sprintf("/api/graph/nodes/%v/owner", node["id"]), `{"action":"confirm"}`, http.StatusOK)
		}
	}
	if err := f.p.MaintainKnowledge(t.Context()); err != nil {
		t.Fatal(err)
	}
	if left := openKnowledgeAlerts(t, f, ana); len(left) != 0 {
		t.Fatalf("alerts left open once their causes are gone: %q", left)
	}
}
