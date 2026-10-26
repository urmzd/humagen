package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestContextPreviewUsesTurnLayersAndConfinesReads(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", append(operator, "humagen:agents:write")...)
	// Source declarations are capabilities: create it first and configure as a
	// knowledge reader, then exercise preview reads without that permission.
	sourceEditor := f.machine(t, "acme", everything...)
	want(t, f, sourceEditor, "POST", "/api/knowledge", `{"name":"source-canary"}`, http.StatusCreated)
	spec := strings.Replace(helperSpec, `"name":"helper"`, `"name":"helper","sources":[{"name":"source-canary","uri":"knowledge://source-canary"}]`, 1)
	spec = strings.Replace(spec, "Help.", strings.Repeat("é", 32001), 1)
	want(t, f, sourceEditor, "POST", "/api/agents", spec, http.StatusCreated)
	want(t, f, owner, "POST", "/api/projects", `{"name":"Field","instructions":"FOLLOW-FIELD","members":[{"who":"svc-acme","role":"owner"}]}`, http.StatusCreated)
	out := want(t, f, owner, "GET", "/api/projects/Field/context?agent=helper", "", http.StatusOK)
	layers := out["layers"].([]any)
	if len(layers) < 2 || !strings.Contains(layers[1].(map[string]any)["text"].(string), "FOLLOW-FIELD") {
		t.Fatal("preview omits real instructions")
	}
	system := layers[0].(map[string]any)
	if system["kind"] != "agent_instructions" || system["budget"] != float64(0) || system["preview_omitted"] != float64(1) || utf8.RuneCountInString(system["text"].(string)) != 32000 {
		t.Fatal("system prompt must use its real cap and distinguish preview shortening")
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "source-canary") {
		t.Fatal("source name without knowledge permission")
	}
	knows := f.machine(t, "acme", append(operator, "humagen:knowledge:read")...)
	withKnowledge := want(t, f, knows, "GET", "/api/projects/Field/context?agent=helper", "", http.StatusOK)
	raw, _ = json.Marshal(withKnowledge)
	if !strings.Contains(string(raw), "source-canary") {
		t.Fatal("source name missing for knowledge reader")
	}
	governor := f.machineAs(t, "context-governor", "acme", everything...)
	before := time.Now().Add(-time.Second)
	want(t, f, governor, "GET", "/api/projects/Field/context?agent=helper", "", http.StatusOK)
	count := 0
	for _, row := range f.p.ReadAudit.List("acme", before, time.Now().Add(time.Second), 500) {
		if row.Subject == "context-governor" && row.Project == "Field" {
			count++
			if row.Route != "GET /api/projects/{name}/context" {
				t.Fatalf("context preview lost its audit route: %s", row.Route)
			}
		}
	}
	if count != 1 {
		t.Fatalf("context preview recorded %d break-glass reads, want one per request", count)
	}
	stranger := f.machineAs(t, "outsider", "acme", "humagen:projects:read")
	want(t, f, stranger, "GET", "/api/projects/Field/context?agent=helper", "", http.StatusNotFound)
	want(t, f, owner, "GET", "/api/projects/Field/context?agent=missing", "", http.StatusNotFound)
	want(t, f, owner, "GET", "/api/projects/Field/context?agent=helper&channel=missing", "", http.StatusNotFound)
	want(t, f, owner, "GET", "/api/projects/Field/context", "", http.StatusBadRequest)
}
