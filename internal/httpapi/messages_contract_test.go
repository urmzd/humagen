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
