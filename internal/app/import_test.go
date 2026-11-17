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
	"strings"
	"sync"
	"testing"

	"github.com/urmzd/humagen/pkg/library"
)

// importServer plays the knowledge and skill library routes the import
// uses. A source takes at most cap documents, and says so the way the
// server does.
type importServer struct {
	mu      sync.Mutex
	cap     int
	docs    map[string][]map[string]string
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
				sources = append(sources, map[string]string{"name": name})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"sources": sources})
		case r.Method == "POST" && r.URL.Path == "/api/knowledge":
			var body struct{ Name string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			if _, ok := s.docs[body.Name]; ok {
				problem(409, "exists")
				return
			}
			s.docs[body.Name] = []map[string]string{}
			w.WriteHeader(201)
			fmt.Fprint(w, `{}`)
		case len(parts) == 3 && parts[0] == "knowledge" && parts[2] == "documents" && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{"documents": s.docs[parts[1]]})
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
			s.docs[parts[1]] = append(s.docs[parts[1]], doc)
			w.WriteHeader(201)
			fmt.Fprint(w, `{}`)
		case r.Method == "GET" && r.URL.Path == "/api/projects":
			out := []map[string]string{}
			for _, name := range s.projects {
				out = append(out, map[string]string{"name": name})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"projects": out})
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
		!strings.Contains(out, "no room for 2 more: the personal library holds at most 32 skills. Choose which with --only, or draft the rest in another scope: ship, tidy") {
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
		"/work/app":                                      "app",
		"/Users/me/github/urmzd.com":                     "urmzd-com",
		"/Users/me/Documents/Frebarz Business":           "frebarz-business",
		"/work/__init__":                                 "init",
		"/work/a-very-long-repository-name-that-goes-on": "a-very-long-repository-n",
		"":  "",
		"/": "",
	} {
		if got := dirSlug(dir); got != want {
			t.Errorf("dirSlug(%q) = %q, want %q", dir, got, want)
		}
	}
}
