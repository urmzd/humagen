package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
)

// importedBody is a made-up session's structure as an import sends it.
func importedBody(id string, turns int) string {
	var ts []string
	for i := range turns {
		speaker := "user"
		if i%2 == 1 {
			speaker = "assistant"
		}
		ts = append(ts, fmt.Sprintf(`{"speaker":%q,"at":"2026-10-07T08:%02d:00Z","text":"generated turn %d"}`, speaker, i, i))
	}
	return fmt.Sprintf(`{"tool":"chatgpt","id":%q,"started_at":"2026-10-07T08:00:00Z","ended_at":"2026-10-07T08:30:00Z","turns":[%s],"branches":1,"omitted":{"turns":0,"results":0,"cut_turns":0,"attachments":null}}`, id, strings.Join(ts, ","))
}

// A document an import adds or replaces carries its session's structure in
// the same call, under the permission the call already needs. An ordinary
// edit carries none and leaves the structure untrusted; a structure that is
// not of the document's conversation, or not within the bounds, is refused
// with the document; another workspace's document is not found.
func TestADocumentsImportedStructureIsWrittenOnlyWithItsText(t *testing.T) {
	f := newFixture(t)
	ana := f.person(t, "acme", "00u-ana", "Ana", "humagen-admins")
	bo := f.person(t, "globex", "00u-bo", "Bo", "humagen-admins")
	reader := auth.Principal{Tenant: "acme", Subject: "00u-ana", Kind: auth.User, Scopes: []auth.Permission{auth.KnowledgeRead}}
	want(t, f, ana, "POST", "/api/knowledge", `{"name":"chats"}`, http.StatusCreated)
	docs := "/api/knowledge/chats/documents"
	origin := func(id string) knowledge.Origin {
		t.Helper()
		got, err := f.p.ImportedOrigin(reader, "chats", id)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	count := func() int {
		t.Helper()
		return len(sliceOf(want(t, f, ana, "GET", docs, "", http.StatusOK)["documents"]))
	}

	// Refused, and nothing is added: a structure of another conversation
	// than the uri names, of an unknown tool, with a field it does not
	// know, or past its bounds.
	for name, session := range map[string]string{
		"another conversation": importedBody("generated-other", 2),
		"an unknown tool":      strings.Replace(importedBody("generated-0001", 2), `"chatgpt"`, `"other"`, 1),
		"an unknown field":     strings.Replace(importedBody("generated-0001", 2), `"branches":1`, `"branches":1,"trusted":true`, 1),
		"too many turns":       importedBody("generated-0001", knowledge.MaxImportedTurns+1),
	} {
		body := `{"title":"A generated chat","uri":"chatgpt:generated-0001","text":"generated text","session":` + session + `}`
		if res, out := f.do(t, nil, "POST", docs, ana, body); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d %v", name, res.StatusCode, out)
		}
	}
	if n := count(); n != 0 {
		t.Fatalf("a refused structure added %d documents", n)
	}

	doc := want(t, f, ana, "POST", docs, `{"title":"A generated chat","uri":"chatgpt:generated-0001","text":"generated text","session":`+importedBody("generated-0001", 2)+`}`, http.StatusCreated)
	id, digest := fmt.Sprint(doc["id"]), fmt.Sprint(doc["digest"])
	got := origin(id)
	if got.Session == nil || len(got.Session.Turns) != 2 || got.Session.Turns[1].Text != "generated turn 1" || got.Session.Omitted.Attachments != nil || got.Document.Digest != digest {
		t.Fatalf("added: %+v", got)
	}
	// The document's answer does not show the structure.
	if raw, _ := json.Marshal(doc); strings.Contains(string(raw), "generated turn") {
		t.Fatalf("the answer shows the structure: %s", raw)
	}
	plain := want(t, f, ana, "POST", docs, `{"title":"A note","uri":"chatgpt:generated-0002","text":"written as before"}`, http.StatusCreated)
	if got := origin(fmt.Sprint(plain["id"])); got.Session != nil || got.Unknown != knowledge.NotKept {
		t.Fatalf("a document added without a structure: %+v", got)
	}

	path := docs + "/" + id + "/text"
	// Another workspace's document is not found, and nothing changes.
	want(t, f, bo, "PUT", path, `{"title":"A generated chat","text":"grown","digest":"`+digest+`","session":`+importedBody("generated-0001", 4)+`}`, http.StatusNotFound)
	// A refused replacement changes neither.
	want(t, f, ana, "PUT", path, `{"title":"A generated chat","text":"grown","digest":"stale","session":`+importedBody("generated-0001", 4)+`}`, http.StatusConflict)
	want(t, f, ana, "PUT", path, `{"title":"A generated chat","text":"grown","digest":"`+digest+`","session":`+importedBody("generated-other", 4)+`}`, http.StatusBadRequest)
	if got := origin(id); got.Session == nil || len(got.Session.Turns) != 2 || got.Document.Digest != digest {
		t.Fatalf("after refusals: %+v", got)
	}

	// An ordinary edit, as the console makes it, leaves it untrusted.
	edited := want(t, f, ana, "PUT", path, `{"title":"A generated chat, renamed","text":"generated text","digest":"`+digest+`"}`, http.StatusOK)
	if got := origin(id); got.Session != nil || got.Unknown != knowledge.Changed {
		t.Fatalf("after an edit: %+v", got)
	}
	// An import of the session as it now is writes both again.
	grown := want(t, f, ana, "PUT", path, `{"title":"A generated chat","text":"grown","digest":"`+fmt.Sprint(edited["digest"])+`","session":`+importedBody("generated-0001", 4)+`}`, http.StatusOK)
	if got := origin(id); got.Session == nil || len(got.Session.Turns) != 4 || got.Document.Digest != grown["digest"] {
		t.Fatalf("after an import: %+v", got)
	}

	want(t, f, ana, "DELETE", docs+"/"+id, "", http.StatusNoContent)
	if _, err := f.p.ImportedOrigin(reader, "chats", id); !errors.Is(err, knowledge.ErrNotFound) {
		t.Fatalf("removed: err = %v", err)
	}
}
