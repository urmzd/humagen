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
