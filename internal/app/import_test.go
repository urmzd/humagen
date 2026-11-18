package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/library"
)

// importServer plays the knowledge and skill library routes the import
// uses. A source takes at most cap documents, and says so the way the
// server does.
type importServer struct {
	mu      sync.Mutex
	cap     int
	docs    map[string][]map[string]string
	said    map[string]string // each source's description
	skills  []map[string]any
	agents  []map[string]any
	corpora map[string][]string
	// projects are the workspace's project names, and attached what each
	// source was attached to.
	projects []string
	attached map[string]string
	auth     []string
	writes   int
	refused  map[string]bool
	// seq numbers the documents. docWrites counts the calls that add or
	// replace one, and failReplace makes a replacement fail as a server
	// that could not write does.
	seq         int
	docWrites   int
	failReplace bool
}

func (s *importServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	if s.docs == nil {
		s.docs = map[string][]map[string]string{}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.auth = append(s.auth, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		problem := func(status int, detail string) {
			w.WriteHeader(status)
			fmt.Fprintf(w, `{"status":%d,"detail":%q}`, status, detail)
		}
		if r.Method != http.MethodGet {
			s.writes++
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/"), "/")
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/knowledge":
			var sources []map[string]string
			for name := range s.docs {
				sources = append(sources, map[string]string{"name": name, "description": s.said[name]})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"sources": sources})
		case r.Method == "POST" && r.URL.Path == "/api/knowledge":
			var body struct{ Name, Description string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			if _, ok := s.docs[body.Name]; ok {
				problem(409, "exists")
				return
			}
			s.docs[body.Name] = []map[string]string{}
			if s.said == nil {
				s.said = map[string]string{}
			}
			s.said[body.Name] = body.Description
			w.WriteHeader(201)
			fmt.Fprint(w, `{}`)
		case len(parts) == 3 && parts[0] == "knowledge" && parts[2] == "documents" && r.Method == "GET":
			out := []map[string]any{}
			for _, d := range s.docs[parts[1]] {
				out = append(out, map[string]any{"id": d["id"], "title": d["title"], "uri": d["uri"], "digest": d["digest"], "excluded": d["excluded"] == "true"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"documents": out})
		case len(parts) == 5 && parts[0] == "knowledge" && parts[2] == "documents" && parts[4] == "text" && r.Method == "PUT":
			// The replacement the server makes: only from the digest the
			// document has, in place, keeping its id and its exclusion.
			s.docWrites++
			var body struct {
				Title, Text string
				Digest      *string
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if s.failReplace {
				problem(500, "the store is not answering")
				return
			}
			for _, d := range s.docs[parts[1]] {
				if d["id"] != parts[3] {
					continue
				}
				if body.Digest == nil || *body.Digest != d["digest"] {
					problem(409, "the document changed since it was read; read it again")
					return
				}
				d["title"], d["text"], d["digest"] = body.Title, body.Text, knowledge.TextDigest(body.Text)
				fmt.Fprintf(w, `{"id":%q}`, d["id"])
				return
			}
			problem(404, "no such document")
		case len(parts) == 3 && parts[0] == "knowledge" && parts[2] == "documents" && r.Method == "POST":
			var doc map[string]string
			_ = json.NewDecoder(r.Body).Decode(&doc)
			if s.refused[doc["uri"]] {
				problem(413, "too large")
				return
			}
			if s.cap > 0 && len(s.docs[parts[1]]) >= s.cap {
				problem(400, "knowledge: invalid: source document or byte limit reached")
				return
			}
			s.seq++
			s.docWrites++
			doc["id"], doc["digest"] = fmt.Sprintf("d%d", s.seq), knowledge.TextDigest(doc["text"])
			s.docs[parts[1]] = append(s.docs[parts[1]], doc)
			w.WriteHeader(201)
			fmt.Fprintf(w, `{"id":%q}`, doc["id"])
		case r.Method == "GET" && r.URL.Path == "/api/projects":
			// Two to a page, as the real list pages at its limit.
			from, _ := strconv.Atoi(r.URL.Query().Get("before"))
			out := []map[string]string{}
			var next any
			for i := from; i < len(s.projects); i++ {
				if len(out) == 2 {
					next = strconv.Itoa(i)
					break
				}
				out = append(out, map[string]string{"name": s.projects[i]})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"projects": out, "next": next})
		case len(parts) == 3 && parts[0] == "knowledge" && parts[2] == "attachment" && r.Method == "PUT":
			var body struct{ Project string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			if s.attached == nil {
				s.attached = map[string]string{}
			}
			s.attached[parts[1]] = body.Project
			_ = json.NewEncoder(w).Encode(map[string]string{"source": parts[1], "project": body.Project})
		case len(parts) >= 1 && parts[0] == "corpora":
			if s.corpora == nil {
				s.corpora = map[string][]string{}
			}
			members := func(name string) {
				out := []map[string]string{}
				for _, m := range s.corpora[name] {
					out = append(out, map[string]string{"kind": "source", "name": m})
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"name": name, "members": out})
			}
			switch {
			case r.Method == "GET" && len(parts) == 2:
				if _, ok := s.corpora[parts[1]]; !ok {
					problem(404, "no such corpus")
					return
				}
				members(parts[1])
			case r.Method == "POST" && len(parts) == 1:
				var body struct{ Name string }
				_ = json.NewDecoder(r.Body).Decode(&body)
				s.corpora[body.Name] = []string{}
				w.WriteHeader(201)
				members(body.Name)
			case r.Method == "POST" && len(parts) == 3 && parts[2] == "members":
				var body struct{ Kind, Name string }
				_ = json.NewDecoder(r.Body).Decode(&body)
				if len(s.corpora[parts[1]]) >= 50 {
					problem(400, "a corpus holds at most 50 members")
					return
				}
				s.corpora[parts[1]] = append(s.corpora[parts[1]], body.Name)
				members(parts[1])
			default:
				problem(404, "no such route")
			}
		case r.Method == "GET" && r.URL.Path == "/api/agents":
			_ = json.NewEncoder(w).Encode(map[string]any{"agents": s.agents})
		case r.Method == "POST" && r.URL.Path == "/api/agents":
			var spec map[string]any
			_ = json.NewDecoder(r.Body).Decode(&spec)
			if spec["name"] == "taken" {
				problem(409, "a person goes by @taken")
				return
			}
			s.agents = append(s.agents, map[string]any{"spec": spec})
			w.WriteHeader(201)
			fmt.Fprint(w, `{}`)
		case r.Method == "GET" && r.URL.Path == "/api/library/skills":
			_ = json.NewEncoder(w).Encode(map[string]any{"skills": s.skills})
		case r.Method == "POST" && r.URL.Path == "/api/library/skills":
			var body struct {
				Scope      library.Scope
				Definition library.Definition
				Version    int
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !body.Definition.Valid() || body.Version != 0 {
				problem(400, "library: invalid")
				return
			}
			s.skills = append(s.skills, map[string]any{"name": body.Definition.Name, "scope": body.Scope.Kind, "files": len(body.Definition.Files), "instructions": body.Definition.Instructions})
			w.WriteHeader(201)
			fmt.Fprint(w, `{}`)
		default:
			problem(404, "no such route")
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func putFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func claudeSession(id, started, asked string) string {
	return fmt.Sprintf(`{"type":"user","sessionId":%q,"cwd":"/work/app","timestamp":%q,"message":{"content":%q}}
{"type":"assistant","sessionId":%q,"message":{"content":[{"type":"text","text":"Done."}]}}
`, id, started, asked, id)
}

func codexSession(id, started, asked, parent string) string {
	return fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"cwd":"/work/api","timestamp":%q,"parent_thread_id":%q}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":%q}]}}
`, id, started, parent, asked)
}

// sessionHome is a home directory with three Claude Code sessions, a Codex
// session and a Codex sub-agent's.
func sessionHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	putFile(t, home, ".claude/projects/-work-app/c-1.jsonl", claudeSession("c-1", "2026-10-03T10:00:00Z", "fix the login bug"))
	putFile(t, home, ".claude/projects/-work-app/c-2.jsonl", claudeSession("c-2", "2026-10-09T10:00:00Z", "add a rate limit"))
	putFile(t, home, ".claude/projects/-work-app/c-3.jsonl", claudeSession("c-3", "2026-11-02T10:00:00Z", "write the readme"))
	putFile(t, home, ".claude/projects/-work-app/empty.jsonl", `{"type":"permission-mode","sessionId":"e"}`+"\n")
	putFile(t, home, ".codex/sessions/2026/11/03/rollout-x-1.jsonl", codexSession("x-1", "2026-11-03T09:00:00Z", "add a health route", ""))
	putFile(t, home, ".codex/sessions/2026/11/03/rollout-x-2.jsonl", codexSession("x-2", "2026-11-03T09:01:00Z", "judge this action", "x-1"))
	return home
}

func runImport(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Setenv("HUMAGEN_TOKEN", "")
	var out bytes.Buffer
	err := importCmd(args, &out)
	return out.String(), err
}

func TestImportSessionsAddsEachOnceToItsMonthsSource(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	home := sessionHome(t)
	out, err := runImport(t, "sessions", "--home", home, "--server", server.URL)
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if !strings.HasPrefix(out, "Added 4 sessions (claude 3, codex 1) to 3 knowledge sources; 0 already there, 2 skipped, 0 failed.\n") || !strings.Contains(out, "They are in the corpus sessions under Knowledge. Next: give an agent that corpus, so it can search them.") {
		t.Fatalf("out = %s", out)
	}
	sort.Strings(api.corpora["sessions"])
	if got := strings.Join(api.corpora["sessions"], " "); got != "sessions-claude-2026-10 sessions-claude-2026-11 sessions-codex-2026-11" {
		t.Fatalf("corpus members = %s", got)
	}
	var names []string
	for name, docs := range api.docs {
		names = append(names, fmt.Sprintf("%s=%d", name, len(docs)))
	}
	sort.Strings(names)
	if got := strings.Join(names, " "); got != "sessions-claude-2026-10=2 sessions-claude-2026-11=1 sessions-codex-2026-11=1" {
		t.Fatalf("sources = %s", got)
	}
	doc := api.docs["sessions-codex-2026-11"][0]
	if doc["uri"] != "codex:x-1" || doc["title"] != "add a health route (api)" || !strings.Contains(doc["text"], "## Asked\n\nadd a health route") {
		t.Fatalf("doc = %v", doc)
	}

	// A second run finds them there and writes nothing.
	api.writes = 0
	out, err = runImport(t, "sessions", "--home", home, "--server", server.URL, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var res sessionsResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if res.Added != 0 || res.Present != 4 || api.writes != 0 || len(api.corpora["sessions"]) != 3 || strings.Join(res.Corpora, ",") != "sessions" {
		t.Fatalf("second run = %+v, writes = %d", res, api.writes)
	}
}

func TestImportSessionsFiltersAndTakesExports(t *testing.T) {
	home := sessionHome(t)
	export := filepath.Join(t.TempDir(), "conversations.json")
	putFile(t, filepath.Dir(export), "conversations.json", `[{"conversation_id":"g-1","title":"Trip plan","create_time":1760000000,"current_node":"n1","mapping":{"n1":{"message":{"author":{"role":"user"},"content":{"parts":["plan a trip"]}}}}}]`)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"since a date", []string{"--since", "2026-11-01"}, "claude:c-3 codex:x-1"},
		{"children too", []string{"--from", "codex", "--children"}, "codex:x-1 codex:x-2"},
		{"matching a title", []string{"--match", "rate limit"}, "claude:c-2"},
		{"matching a directory", []string{"--match", "/work/api"}, "codex:x-1"},
		{"only an export", []string{"--from", "none", "--export", export}, "chatgpt:g-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &importServer{}
			server := api.start(t)
			if out, err := runImport(t, append([]string{"sessions", "--home", home, "--server", server.URL}, tt.args...)...); err != nil {
				t.Fatalf("err = %v\n%s", err, out)
			}
			var uris []string
			for _, docs := range api.docs {
				for _, d := range docs {
					uris = append(uris, d["uri"])
				}
			}
			sort.Strings(uris)
			if got := strings.Join(uris, " "); got != tt.want {
				t.Fatalf("added %s, want %s", got, tt.want)
			}
		})
	}
}

func TestImportSessionsMovesOnWhenASourceIsFull(t *testing.T) {
	api := &importServer{cap: 1}
	server := api.start(t)
	if out, err := runImport(t, "sessions", "--home", sessionHome(t), "--from", "claude", "--server", server.URL); err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if len(api.docs["sessions-claude-2026-10"]) != 1 || len(api.docs["sessions-claude-2026-10-2"]) != 1 || len(api.docs["sessions-claude-2026-11"]) != 1 {
		t.Fatalf("docs = %v", api.docs)
	}
}

func TestImportSessionsDryRunSendsNothingAndAFailureIsReported(t *testing.T) {
	api := &importServer{refused: map[string]bool{"claude:c-2": true}}
	server := api.start(t)
	home := sessionHome(t)
	out, err := runImport(t, "sessions", "--home", home, "--server", server.URL, "--dry-run", "--token", "env:NOT_SET_ANYWHERE")
	if err != nil || len(api.auth) != 0 || !strings.HasPrefix(out, "Would add 4 sessions") {
		t.Fatalf("dry run: err = %v, requests = %d, out = %s", err, len(api.auth), out)
	}
	t.Setenv("IMPORT_TOKEN", "writer")
	var buf bytes.Buffer
	err = importCmd([]string{"sessions", "--home", home, "--server", server.URL, "--token", "env:IMPORT_TOKEN"}, &buf)
	if err == nil || err.Error() != "1 sessions were not added" || !strings.Contains(buf.String(), "failed claude:c-2: POST /api/knowledge/sessions-claude-2026-10/documents: HTTP 413") || !strings.Contains(buf.String(), "Added 3 sessions") {
		t.Fatalf("err = %v\n%s", err, buf.String())
	}
	for _, got := range api.auth {
		if got != "Bearer writer" {
			t.Fatalf("authorization = %q", got)
		}
	}
	if strings.Contains(buf.String(), "writer") {
		t.Fatal("the token was printed")
	}
}

const skillMD = "---\nname: %s\ndescription: Does %s.\n---\n\n# %s\n\nFollow these steps.\n"

func skillStore(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"ship", "review", "tidy"} {
		putFile(t, dir, name+"/SKILL.md", fmt.Sprintf(skillMD, name, name, name))
	}
	putFile(t, dir, "ship/references/notes.md", "notes")
	putFile(t, dir, "ship/.DS_Store", "junk")
	putFile(t, dir, "ship/.git/config", "junk")
	putFile(t, dir, "broken/SKILL.md", "no frontmatter")
	putFile(t, dir, "big/SKILL.md", fmt.Sprintf(skillMD, "big", "big", "big"))
	putFile(t, dir, "big/data.bin", strings.Repeat("x", library.MaxPackageFileBytes+1))
	putFile(t, dir, "not-a-skill/README.md", "hello")
	putFile(t, dir, "AGENTS.md", "hello")
	return dir
}

func TestImportSkillsDraftsEachFolderAndSkipsWhatIsThere(t *testing.T) {
	api := &importServer{skills: []map[string]any{{"name": "review"}}}
	server := api.start(t)
	dir := skillStore(t)
	out, err := runImport(t, "skills", dir, "--server", server.URL)
	if err == nil || err.Error() != "2 skills were not drafted" {
		t.Fatalf("err = %v\n%s", err, out)
	}
	for _, want := range []string{
		"Drafted 2 skills in the personal library; 1 already there, 2 failed.",
		"failed big: data.bin is over 131072 bytes",
		"failed broken: library: invalid",
		"Next: test and publish each draft under Library, Skills.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("out lacks %q:\n%s", want, out)
		}
	}
	if len(api.skills) != 3 {
		t.Fatalf("skills = %v", api.skills)
	}
	ship := api.skills[1]
	if ship["name"] != "ship" || ship["scope"] != "personal" || ship["files"] != 2 || !strings.Contains(ship["instructions"].(string), "Follow these steps.") {
		t.Fatalf("ship = %v", ship)
	}
}

func TestImportSkillsOnlyNamedOnesInAScope(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	out, err := runImport(t, "skills", "--scope", "workspace", "--only", "tidy,missing", "--json", skillStore(t), "--server", server.URL)
	var res skillsResult
	if jerr := json.Unmarshal([]byte(out), &res); jerr != nil || err == nil {
		t.Fatalf("err = %v, json = %v\n%s", err, jerr, out)
	}
	if strings.Join(res.Drafted, ",") != "tidy" || len(res.Failed) != 1 || res.Failed[0].Name != "missing" || api.skills[0]["scope"] != "workspace" {
		t.Fatalf("res = %+v, skills = %v", res, api.skills)
	}
}

func TestImportArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no verb", nil, "needs a verb"},
		{"unknown verb", []string{"mcp"}, "unknown import verb"},
		{"agents without a model", []string{"agents", "--provider", "anthropic"}, "needs --provider NAME and --model NAME"},
		{"two agent directories", []string{"agents", "a", "b", "--provider", "p", "--model", "m"}, "takes one directory"},
		{"sessions with an argument", []string{"sessions", "extra"}, "unexpected arguments"},
		{"two directories", []string{"skills", "a", "b"}, "takes one directory"},
		{"bad scope", []string{"skills", "--scope", "project"}, "--scope must be"},
		{"bad source", []string{"sessions", "--source", "My Sessions"}, "--source must be"},
		{"small max", []string{"sessions", "--max-bytes", "10"}, "--max-bytes must be"},
		{"bad token", []string{"sessions", "--token", "plain"}, "--token must be a credential reference"},
		{"bad server", []string{"sessions", "--server", "ftp://x"}, "must be an http or https URL"},
		{"bad since", []string{"sessions", "--dry-run", "--since", "yesterday"}, "--since must be"},
		{"unknown tool", []string{"sessions", "--dry-run", "--from", "chatgpt"}, "--export FILE"},
		{"nothing to read", []string{"sessions", "--dry-run", "--from", "none"}, "nothing to read"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := runImport(t, tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
	for _, args := range [][]string{{"--help"}, {"sessions", "--help"}, {"skills", "-h"}, {"agents", "-h"}} {
		var out bytes.Buffer
		if err := importCmd(args, &out); !errors.Is(err, flag.ErrHelp) || !strings.Contains(out.String(), "usage: humagen import") {
			t.Fatalf("%v: err = %v, out = %s", args, err, out.String())
		}
	}
	if err := importCmd(nil, io.Discard); err == nil {
		t.Fatal("no verb was accepted")
	}
}

func TestImportSaysWhenNoServerAnswers(t *testing.T) {
	_, err := runImport(t, "sessions", "--home", sessionHome(t), "--server", "http://127.0.0.1:1")
	if err == nil || !strings.Contains(err.Error(), "is the server running?") {
		t.Fatalf("err = %v", err)
	}
}

func TestImportSkillsStopsAtWhatAScopeHolds(t *testing.T) {
	api := &importServer{}
	for i := 0; i < library.MaxSkills-1; i++ {
		api.skills = append(api.skills, map[string]any{"name": fmt.Sprintf("held-%d", i)})
	}
	server := api.start(t)
	out, err := runImport(t, "skills", skillStore(t), "--only", "review,ship,tidy", "--server", server.URL)
	if err == nil || err.Error() != "2 skills were not drafted" || !strings.Contains(out, "Drafted 1 skills") ||
		!strings.Contains(out, fmt.Sprintf("no room for 2 more: the personal library holds at most %d skills. Choose which with --only, or draft the rest in another scope: ship, tidy", library.MaxSkills)) {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if len(api.skills) != library.MaxSkills {
		t.Fatalf("the server holds %d skills", len(api.skills))
	}
}

func TestImportSkillsKeepsASkillsOwnName(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	dir := t.TempDir()
	putFile(t, dir, "folder-name/SKILL.md", fmt.Sprintf(skillMD, "own-name", "it", "it"))
	if out, err := runImport(t, "skills", dir, "--server", server.URL); err != nil || api.skills[0]["name"] != "own-name" {
		t.Fatalf("err = %v, skills = %v\n%s", err, api.skills, out)
	}
	putFile(t, dir, "second-folder/SKILL.md", fmt.Sprintf(skillMD, "own-name", "it", "it"))
	out, err := runImport(t, "skills", dir, "--server", server.URL)
	if err != nil || !strings.Contains(out, "Drafted 0 skills in the personal library; 2 already there") {
		t.Fatalf("err = %v\n%s", err, out)
	}
}

func TestImportAgentsCreatesEachFileOnTheNamedModel(t *testing.T) {
	api := &importServer{agents: []map[string]any{{"spec": map[string]any{"name": "writer"}}}}
	server := api.start(t)
	dir := t.TempDir()
	putFile(t, dir, "architect.md", "---\nname: architect\ndescription: Reviews\n  boundaries.\ntools: Read, Grep\nmodel: inherit\n---\n\n# The Architect\n\nThink in systems.\n")
	putFile(t, dir, "reviewer.agent.md", "---\ndescription: 'Deep reviewer.'\ntools:\n  ['read/file', 'agent']\nmodel: ['A', 'B']\n---\nReview with care.\n")
	putFile(t, dir, "writer.md", "---\nname: writer\n---\nWrite.\n")
	putFile(t, dir, "taken.md", "---\nname: taken\n---\nTaken.\n")
	putFile(t, dir, "plain.md", "No frontmatter here.\n")
	putFile(t, dir, "empty.md", "---\nname: empty\n---\n")
	putFile(t, dir, "notes.txt", "not an agent")
	out, err := runImport(t, "agents", dir, "--provider", "anthropic", "--model", "claude-test", "--server", server.URL)
	if err == nil || err.Error() != "3 agents were not created" {
		t.Fatalf("err = %v\n%s", err, out)
	}
	for _, want := range []string{
		"Created 2 agents on anthropic claude-test; 1 already there, 3 failed.",
		"failed empty: empty.md has no text to be the system prompt",
		"failed plain: plain.md does not start with YAML frontmatter",
		"failed taken: POST /api/agents: HTTP 409",
		"Next: each agent has no tools. Grant what it needs under Agents.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("out lacks %q:\n%s", want, out)
		}
	}
	if len(api.agents) != 3 {
		t.Fatalf("agents = %v", api.agents)
	}
	architect, reviewer := api.agents[1]["spec"].(map[string]any), api.agents[2]["spec"].(map[string]any)
	if architect["name"] != "architect" || architect["description"] != "Reviews boundaries." || architect["provider"] != "anthropic" || architect["model"] != "claude-test" ||
		architect["system_prompt"] != "# The Architect\n\nThink in systems." || len(architect["tools"].([]any)) != 0 {
		t.Fatalf("architect = %v", architect)
	}
	if reviewer["name"] != "reviewer" || reviewer["system_prompt"] != "Review with care." {
		t.Fatalf("reviewer = %v", reviewer)
	}

	api.agents = nil
	out, err = runImport(t, "agents", dir, "--provider", "policy", "--model", "default", "--only", "architect,nobody", "--json", "--server", server.URL)
	var res agentsResult
	if jerr := json.Unmarshal([]byte(out), &res); jerr != nil || err == nil || strings.Join(res.Created, ",") != "architect" || len(res.Failed) != 1 || res.Failed[0].Name != "nobody" {
		t.Fatalf("err = %v, json = %v\n%s", err, jerr, out)
	}
	if out, err := runImport(t, "agents", dir, "--provider", "p", "--model", "m", "--dry-run", "--only", "architect"); err != nil || !strings.HasPrefix(out, "Would create 1 agents") {
		t.Fatalf("dry run: err = %v\n%s", err, out)
	}
}

func TestImportSkillsNamesTheFrontmatterItLeavesOut(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	dir := t.TempDir()
	putFile(t, dir, "ship/SKILL.md", "---\nname: ship\ndescription: >\n  Commit and\n  push.\nallowed-tools: Bash(git *), Read\nuser-invocable: true\narguments:\n  - name: message\n    required: false\n---\n\n# Ship\n\nCommit, then push.\n")
	out, err := runImport(t, "skills", dir, "--server", server.URL)
	if err != nil || !strings.Contains(out, "Drafted 1 skills") || !strings.Contains(out, "ship: drafted without frontmatter the library does not read: arguments, user-invocable") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if got := api.skills[0]["instructions"]; got != "# Ship\n\nCommit, then push.\n" {
		t.Fatalf("instructions = %q", got)
	}
}

func TestImportSessionsCanLeaveSourcesOutOfACorpusOrNameIt(t *testing.T) {
	home := sessionHome(t)
	api := &importServer{}
	server := api.start(t)
	out, err := runImport(t, "sessions", "--home", home, "--server", server.URL, "--corpus", "none")
	if err != nil || len(api.corpora) != 0 || !strings.Contains(out, "Next: give an agent or a project the sources named sessions-…") {
		t.Fatalf("err = %v, corpora = %v\n%s", err, api.corpora, out)
	}
	// A later run files what is already there.
	if out, err := runImport(t, "sessions", "--home", home, "--server", server.URL, "--corpus", "history"); err != nil || len(api.corpora["history"]) != 3 || !strings.Contains(out, "They are in the corpus history") {
		t.Fatalf("err = %v, corpora = %v\n%s", err, api.corpora, out)
	}
	if _, err := runImport(t, "sessions", "--dry-run", "--corpus", "Not Valid"); err == nil || !strings.Contains(err.Error(), "--corpus must be") {
		t.Fatalf("err = %v", err)
	}
}

func TestImportSessionsByProjectKeepsADirectorysSessionsTogether(t *testing.T) {
	api := &importServer{projects: []string{"App", "Field notes"}}
	server := api.start(t)
	home := sessionHome(t)
	out, err := runImport(t, "sessions", "--home", home, "--server", server.URL, "--by-project")
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	var names []string
	for name, docs := range api.docs {
		names = append(names, fmt.Sprintf("%s=%d", name, len(docs)))
	}
	sort.Strings(names)
	if got := strings.Join(names, " "); got != "sessions-in-api=1 sessions-in-app=3" {
		t.Fatalf("sources = %s", got)
	}
	// The directory with a project of its name is attached to it. The
	// other is named, and stays in the corpus.
	if len(api.attached) != 1 || api.attached["sessions-in-app"] != "App" {
		t.Fatalf("attached = %v", api.attached)
	}
	for _, want := range []string{
		"Attached to projects: sessions-in-app=App.",
		"No project is named for: api. Their sessions are in the corpus; make the project and run this again to attach them.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("out lacks %q:\n%s", want, out)
		}
	}
	if len(api.corpora["sessions"]) != 2 {
		t.Fatalf("corpus = %v", api.corpora)
	}
	// Once the project exists, a later run attaches what is already there.
	api.projects = append(api.projects, "api")
	out, err = runImport(t, "sessions", "--home", home, "--server", server.URL, "--by-project", "--json")
	var res sessionsResult
	if jerr := json.Unmarshal([]byte(out), &res); err != nil || jerr != nil || res.Added != 0 || api.attached["sessions-in-api"] != "api" || len(res.Unfiled) != 0 {
		t.Fatalf("err = %v, json = %v, attached = %v\n%s", err, jerr, api.attached, out)
	}
}

func TestImportSessionsByProjectAttachesTheSourcesThatContinueAFullOne(t *testing.T) {
	api := &importServer{cap: 1, projects: []string{"app"}}
	server := api.start(t)
	if out, err := runImport(t, "sessions", "--home", sessionHome(t), "--from", "claude", "--server", server.URL, "--by-project"); err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	for _, name := range []string{"sessions-in-app", "sessions-in-app-2", "sessions-in-app-3"} {
		if len(api.docs[name]) != 1 || api.attached[name] != "app" {
			t.Fatalf("%s: docs = %d, attached = %q", name, len(api.docs[name]), api.attached[name])
		}
	}
}

func TestDirSlug(t *testing.T) {
	for dir, want := range map[string]string{
		"/work/app":      "app",
		"/work/App":      "app",
		"/work/site.dev": "site-dev-x",
		"/work/My Notes": "my-notes-x",
		"/work/__init__": "init-x",
		"/work/app-2":    "app-2-x",
		"/work/a-very-long-repository-name-that-goes-on": "a-very-long-re-x",
		"":  "",
		"/": "",
	} {
		// A name ending in -x is a prefix: eight digits of the hash follow.
		got := dirSlug(dir)
		if strings.HasSuffix(want, "-x") {
			if !strings.HasPrefix(got, want) || len(got) != len(want)+8 || len(got) > 24 {
				t.Errorf("dirSlug(%q) = %q, want %sNNNN", dir, got, want)
			}
			continue
		}
		if got != want {
			t.Errorf("dirSlug(%q) = %q, want %q", dir, got, want)
		}
	}
}

// cwdSession is a Claude Code session that ran in dir.
func cwdSession(id, dir string) string {
	return strings.Replace(claudeSession(id, "2026-10-03T10:00:00Z", "ask "+id), "/work/app", dir, 1)
}

func TestImportSessionsByProjectReadsEveryPageOfProjects(t *testing.T) {
	// The directory's project is on the third page of the list.
	api := &importServer{projects: []string{"a", "b", "c", "d", "app"}}
	server := api.start(t)
	if out, err := runImport(t, "sessions", "--home", sessionHome(t), "--from", "claude", "--server", server.URL, "--by-project"); err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if api.attached["sessions-in-app"] != "app" {
		t.Fatalf("attached = %v", api.attached)
	}
}

func TestImportSessionsByProjectTellsDirectoriesOfLikeNamesApart(t *testing.T) {
	api := &importServer{projects: []string{"foo_bar", "foo-bar", "app"}}
	server := api.start(t)
	home := t.TempDir()
	putFile(t, home, ".claude/projects/-work-foo-bar/a.jsonl", cwdSession("a", "/work/foo-bar"))
	putFile(t, home, ".claude/projects/-work-foo_bar/b.jsonl", cwdSession("b", "/work/foo_bar"))
	putFile(t, home, ".claude/projects/-work-app/c.jsonl", cwdSession("c", "/work/app"))
	putFile(t, home, ".claude/projects/-work-app-2/d.jsonl", cwdSession("d", "/work/app-2"))
	putFile(t, home, ".claude/projects/-work-My-App/e.jsonl", cwdSession("e", "/work/My App"))
	out, err := runImport(t, "sessions", "--home", home, "--server", server.URL, "--by-project", "--json")
	var res sessionsResult
	if jerr := json.Unmarshal([]byte(out), &res); err != nil || jerr != nil {
		t.Fatalf("err = %v, json = %v\n%s", err, jerr, out)
	}
	// Each directory has a source of its own, and each source one session.
	if len(api.docs) != 5 {
		t.Fatalf("sources = %v", api.docs)
	}
	for name, docs := range api.docs {
		if len(docs) != 1 {
			t.Errorf("%s holds %d sessions", name, len(docs))
		}
	}
	// A project takes the directory named as it is and no other: foo-bar's
	// is not foo_bar's, and app-2 is not a second source of app.
	projects := map[string]string{}
	for source, project := range api.attached {
		if projects[project] != "" {
			t.Errorf("%s has %s and %s", project, projects[project], source)
		}
		projects[project] = source
	}
	if len(api.attached) != 3 || projects["foo-bar"] != "sessions-in-foo-bar" || projects["app"] != "sessions-in-app" || !strings.HasPrefix(projects["foo_bar"], "sessions-in-foo-bar-x") {
		t.Fatalf("attached = %v", api.attached)
	}
	if got := strings.Join(res.Unfiled, ", "); got != "My App, app-2" {
		t.Fatalf("unfiled = %s", got)
	}
}

func TestImportSessionsByProjectRefusesToMixTwoDirectoriesInOneSource(t *testing.T) {
	// Every hash is the same, as two names' may be.
	sum := slugSum
	slugSum = func(string) string { return "00000000" }
	t.Cleanup(func() { slugSum = sum })
	api := &importServer{projects: []string{"a_1", "a 1"}}
	server := api.start(t)
	home := t.TempDir()
	putFile(t, home, ".claude/projects/-work-a-1/a.jsonl", cwdSession("a", "/work/a_1"))
	out, err := runImport(t, "sessions", "--home", home, "--server", server.URL, "--by-project")
	if err != nil || api.attached["sessions-in-a-1-x00000000"] != "a_1" {
		t.Fatalf("err = %v, attached = %v\n%s", err, api.attached, out)
	}
	// A later run meets a directory of the same slug: its session is not
	// added to the first one's source, and the source stays a_1's.
	putFile(t, home, ".claude/projects/-work-a-2/b.jsonl", cwdSession("b", "/work/a 1"))
	out, err = runImport(t, "sessions", "--home", home, "--server", server.URL, "--by-project", "--json")
	var res sessionsResult
	if jerr := json.Unmarshal([]byte(out), &res); jerr != nil {
		t.Fatalf("json = %v\n%s", jerr, out)
	}
	if err == nil || len(res.Failed) != 1 || !strings.Contains(res.Failed[0].Reason, "would share the source") {
		t.Fatalf("err = %v, failed = %v", err, res.Failed)
	}
	if len(api.docs) != 1 || len(api.docs["sessions-in-a-1-x00000000"]) != 1 || api.attached["sessions-in-a-1-x00000000"] != "a_1" {
		t.Fatalf("docs = %v, attached = %v", api.docs, api.attached)
	}
	// The same holds when only the second directory is read: the source
	// says whose it is.
	home = t.TempDir()
	putFile(t, home, ".claude/projects/-work-a-2/b.jsonl", cwdSession("b", "/work/a 1"))
	out, err = runImport(t, "sessions", "--home", home, "--server", server.URL, "--by-project", "--json")
	res = sessionsResult{}
	if jerr := json.Unmarshal([]byte(out), &res); jerr != nil {
		t.Fatalf("json = %v\n%s", jerr, out)
	}
	// Neither is the source attached to the second directory's project,
	// which exists: the source is not that directory's.
	if err == nil || len(res.Failed) != 2 || !strings.Contains(res.Failed[0].Reason, "holds another directory") || !strings.Contains(res.Failed[1].Reason, "not attached for a 1") || len(res.Attached) != 0 {
		t.Fatalf("err = %v, failed = %v, attached = %v", err, res.Failed, res.Attached)
	}
	if len(api.docs["sessions-in-a-1-x00000000"]) != 1 || api.attached["sessions-in-a-1-x00000000"] != "a_1" {
		t.Fatalf("docs = %v, attached = %v", api.docs, api.attached)
	}
}

// syntheticHome is a home directory and two exports made from the synthetic
// exports under pkg/sessionimport/testdata, one of each format.
func syntheticHome(t *testing.T) (home, chatgpt, claudeAI string) {
	t.Helper()
	home = t.TempDir()
	data := func(name string) string {
		b, err := os.ReadFile(filepath.Join("..", "..", "pkg", "sessionimport", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	putFile(t, home, ".claude/projects/-synthetic-work-alpha-site/cc0rig1d-aaaa-4bbb-8ccc-000000000001.jsonl", data("claude-session.jsonl"))
	putFile(t, home, ".codex/sessions/2026/10/06/rollout-2026-10-06T09-00-00-c0dex0r1-bbbb-4ccc-8ddd-000000000002.jsonl", data("codex-rollout.jsonl"))
	putFile(t, home, "chatgpt/conversations.json", data("chatgpt-conversations.json"))
	putFile(t, home, "claude-ai/conversations.json", data("claudeai-conversations.json"))
	return home, filepath.Join(home, "chatgpt", "conversations.json"), filepath.Join(home, "claude-ai", "conversations.json")
}

// counts reads the added, updated and present counts of an import's JSON.
func counts(t *testing.T, out string) string {
	t.Helper()
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return fmt.Sprintf("added %v updated %v present %v failed %v", res["added"], res["updated"], res["present"], res["failed"])
}

// held is every document the server has, as source, id, uri and text.
func (s *importServer) held() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for name, docs := range s.docs {
		for _, d := range docs {
			out = append(out, strings.Join([]string{name, d["id"], d["uri"], d["title"], d["text"], d["excluded"]}, "\x00"))
		}
	}
	sort.Strings(out)
	return strings.Join(out, "\x01")
}

func TestImportSessionsKeepsWhatEachExportHoldsAndASecondRunChangesNothing(t *testing.T) {
	api := &importServer{projects: []string{"alpha-site", "beta-tools"}}
	server := api.start(t)
	home, chatgpt, claudeAI := syntheticHome(t)
	args := []string{"sessions", "--home", home, "--export", chatgpt, "--export", claudeAI, "--by-project", "--server", server.URL, "--json"}
	out, err := runImport(t, args...)
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if got := counts(t, out); got != "added 4 updated 0 present 0 failed []" {
		t.Fatalf("first run: %s", got)
	}
	for _, tt := range []struct {
		source, uri string
		has         []string
	}{
		{"sessions-in-alpha-site", "claude:cc0rig1d-aaaa-4bbb-8ccc-000000000001", []string{
			"## Asked [2026-10-05T10:00:00Z id=cc-msg-u1]",
			"- Bash: ls alpha-site --CC-TOOL-CALL [2026-10-05T10:30:00Z id=cc-msg-a1 call=toolu_cc_orig_42]",
			"Tool result [2026-10-05T10:05:00Z id=cc-msg-r1 call=toolu_cc_orig_42]:",
			"[attachment: image/png, 8 bytes; the image itself is not imported]",
			"## Branch 1: forks after id=cc-msg-a2\n\n### Asked [2026-10-05T10:40:00Z id=cc-msg-u3a]",
		}},
		{"sessions-in-beta-tools", "codex:c0dex0r1-bbbb-4ccc-8ddd-000000000002", []string{
			"## Asked [2026-10-06T09:01:00Z id=cx-msg-u1]",
			"- shell: ls beta-tools --CX-TOOL-CALL [2026-10-06T09:05:00Z id=cx-fc-1 call=call_cx_orig_77]",
			"Tool result [2026-10-06T09:06:00Z call=call_cx_orig_77]:",
			"[attachment: image/png, 8 bytes; the image itself is not imported]",
		}},
		{"sessions-chatgpt-2026-10", "chatgpt:gpt0r1g1-cccc-4ddd-8eee-000000000003", []string{
			"## Asked [2026-10-07T08:00:00Z id=gpt-node-u1]",
			"- python: print(\"GPT-TOOL-CALL\") [2026-10-07T08:40:00Z id=gpt-node-a1]",
			"Tool result [2026-10-07T08:41:00Z id=gpt-node-t1 call=gpt-node-a1]:\n\n    GPT-TOOL-RESULT 42 rows",
			"[attachment: gpt-sheet.png, image/png, 11 bytes; the image itself is not imported]",
			"## Branch 1: forks after id=gpt-node-a2\n\n### Asked [2026-10-07T08:50:00Z id=gpt-node-u2a]",
		}},
		{"sessions-claude-ai-2026-10", "claude-ai:cai0r1g1-dddd-4eee-8fff-000000000004", []string{
			"## Asked [2026-10-08T07:00:00Z id=cai-msg-u1]",
			"- web_search: CAI-TOOL-CALL lime note [2026-10-08T07:30:00Z id=cai-msg-a1 call=toolu_cai_orig_9]",
			"Tool result [2026-10-08T07:30:00Z id=cai-msg-a1 call=toolu_cai_orig_9]:",
			"[attachment: cai-note.txt, txt, 28 bytes]\nCAI-ATTACHMENT-TEXT lime 9",
			"## Branch 1: forks after id=cai-msg-a1\n\n### Asked [2026-10-08T07:05:00Z id=cai-msg-u2a]",
		}},
	} {
		docs := api.docs[tt.source]
		if len(docs) != 1 || docs[0]["uri"] != tt.uri {
			t.Fatalf("%s holds %v", tt.source, docs)
		}
		for _, h := range tt.has {
			if !strings.Contains(docs[0]["text"], h) {
				t.Errorf("%s: no %q in:\n%s", tt.uri, h, docs[0]["text"])
			}
		}
	}
	if api.attached["sessions-in-alpha-site"] != "alpha-site" || api.attached["sessions-in-beta-tools"] != "beta-tools" || len(api.attached) != 2 {
		t.Fatalf("attached = %v", api.attached)
	}
	// A second run adds, replaces and removes nothing: the same documents,
	// under the same ids.
	before := api.held()
	api.docWrites = 0
	out, err = runImport(t, args...)
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if got := counts(t, out); got != "added 0 updated 0 present 4 failed []" || api.held() != before || api.docWrites != 0 {
		t.Fatalf("second run: %s, changed: %v, %d document writes", got, api.held() != before, api.docWrites)
	}
}

const (
	syntheticURI  = "claude:cc0rig1d-aaaa-4bbb-8ccc-000000000001"
	syntheticFile = ".claude/projects/-synthetic-work-alpha-site/cc0rig1d-aaaa-4bbb-8ccc-000000000001.jsonl"
	// earlierForm is the session's document as the version before this one
	// wrote it: no times, no ids, no branches, and no digest kept with it.
	earlierForm = "# CC-USER-ONE list the files in the alpha site\n\n- Source: claude session cc0rig1d-aaaa-4bbb-8ccc-000000000001\n- Directory: /synthetic/work/alpha-site\n- Started: 2026-10-05 10:00 UTC\n\n## Asked\n\nCC-USER-ONE list the files in the alpha site\n\n## Asked\n\n[image]\n\n## Answered\n\nCC-ASSISTANT-LATE-STAMP (stamped 10:30, second in file)\n\nTools used:\n- Bash: ls alpha-site --CC-TOOL-CALL\n"
)

// grow appends a turn to the synthetic Claude Code session, stamped the day
// after the session's other turns: long before any import of it.
func grow(t *testing.T, home string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(home, syntheticFile), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fmt.Fprintln(f, `{"type":"user","uuid":"cc-msg-u4","parentUuid":"cc-msg-a3b","sessionId":"cc0rig1d-aaaa-4bbb-8ccc-000000000001","cwd":"/synthetic/work/alpha-site","timestamp":"2026-10-06T12:00:00.000Z","message":{"role":"user","content":"CC-GREW one more thing"}}`)
}

const grown = "## Asked [2026-10-06T12:00:00Z id=cc-msg-u4]\n\nCC-GREW one more thing"

// A session is known by its URI, not by what its document says: one an
// earlier version wrote is not added a second time. It reads differently
// from what this version writes, so it is replaced in place, once.
func TestImportSessionsUpgradesInPlaceADocumentAnEarlierVersionWrote(t *testing.T) {
	const source = "sessions-claude-2026-10"
	api := &importServer{docs: map[string][]map[string]string{source: {{
		"id": "old1", "uri": syntheticURI, "title": "CC-USER-ONE list the files in the alpha site (alpha-site)", "text": earlierForm,
	}}}}
	server := api.start(t)
	home, _, _ := syntheticHome(t)
	args := []string{"sessions", "--home", home, "--from", "claude", "--server", server.URL, "--json"}
	out, err := runImport(t, args...)
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	doc := api.docs[source][0]
	if got := counts(t, out); got != "added 0 updated 1 present 0 failed []" || len(api.docs) != 1 || len(api.docs[source]) != 1 || doc["id"] != "old1" || doc["uri"] != syntheticURI ||
		!strings.Contains(doc["text"], "## Asked [2026-10-05T10:00:00Z id=cc-msg-u1]") || !strings.Contains(doc["text"], "## Branch 1: forks after id=cc-msg-a2") {
		t.Fatalf("%s\n%v", got, api.docs)
	}
	// Upgraded, it reads as this version writes it: later runs write
	// nothing, by month or by project.
	before := api.held()
	api.docWrites = 0
	for _, more := range [][]string{nil, {"--by-project"}} {
		out, err := runImport(t, append(args, more...)...)
		if err != nil {
			t.Fatalf("err = %v\n%s", err, out)
		}
		if got := counts(t, out); got != "added 0 updated 0 present 1 failed []" || api.held() != before || api.docWrites != 0 {
			t.Fatalf("%v: %s, changed: %v, %d document writes", more, got, api.held() != before, api.docWrites)
		}
	}
}

// A session whose document would read differently has it replaced in place:
// the same document, under the same id, in the same source, left out of
// search if it was. What the export's times say does not come into it.
func TestImportSessionsReplacesInPlaceTheDocumentOfASessionThatChanged(t *testing.T) {
	for _, tt := range []struct {
		name string
		// change is what happens to the session after it is imported, and
		// want a text its document then holds.
		change   func(t *testing.T, home string)
		want     string
		excluded bool
	}{
		{name: "a turn dated long before the import is added", change: grow, want: grown},
		{name: "left out of search", change: grow, want: grown, excluded: true},
		// A session whose export gives no times follows the same rule.
		{name: "a session with no times", change: func(t *testing.T, home string) {
			putFile(t, home, syntheticFile, `{"type":"user","uuid":"u1","sessionId":"cc0rig1d-aaaa-4bbb-8ccc-000000000001","cwd":"/synthetic/work/alpha-site","message":{"content":"CC-USER-ONE list the files in the alpha site"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"cc0rig1d-aaaa-4bbb-8ccc-000000000001","message":{"content":[{"type":"text","text":"CC-UNTIMED answer"}]}}
`)
		}, want: "## Answered [id=a1]\n\nCC-UNTIMED answer"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const source = "sessions-claude-2026-10"
			api := &importServer{}
			server := api.start(t)
			home, _, _ := syntheticHome(t)
			args := []string{"sessions", "--home", home, "--from", "claude", "--server", server.URL, "--json"}
			run := func(want string) {
				t.Helper()
				out, _ := runImport(t, args...)
				if got := counts(t, out); got != want {
					t.Fatalf("got %s, want %s", got, want)
				}
			}
			run("added 1 updated 0 present 0 failed []")
			doc := api.docs[source][0]
			id := doc["id"]
			if tt.excluded {
				doc["excluded"] = "true"
			}
			run("added 0 updated 0 present 1 failed []")
			tt.change(t, home)

			// A replacement that fails leaves the document as it was.
			before := api.held()
			api.failReplace = true
			out, err := runImport(t, args...)
			if err == nil || !strings.Contains(out, "not updated, and left as it was") || api.held() != before {
				t.Fatalf("a failed replacement: err = %v, changed: %v\n%s", err, api.held() != before, out)
			}
			api.failReplace = false

			run("added 0 updated 1 present 0 failed []")
			docs := api.docs[source]
			if len(api.docs) != 1 || len(docs) != 1 || docs[0]["id"] != id || docs[0]["uri"] != syntheticURI || !strings.Contains(docs[0]["text"], tt.want) {
				t.Fatalf("documents = %v", api.docs)
			}
			if got := docs[0]["excluded"] == "true"; got != tt.excluded {
				t.Fatalf("excluded = %v", got)
			}
			// Nothing more changed: the next run writes nothing.
			before = api.held()
			api.docWrites = 0
			run("added 0 updated 0 present 1 failed []")
			if api.held() != before || api.docWrites != 0 {
				t.Fatalf("a run with nothing new changed the document, in %d writes", api.docWrites)
			}
		})
	}
}

// Two imports of a session that grew, run at once, leave one document with
// the newer text: whichever writes second finds it already as it would
// write it.
func TestTwoImportsAtOnceOfASessionThatGrewLeaveOneDocument(t *testing.T) {
	const source = "sessions-claude-2026-10"
	api := &importServer{}
	server := api.start(t)
	home, _, _ := syntheticHome(t)
	args := []string{"sessions", "--home", home, "--from", "claude", "--server", server.URL, "--token", "", "--json"}
	if out, err := runImport(t, args...); err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	id := api.docs[source][0]["id"]
	grow(t, home)
	const runs = 6
	outs, errs := make([]bytes.Buffer, runs), make([]error, runs)
	var wg sync.WaitGroup
	for i := range runs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = importCmd(args, &outs[i])
		}()
	}
	wg.Wait()
	updated := 0
	for i := range runs {
		var res sessionsResult
		if err := json.Unmarshal(outs[i].Bytes(), &res); errs[i] != nil || err != nil || len(res.Failed) != 0 || res.Added != 0 || res.Updated+res.Present != 1 {
			t.Fatalf("run %d: %v %v %s", i, errs[i], err, outs[i].String())
		}
		updated += res.Updated
	}
	docs := api.docs[source]
	if updated < 1 || len(api.docs) != 1 || len(docs) != 1 || docs[0]["id"] != id || !strings.Contains(docs[0]["text"], grown) {
		t.Fatalf("%d updated, documents = %v", updated, api.docs)
	}
}
