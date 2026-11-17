package sessionimport

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

const claudeFile = `{"type":"permission-mode","permissionMode":"default","sessionId":"c-1"}
{"type":"user","sessionId":"c-1","cwd":"/work/app","timestamp":"2026-10-03T10:00:00Z","message":{"role":"user","content":"<system-reminder>ignore me</system-reminder>\nfix the login bug"}}
{"type":"assistant","sessionId":"c-1","timestamp":"2026-10-03T10:00:05Z","message":{"content":[{"type":"thinking","thinking":"hm"},{"type":"text","text":"Looking."}]}}
{"type":"assistant","sessionId":"c-1","timestamp":"2026-10-03T10:00:06Z","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}
{"type":"assistant","sessionId":"c-1","timestamp":"2026-10-03T10:00:07Z","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/work/app/login.go"}}]}}
{"type":"user","sessionId":"c-1","timestamp":"2026-10-03T10:00:08Z","message":{"content":[{"type":"tool_result","content":"ok secret output"}]}}
{"type":"user","sessionId":"c-1","isMeta":true,"message":{"content":"harness note"}}
{"type":"user","sessionId":"c-1","isSidechain":true,"message":{"content":"sub-agent prompt"}}
{"type":"user","sessionId":"c-1","message":{"content":"<local-command-stdout>noise</local-command-stdout>"}}
{"type":"user","sessionId":"c-1","message":{"content":"<command-name>/goal</command-name>\n<command-args>ship it</command-args>"}}
{"type":"user","sessionId":"c-1","message":{"content":[{"type":"image","source":{}},{"type":"text","text":"why is this red"}]}}
not json
{"type":"assistant","sessionId":"c-1","message":{"content":[{"type":"text","text":"Fixed."}]}}
{"type":"ai-title","aiTitle":"Fix the login bug","sessionId":"c-1"}
`

func roles(s Session) string {
	var out []string
	for _, t := range s.Turns {
		out = append(out, t.Role+"="+t.Text)
	}
	return strings.Join(out, " | ")
}

func TestClaudeKeepsWhatWasSaidAndNamesToolCalls(t *testing.T) {
	s, err := ReadClaude(strings.NewReader(claudeFile))
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "c-1" || s.Dir != "/work/app" || s.Title != "Fix the login bug" || s.URI() != "claude:c-1" {
		t.Fatalf("session = %+v", s)
	}
	if want := time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC); !s.Started.Equal(want) {
		t.Fatalf("started = %v", s.Started)
	}
	want := "user=fix the login bug | assistant=Looking. | tool=Bash: go test ./...\nRead: /work/app/login.go | user=/goal ship it | user=[image] | user=why is this red | assistant=Fixed."
	if got := roles(s); got != want {
		t.Fatalf("turns:\n got %s\nwant %s", got, want)
	}
	if s.Asked() != 4 {
		t.Fatalf("asked = %d", s.Asked())
	}
}

const codexFile = `{"timestamp":"2026-11-03T16:27:42Z","type":"session_meta","payload":{"id":"x-1","cwd":"/work/api","timestamp":"2026-11-03T16:25:56Z","source":"vscode"}}
{"type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"rules"}]}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"# AGENTS.md instructions\n\nstuff"}]}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>\ncwd\n</environment_context>"}]}}
{"timestamp":"2026-11-03T16:28:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"add a health route"},{"type":"input_image","image_url":"data:"}]}}
{"type":"event_msg","payload":{"type":"user_message","message":"add a health route"}}
{"type":"response_item","payload":{"type":"reasoning","encrypted_content":"zzz"}}
{"type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":[\"bash\",\"-lc\",\"make test\"]}"}}
{"type":"response_item","payload":{"type":"custom_tool_call","name":"exec","input":"const r = 1"}}
{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Added."}]}}
`

func TestCodexSkipsItsOwnPreambleAndEvents(t *testing.T) {
	s, err := ReadCodex(strings.NewReader(codexFile))
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "x-1" || s.Dir != "/work/api" || s.Child || s.Title != "add a health route" {
		t.Fatalf("session = %+v", s)
	}
	want := "user=add a health route | user=[image] | tool=shell: bash -lc make test\nexec: const r = 1 | assistant=Added."
	if got := roles(s); got != want {
		t.Fatalf("turns:\n got %s\nwant %s", got, want)
	}
}

func TestCodexMarksSessionsAnotherSessionStarted(t *testing.T) {
	for _, meta := range []string{
		`{"type":"session_meta","payload":{"id":"x-2","parent_thread_id":"x-1"}}`,
		`{"type":"session_meta","payload":{"id":"x-2","source":{"subagent":{"other":"guardian"}}}}`,
	} {
		s, err := ReadCodex(strings.NewReader(meta + "\n" + `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"judge this"}]}}`))
		if err != nil || !s.Child {
			t.Fatalf("%s: child = %v, err = %v", meta, s.Child, err)
		}
	}
}

func TestCodexReadsLinesFromBeforeEnvelopes(t *testing.T) {
	s, err := ReadCodex(strings.NewReader(`{"id":"old-1","timestamp":"2025-10-25T10:00:00Z","instructions":null}
{"type":"message","role":"user","content":[{"type":"input_text","text":"clean the docs"}]}
{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Done."}]}
`))
	if err != nil || s.ID != "old-1" || roles(s) != "user=clean the docs | assistant=Done." {
		t.Fatalf("session = %+v, err = %v", s, err)
	}
}

func TestAFileNobodyAskedAnythingInIsNotASession(t *testing.T) {
	for name, read := range map[string]func() (Session, error){
		"claude": func() (Session, error) {
			return ReadClaude(strings.NewReader(`{"type":"assistant","sessionId":"c","message":{"content":[{"type":"text","text":"hi"}]}}`))
		},
		"codex": func() (Session, error) {
			return ReadCodex(strings.NewReader(`{"type":"session_meta","payload":{"id":"x"}}`))
		},
		"empty": func() (Session, error) { return ReadClaude(strings.NewReader("")) },
	} {
		if _, err := read(); !errors.Is(err, ErrNotSession) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

const exportFile = `[
 {"conversation_id":"g-1","title":"Trip plan","create_time":1760000000.5,"current_node":"n3","mapping":{
   "n0":{"parent":null,"message":null},
   "n1":{"parent":"n0","message":{"author":{"role":"system"},"content":{"parts":["sys"]}}},
   "n2":{"parent":"n1","message":{"author":{"role":"user"},"create_time":1760000001,"content":{"parts":["plan a trip",{"asset_pointer":"file"}]}}},
   "n2b":{"parent":"n1","message":{"author":{"role":"user"},"content":{"parts":["an edit left behind"]}}},
   "n3":{"parent":"n2","message":{"author":{"role":"assistant"},"content":{"parts":["Here is a plan."]}}}
 }},
 {"conversation_id":"g-2","title":"Empty","mapping":{},"current_node":""},
 {"uuid":"a-1","name":"Essay notes","created_at":"2026-09-01T08:00:00Z","chat_messages":[
   {"sender":"human","text":"outline my essay","created_at":"2026-09-01T08:00:01Z"},
   {"sender":"assistant","text":"Three parts."}
 ]}
]`

func TestAnExportHoldsChatGPTAndClaudeAIConversations(t *testing.T) {
	var got []Session
	if err := ReadExport(strings.NewReader(exportFile), func(s Session) error { got = append(got, s); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d sessions", len(got))
	}
	if g := got[0]; g.URI() != "chatgpt:g-1" || g.Title != "Trip plan" || g.Started.Unix() != 1760000000 || roles(g) != "user=plan a trip\n[attachment] | assistant=Here is a plan." {
		t.Fatalf("chatgpt = %+v", g)
	}
	if a := got[1]; a.URI() != "claude-ai:a-1" || a.Title != "Essay notes" || roles(a) != "user=outline my essay | assistant=Three parts." {
		t.Fatalf("claude.ai = %+v", a)
	}
	if err := ReadExport(strings.NewReader(`{"not":"a list"}`), func(Session) error { return nil }); !errors.Is(err, ErrNotSession) {
		t.Fatalf("err = %v", err)
	}
}

func TestMarkdownRedactsCredentials(t *testing.T) {
	s := Session{Tool: Claude, ID: "c", Title: "Keys", Turns: []Turn{
		{Role: "user", Text: "my key is sk-" + strings.Repeat("a1", 15) + " and ghp_" + strings.Repeat("b2", 18)},
		{Role: "tool", Text: "Bash: export AWS=AKIA" + strings.Repeat("C", 16)},
	}}
	out := s.Markdown(1 << 20)
	for _, leak := range []string{"sk-a1", "ghp_", "AKIA"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q is in:\n%s", leak, out)
		}
	}
	if strings.Count(out, "[redacted]") != 3 {
		t.Errorf("out:\n%s", out)
	}
}

func TestALongSessionKeepsWhatWasAskedAndCutsAnswersFirst(t *testing.T) {
	s := Session{Tool: Codex, ID: "x", Title: "Long", Dir: "/w"}
	for i := 0; i < 20; i++ {
		s.Turns = append(s.Turns, Turn{Role: "user", Text: "question " + strings.Repeat("q", 50)}, Turn{Role: "assistant", Text: strings.Repeat("é", 5000)})
	}
	full := s.Markdown(1 << 30)
	if len(full) < 200000 {
		t.Fatalf("full is %d bytes", len(full))
	}
	out := s.Markdown(40000)
	if len(out) > 40000 || strings.Count(out, "## Asked") != 20 || strings.Contains(out, "left out") {
		t.Fatalf("%d bytes, %d asked", len(out), strings.Count(out, "## Asked"))
	}
	small := s.Markdown(100)
	if len(small) > MinMarkdown || !strings.Contains(small, "left out") || !strings.HasPrefix(small, "# Long") || strings.ToValidUTF8(small, "") != small {
		t.Fatalf("small is %d bytes:\n%s", len(small), small)
	}
}

func TestDiscoverFindsEachToolsFilesAndOpenReadsZstd(t *testing.T) {
	home := t.TempDir()
	write := func(rel, body string) string {
		t.Helper()
		path := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		data := []byte(body)
		if strings.HasSuffix(rel, ".zst") {
			enc, _ := zstd.NewWriter(nil)
			data = enc.EncodeAll(data, nil)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write(".claude/projects/-work-app/c-1.jsonl", claudeFile)
	write(".claude/projects/-work-app/c-1/subagents/agent-1.jsonl", claudeFile)
	zst := write(".codex/sessions/2026/11/03/rollout-x-1.jsonl.zst", codexFile)
	write(".codex/sessions/2025/10/25/rollout-old.jsonl", codexFile)
	write(".codex/archived_sessions/rollout-arch.jsonl", codexFile)

	claude, err := Discover(Claude, home)
	if err != nil || len(claude) != 1 {
		t.Fatalf("claude = %v, err = %v", claude, err)
	}
	codex, err := Discover(Codex, home)
	if err != nil || len(codex) != 3 {
		t.Fatalf("codex = %v, err = %v", codex, err)
	}
	if s, err := Open(Codex, zst); err != nil || s.ID != "x-1" {
		t.Fatalf("zst: %+v, %v", s, err)
	}
	if none, err := Discover(Codex, t.TempDir()); err != nil || len(none) != 0 {
		t.Fatalf("none = %v, err = %v", none, err)
	}
	if _, err := Discover("cursor", home); err == nil {
		t.Fatal("an unknown tool was read")
	}
}
