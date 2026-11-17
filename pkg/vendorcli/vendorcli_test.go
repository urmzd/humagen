package vendorcli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/pkg/runs"
)

// program writes a stand-in for a vendor program: a shell script that
// records its arguments and environment, one run a file, and prints out.
func program(t *testing.T, out string, exit int) (command, runs string) {
	t.Helper()
	dir := t.TempDir()
	runs = filepath.Join(dir, "runs")
	if err := os.Mkdir(runs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "out"), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
n=$(ls "` + runs + `" | grep -c args)
{ for a in "$@"; do printf '%s\n' "$a"; done; } > "` + runs + `/$n.args"
env > "` + runs + `/$n.env"
pwd > "` + runs + `/$n.pwd"
sed "s/SESSION/sess-$n/g" "` + dir + `/out"
exit ` + string(rune('0'+exit)) + "\n"
	command = filepath.Join(dir, "fake")
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return command, runs
}

func args(t *testing.T, runs string, n int) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(runs, string(rune('0'+n))+".args"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

func build(t *testing.T, kind string, c Config, model string) saigetypes.Provider {
	t.Helper()
	if c.Dir == "" {
		c.Dir = t.TempDir()
	}
	if c.StateDir == "" {
		c.StateDir = t.TempDir()
	}
	fn, err := New(kind, c)
	if err != nil {
		t.Fatal(err)
	}
	p, err := fn(native.Spec{Name: "coder", Model: model})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func system(s string) saigetypes.Message {
	return saigetypes.SystemMessage{Parts: []saigetypes.SystemPart{saigetypes.TextPart{Text: s}}}
}

const claudeOut = `a notice that is not JSON
{"type":"system","subtype":"init","session_id":"sess-1","model":"claude-test"}
{"type":"assistant","session_id":"sess-1","message":{"content":[{"type":"thinking","thinking":"hm"},{"type":"text","text":"Looking."}]}}
{"type":"assistant","session_id":"sess-1","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go  test ./..."}}]}}
{"type":"user","session_id":"sess-1","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"FAIL","is_error":true}]}}
{"type":"assistant","session_id":"sess-1","message":{"content":[{"type":"tool_use","id":"t2","name":"Edit","input":{"file_path":"/w/calc.go"}}]}}
{"type":"user","session_id":"sess-1","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":[{"type":"text","text":"updated"},{"type":"image"}]}]}}
{"type":"assistant","session_id":"sess-1","parent_tool_use_id":"t9","message":{"content":[{"type":"text","text":"a sub-agent speaking"}]}}
{"type":"assistant","session_id":"sess-1","message":{"content":[{"type":"text","text":"Fixed."}]}}
{"type":"result","subtype":"success","is_error":false,"session_id":"sess-1","result":"Fixed.","usage":{"input_tokens":4,"cache_creation_input_tokens":10,"cache_read_input_tokens":100,"output_tokens":7}}
`

func TestClaudeCodeStreamsWhatWasSaidAndEachToolItRan(t *testing.T) {
	command, runs := program(t, claudeOut, 0)
	dir := t.TempDir()
	p := build(t, ClaudeCode, Config{Dir: dir, Command: command, Allow: []string{"Bash(go test:*)", "Bash(git:*)"}, Args: []string{"--max-turns", "9"}}, "opus")
	said, calls, results, usage, err := collect(t, p, system("Be brief."), user("fix the test"))
	if err != nil {
		t.Fatal(err)
	}
	if said != "Looking.\n\nFixed." {
		t.Fatalf("said %q", said)
	}
	if strings.Join(calls, ",") != "Bash,Edit" || len(results) != 2 || results[0] != "$ go test ./...\nFAIL" || results[1] != "$ /w/calc.go\nupdated\n[image]" {
		t.Fatalf("calls %v results %q", calls, results)
	}
	if usage.PromptTokens != 114 || usage.CachedPromptTokens != 100 || usage.CacheWriteTokens != 10 || usage.CompletionTokens != 7 || usage.ResponseModel != "claude-test" || usage.ResponseID != "sess-1" {
		t.Fatalf("usage %+v", usage)
	}
	want := []string{"-p", "fix the test", "--output-format", "stream-json", "--verbose", "--permission-mode", "acceptEdits", "--model", "opus", "--append-system-prompt", "Be brief.", "--allowedTools", "Bash(go test:*),Bash(git:*)", "--max-turns", "9"}
	if got := args(t, runs, 0); strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args\n got %q\nwant %q", got, want)
	}
	if pwd, _ := os.ReadFile(filepath.Join(runs, "0.pwd")); !strings.HasSuffix(strings.TrimSpace(string(pwd)), filepath.Base(dir)) {
		t.Fatalf("ran in %s, not %s", pwd, dir)
	}
}

func TestAFailedToolIsAnErrorOfThatToolNotOfTheTurn(t *testing.T) {
	command, _ := program(t, claudeOut, 0)
	p := build(t, ClaudeCode, Config{Command: command}, "default")
	ch, err := p.Stream(context.Background(), saigetypes.Request{Messages: []saigetypes.Message{user("go")}})
	if err != nil {
		t.Fatal(err)
	}
	var failed, passed int
	for d := range ch {
		if end, ok := d.(saigetypes.ToolExecEndDelta); ok {
			if end.Error != "" {
				failed++
			} else {
				passed++
			}
		}
		if e, ok := d.(saigetypes.ErrorDelta); ok {
			t.Fatal(e.Error)
		}
	}
	if failed != 1 || passed != 1 {
		t.Fatalf("failed %d passed %d", failed, passed)
	}
}

const codexOut = `{"type":"thread.started","thread_id":"th-1"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"Reading."}}
{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"cat note.txt","status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"cat note.txt","aggregated_output":"hello\n","exit_code":0,"status":"completed"}}
{"type":"item.started","item":{"id":"item_2","type":"command_execution","command":"false","status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_2","type":"command_execution","command":"false","aggregated_output":"","exit_code":1,"status":"failed"}}
{"type":"item.completed","item":{"id":"item_3","type":"file_change","changes":[{"path":"a.go","kind":"update"}],"status":"completed"}}
{"type":"item.completed","item":{"id":"item_4","type":"agent_message","text":"hello"}}
{"type":"turn.completed","usage":{"input_tokens":50,"cached_input_tokens":20,"output_tokens":5}}
`

func TestCodexStreamsWhatWasSaidAndEachToolItRan(t *testing.T) {
	command, runs := program(t, codexOut, 0)
	dir := t.TempDir()
	p := build(t, Codex, Config{Dir: dir, Command: command, Permissions: "read-only"}, "default")
	said, calls, results, usage, err := collect(t, p, system("Be brief."), user("read the note"))
	if err != nil {
		t.Fatal(err)
	}
	if said != "Reading.\n\nhello" || strings.Join(calls, ",") != "shell,shell,file_change" || len(results) != 3 || results[0] != "$ cat note.txt\nhello\n" {
		t.Fatalf("said %q calls %v results %q", said, calls, results)
	}
	if usage.PromptTokens != 50 || usage.CachedPromptTokens != 20 || usage.CompletionTokens != 5 || usage.ResponseID != "th-1" {
		t.Fatalf("usage %+v", usage)
	}
	want := []string{"exec", "--json", "--skip-git-repo-check", "--sandbox", "read-only", "--cd", dir, "Instructions for this conversation:", "Be brief.", "", "read the note"}
	if got := args(t, runs, 0); strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args\n got %q\nwant %q", got, want)
	}
}

func TestTheNextTurnOfAConversationContinuesTheProgramsSession(t *testing.T) {
	for _, tt := range []struct {
		kind, out string
		resumed   func([]string) bool
	}{
		{ClaudeCode, claudeOut, func(a []string) bool { return strings.Contains(strings.Join(a, " "), "--resume sess-1") }},
		{Codex, codexOut, func(a []string) bool { return len(a) > 2 && a[1] == "resume" && a[2] == "th-1" }},
	} {
		t.Run(tt.kind, func(t *testing.T) {
			command, runs := program(t, tt.out, 0)
			p := build(t, tt.kind, Config{Command: command}, "default")
			said, _, _, _, err := collect(t, p, user("fix the test"))
			if err != nil {
				t.Fatal(err)
			}
			// The same conversation, as a caller that keeps messages sends it.
			if _, _, _, _, err := collect(t, p, user("fix the test"), assistant(said), user("and the docs")); err != nil {
				t.Fatal(err)
			}
			second := args(t, runs, 1)
			if !tt.resumed(second) || !containsArg(second, "and the docs") || strings.Contains(strings.Join(second, " "), "already under way") {
				t.Fatalf("second run: %q", second)
			}
			// A conversation the program has no session for starts one, told
			// what was said.
			if _, _, _, _, err := collect(t, p, user("something else"), assistant("an answer from elsewhere"), user("go on")); err != nil {
				t.Fatal(err)
			}
			third := strings.Join(args(t, runs, 2), "\n")
			if tt.resumed(args(t, runs, 2)) || !strings.Contains(third, "already under way") || !strings.Contains(third, "You said:\nan answer from elsewhere") || !strings.Contains(third, "Now the person says:\ngo on") {
				t.Fatalf("third run: %s", third)
			}
		})
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// platformAsk is a turn as the platform writes it: one message holding the
// trigger's context, the conversation so far, and what is asked.
func platformAsk(thread, sofar, ask string) saigetypes.Message {
	s := "Context from the trigger:\n- " + runs.MetaAsk + ": " + ask + "\n"
	if thread != "" {
		s += "- " + runs.MetaPrev + ": r1\n- " + runs.MetaThread + ": " + thread + "\n"
	}
	s += "\n"
	if sofar != "" {
		s += "Conversation so far, oldest first.\n" + sofar + "\nEnd of the conversation so far.\n\n"
	}
	return user(s + ask)
}

func TestAPlatformThreadKeepsOneSessionFromItsFirstTurn(t *testing.T) {
	command, runs := program(t, claudeOut, 0)
	p := build(t, ClaudeCode, Config{Command: command}, "default")
	resumed := func(n int) bool { return strings.Contains(strings.Join(args(t, runs, n), " "), "--resume sess-1") }
	// The first turn names no thread.
	if _, _, _, _, err := collect(t, p, platformAsk("", "", "fix the test")); err != nil {
		t.Fatal(err)
	}
	// The second names it, and repeats the first turn.
	if _, _, _, _, err := collect(t, p, platformAsk("r1", "Dev (@dev): fix the test\ncoder (agent): Looking. Fixed.", "and the docs")); err != nil {
		t.Fatal(err)
	}
	// From then on the thread's name is enough.
	if _, _, _, _, err := collect(t, p, platformAsk("r1", "trimmed away", "and the changelog")); err != nil {
		t.Fatal(err)
	}
	// Another thread is another session.
	if _, _, _, _, err := collect(t, p, platformAsk("r2", "Dev (@dev): something else\ncoder (agent): Done.", "go on")); err != nil {
		t.Fatal(err)
	}
	if resumed(0) || !resumed(1) || !resumed(2) || resumed(3) {
		t.Fatalf("resumed: %v %v %v %v", resumed(0), resumed(1), resumed(2), resumed(3))
	}
}

func TestTwoConversationsThatBeganAlikeDoNotShareASession(t *testing.T) {
	// Each run of the program is a session of its own.
	command, runs := program(t, strings.ReplaceAll(claudeOut, "sess-1", "SESSION"), 0)
	p := build(t, ClaudeCode, Config{Command: command}, "default")
	for range 2 {
		if _, _, _, _, err := collect(t, p, platformAsk("", "", "fix the test")); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, _, _, err := collect(t, p, platformAsk("r7", "Dev (@dev): fix the test\ncoder (agent): Looking. Fixed.", "and the docs")); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(args(t, runs, 2), " "); strings.Contains(got, "--resume") {
		t.Fatalf("a session was continued though two fit: %s", got)
	}
}

func TestAKeyInTheEnvironmentIsWithheldUnlessAskedFor(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "key-in-env")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "token-in-env")
	t.Setenv("OPENAI_API_KEY", "other-vendor")
	for _, tt := range []struct {
		apiKey bool
		want   bool
	}{{false, false}, {true, true}} {
		command, runs := program(t, claudeOut, 0)
		p := build(t, ClaudeCode, Config{Command: command, APIKey: tt.apiKey}, "default")
		if _, _, _, _, err := collect(t, p, user("go")); err != nil {
			t.Fatal(err)
		}
		env, _ := os.ReadFile(filepath.Join(runs, "0.env"))
		if got := strings.Contains(string(env), "key-in-env") && strings.Contains(string(env), "token-in-env"); got != tt.want {
			t.Fatalf("api_key %v: the key was passed: %v", tt.apiKey, got)
		}
		if !strings.Contains(string(env), "other-vendor") {
			t.Fatal("another vendor's variable was withheld")
		}
	}
}

func TestARunThatFailsIsTheTurnsError(t *testing.T) {
	tests := []struct {
		name, kind, out string
		exit            int
		want            string
	}{
		{"claude reports an error", ClaudeCode, `{"type":"result","subtype":"success","is_error":true,"result":"Invalid API key","session_id":"s"}` + "\n", 0, "Invalid API key"},
		{"claude runs out of turns", ClaudeCode, `{"type":"result","subtype":"error_max_turns","is_error":false,"session_id":"s"}` + "\n", 0, "error_max_turns"},
		{"codex turn fails", Codex, `{"type":"thread.started","thread_id":"t"}` + "\n" + `{"type":"turn.failed","error":{"message":"usage limit reached"}}` + "\n", 1, "usage limit reached"},
		{"no answer at all", Codex, "not signed in\n", 1, "ended without an answer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, _ := program(t, tt.out, tt.exit)
			_, _, _, _, err := collect(t, build(t, tt.kind, Config{Command: command}, "default"), user("go"))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
	p := build(t, ClaudeCode, Config{Command: filepath.Join(t.TempDir(), "missing")}, "default")
	if _, err := p.Stream(context.Background(), saigetypes.Request{Messages: []saigetypes.Message{user("go")}}); err == nil || !strings.Contains(err.Error(), "is it installed") {
		t.Fatalf("err = %v", err)
	}
	if _, err := p.Stream(context.Background(), saigetypes.Request{Messages: []saigetypes.Message{assistant("hi")}}); err == nil {
		t.Fatal("a request that asks nothing was run")
	}
}

func TestStoppingTheTurnStopsTheProgram(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "slow")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nsleep 60 &\nwait\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := build(t, ClaudeCode, Config{Command: command}, "default")
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := p.Stream(ctx, saigetypes.Request{Messages: []saigetypes.Message{user("go")}})
	if err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(200*time.Millisecond, cancel)
	done := make(chan error, 1)
	go func() {
		var last error
		for d := range ch {
			if e, ok := d.(saigetypes.ErrorDelta); ok {
				last = e.Error
			}
		}
		done <- last
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "was stopped") {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the stream did not end when the turn was stopped")
	}
}

func TestConfig(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, kind, block, want string
	}{
		{"no dir", ClaudeCode, `{}`, "needs dir"},
		{"relative dir", ClaudeCode, `{"dir":"repo"}`, "needs dir"},
		{"dir is a file", Codex, `{"dir":"` + file + `"}`, "is not a directory"},
		{"claude takes no codex sandbox", ClaudeCode, `{"dir":"` + dir + `","permissions":"read-only"}`, "permissions is one of acceptEdits"},
		{"codex takes no claude mode", Codex, `{"dir":"` + dir + `","permissions":"plan"}`, "permissions is one of workspace-write"},
		{"codex takes no allow", Codex, `{"dir":"` + dir + `","allow":["Bash(git:*)"]}`, "takes no allow"},
		{"allow is not a flag", ClaudeCode, `{"dir":"` + dir + `","allow":["--dangerously-skip-permissions"]}`, "allow holds permission rules"},
		{"unknown field", ClaudeCode, `{"dir":"` + dir + `","sandbox":"x"}`, "unknown field"},
		{"bad timeout", ClaudeCode, `{"dir":"` + dir + `","timeout":"soon"}`, "not a positive duration"},
		{"unknown kind", "cursor", `{"dir":"` + dir + `"}`, "no program of kind"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Factory(tt.kind)(context.Background(), json.RawMessage(tt.block))
			if !errors.Is(err, ErrConfig) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
	v, err := Factory(Codex)(context.Background(), json.RawMessage(`{"dir":"`+dir+`","timeout":"30m","permissions":"danger-full-access"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.(native.ProviderFunc); !ok {
		t.Fatalf("the factory returned %T", v)
	}
}

func TestAnAgentsEffortIsAskedOfTheProgram(t *testing.T) {
	for _, tt := range []struct{ kind, out, want string }{
		{ClaudeCode, claudeOut, "--effort\x00high"},
		{Codex, codexOut, "-c\x00model_reasoning_effort=\"high\""},
	} {
		command, runs := program(t, tt.out, 0)
		fn, err := New(tt.kind, Config{Dir: t.TempDir(), StateDir: t.TempDir(), Command: command})
		if err != nil {
			t.Fatal(err)
		}
		for i, effort := range []string{"high", ""} {
			p, err := fn(native.Spec{Name: "coder", Model: "default", Effort: effort})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, _, _, err := collect(t, p, user("go")); err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(strings.Join(args(t, runs, i), "\x00"), tt.want); got != (effort != "") {
				t.Fatalf("%s effort %q: asked = %v: %q", tt.kind, effort, got, args(t, runs, i))
			}
		}
	}
}

const compactOut = `{"type":"system","subtype":"init","session_id":"sess-1","model":"claude-test"}
{"type":"system","subtype":"status","status":"compacting","session_id":"sess-1"}
{"type":"system","subtype":"compact_boundary","session_id":"sess-1","compact_metadata":{"trigger":"manual","pre_tokens":32866,"post_tokens":11604}}
{"type":"result","subtype":"success","is_error":false,"session_id":"sess-1","result":"","usage":{"input_tokens":1,"output_tokens":0}}
`

func TestACommandOfTheProgramsOwnReachesAContinuedSessionAlone(t *testing.T) {
	command, runs := program(t, claudeOut, 0)
	state := t.TempDir()
	p := build(t, ClaudeCode, Config{Command: command, StateDir: state}, "default")
	// A thread's first turn is not a session to command yet: it is asked whole.
	if _, _, _, _, err := collect(t, p, platformAsk("", "", "/compact")); err != nil {
		t.Fatal(err)
	}
	if first := args(t, runs, 0); first[1] == "/compact" {
		t.Fatalf("a first turn was sent as a command: %q", first[1])
	}
	if _, _, _, _, err := collect(t, p, platformAsk("r1", "Dev (@dev): /compact\ncoder (agent): Looking. Fixed.", "and the docs")); err != nil {
		t.Fatal(err)
	}
	// The program's answer to its own command is the turn's answer.
	command2, runs2 := program(t, compactOut, 0)
	p2 := build(t, ClaudeCode, Config{Command: command2, StateDir: state, Dir: providerDir(t, p)}, "default")
	said, _, _, _, err := collect(t, p2, platformAsk("r1", "trimmed", "/compact"))
	if err != nil {
		t.Fatal(err)
	}
	if got := args(t, runs2, 0); got[1] != "/compact" || !containsArg(got, "--resume") {
		t.Fatalf("args %q", got)
	}
	if said != "Compacted the session: 32866 tokens before, 11604 after." {
		t.Fatalf("said %q", said)
	}
	// Something typed that only starts like a command, over more than a line, is asked whole.
	if _, _, _, _, err := collect(t, p2, platformAsk("r1", "trimmed", "/tmp/a.log is full\nwhy?")); err != nil {
		t.Fatal(err)
	}
	if got := args(t, runs2, 1); !strings.HasPrefix(got[1], "Context from the trigger:") {
		t.Fatalf("args %q", got[1])
	}
}

func providerDir(t *testing.T, p saigetypes.Provider) string {
	t.Helper()
	return p.(*provider).cfg.Dir
}
