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
)

func exerciseSessionContract(t *testing.T, c *contract) map[string]string {
	t.Helper()
	f := c.f
	token := f.person(t, "session-contract", "ana", "Ana", "humagen-admins")
	f.person(t, "session-contract", "ben", "Ben", "humagen-admins")
	c.call(token, "POST", "/api/agents", "/api/agents", helperSpec, 201)
	root := c.call(token, "POST", "/api/sessions", "/api/sessions", `{"agent":"helper","project":"~","text":"Check the service"}`, 201)
	id := root.str("id")
	path := "/api/sessions/" + id
	c.call(token, "GET", "/api/sessions/{id}", path+"?summary=true", "", 200)
	c.call(token, "GET", "/api/sessions", "/api/sessions?roots=true&mine=true", "", 200)
	c.call(token, "GET", "/api/sessions", "/api/sessions?limit=0", "", 400)
	c.call(token, "GET", "/api/sessions", "/api/sessions?parent=missing", "", 404)
	c.call(token, "GET", "/api/sessions/{id}", path+"?since=broken", "", 400)
	c.call(token, "POST", "/api/sessions", "/api/sessions", `{"parent":"missing","agent":"helper","text":"Hello"}`, 404)
	if res, _ := f.do(t, nil, "GET", path+"/stream", token, "", "Last-Event-ID", "broken"); res.StatusCode != 400 {
		t.Fatalf("bad cursor: %d", res.StatusCode)
	}
	c.seen["GET /api/sessions/{id}/stream 400"] = true

	child := c.call(token, "POST", "/api/sessions", "/api/sessions", fmt.Sprintf(`{"parent":%q,"agent":"helper","text":"Inspect the details"}`, id), 201)
	c.call(token, "POST", "/api/sessions/{id}/messages", path+"/messages", `{"text":"Continue"}`, 201)
	c.call(token, "POST", "/api/sessions/{id}/messages", path+"/messages", `{"text":"@helper independent check","agent":"helper","fresh":true}`, 201)
	c.call(token, "POST", "/api/sessions/{id}/people", path+"/people", `{"person":"ben"}`, 200)
	c.call(token, "POST", "/api/sessions/{id}/links", path+"/links", fmt.Sprintf(`{"target":%q,"kind":"evidence"}`, child.str("id")), 204)
	runID := ""
	for _, msg := range sliceOf(root.field("messages")) {
		if run := obj(obj(msg)["run"]); run != nil {
			runID, _ = run["id"].(string)
		}
	}
	if runID == "" {
		t.Fatal("session has no accepted run")
	}
	c.settled(token, runID)
	c.call(token, "GET", "/api/sessions/{id}", path+"?around="+runID, "", 200)
	c.call(token, "GET", "/api/sessions/{id}", path+"?around=missing", "", 404)
	c.call(token, "GET", "/api/sessions/{id}", path+"?around="+runID+"&before=bad", "", 400)
	c.call(token, "GET", "/api/sessions/{id}", path+"?since="+root.str("cursor"), "", 200)
	fpath := path + "/messages/" + runID + "/feedback"
	c.call(token, "PUT", "/api/sessions/{id}/messages/{mid}/feedback", fpath, `{"rating":"up"}`, 200)
	c.call(token, "DELETE", "/api/sessions/{id}/messages/{mid}/feedback", fpath, "", 204)
	c.call(token, "DELETE", "/api/runs/{id}/feedback", "/api/runs/"+runID+"/feedback", "", 204)
	alert := c.call(token, "POST", "/api/sessions/{id}/alerts", path+"/alerts", `{"title":"Service needs review","priority":"normal"}`, 201)
	c.call(token, "PATCH", "/api/alerts/{id}", "/api/alerts/"+alert.str("id"), `{"state":"resolved"}`, 200)
	c.call(token, "POST", "/api/sessions/{id}/stop", path+"/stop", `{"children":true}`, 204)
	// Streaming bodies have their own reconnect test. Validate the first real
	// session event here so success coverage still requires a live response.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", f.srv.URL+path+"/stream", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != 200 {
		t.Fatalf("session stream: %d", res.StatusCode)
	}
	scan := bufio.NewScanner(res.Body)
	scan.Buffer(make([]byte, 4096), 2<<20)
	for scan.Scan() {
		if data, ok := strings.CutPrefix(scan.Text(), "data: "); ok {
			var body any
			if err := json.Unmarshal([]byte(data), &body); err != nil {
				t.Fatal(err)
			}
			schema, err := c.spec.c.Compile("openapi.json#/components/schemas/AgentSession")
			if err != nil {
				t.Fatal(err)
			}
			if err = schema.Validate(body); err != nil {
				t.Fatal(err)
			}
			c.seen["GET /api/sessions/{id}/stream 200"] = true
			break
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	return map[string]string{"token": token, "id": id, "run": runID, "alert": alert.str("id")}
}
