package httpapi

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// exerciseMessagesContract checks every messages route against the spec,
// as a person of the session contract's workspace.
func exerciseMessagesContract(t *testing.T, c *contract, token, other string) {
	t.Helper()
	f := c.f
	pr := `"ref":{"type":"github","repo":"acme/web","kind":"pull","number":5}`
	first := c.call(token, "POST", "/api/messages", "/api/messages", `{"messages":[{"role":"user","content":"@helper review the change"}],`+pr+`}`, http.StatusCreated)
	id := first.str("id")
	c.call(token, "POST", "/api/messages", "/api/messages", `{"messages":[{"role":"user","content":[{"type":"text","text":"and the tests"}]}],`+pr+`}`, http.StatusOK)
	c.call(other, "POST", "/api/messages", "/api/messages", `{"messages":[{"role":"user","content":"let me in"}],`+pr+`}`, http.StatusNotFound)
	c.call(token, "POST", "/api/messages", "/api/messages", `{"messages":[{"role":"assistant","content":"hi"}]}`, http.StatusBadRequest)
	c.call(token, "POST", "/api/messages/{id}", "/api/messages/"+id, `{"messages":[{"role":"user","content":"one more thing"}]}`, http.StatusOK)
	c.call(token, "POST", "/api/messages/{id}", "/api/messages/"+id, `{"messages":[]}`, http.StatusBadRequest)
	c.call(token, "GET", "/api/messages/{id}", "/api/messages/"+id, "", http.StatusOK)
	c.call(token, "POST", "/api/messages/{id}/context", "/api/messages/"+id+"/context", `{"request":"what changed in this pull request?"}`, http.StatusOK)
	c.call(token, "POST", "/api/messages/{id}/context", "/api/messages/"+id+"/context", `{"request":"  "}`, http.StatusBadRequest)
	run := obj(sliceOf(obj(first.field("posted"))["runs"])[0])
	runID, child := run["id"].(string), run["thread"].(string)
	c.settled(token, runID)
	c.call(token, "POST", "/api/messages/{id}/feedback", "/api/messages/"+child+":"+runID+"/feedback", `{"rating":"up","note":"spot on"}`, http.StatusOK)
	c.call(token, "POST", "/api/messages/{id}/feedback", "/api/messages/"+child+":"+runID+"/feedback", `{"rating":"meh"}`, http.StatusBadRequest)
	c.call(token, "POST", "/api/messages/{id}/stop", "/api/messages/"+id+"/stop", `{"children":true}`, http.StatusNoContent)
	c.call(token, "GET", "/api/messages/{id}", "/api/messages/"+id+"?since=broken", "", http.StatusBadRequest)
	root := first.str("root")
	tree := c.call(token, "GET", "/api/messages/{id}", "/api/messages/"+root+"?depth=2", "", http.StatusOK)
	if tree.str("id") != root || tree.str("role") != "user" || tree.field("children") == nil {
		t.Fatalf("the root as a tree = %s", tree.raw)
	}
	c.call(token, "GET", "/api/messages/{id}", "/api/messages/"+id+"?depth=", "", http.StatusOK)
	c.call(token, "GET", "/api/messages/{id}", "/api/messages/"+root+"?depth=9", "", http.StatusBadRequest)
	c.call(token, "GET", "/api/messages/{id}", "/api/messages/"+root+"?depth=1&summary=true", "", http.StatusBadRequest)
	c.call(token, "GET", "/api/messages/{id}", "/api/messages/"+root+":m-999?depth=1", "", http.StatusNotFound)
	trace := c.call(token, "GET", "/api/messages/{id}/trace", "/api/messages/"+root+"/trace", "", http.StatusOK)
	if trace.str("message") != root || trace.field("nodes") == nil {
		t.Fatalf("the root's trace = %s", trace.raw)
	}
	c.call(token, "GET", "/api/messages/{id}/trace", "/api/messages/"+id+"/trace", "", http.StatusOK)
	c.call(token, "GET", "/api/messages/{id}/trace", "/api/messages/"+root+":m-999/trace", "", http.StatusNotFound)
	c.call(other, "GET", "/api/messages/{id}/trace", "/api/messages/"+root+"/trace", "", http.StatusNotFound)
	// A question an agent asks in a conversation, answered by the call that
	// asked it. It is asked in a place of its own: a message posted where an
	// agent is still answering an earlier one is a follow-up, queued behind
	// that answer, and starts no run to name here until that answer ends.
	c.call(token, "POST", "/api/agents", "/api/agents", `{"name":"asker","model":"x","provider":"anthropic","system_prompt":"Ask.","tools":["ask_person"]}`, http.StatusCreated)
	asked := c.call(token, "POST", "/api/messages", "/api/messages", `{"messages":[{"role":"user","content":"@asker pick a colour"}],"ref":{"type":"github","repo":"acme/web","kind":"pull","number":6}}`, http.StatusCreated)
	started := sliceOf(obj(asked.field("posted"))["runs"])
	if len(started) != 1 {
		t.Fatalf("a question asked in a new place started %d runs: %s", len(started), asked.raw)
	}
	askRun, _ := obj(started[0])["id"].(string)
	asking := asked.str("root")
	var question string
	for deadline := time.Now().Add(10 * time.Second); question == "" && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		now := c.call(token, "GET", "/api/runs/{id}", "/api/runs/"+askRun, "", http.StatusOK)
		question, _ = obj(now.field("question"))["id"].(string)
	}
	if question == "" {
		t.Fatal("the asking run never parked on its question")
	}
	answer, at, use := "/api/messages/{id}/answer", "/api/messages/"+asking+"/answer", "htu_"+askRun+"_1"
	c.call(token, "POST", answer, at, `{"tool_use_id":"`+use+`"}`, http.StatusBadRequest)
	c.call(token, "POST", answer, at, `{"tool_use_id":"`+use+`","answer":{"action":"choose","choice":"red","act_as":{"client":"nope","user":"U0X"}}}`, http.StatusForbidden)
	c.call(token, "POST", answer, at, `{"tool_use_id":"htu_`+askRun+`_9","answer":{"action":"choose","choice":"red"}}`, http.StatusNotFound)
	c.call(other, "POST", answer, at, `{"tool_use_id":"`+use+`","answer":{"action":"choose","choice":"red"}}`, http.StatusNotFound)
	if settled := c.call(token, "POST", answer, at, `{"tool_use_id":"`+use+`","answer":{"action":"choose","choice":"red"}}`, http.StatusOK); obj(obj(sliceOf(settled.field("parts"))[0])["outcome"])["action"] != "choose" {
		t.Fatalf("the answered question = %s", settled.raw)
	}
	roots := c.call(token, "GET", "/api/messages", "/api/messages?roots=true&limit=1", "", http.StatusOK)
	if list := sliceOf(roots.field("messages")); len(list) != 1 || roots.str("next") == "" {
		t.Fatalf("a page of one root = %s", roots.raw)
	}
	more := c.call(token, "GET", "/api/messages", "/api/messages?roots=true&after="+roots.str("next"), "", http.StatusOK)
	for _, m := range sliceOf(more.field("messages")) {
		if obj(m)["id"] == roots.str("next") {
			t.Fatalf("the next page repeats the root before it: %s", more.raw)
		}
	}
	c.call(token, "GET", "/api/messages", "/api/messages", "", http.StatusBadRequest)
	c.call(token, "GET", "/api/messages", "/api/messages?roots=true&limit=0", "", http.StatusBadRequest)
	c.call(token, "GET", "/api/messages", "/api/messages?roots=true&after=s_missing", "", http.StatusBadRequest)
	if mine := c.call(other, "GET", "/api/messages", "/api/messages?roots=true", "", http.StatusOK); strings.Contains(string(mine.raw), root) {
		t.Fatalf("someone outside the root lists it: %s", mine.raw)
	}
	if res, _ := f.do(t, nil, "GET", "/api/messages/"+id+"/stream", token, "", "Last-Event-ID", "broken"); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("a bad stream cursor = %d", res.StatusCode)
	}
	c.seen["GET /api/messages/{id}/stream 400"] = true
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", f.srv.URL+"/api/messages/"+id+"/stream", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("message stream: %d", res.StatusCode)
	}
	scan := bufio.NewScanner(res.Body)
	scan.Buffer(make([]byte, 4096), 2<<20)
	for scan.Scan() {
		if strings.HasPrefix(scan.Text(), "data: ") {
			c.seen["GET /api/messages/{id}/stream 200"] = true
			break
		}
	}
}
