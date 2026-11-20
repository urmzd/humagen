package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
)

// Every conversation imported here is generated for the tests. None is a
// real session.

// portFact is the fact a generated conversation states, and factModel
// answers from.
var portFact = regexp.MustCompile(`listens on port (\d+)`)

// factModel is a model that answers which port the staging proxy uses from
// what it was sent, or says it was not told, and keeps what each turn sent
// it.
type factModel struct {
	mu   sync.Mutex
	sent []string
}

func (m *factModel) Stream(ctx context.Context, req saigetypes.Request) (<-chan saigetypes.Delta, error) {
	var parts []string
	for _, msg := range req.Messages {
		if u, ok := msg.(saigetypes.UserMessage); ok {
			for _, part := range u.Parts {
				if t, ok := part.(saigetypes.TextPart); ok {
					parts = append(parts, t.Text)
				}
			}
		}
	}
	text := strings.Join(parts, "\n")
	m.mu.Lock()
	m.sent = append(m.sent, text)
	m.mu.Unlock()
	reply := "I was not told which port."
	if got := portFact.FindStringSubmatch(text); got != nil {
		reply = "The staging proxy uses port " + got[1] + "."
	}
	return (&agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{agenttest.TextResponse(reply)}}).Stream(ctx, req)
}

func (m *factModel) last() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sent) == 0 {
		return ""
	}
	return m.sent[len(m.sent)-1]
}

// continueFixture is a server whose acme workspace has the agent coder on
// factModel, the project Field, and the source chats attached to Field.
// Ana (an admin) imports; Bo and Cy (operators) work in Field.
type continueFixture struct {
	*fixture
	ana, bo, cy string
	model       *factModel
	st          state.Store
}

func newContinueFixture(t *testing.T, st state.Store) continueFixture {
	t.Helper()
	model := &factModel{}
	f := newFixtureWith(t, platform.Config{Backends: platform.Backends{State: st}, Ext: platform.Extensions{Providers: map[string]native.ProviderFunc{
		"generated": func(native.Spec) (saigetypes.Provider, error) { return model, nil },
	}}})
	c := continueFixture{fixture: f, model: model, st: st,
		ana: f.person(t, "acme", "00u-ana", "Ana", "humagen-admins"),
		bo:  f.person(t, "acme", "00u-bo", "Bo", "humagen-operators"),
		cy:  f.person(t, "acme", "00u-cy", "Cy", "humagen-operators"),
	}
	if _, err := f.p.Agents.Create(t.Context(), "acme", wire.AgentSpec{Name: "coder", Provider: "generated", Model: "g1", SystemPrompt: "Answer."}, "test"); err != nil {
		t.Fatal(err)
	}
	want(t, f, c.ana, "POST", "/api/projects", `{"name":"Field"}`, http.StatusCreated)
	want(t, f, c.ana, "POST", "/api/knowledge", `{"name":"chats"}`, http.StatusCreated)
	want(t, f, c.ana, "PUT", "/api/knowledge/chats/attachment", `{"project":"Field","channel":"general"}`, http.StatusOK)
	return c
}

type generatedTurn struct{ speaker, text string }

// portTurns is a generated conversation's main path, whose assistant
// states the port.
var portTurns = []generatedTurn{
	{"user", "generated question: which port does the staging proxy use?"},
	{"tool", "Bash: grep port proxy.conf"},
	{"result", "port 8443"},
	{"assistant", "The staging proxy listens on port 8443."},
}

// sessionJSON is a generated chatgpt conversation's structure, as an import
// sends it: two branches off its main path, and one turn of it that
// carried a file.
func sessionJSON(id string, turns []generatedTurn) string {
	at := time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)
	var ts []string
	for i, tr := range turns {
		ts = append(ts, fmt.Sprintf(`{"speaker":%q,"at":%q,"text":%q}`, tr.speaker, at.Add(time.Duration(i)*time.Minute).Format(time.RFC3339), tr.text))
	}
	end := at.Add(time.Duration(len(turns)) * time.Minute).Format(time.RFC3339)
	return fmt.Sprintf(`{"tool":"chatgpt","id":%q,"started_at":%q,"ended_at":%q,"turns":[%s],"branches":2,"omitted":{"turns":0,"results":0,"cut_turns":0,"attachments":1}}`, id, at.Format(time.RFC3339), end, strings.Join(ts, ","))
}

// importedDoc is a document an import wrote, and what a person reading it
// holds to pin it: the path to continue it and the digests.
type importedDoc struct {
	id, digest, structure, path string
}

// pins is the body that continues d as read, in project.
func (d importedDoc) pins(project string) string {
	return fmt.Sprintf(`{"document_digest":%q,"structure_digest":%q,"project":%q,"agents":["coder"]}`, d.digest, d.structure, project)
}

// importSession writes a generated session's document into source through
// the import route, as ana, and reads back from the document listing, as a
// client does, what pins it.
func (c continueFixture) importSession(t *testing.T, source, id string, turns []generatedTurn) importedDoc {
	t.Helper()
	doc := want(t, c.fixture, c.ana, "POST", "/api/knowledge/"+source+"/imports", `{"title":"A generated chat","uri":"chatgpt:`+id+`","text":"# A generated chat\n\ngenerated text","session":`+sessionJSON(id, turns)+`}`, http.StatusCreated)
	read := c.listedDocument(t, c.ana, source, fmt.Sprint(doc["id"]))
	if read["structure_digest"] == nil || read["structure_digest"] != doc["structure_digest"] || read["digest"] != doc["digest"] {
		t.Fatalf("the import answered %v and the listing %v", doc, read)
	}
	return importedDoc{id: fmt.Sprint(read["id"]), digest: fmt.Sprint(read["digest"]), structure: fmt.Sprint(read["structure_digest"]), path: "/api/knowledge/" + source + "/documents/" + fmt.Sprint(read["id"]) + "/continue"}
}

// listedDocument is document id as tok reads it in source's listing.
func (c continueFixture) listedDocument(t *testing.T, tok, source, id string) map[string]any {
	t.Helper()
	for _, d := range sliceOf(want(t, c.fixture, tok, "GET", "/api/knowledge/"+source+"/documents", "", http.StatusOK)["documents"]) {
		if obj(d)["id"] == id {
			return obj(d)
		}
	}
	t.Fatalf("%s is not listed in %s", id, source)
	return nil
}

// listed reports whether tok's page of conversations holds id.
func (c continueFixture) listed(t *testing.T, tok, id string) bool {
	t.Helper()
	for _, row := range sliceOf(want(t, c.fixture, tok, "GET", "/api/threads", "", http.StatusOK)["threads"]) {
		if obj(row)["id"] == id {
			return true
		}
	}
	return false
}

// ask posts text in thread as tok, waits for the one run it starts, and
// answers that run, and what the model was sent for it. Runs here are one
// at a time, so the model's latest request is that run's.
func (c continueFixture) ask(t *testing.T, tok, thread, text string) (map[string]any, string) {
	t.Helper()
	posted := want(t, c.fixture, tok, "POST", "/api/threads/"+thread+"/messages", fmt.Sprintf(`{"text":%q}`, text), http.StatusCreated)
	started := sliceOf(posted["runs"])
	if len(started) != 1 {
		t.Fatalf("posting %q started %d runs: %v", text, len(started), posted)
	}
	run := fmt.Sprint(obj(started[0])["id"])
	waitRun(t, c.fixture, tok, run)
	return want(t, c.fixture, tok, "GET", "/api/runs/"+run, "", http.StatusOK), c.model.last()
}

// The route starts a private conversation of the caller's in the source's
// project, whose origin says what a client shows: the turns sent of those
// the import kept, the branches not continued, the files not imported and
// the pinned digest. It is audited, listed for its owner and for no one
// else, and continuing the same document again starts another.
func TestAnImportedDocumentIsContinuedThroughItsRoute(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		c := newContinueFixture(t, open(t))
		doc := c.importSession(t, "chats", "generated-route", portTurns)

		th := want(t, c.fixture, c.bo, "POST", doc.path, doc.pins("Field"), http.StatusCreated)
		id := fmt.Sprint(th["id"])
		im := obj(th["import"])
		omitted, cut := obj(im["omitted"]), obj(im["import_cut"])
		if th["visibility"] != "private" || th["project"] != "Field" || th["created_by"] != "bo" ||
			im["source"] != "chats" || im["document"] != doc.id || im["document_digest"] != doc.digest || im["structure_digest"] != doc.structure ||
			im["structure"] != threads.StructureKept || im["tool"] != "chatgpt" || im["by"] != "bo" || im["imported_by"] != "ana" ||
			len(sliceOf(im["context"])) != len(portTurns) || omitted["turns"] != 0.0 || omitted["results"] != 0.0 ||
			im["branches"] != 2.0 || cut["attachments"] != 1.0 || th["title"] != "A generated chat" {
			t.Fatalf("the continuation answers %v", th)
		}

		// Audited, naming what it pinned.
		rows, err := c.p.Audit.List("acme", audit.Query{Resource: "conversation", From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour), Limit: 50}, nil)
		if err != nil {
			t.Fatal(err)
		}
		audited := 0
		for _, row := range rows {
			if row.Action == "conversation.continue_import" && row.Thread == id && row.Actor == "00u-bo" && row.Outcome == audit.Succeeded &&
				row.After["document"] == doc.id && row.After["document_digest"] == doc.digest && row.After["structure_digest"] == doc.structure && row.Project == "Field" {
				audited++
			}
		}
		if audited != 1 {
			t.Fatalf("%d audit records of the continuation in %+v", audited, rows)
		}

		// Its owner lists and reads it; another person of the workspace,
		// who works in the same project, does neither.
		if !c.listed(t, c.bo, id) {
			t.Fatal("the continuation is not listed for its owner")
		}
		if c.listed(t, c.cy, id) {
			t.Fatal("the continuation is listed for another person")
		}
		want(t, c.fixture, c.bo, "GET", "/api/threads/"+id, "", http.StatusOK)
		want(t, c.fixture, c.cy, "GET", "/api/threads/"+id, "", http.StatusNotFound)

		// Continuing the same document again starts a second conversation,
		// with an origin of its own and its own audit record.
		again := want(t, c.fixture, c.bo, "POST", doc.path, doc.pins("Field"), http.StatusCreated)
		second := fmt.Sprint(again["id"])
		if second == id || obj(again["import"])["document_digest"] != doc.digest {
			t.Fatalf("the second continuation: %v", again)
		}
		if !c.listed(t, c.bo, id) || !c.listed(t, c.bo, second) {
			t.Fatal("both continuations are not listed for their owner")
		}
		// A message in one is not in the other.
		c.ask(t, c.bo, second, "@coder generated marker for the second conversation")
		if room := want(t, c.fixture, c.bo, "GET", "/api/threads/"+id, "", http.StatusOK); strings.Contains(fmt.Sprint(room), "generated marker") {
			t.Fatalf("the first continuation holds the second's message: %v", room)
		}
	})
}

// The first turn of a continuation sends its agent the imported lines,
// fenced and labelled, and the agent answers a fact from them. A second
// turn sends those lines and the conversation's own turns, and nothing of
// another continuation of the same document.
func TestAContinuationsAgentAnswersFromTheImportedLines(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		c := newContinueFixture(t, open(t))
		doc := c.importSession(t, "chats", "generated-fact", portTurns)
		first := fmt.Sprint(want(t, c.fixture, c.bo, "POST", doc.path, doc.pins("Field"), http.StatusCreated)["id"])
		other := fmt.Sprint(want(t, c.fixture, c.bo, "POST", doc.path, doc.pins("Field"), http.StatusCreated)["id"])

		run, sent := c.ask(t, c.bo, first, "@coder which port does the staging proxy use?")
		for _, line := range []string{
			"A line marked imported is from a conversation in another tool",
			"imported from chatgpt, the person there: (imported, not said here) generated question: which port does the staging proxy use?",
			"imported from chatgpt, what a tool there gave back: (imported, not said here) port 8443",
			"imported from chatgpt, the assistant there: (imported, not said here) The staging proxy listens on port 8443.",
			"End of the conversation so far.",
		} {
			if !strings.Contains(sent, line) {
				t.Fatalf("the first turn was not sent %q:\n%s", line, sent)
			}
		}
		if strings.Contains(sent, "earlier turns omitted") {
			t.Fatalf("the first turn left part of the imported lines out:\n%s", sent)
		}
		if run["status"] != "completed" || run["answer"] != "The staging proxy uses port 8443." {
			t.Fatalf("the first turn answered %v (%v)", run["answer"], run["status"])
		}

		c.ask(t, c.bo, other, "@coder generated marker for the other conversation")
		_, sent = c.ask(t, c.bo, first, "@coder and which port again?")
		for _, line := range []string{
			"(imported, not said here) The staging proxy listens on port 8443.",
			"which port does the staging proxy use?",
			"The staging proxy uses port 8443.",
		} {
			if !strings.Contains(sent, line) {
				t.Fatalf("the second turn was not sent %q:\n%s", line, sent)
			}
		}
		if strings.Contains(sent, "generated marker") {
			t.Fatalf("the second turn was sent another conversation's turn:\n%s", sent)
		}
	})
}

// Each refusal starts no conversation: no runs:write (403); no
// knowledge:read, another workspace, a source the caller cannot see, a
// missing, excluded or deleted document or source (404); a document that is
// not an imported session or whose structure an edit made untrusted, a
// missing project, a malformed digest or a body past its bounds (400 or
// 413); a document changed since it was read, a structure digest that is
// not the one kept, or a source detached from the project it was seen in
// (409).
func TestAContinuationIsRefusedThroughItsRoute(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		c := newContinueFixture(t, open(t))
		doc := c.importSession(t, "chats", "generated-refusals", portTurns)
		viewer := c.person(t, "acme", "00u-dee", "Dee", "humagen-viewers")
		runsOnly := c.machine(t, "acme", "humagen:runs:write", "humagen:runs:read")
		foreign := c.person(t, "globex", "00u-bo", "Bo", "humagen-admins")
		// Vault is ana's alone, and so is the source scoped to it.
		want(t, c.fixture, c.ana, "POST", "/api/projects", `{"name":"Vault"}`, http.StatusCreated)
		want(t, c.fixture, c.ana, "PUT", "/api/projects/Vault/members", `{"version":1,"members":[{"who":"00u-ana","role":"owner"}]}`, http.StatusOK)
		if _, err := c.p.Knowledge.Create("acme", "vault-chats", "", "00u-ana", time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := c.p.Knowledge.Configure("acme", "vault-chats", knowledge.SourceConfig{Mode: knowledge.Indexed, Provider: "manual", Owner: "00u-ana", Project: "Vault"}, time.Now()); err != nil {
			t.Fatal(err)
		}
		vault := c.importSession(t, "vault-chats", "generated-vault", portTurns)
		mine := func() int {
			t.Helper()
			list, err := c.p.Threads.Records("acme", func(th threads.Thread) bool { return th.Import != nil })
			if err != nil {
				t.Fatal(err)
			}
			return len(list)
		}
		refused := func(name, tok, path, body string, code int, words string) {
			t.Helper()
			before := mine()
			res, out := c.do(t, nil, "POST", path, tok, body)
			if res.StatusCode != code || !strings.Contains(fmt.Sprint(out["error"]), words) {
				t.Errorf("%s: %d %v, want %d %q", name, res.StatusCode, out, code, words)
			}
			if after := mine(); after != before {
				t.Errorf("%s: started %d conversations", name, after-before)
			}
		}

		refused("no runs:write", viewer, doc.path, doc.pins("Field"), http.StatusForbidden, "runs:write")
		refused("no knowledge:read", runsOnly, doc.path, doc.pins("Field"), http.StatusNotFound, "not found")
		refused("another workspace", foreign, doc.path, doc.pins("Field"), http.StatusNotFound, "not found")
		refused("a source the caller cannot see", c.bo, vault.path, vault.pins("Vault"), http.StatusNotFound, "not found")
		refused("a missing document", c.bo, "/api/knowledge/chats/documents/0000000000000000/continue", doc.pins("Field"), http.StatusNotFound, "not found")
		refused("a missing source", c.bo, "/api/knowledge/no-such-source/documents/"+doc.id+"/continue", doc.pins("Field"), http.StatusNotFound, "not found")

		refused("no project", c.bo, doc.path, fmt.Sprintf(`{"document_digest":%q,"structure_digest":%q}`, doc.digest, doc.structure), http.StatusBadRequest, "name the project")
		refused("a malformed digest", c.bo, doc.path, `{"document_digest":"stale","project":"Field"}`, http.StatusBadRequest, "digest")
		refused("an unknown field", c.bo, doc.path, strings.Replace(doc.pins("Field"), `{`, `{"source":"vault-chats",`, 1), http.StatusBadRequest, "")
		refused("a title past its bound", c.bo, doc.path, strings.Replace(doc.pins("Field"), `{`, `{"title":"`+strings.Repeat("t", threads.MaxTitle+1)+`",`, 1), http.StatusBadRequest, "title")
		refused("too many agents", c.bo, doc.path, strings.Replace(doc.pins("Field"), `["coder"]`, `["a","b","c","d","e","f"]`, 1), http.StatusBadRequest, "")
		refused("a body past the cap", c.bo, doc.path, strings.Replace(doc.pins("Field"), `{`, `{"title":"`+strings.Repeat("t", maxBody)+`",`, 1), http.StatusRequestEntityTooLarge, "too large")

		refused("a wrong document digest alone", c.bo, doc.path, strings.Replace(doc.pins("Field"), doc.digest, strings.Repeat("0", 64), 1), http.StatusConflict, "changed since it was read")
		refused("a stale structure digest", c.bo, doc.path, strings.Replace(doc.pins("Field"), doc.structure, strings.Repeat("0", 64), 1), http.StatusConflict, "changed since it was read")
		refused("no structure digest for a kept structure", c.bo, doc.path, strings.Replace(doc.pins("Field"), `"structure_digest":"`+doc.structure+`",`, "", 1), http.StatusConflict, "changed since it was read")

		// A document an ordinary route added whose uri names no session is
		// not an imported conversation.
		note := want(t, c.fixture, c.ana, "POST", "/api/knowledge/chats/documents", `{"title":"A runbook","uri":"https://example.invalid/runbook","text":"generated runbook"}`, http.StatusCreated)
		notePath := "/api/knowledge/chats/documents/" + fmt.Sprint(note["id"]) + "/continue"
		refused("a document that is no session", c.bo, notePath, fmt.Sprintf(`{"document_digest":%q,"project":"Field"}`, note["digest"]), http.StatusBadRequest, "not an imported conversation")

		// An edit leaves the structure untrusted.
		edited := c.importSession(t, "chats", "generated-edited", portTurns)
		rewritten := want(t, c.fixture, c.ana, "PUT", "/api/knowledge/chats/documents/"+edited.id+"/text", fmt.Sprintf(`{"title":"A generated chat","text":"rewritten by hand","digest":%q}`, edited.digest), http.StatusOK)
		refused("an untrusted structure", c.bo, edited.path, fmt.Sprintf(`{"document_digest":%q,"project":"Field"}`, rewritten["digest"]), http.StatusBadRequest, "cannot be trusted")

		// Reimported since it was read, it is not what was read.
		grown := c.importSession(t, "chats", "generated-grown", portTurns)
		longer := append(append([]generatedTurn{}, portTurns...), generatedTurn{"user", "generated follow-up"})
		want(t, c.fixture, c.ana, "PUT", "/api/knowledge/chats/imports/"+grown.id, fmt.Sprintf(`{"title":"A generated chat","text":"generated text, grown","digest":%q,"session":%s}`, grown.digest, sessionJSON("generated-grown", longer)), http.StatusOK)
		refused("a changed document", c.bo, grown.path, grown.pins("Field"), http.StatusConflict, "changed since it was read")

		// Excluded from search, it is not found.
		excluded := c.importSession(t, "chats", "generated-excluded", portTurns)
		want(t, c.fixture, c.ana, "PUT", "/api/knowledge/chats/documents/"+excluded.id, `{"excluded":true}`, http.StatusOK)
		refused("an excluded document", c.bo, excluded.path, excluded.pins("Field"), http.StatusNotFound, "not found")

		// A source deleted since it was read is not found.
		want(t, c.fixture, c.ana, "POST", "/api/knowledge", `{"name":"old-chats"}`, http.StatusCreated)
		gone := c.importSession(t, "old-chats", "generated-gone", portTurns)
		want(t, c.fixture, c.ana, "DELETE", "/api/knowledge/old-chats", "", http.StatusNoContent)
		refused("a deleted source", c.bo, gone.path, gone.pins("~"), http.StatusNotFound, "not found")

		// A project the caller only views is not theirs to start a
		// conversation in.
		want(t, c.fixture, c.ana, "POST", "/api/projects", `{"name":"Gallery"}`, http.StatusCreated)
		want(t, c.fixture, c.ana, "PUT", "/api/projects/Gallery/members", `{"version":1,"members":[{"who":"*","role":"viewer"},{"who":"00u-ana","role":"owner"}]}`, http.StatusOK)
		want(t, c.fixture, c.ana, "POST", "/api/knowledge", `{"name":"gallery-chats"}`, http.StatusCreated)
		want(t, c.fixture, c.ana, "PUT", "/api/knowledge/gallery-chats/attachment", `{"project":"Gallery","channel":"general"}`, http.StatusOK)
		viewed := c.importSession(t, "gallery-chats", "generated-viewed", portTurns)
		refused("a project the caller may not post in", c.bo, viewed.path, viewed.pins("Gallery"), http.StatusForbidden, "member of Gallery")

		// A workspace that sends agents no earlier conversation starts no
		// continuation.
		_, settings := read(t, c.fixture, c.ana, "/api/settings")
		settings["history"] = "none"
		want(t, c.fixture, c.ana, "PUT", "/api/settings", mustJSON(t, settings), http.StatusOK)
		refused("history none", c.bo, doc.path, doc.pins("Field"), http.StatusBadRequest, "sends no earlier conversation")
		settings["history"] = "recent"
		want(t, c.fixture, c.ana, "PUT", "/api/settings", mustJSON(t, settings), http.StatusOK)

		// Detached from Field since it was read there, it is refused, and
		// it is not continued in the workspace itself instead.
		want(t, c.fixture, c.ana, "DELETE", "/api/knowledge/chats/attachment?project=Field&channel=general", "", http.StatusNoContent)
		refused("a detached source", c.bo, doc.path, doc.pins("Field"), http.StatusConflict, "no longer filed in that project")
		// Read again where it now is, it is continued.
		if th := want(t, c.fixture, c.bo, "POST", doc.path, doc.pins("~"), http.StatusCreated); th["project"] != "" && th["project"] != "~" {
			t.Fatalf("continued in %v", th["project"])
		}
	})
}

// The imported lines a continuation keeps are the newest of the main path
// that fit half the workspace's history limit, counted as the transcript
// writes them (each with its label, its mark and its line break), and at
// most a fork's bounds. Every turn left out is counted, and the first turn
// sends all that were kept.
func TestAContinuationsLinesFitHalfTheHistoryLimit(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		c := newContinueFixture(t, open(t))
		var long []generatedTurn
		for i := range knowledge.MaxImportedTurns {
			speaker := "user"
			if i%2 == 1 {
				speaker = "assistant"
			}
			long = append(long, generatedTurn{speaker, fmt.Sprintf("generated turn %03d ", i) + strings.Repeat("x", 400)})
		}
		doc := c.importSession(t, "chats", "generated-long", long)
		th := want(t, c.fixture, c.bo, "POST", doc.path, doc.pins("Field"), http.StatusCreated)
		im := obj(th["import"])
		lines, omitted := sliceOf(im["context"]), obj(im["omitted"])
		budget := c.p.Settings("acme").HistoryLimit / 2
		labels := map[string]string{"user": "imported from chatgpt, the person there", "assistant": "imported from chatgpt, the assistant there"}
		rendered := 0
		for _, turn := range lines {
			tr := obj(turn)
			mark := "(imported, not said here) "
			if tr["cut"] == true {
				mark += "(the opening of a longer turn) "
			}
			rendered += len(labels[fmt.Sprint(tr["speaker"])] + ": " + mark + fmt.Sprint(tr["text"]) + "\n")
		}
		kept := len(lines) + int(omitted["turns"].(float64)) + int(omitted["results"].(float64))
		if len(lines) == 0 || len(lines) >= len(long) || rendered > budget || kept != len(long) || omitted["cut"] != 0.0 {
			t.Fatalf("kept %d lines of %d (%d bytes rendered, budget %d), omitted %v", len(lines), len(long), rendered, budget, omitted)
		}
		if newest := fmt.Sprint(obj(lines[len(lines)-1])["text"]); !strings.HasPrefix(newest, fmt.Sprintf("generated turn %03d ", len(long)-1)) {
			t.Fatalf("the newest line kept is %.40q", newest)
		}
		_, sent := c.ask(t, c.bo, fmt.Sprint(th["id"]), "@coder go on")
		if strings.Contains(sent, "earlier turns omitted") || strings.Count(sent, "(imported, not said here)") != len(lines) {
			t.Fatalf("the first turn did not send every kept line:\n%s", sent)
		}
	})
}

// A client continues an imported conversation with only what the API gives
// it: the import route's answer, then the document listing's `digest` and
// `structure_digest`, sent back as they were read. A document no import
// kept a structure with is listed without either structure field.
func TestAClientContinuesWithWhatTheListingGivesIt(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		c := newContinueFixture(t, open(t))
		imported := want(t, c.fixture, c.ana, "POST", "/api/knowledge/chats/imports", `{"title":"A generated chat","uri":"chatgpt:generated-client","text":"generated text","session":`+sessionJSON("generated-client", portTurns)+`}`, http.StatusCreated)
		id := fmt.Sprint(imported["id"])
		read := c.listedDocument(t, c.bo, "chats", id)
		digest, structure := read["digest"], read["structure_digest"]
		if s, ok := structure.(string); !ok || len(s) != 64 || read["structure_bytes"] == nil || structure != imported["structure_digest"] {
			t.Fatalf("listed %v, imported %v", read, imported)
		}
		th := want(t, c.fixture, c.bo, "POST", "/api/knowledge/chats/documents/"+id+"/continue", fmt.Sprintf(`{"document_digest":%q,"structure_digest":%q,"project":"Field"}`, digest, structure), http.StatusCreated)
		if im := obj(th["import"]); im["document_digest"] != digest || im["structure_digest"] != structure || im["structure"] != threads.StructureKept {
			t.Fatalf("continued %v", th)
		}

		plain := want(t, c.fixture, c.ana, "POST", "/api/knowledge/chats/documents", `{"title":"A runbook","uri":"https://example.invalid/runbook","text":"generated runbook"}`, http.StatusCreated)
		if got := c.listedDocument(t, c.bo, "chats", fmt.Sprint(plain["id"])); got["structure_digest"] != nil || got["structure_bytes"] != nil {
			t.Fatalf("a document with no structure is listed %v", got)
		}
	})
}

// A reader who cannot see a source gets neither a document's digest nor
// its structure's: the listing is not found for a project they are not
// in, or another workspace, and is refused without knowledge:read.
func TestAStructuresDigestIsReadOnlyWithItsSource(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		c := newContinueFixture(t, open(t))
		want(t, c.fixture, c.ana, "POST", "/api/projects", `{"name":"Vault"}`, http.StatusCreated)
		want(t, c.fixture, c.ana, "PUT", "/api/projects/Vault/members", `{"version":1,"members":[{"who":"00u-ana","role":"owner"}]}`, http.StatusOK)
		if _, err := c.p.Knowledge.Create("acme", "vault-chats", "", "00u-ana", time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := c.p.Knowledge.Configure("acme", "vault-chats", knowledge.SourceConfig{Mode: knowledge.Indexed, Provider: "manual", Owner: "00u-ana", Project: "Vault"}, time.Now()); err != nil {
			t.Fatal(err)
		}
		vault := c.importSession(t, "vault-chats", "generated-hidden", portTurns)
		foreign := c.person(t, "globex", "00u-bo", "Bo", "humagen-admins")
		runsOnly := c.machine(t, "acme", "humagen:runs:write", "humagen:runs:read")
		for name, tok := range map[string]string{"a person outside Vault": c.bo, "another workspace": foreign, "no knowledge:read": runsOnly} {
			res, out := c.do(t, nil, "GET", "/api/knowledge/vault-chats/documents", tok, "")
			if res.StatusCode != http.StatusNotFound && res.StatusCode != http.StatusForbidden {
				t.Errorf("%s: %d %v", name, res.StatusCode, out)
			}
			if raw := fmt.Sprint(out); strings.Contains(raw, vault.structure) || strings.Contains(raw, vault.digest) {
				t.Errorf("%s was given a digest: %v", name, out)
			}
		}
		// Bo, outside Vault, finds no digest anywhere else either: the
		// source list and the search.
		for _, path := range []string{"/api/knowledge", "/api/knowledge/search?q=generated"} {
			res, out := c.do(t, nil, "GET", path, c.bo, "")
			if raw := fmt.Sprint(out); strings.Contains(raw, vault.structure) || strings.Contains(raw, vault.digest) {
				t.Errorf("%s (%d) gave a digest: %v", path, res.StatusCode, out)
			}
		}
		// Its owner reads both.
		if got := c.listedDocument(t, c.ana, "vault-chats", vault.id); got["structure_digest"] != vault.structure {
			t.Fatalf("the owner reads %v", got)
		}
	})
}

// earlier writes document id of source back as a release before documents
// recorded their structure's digest kept it: its structure's size, and no
// digest. change, when set, also rewrites the structure's record.
func (c continueFixture) earlier(t *testing.T, source, id string, change func(record map[string]any)) {
	t.Helper()
	tab := state.NewTable[map[string]any](c.st, "knowledge")
	rows, err := tab.All("acme", "doc/"+source+"/", 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if obj(row.Value["document"])["id"] != id {
			continue
		}
		if _, err := tab.Update("acme", row.Key, func(r map[string]any, _ bool) (map[string]any, error) {
			delete(obj(r["document"]), "structure_digest")
			return r, nil
		}); err != nil {
			t.Fatal(err)
		}
		if change != nil {
			key := "imported/" + source + "/" + row.Key[strings.LastIndexByte(row.Key, '/')+1:]
			if _, err := tab.Update("acme", key, func(r map[string]any, _ bool) (map[string]any, error) {
				change(obj(r["imported"]))
				return r, nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	t.Fatalf("no document %s in %s", id, source)
}

// A document whose structure an earlier release kept is listed with its
// structure's size and no digest of it, and a client continues it through
// the API alone by naming none: the continuation pins the digest of the
// trusted structure. It is refused when its structure's record is not
// trusted (another revision's, or malformed), and when a structure was
// written for it since it was read, since that write records a digest.
func TestADocumentFromAnEarlierReleaseIsContinuedWithoutAStructureDigest(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		c := newContinueFixture(t, open(t))
		continued := func(t *testing.T) int {
			t.Helper()
			list, err := c.p.Threads.Records("acme", func(th threads.Thread) bool { return th.Import != nil })
			if err != nil {
				t.Fatal(err)
			}
			return len(list)
		}
		answered := want(t, c.fixture, c.ana, "POST", "/api/knowledge/chats/imports", `{"title":"A generated chat","uri":"chatgpt:generated-earlier","text":"generated text","session":`+sessionJSON("generated-earlier", portTurns)+`}`, http.StatusCreated)
		id := fmt.Sprint(answered["id"])
		c.earlier(t, "chats", id, nil)
		read := c.listedDocument(t, c.bo, "chats", id)
		if read["structure_digest"] != nil || read["structure_bytes"] == nil {
			t.Fatalf("an earlier document is listed %v", read)
		}
		path := "/api/knowledge/chats/documents/" + id + "/continue"
		body := fmt.Sprintf(`{"document_digest":%q,"project":"Field"}`, read["digest"])
		th := want(t, c.fixture, c.bo, "POST", path, body, http.StatusCreated)
		if im := obj(th["import"]); im["structure"] != threads.StructureKept || im["structure_digest"] != answered["structure_digest"] || im["document_digest"] != read["digest"] {
			t.Fatalf("an earlier document continued as %v", im)
		}

		before := continued(t)
		for name, change := range map[string]func(map[string]any){
			"another revision's": func(r map[string]any) { r["document"] = strings.Repeat("0", 64) },
			"malformed":          func(r map[string]any) { r["structure"] = strings.Repeat("0", 64) },
		} {
			doc := want(t, c.fixture, c.ana, "POST", "/api/knowledge/chats/imports", `{"title":"A generated chat","uri":"chatgpt:generated-`+name[:5]+`","text":"generated text","session":`+sessionJSON("generated-"+name[:5], portTurns)+`}`, http.StatusCreated)
			c.earlier(t, "chats", fmt.Sprint(doc["id"]), change)
			res, out := c.do(t, nil, "POST", "/api/knowledge/chats/documents/"+fmt.Sprint(doc["id"])+"/continue", c.bo, fmt.Sprintf(`{"document_digest":%q,"project":"Field"}`, doc["digest"]))
			if res.StatusCode != http.StatusBadRequest || !strings.Contains(fmt.Sprint(out["error"]), "cannot be trusted") {
				t.Errorf("a structure record that is %s: %d %v", name, res.StatusCode, out)
			}
		}
		if after := continued(t); after != before {
			t.Fatalf("an untrusted structure started %d conversations", after-before)
		}

		// Read without a digest, then edited back to the same text (so its
		// structure is untrusted) and restored with another structure: the
		// restore records the new structure's digest, so what was read is
		// no longer so.
		raced := want(t, c.fixture, c.ana, "POST", "/api/knowledge/chats/imports", `{"title":"A generated chat","uri":"chatgpt:generated-raced","text":"generated text","session":`+sessionJSON("generated-raced", portTurns)+`}`, http.StatusCreated)
		rid := fmt.Sprint(raced["id"])
		c.earlier(t, "chats", rid, nil)
		seen := c.listedDocument(t, c.bo, "chats", rid)
		edited := want(t, c.fixture, c.ana, "PUT", "/api/knowledge/chats/documents/"+rid+"/text", fmt.Sprintf(`{"title":"A generated chat","text":"generated text","digest":%q}`, seen["digest"]), http.StatusOK)
		other := append(append([]generatedTurn{}, portTurns...), generatedTurn{"user", "generated other turn"})
		restored := want(t, c.fixture, c.ana, "PUT", "/api/knowledge/chats/imports/"+rid, fmt.Sprintf(`{"title":"A generated chat","text":"generated text","digest":%q,"session":%s}`, edited["digest"], sessionJSON("generated-raced", other)), http.StatusOK)
		if restored["digest"] != seen["digest"] || restored["structure_digest"] == nil {
			t.Fatalf("the restore answered %v, read %v", restored, seen)
		}
		res, out := c.do(t, nil, "POST", "/api/knowledge/chats/documents/"+rid+"/continue", c.bo, fmt.Sprintf(`{"document_digest":%q,"project":"Field"}`, seen["digest"]))
		if res.StatusCode != http.StatusConflict || !strings.Contains(fmt.Sprint(out["error"]), "changed since it was read") {
			t.Fatalf("a structure restored since it was read: %d %v", res.StatusCode, out)
		}
	})
}

// The project named must be the one the source is filed in where the caller
// may post: another project it is attached to that the caller only views
// is theirs to read, not to continue in (403); the workspace itself, a
// project the source is not attached to, and a name in another case are
// not where it is filed (409). In Field, the continuation is the caller's
// alone: another member of Field neither reads nor posts in it.
func TestAContinuationIsFiledOnlyWhereTheCallerSawTheSource(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		c := newContinueFixture(t, open(t))
		want(t, c.fixture, c.ana, "POST", "/api/projects", `{"name":"Ops"}`, http.StatusCreated)
		want(t, c.fixture, c.ana, "PUT", "/api/projects/Ops/members", `{"version":1,"members":[{"who":"*","role":"viewer"},{"who":"00u-ana","role":"owner"}]}`, http.StatusOK)
		want(t, c.fixture, c.ana, "PUT", "/api/knowledge/chats/attachment", `{"project":"Ops","channel":"general"}`, http.StatusOK)
		doc := c.importSession(t, "chats", "generated-scoping", portTurns)
		for _, tc := range []struct {
			project string
			code    int
		}{{"Ops", http.StatusForbidden}, {"", http.StatusConflict}, {"~", http.StatusConflict}, {"Initech", http.StatusConflict}, {"field", http.StatusConflict}} {
			if res, out := c.do(t, nil, "POST", doc.path, c.bo, doc.pins(tc.project)); res.StatusCode != tc.code {
				t.Errorf("project %q: %d %v, want %d", tc.project, res.StatusCode, out, tc.code)
			}
		}
		th := want(t, c.fixture, c.bo, "POST", doc.path, doc.pins("Field"), http.StatusCreated)
		id := fmt.Sprint(th["id"])
		if th["project"] != "Field" || th["visibility"] != "private" || obj(th["import"])["source_id"] == "" {
			t.Fatalf("continued as %v", th)
		}
		want(t, c.fixture, c.cy, "GET", "/api/threads/"+id, "", http.StatusNotFound)
		want(t, c.fixture, c.cy, "POST", "/api/threads/"+id+"/messages", `{"text":"generated reply"}`, http.StatusNotFound)
	})
}
