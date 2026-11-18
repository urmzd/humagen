package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// A document's text is replaced in place, only from the digest it was last
// read with. The document keeps its id and whether it is left out of search,
// a refused replacement changes nothing, and of several made at once from
// one digest one is taken.
func TestADocumentsTextIsReplacedInPlaceFromTheDigestItWasReadWith(t *testing.T) {
	f := newFixture(t)
	ana := f.person(t, "acme", "00u-ana", "Ana", "humagen-admins")
	bo := f.person(t, "globex", "00u-bo", "Bo", "humagen-admins")
	want(t, f, ana, "POST", "/api/knowledge", `{"name":"handbook"}`, http.StatusCreated)
	doc := want(t, f, ana, "POST", "/api/knowledge/handbook/documents", `{"title":"Refunds","uri":"note:refunds","text":"Refunds are issued within five days."}`, http.StatusCreated)
	id, digest := fmt.Sprint(doc["id"]), fmt.Sprint(doc["digest"])
	path := "/api/knowledge/handbook/documents/" + id
	hits := func(q string) int {
		t.Helper()
		return len(sliceOf(want(t, f, ana, "GET", "/api/knowledge/search?source=handbook&q="+q, "", http.StatusOK)["hits"]))
	}
	listed := func() map[string]any {
		t.Helper()
		docs := sliceOf(want(t, f, ana, "GET", "/api/knowledge/handbook/documents", "", http.StatusOK)["documents"])
		if len(docs) != 1 {
			t.Fatalf("documents = %v", docs)
		}
		return obj(docs[0])
	}
	if len(digest) != 64 || listed()["digest"] != digest || hits("refunds") != 1 {
		t.Fatalf("added = %v", doc)
	}

	// Refused: a stale digest, no digest, another tenant, no such document.
	want(t, f, ana, "PUT", path+"/text", `{"title":"Refunds","text":"Zeppelins fly on Tuesdays.","digest":"stale"}`, http.StatusConflict)
	want(t, f, ana, "PUT", path+"/text", `{"title":"Refunds","text":"Zeppelins fly on Tuesdays.","digest":""}`, http.StatusConflict)
	want(t, f, ana, "PUT", path+"/text", `{"title":"Refunds","text":"Zeppelins fly on Tuesdays."}`, http.StatusBadRequest)
	want(t, f, ana, "PUT", path+"/text", `{"title":"Refunds","text":"","digest":"`+digest+`"}`, http.StatusBadRequest)
	want(t, f, bo, "PUT", path+"/text", `{"title":"Refunds","text":"Zeppelins fly on Tuesdays.","digest":"`+digest+`"}`, http.StatusNotFound)
	want(t, f, ana, "PUT", "/api/knowledge/handbook/documents/nope/text", `{"title":"Refunds","text":"Zeppelins fly on Tuesdays.","digest":"`+digest+`"}`, http.StatusNotFound)
	if got := listed(); got["id"] != id || got["digest"] != digest || hits("refunds") != 1 || hits("zeppelins") != 0 {
		t.Fatalf("a refused replacement changed the document: %v", got)
	}

	// Left out of search, it stays out through a refusal and a replacement.
	want(t, f, ana, "PUT", path, `{"excluded":true}`, http.StatusOK)
	want(t, f, ana, "PUT", path+"/text", `{"title":"Refunds","text":"Zeppelins fly on Tuesdays.","digest":"stale"}`, http.StatusConflict)
	if got := listed(); got["excluded"] != true || hits("refunds") != 0 {
		t.Fatalf("after a refusal: %v", got)
	}

	// Several at once from the one digest: one is taken.
	const writers = 6
	codes := make([]int, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, _ := f.do(t, nil, "PUT", path+"/text", ana, `{"title":"Refunds, revised","text":"Zeppelins fly on Tuesdays.","digest":"`+digest+`"}`)
			codes[i] = res.StatusCode
		}()
	}
	wg.Wait()
	taken := 0
	for _, code := range codes {
		switch code {
		case http.StatusOK:
			taken++
		case http.StatusConflict:
		default:
			t.Fatalf("codes = %v", codes)
		}
	}
	got := listed()
	if taken != 1 || got["id"] != id || got["title"] != "Refunds, revised" || got["uri"] != "note:refunds" || got["excluded"] != true || got["digest"] == digest || got["added_at"] != doc["added_at"] || got["updated_at"] == nil {
		t.Fatalf("%d taken (%v), document = %v", taken, codes, got)
	}
	if hits("zeppelins") != 0 || hits("refunds") != 0 {
		t.Fatal("a document left out of search is found after its replacement")
	}
	want(t, f, ana, "PUT", path, `{"excluded":false}`, http.StatusOK)
	if hits("zeppelins") != 1 || hits("refunds") != 0 {
		t.Fatal("put back, the document is not found by its new text alone")
	}
	if !strings.Contains(fmt.Sprint(want(t, f, ana, "GET", "/api/knowledge/search?source=handbook&q=zeppelins", "", http.StatusOK)["hits"]), id) {
		t.Fatal("the hit is not the same document")
	}
}
