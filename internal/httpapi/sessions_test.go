package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/saige/flow/event"
)

func TestAgentSessionContractAndResumeConfinement(t *testing.T) {
	f := newFixture(t)
	ana := f.person(t, "acme", "ana", "Ana", "humagen-admins")
	if res, out := f.do(t, nil, "POST", "/api/agents", ana, helperSpec); res.StatusCode != 201 {
		t.Fatalf("agent: %d %v", res.StatusCode, out)
	}
	res, out := f.do(t, nil, "POST", "/api/sessions", ana, `{"agent":"helper","text":"First request"}`)
	if res.StatusCode != 201 {
		t.Fatalf("start: %d %v", res.StatusCode, out)
	}
	id := out["id"].(string)
	path := "/api/sessions/" + id
	other := f.person(t, "elsewhere", "ana", "Ana", "humagen-admins")
	if res, _ := f.do(t, nil, "GET", path, other, ""); res.StatusCode != 404 {
		t.Fatalf("cross tenant read: %d", res.StatusCode)
	}
	if res, _ := f.do(t, nil, "GET", path+"/stream", ana, "", "Last-Event-ID", "broken"); res.StatusCode != 400 {
		t.Fatalf("bad stream cursor: %d", res.StatusCode)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	open := func(cursor string) *http.Response {
		req, _ := http.NewRequestWithContext(ctx, "GET", f.srv.URL+path+"/stream", nil)
		req.Header.Set("Authorization", "Bearer "+ana)
		if cursor != "" {
			req.Header.Set("Last-Event-ID", cursor)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 200 {
			t.Fatalf("stream: %d", res.StatusCode)
		}
		return res
	}
	read := func(scan *bufio.Scanner) (string, string, string) {
		var cursor, kind, data string
		for scan.Scan() {
			line := scan.Text()
			if line == "" && kind != "" {
				return cursor, kind, data
			}
			if v, ok := strings.CutPrefix(line, "id: "); ok {
				cursor = v
			}
			if v, ok := strings.CutPrefix(line, "event: "); ok {
				kind = v
			}
			if v, ok := strings.CutPrefix(line, "data: "); ok {
				data = v
			}
		}
		t.Fatalf("stream ended: %v", scan.Err())
		return "", "", ""
	}
	first := open("")
	scan := bufio.NewScanner(first.Body)
	scan.Buffer(make([]byte, 4096), 2<<20)
	var cursor, oldRun string
	for {
		c, kind, data := read(scan)
		if kind != "run" {
			continue
		}
		var e platform.APIEvent
		if err := json.Unmarshal([]byte(data), &e); err != nil {
			t.Fatal(err)
		}
		if e.Type == event.TypeCompleted {
			cursor, oldRun = c, e.Run
			break
		}
	}
	first.Body.Close()
	second := open(cursor)
	defer second.Body.Close()
	scan = bufio.NewScanner(second.Body)
	scan.Buffer(make([]byte, 4096), 2<<20)
	if res, out := f.do(t, nil, "POST", path+"/messages", ana, `{"text":"Second request"}`); res.StatusCode != 201 {
		t.Fatalf("post: %d %v", res.StatusCode, out)
	}
	for {
		_, kind, data := read(scan)
		if kind != "run" {
			continue
		}
		var e platform.APIEvent
		if err := json.Unmarshal([]byte(data), &e); err != nil {
			t.Fatal(err)
		}
		if e.Run == oldRun {
			t.Fatalf("replayed acknowledged event: %s seq %d", e.Type, e.Seq)
		}
		if e.Type == event.TypeCompleted {
			break
		}
	}
}

func TestSessionAncestorRevocationConfinesCompatibilityRoutesAndStream(t *testing.T) {
	f := newFixture(t)
	owner := f.person(t, "acme", "ana", "Ana", "humagen-admins")
	reader := f.person(t, "acme", "bo", "Bo", "humagen-operators")
	if res, out := f.do(t, nil, "POST", "/api/agents", owner, helperSpec); res.StatusCode != 201 {
		t.Fatalf("agent: %d %v", res.StatusCode, out)
	}
	res, root := f.do(t, nil, "POST", "/api/sessions", owner, `{"agent":"helper","project":"~","text":"Parent context"}`)
	if res.StatusCode != 201 {
		t.Fatalf("root: %d %v", res.StatusCode, root)
	}
	id := root["id"].(string)
	if res, out := f.do(t, nil, "POST", "/api/sessions/"+id+"/people", owner, `{"person":"bo"}`); res.StatusCode != 200 {
		t.Fatalf("invite: %d %v", res.StatusCode, out)
	}
	res, child := f.do(t, nil, "POST", "/api/sessions", owner, fmt.Sprintf(`{"agent":"helper","parent":%q,"text":"Child context"}`, id))
	if res.StatusCode != 201 {
		t.Fatalf("child: %d %v", res.StatusCode, child)
	}
	childID := child["id"].(string)
	if child["project"] != root["project"] {
		t.Fatalf("child moved out of parent's project: parent=%v child=%v", root["project"], child["project"])
	}
	runID := ""
	for _, msg := range child["messages"].([]any) {
		if run, ok := msg.(map[string]any)["run"].(map[string]any); ok {
			runID = run["id"].(string)
		}
	}
	if runID == "" {
		t.Fatalf("child has no accepted run: %v", child)
	}
	person, err := f.p.People.ByHandle("acme", "bo")
	if err != nil {
		t.Fatal(err)
	}
	gate := newRunGate(t.Context(), f.p, auth.Principal{Tenant: "acme", Subject: person.Subject, Kind: auth.User, Roles: []auth.Role{auth.Operator}})
	if !gate.seen(runID) {
		t.Fatalf("invited reader %q could not see child run %q", person.Subject, runID)
	}
	_, th := f.do(t, nil, "GET", "/api/threads/"+id, owner, "")
	version := th["thread"].(map[string]any)["version"]
	if res, out := f.do(t, nil, "PUT", "/api/threads/"+id+"/participants", owner, fmt.Sprintf(`{"version":%v,"remove":["bo"]}`, version)); res.StatusCode != 200 {
		t.Fatalf("remove: %d %v", res.StatusCode, out)
	}
	for _, path := range []string{"/api/sessions/" + childID, "/api/threads/" + childID, "/api/runs/" + runID} {
		if res, _ := f.do(t, nil, "GET", path, reader, ""); res.StatusCode != 404 {
			t.Errorf("revoked reader %s: %d", path, res.StatusCode)
		}
	}
	if gate.seen(runID) {
		t.Fatal("existing stream gate retained revoked inherited access")
	}
}
