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
		text := t.Text
		if t.Attached != "" {
			text += "+" + t.Attached
		}
		out = append(out, t.Role+"="+text)
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
	want := "user=fix the login bug | assistant=Looking. | tool=Bash: go test ./... | tool=Read: /work/app/login.go | result=ok secret output | user=/goal ship it | user=why is this red | user=+[attachment; the image itself is not imported] | assistant=Fixed."
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
{"type":"response_item","payload":{"type":"function_call_output","call_id":"c1","output":"PASS\nok"}}
{"type":"response_item","payload":{"type":"custom_tool_call","name":"exec","input":"const r = 1"}}
{"type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"c2","output":{"output":"1"}}}
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
	want := "user=add a health route | user=+[attachment: image; the image itself is not imported] | tool=shell: bash -lc make test | result=PASS\nok | tool=exec: const r = 1 | result=1 | assistant=Added."
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
		if strings.Contains(meta, "parent_thread_id") && (s.Parent != "codex:x-1" || !strings.Contains(s.Markdown(4096), "- Started by: codex:x-1\n")) {
			t.Fatalf("parent = %q", s.Parent)
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
 ]},
 {"uuid":"a-2","name":"","chat_messages":[{"sender":"human","text":"","content":[{"type":"text","text":""}]},{"sender":"assistant","text":""}]},
 {"uuid":"a-3","name":"Detention figures","chat_messages":[
   {"sender":"human","text":"find the figures","content":[{"type":"text","text":"find the figures"}],
    "attachments":[{"file_name":"notes.txt","extracted_content":"my notes"}],"files":[{"file_name":"chart.png"}]},
   {"sender":"assistant","text":"Searching. Found them.","content":[
     {"type":"text","text":"Searching."},
     {"type":"tool_use","name":"web_search","input":{"query":"detention statistics"}},
     {"type":"tool_result","name":"web_search","content":[{"type":"knowledge","title":"A source page"},{"type":"text","text":"12 results"}]},
     {"type":"text","text":"Found them."}]}
 ]}
]`

func TestAnExportHoldsChatGPTAndClaudeAIConversations(t *testing.T) {
	var got []Session
	empty, err := ReadExport(strings.NewReader(exportFile), func(s Session) error { got = append(got, s); return nil })
	if err != nil {
		t.Fatal(err)
	}
	// The empty ChatGPT conversation and the empty claude.ai one.
	if len(got) != 3 || empty != 2 {
		t.Fatalf("got %d sessions, %d empty", len(got), empty)
	}
	if g := got[0]; len(g.Side) != 1 || g.Branches != 0 || !strings.Contains(g.Markdown(4096), "\n## Branch 1: starts the conversation again\n\n### Asked [id=n2b]\n\nan edit left behind\n") {
		t.Fatalf("branches = %+v\n%s", g.Side, g.Markdown(4096))
	}
	want := "user=find the figures | user=+[attachment: notes.txt]\nmy notes\n\n[attachment: chart.png; the image itself is not imported] | assistant=Searching. | tool=web_search: detention statistics | result=A source page\n12 results | assistant=Found them."
	if r := roles(got[2]); r != want {
		t.Fatalf("claude.ai blocks:\n got %s\nwant %s", r, want)
	}
	if g := got[0]; g.URI() != "chatgpt:g-1" || g.Title != "Trip plan" || g.Started.Unix() != 1760000000 || roles(g) != "user=plan a trip+[attachment; its content is not in the export] | assistant=Here is a plan." {
		t.Fatalf("chatgpt = %+v", g)
	}
	if a := got[1]; a.URI() != "claude-ai:a-1" || a.Title != "Essay notes" || roles(a) != "user=outline my essay | assistant=Three parts." {
		t.Fatalf("claude.ai = %+v", a)
	}
	if _, err := ReadExport(strings.NewReader(`{"not":"a list"}`), func(Session) error { return nil }); !errors.Is(err, ErrNotSession) {
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
	if len(out) > 40000 || strings.Count(out, "## Asked") != 20 || strings.Contains(out, "the middle of this session is left out") || !strings.Contains(out, "- Shortened to fit the size limit: tool results left out, answers cut to 1200 bytes, runs of tool calls to 6\n") {
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

func TestToolResultsAreKeptCutAndTheFirstThingLeftOut(t *testing.T) {
	s := Session{Tool: Claude, ID: "c", Title: "Results", Turns: []Turn{
		{Role: "user", Text: "run the tests"},
		{Role: "tool", Text: "Bash: go test ./..."},
		{Role: "result", Text: result("## Asked\n" + strings.Repeat("x", 5000))},
		{Role: "tool", Text: "Bash: go vet ./..."},
		{Role: "result", Text: result(strings.Repeat("y", 5000))},
		{Role: "assistant", Text: "They pass."},
	}}
	if got := len(s.Turns[2].Text); got > MaxResult+len("…") {
		t.Fatalf("a result of %d bytes was kept", got)
	}
	full := s.Markdown(1 << 20)
	// A result that looks like a heading does not become one.
	if !strings.Contains(full, "Tool result:\n\n    ## Asked\n    xxx") || strings.Count(full, "\n## Asked") != 1 {
		t.Fatalf("full:\n%s", full[:300])
	}
	short := s.Markdown(MinMarkdown)
	if strings.Contains(short, "Tool result:") || !strings.Contains(short, "They pass.") || !strings.Contains(short, "- Bash: go test ./...") {
		t.Fatalf("short:\n%s", short)
	}
}

// want is what a format's document must hold: each of has, the texts of in
// in that order, and none of not.
type want struct {
	name string
	read func(t *testing.T) Session
	has  []string
	in   []string
	not  []string
}

func fromFile(tool, path string) func(*testing.T) Session {
	return func(t *testing.T) Session {
		t.Helper()
		if tool == Claude || tool == Codex {
			s, err := Open(tool, path)
			if err != nil {
				t.Fatal(err)
			}
			return s
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		var got []Session
		if _, err := ReadExport(f, func(s Session) error { got = append(got, s); return nil }); err != nil || len(got) != 1 {
			t.Fatalf("read %d sessions: %v", len(got), err)
		}
		return got[0]
	}
}

func fromText(tool, text string) func(*testing.T) Session {
	return func(t *testing.T) Session {
		t.Helper()
		var s Session
		var err error
		switch tool {
		case Claude:
			s, err = ReadClaude(strings.NewReader(text))
		case Codex:
			s, err = ReadCodex(strings.NewReader(text))
		default:
			_, err = ReadExport(strings.NewReader(text), func(got Session) error { s = got; return nil })
		}
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
}

func (w want) check(t *testing.T) {
	t.Helper()
	doc := w.read(t).Markdown(48 << 10)
	for _, h := range w.has {
		if !strings.Contains(doc, h) {
			t.Errorf("the document does not hold %q", h)
		}
	}
	at := 0
	for _, h := range w.in {
		i := strings.Index(doc[at:], h)
		if i < 0 {
			t.Errorf("%q is missing, or is not after what it follows", h)
			continue
		}
		at += i + len(h)
	}
	for _, h := range w.not {
		if strings.Contains(doc, h) {
			t.Errorf("the document holds %q", h)
		}
	}
	if t.Failed() {
		t.Logf("document:\n%s", doc)
	}
}

// The exports under testdata are synthetic: each holds a turn stamped out of
// order, a tool call and its result, an attachment and, where the format
// has branches, a fork.
func TestADocumentKeepsWhatEachExportHolds(t *testing.T) {
	for _, w := range []want{
		{name: "claude", read: fromFile(Claude, "testdata/claude-session.jsonl"),
			has: []string{
				"- Source: claude session cc0rig1d-aaaa-4bbb-8ccc-000000000001\n- Directory: /synthetic/work/alpha-site\n- Started: 2026-10-05 10:00 UTC\n",
				"- Order: each turn follows the turn it replies to; 2 stamped earlier than a turn before them keep that place and show the time the export states\n",
				"- Branches: 1, each in its own section after the main path\n",
				"[attachment: image/png, 8 bytes; the image itself is not imported]",
			},
			// The result is stamped 10:05 and its call 10:30, and the answer
			// after it 10:02: each stays after what it follows.
			in: []string{
				"## Asked [2026-10-05T10:00:00Z id=cc-msg-u1]\n\nCC-USER-ONE",
				"## Answered [2026-10-05T10:30:00Z id=cc-msg-a1]\n\nCC-ASSISTANT-LATE-STAMP",
				"Tools used:\n- Bash: ls alpha-site --CC-TOOL-CALL [2026-10-05T10:30:00Z id=cc-msg-a1 call=toolu_cc_orig_42]\n",
				"Tool result [2026-10-05T10:05:00Z id=cc-msg-r1 call=toolu_cc_orig_42]:\n\n    index.html CC-TOOL-RESULT",
				"## Answered [2026-10-05T10:02:00Z id=cc-msg-a2]\n\nCC-ASSISTANT-EARLY-STAMP",
				"## Asked [2026-10-05T10:50:00Z id=cc-msg-u3b]\n\nCC-BRANCH-B",
				"## Answered [2026-10-05T10:51:00Z id=cc-msg-a3b]\n\nCC-BRANCH-B-ANSWER",
				"\n## Branch 1: forks after id=cc-msg-a2\n",
				"### Asked [2026-10-05T10:40:00Z id=cc-msg-u3a]\n\nCC-BRANCH-A",
				"### Answered [2026-10-05T10:41:00Z id=cc-msg-a3a]\n\nCC-BRANCH-A-ANSWER",
			},
			not: []string{"[image]"},
		},
		{name: "codex", read: fromFile(Codex, "testdata/codex-rollout.jsonl"),
			has: []string{
				"- Source: codex session c0dex0r1-bbbb-4ccc-8ddd-000000000002\n- Directory: /synthetic/work/beta-tools\n- Started: 2026-10-06 09:00 UTC\n",
				"- Order: by time, a tool's result after its call\n",
				"[attachment: image/png, 8 bytes; the image itself is not imported]",
			},
			// A rollout names no parents, so time orders it.
			in: []string{
				"## Asked [2026-10-06T09:01:00Z id=cx-msg-u1]\n\nCX-USER-ONE",
				"## Answered [2026-10-06T09:02:00Z id=cx-msg-a2]\n\nCX-ASSISTANT-EARLY-STAMP",
				"- shell: ls beta-tools --CX-TOOL-CALL [2026-10-06T09:05:00Z id=cx-fc-1 call=call_cx_orig_77]\n",
				"Tool result [2026-10-06T09:06:00Z call=call_cx_orig_77]:\n\n    {\"output\": \"hammer saw CX-TOOL-RESULT\"",
				"## Answered [2026-10-06T09:30:00Z id=cx-msg-a1]\n\nCX-ASSISTANT-LATE-STAMP",
			},
			not: []string{"[image]", "Branch"},
		},
		{name: "chatgpt", read: fromFile(ChatGPT, "testdata/chatgpt-conversations.json"),
			has: []string{
				"- Source: chatgpt session gpt0r1g1-cccc-4ddd-8eee-000000000003\n- Started: 2026-10-07 08:00 UTC\n",
				"- Branches: 1, each in its own section after the main path\n",
				"[attachment: gpt-sheet.png, image/png, 11 bytes; the image itself is not imported]",
			},
			// The branch shown is stamped before the answer it follows.
			in: []string{
				"## Asked [2026-10-07T08:00:00Z id=gpt-node-u1]\n\nGPT-USER-ONE",
				"Tools used:\n- python: print(\"GPT-TOOL-CALL\") [2026-10-07T08:40:00Z id=gpt-node-a1]\n",
				"Tool result [2026-10-07T08:41:00Z id=gpt-node-t1 call=gpt-node-a1]:\n\n    GPT-TOOL-RESULT 42 rows",
				"## Answered [2026-10-07T08:45:00Z id=gpt-node-a2]\n\nGPT-ASSISTANT-LATE-STAMP",
				"## Asked [2026-10-07T08:10:00Z id=gpt-node-u2b]\n\nGPT-BRANCH-B",
				"## Answered [2026-10-07T08:05:00Z id=gpt-node-a3b]\n\nGPT-BRANCH-B-ANSWER",
				"\n## Branch 1: forks after id=gpt-node-a2\n",
				"### Asked [2026-10-07T08:50:00Z id=gpt-node-u2a]\n\nGPT-BRANCH-A",
				"### Answered [2026-10-07T08:51:00Z id=gpt-node-a3a]\n\nGPT-BRANCH-A-ANSWER",
			},
			not: []string{"[attachment]", "Directory:", "Branches not shown"},
		},
		{name: "claude-ai", read: fromFile(ClaudeAI, "testdata/claudeai-conversations.json"),
			has: []string{
				"- Source: claude-ai session cai0r1g1-dddd-4eee-8fff-000000000004\n- Started: 2026-10-08 07:00 UTC\n",
				"- Branches: 1, each in its own section after the main path\n",
			},
			in: []string{
				"## Asked [2026-10-08T07:00:00Z id=cai-msg-u1]\n\nCAI-USER-ONE summarise the attached note\n\n[attachment: cai-note.txt, txt, 28 bytes]\nCAI-ATTACHMENT-TEXT lime 9\n\n[attachment: cai-photo.png; the image itself is not imported]\n",
				"## Answered [2026-10-08T07:30:00Z id=cai-msg-a1]\n\nCAI-ASSISTANT-LATE-STAMP",
				"- web_search: CAI-TOOL-CALL lime note [2026-10-08T07:30:00Z id=cai-msg-a1 call=toolu_cai_orig_9]\n",
				"Tool result [2026-10-08T07:30:00Z id=cai-msg-a1 call=toolu_cai_orig_9]:\n\n    CAI-TOOL-RESULT one hit",
				"## Asked [2026-10-08T07:50:00Z id=cai-msg-u2b]\n\nCAI-BRANCH-B",
				"## Answered [2026-10-08T07:51:00Z id=cai-msg-a2b]\n\nCAI-BRANCH-B-ANSWER",
				"\n## Branch 1: forks after id=cai-msg-a1\n",
				"### Asked [2026-10-08T07:05:00Z id=cai-msg-u2a]\n\nCAI-BRANCH-A",
				"### Answered [2026-10-08T07:06:00Z id=cai-msg-a2a]\n\nCAI-BRANCH-A-ANSWER",
			},
			not: []string{"Directory:"},
		},
	} {
		t.Run(w.name, w.check)
	}
}

// Where an export says what a turn replies to, that orders the turns, and
// the times they are stamped with are shown as stated.
func TestParentageOrdersTurnsWhateverTheirTimesSay(t *testing.T) {
	for _, w := range []want{
		{name: "a claude child stamped before its parent", read: fromText(Claude, `{"type":"user","uuid":"u1","sessionId":"c","timestamp":"2026-10-05T10:20:00Z","message":{"content":"first"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"c","timestamp":"2026-10-05T10:00:00Z","message":{"content":[{"type":"text","text":"second"}]}}
`), in: []string{"## Asked [2026-10-05T10:20:00Z id=u1]\n\nfirst", "## Answered [2026-10-05T10:00:00Z id=a1]\n\nsecond"},
			has: []string{"- Order: each turn follows the turn it replies to; 1 stamped earlier than a turn before them keep that place and show the time the export states\n"}},
		{name: "a chatgpt child stamped before its parent", read: fromText(ChatGPT, `[{"conversation_id":"g","current_node":"b","mapping":{
 "a":{"parent":null,"children":["b"],"message":{"id":"a","author":{"role":"user"},"create_time":1760000900,"content":{"parts":["first"]}}},
 "b":{"parent":"a","children":[],"message":{"id":"b","author":{"role":"assistant"},"create_time":1760000000,"content":{"parts":["second"]}}}}}]`),
			in: []string{"## Asked [2025-10-09T09:08:20Z id=a]\n\nfirst", "## Answered [2025-10-09T08:53:20Z id=b]\n\nsecond"}},
		{name: "a claude.ai child stamped before its parent, listed first", read: fromText(ClaudeAI, `[{"uuid":"c","chat_messages":[
 {"uuid":"m2","parent_message_uuid":"m1","sender":"assistant","text":"second","created_at":"2026-09-01T08:00:00Z"},
 {"uuid":"m1","parent_message_uuid":"00000000-0000-4000-8000-000000000000","sender":"human","text":"first","created_at":"2026-09-01T09:00:00Z"}],
 "current_leaf_message_uuid":"m2"}]`),
			in: []string{"## Asked [2026-09-01T09:00:00Z id=m1]\n\nfirst", "## Answered [2026-09-01T08:00:00Z id=m2]\n\nsecond"}, not: []string{"Branch"}},
		{name: "a claude result stamped before its call", read: fromText(Claude, `{"type":"user","uuid":"u1","sessionId":"c","timestamp":"2026-10-05T10:00:00Z","message":{"content":"run it"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"c","timestamp":"2026-10-05T10:30:00Z","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"make"}}]}}
{"type":"user","uuid":"r1","parentUuid":"a1","sessionId":"c","timestamp":"2026-10-05T10:05:00Z","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"made"}]}}
`), in: []string{"- Bash: make [2026-10-05T10:30:00Z id=a1 call=t1]", "Tool result [2026-10-05T10:05:00Z id=r1 call=t1]:\n\n    made"}},
		// A rollout has no parents, so time orders it: all but a result,
		// which stays after the call it answers.
		{name: "a codex result stamped before its call", read: fromText(Codex, `{"type":"session_meta","payload":{"id":"x","cwd":"/w","timestamp":"2026-10-06T09:00:00Z"}}
{"timestamp":"2026-10-06T09:01:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"run it"}]}}
{"timestamp":"2026-10-06T09:02:00Z","type":"response_item","payload":{"type":"function_call_output","call_id":"k1","output":"made"}}
{"timestamp":"2026-10-06T09:20:00Z","type":"response_item","payload":{"type":"function_call","call_id":"k1","name":"shell","arguments":"{\"command\":[\"make\"]}"}}
{"timestamp":"2026-10-06T09:10:00Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"between"}]}}
`), in: []string{"## Asked [2026-10-06T09:01:00Z]\n\nrun it", "## Answered [2026-10-06T09:10:00Z]\n\nbetween", "- shell: make [2026-10-06T09:20:00Z call=k1]", "Tool result [2026-10-06T09:02:00Z call=k1]:\n\n    made"}},
		// Tool results that hang off the path with nothing said are not a
		// branch: they stay on the path, after the record they hang from.
		{name: "claude results off the path", read: fromText(Claude, `{"type":"user","uuid":"u1","sessionId":"c","message":{"content":"run both"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"c","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"one"}}]}}
{"type":"assistant","uuid":"a2","parentUuid":"a1","sessionId":"c","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"two"}}]}}
{"type":"user","uuid":"r1","parentUuid":"a1","sessionId":"c","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"1"}]}}
{"type":"user","uuid":"r2","parentUuid":"a2","sessionId":"c","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":"2"}]}}
{"type":"assistant","uuid":"a3","parentUuid":"r2","sessionId":"c","message":{"content":[{"type":"text","text":"Both ran."}]}}
`), in: []string{"- Bash: one [id=a1 call=t1]", "Tool result [id=r1 call=t1]", "- Bash: two [id=a2 call=t2]", "Tool result [id=r2 call=t2]", "## Answered [id=a3]\n\nBoth ran."}, not: []string{"Branch"}},
		// A file that goes on after its history was compacted starts a new
		// chain, which follows the one before it.
		{name: "claude after a compaction", read: fromText(Claude, `{"type":"user","uuid":"u1","sessionId":"c","message":{"content":"before"}}
{"type":"system","uuid":"s1","parentUuid":null,"sessionId":"c"}
{"type":"user","uuid":"u2","parentUuid":"s1","sessionId":"c","message":{"content":"after"}}
`), in: []string{"## Asked [id=u1]\n\nbefore", "## Asked [id=u2]\n\nafter"}, not: []string{"Branch"}},
		// Times order branches that fork after the same turn.
		{name: "chatgpt sibling branches by time", read: fromText(ChatGPT, `[{"conversation_id":"g","current_node":"cur","mapping":{
 "a":{"parent":null,"children":["late","cur","early"],"message":{"id":"a","author":{"role":"user"},"content":{"parts":["first"]}}},
 "late":{"parent":"a","message":{"author":{"role":"assistant"},"create_time":1760000900,"content":{"parts":["late retry"]}}},
 "cur":{"parent":"a","children":["nest"],"message":{"author":{"role":"assistant"},"create_time":1760000500,"content":{"parts":["shown"]}}},
 "early":{"parent":"a","children":["e2","e3"],"message":{"author":{"role":"assistant"},"create_time":1760000100,"content":{"parts":["early retry"]}}},
 "e2":{"parent":"early","message":{"author":{"role":"user"},"create_time":1760000200,"content":{"parts":["nested one"]}}},
 "e3":{"parent":"early","message":{"author":{"role":"user"},"create_time":1760000300,"content":{"parts":["nested two"]}}}}}]`),
			in:  []string{"## Answered [2025-10-09T09:01:40Z id=cur]\n\nshown", "## Branch 1: forks after id=a\n", "early retry", "nested two", "## Branch 2: forks after id=early in branch 1\n", "nested one", "## Branch 3: forks after id=a\n", "late retry"},
			has: []string{"- Branches: 3, each in its own section after the main path\n"}},
		// An older claude.ai export names no parents, so time orders it. A
		// turn with no time stays after the one it is listed after.
		{name: "claude.ai without parents", read: fromText(ClaudeAI, `[{"uuid":"c","chat_messages":[
 {"uuid":"m2","sender":"assistant","text":"second","created_at":"2026-09-01T09:00:00Z"},
 {"uuid":"m1","sender":"human","text":"first","created_at":"2026-09-01T08:00:00Z"},
 {"uuid":"m3","sender":"human","text":"untimed"}]}]`),
			in:  []string{"## Asked [2026-09-01T08:00:00Z id=m1]\n\nfirst", "## Asked [id=m3]\n\nuntimed", "## Answered [2026-09-01T09:00:00Z id=m2]\n\nsecond"},
			has: []string{"- Order: by time, a tool's result after its call; 1 with no time follow the turn before them in the export\n"}},
	} {
		t.Run(w.name, w.check)
	}
}

func TestTheSameExportAlwaysMakesTheSameDocument(t *testing.T) {
	for _, read := range []func(*testing.T) Session{fromFile(Claude, "testdata/claude-session.jsonl"), fromFile(ChatGPT, "testdata/chatgpt-conversations.json"), fromFile(ClaudeAI, "testdata/claudeai-conversations.json")} {
		first := read(t).Markdown(48 << 10)
		for i := 0; i < 20; i++ {
			if again := read(t).Markdown(48 << 10); again != first {
				t.Fatalf("read %d differs:\n%s\n---\n%s", i, again, first)
			}
		}
	}
}

// Ids and branches are bounded, and a document says what it leaves out.
func TestIDsAndBranchesAreBoundedAndADocumentSaysWhatItCuts(t *testing.T) {
	var b strings.Builder
	b.WriteString(`[{"conversation_id":"g","current_node":"shown","mapping":{"root":{"parent":null,"message":{"id":"` + strings.Repeat("i", 500) + `","author":{"role":"user"},"content":{"parts":["first"]}}},"shown":{"parent":"root","message":{"author":{"role":"assistant"},"content":{"parts":["shown"]}}}`)
	for i := 0; i < MaxBranches+5; i++ {
		b.WriteString(`,"retry-` + string(rune('a'+i)) + `":{"parent":"root","message":{"author":{"role":"assistant"},"content":{"parts":["` + strings.Repeat("retry ", 300) + `"]}}}`)
	}
	b.WriteString(`}}]`)
	s := fromText(ChatGPT, b.String())(t)
	if len(s.Side) != MaxBranches || s.Branches != 5 {
		t.Fatalf("kept %d branches, left out %d", len(s.Side), s.Branches)
	}
	full := s.Markdown(1 << 20)
	id := strings.Repeat("i", maxID) + "…"
	for _, h := range []string{"## Asked [id=" + id + "]\n", "## Branch 20: forks after id=" + id + "\n", "- Branches: 20, each in its own section after the main path\n", "- Branches not shown: 5, past the 20 kept\n"} {
		if !strings.Contains(full, h) {
			t.Fatalf("no %q in:\n%s", h, full[:600])
		}
	}
	if strings.Contains(full, strings.Repeat("i", maxID+1)) || strings.Contains(full, "## Branch 21") {
		t.Fatalf("an id or the branches are not bounded:\n%s", full[:600])
	}
	// Too long for the limit: answers are cut first, then the branches,
	// and the heading says each.
	short := s.Markdown(4096)
	for _, h := range []string{"- Branches: 20, left out to fit the size limit (forks after id=" + id + "; ", "; and 10 more)\n", "- Shortened to fit the size limit: tool results left out, answers cut to 400 bytes, runs of tool calls to 6\n", "## Answered [id=shown]\n\nshown"} {
		if !strings.Contains(short, h) {
			t.Fatalf("no %q in:\n%s", h, short)
		}
	}
	if len(short) > 4096 || strings.Contains(short, "## Branch") || strings.Contains(short, "the middle of this session is left out") {
		t.Fatalf("%d bytes:\n%s", len(short), short)
	}
}
