package app

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Every agent file in these tests is generated, not a real one.

// guardedAgent has a hook, a permissions block with a deny rule, tools, a
// model and other fields an agent here does not carry.
const guardedAgent = `---
name: guarded
description: Reviews changes.
tools: Read, Grep, Bash
model: sonnet
color: blue
effort: high
permissionMode: plan
hooks:
  PreToolUse:
    - matcher: Bash
      hooks:
        - type: command
          command: ./generated-check.sh
permissions:
  allow:
    - Read
  deny:
    - Bash(generated-rm:*)
---

Review each change with care.
`

// plainAgent is an agent file whose every field is carried.
const plainAgent = "---\nname: plain\ndescription: Says hello.\n---\nSay hello.\n"

const (
	guardedLines = "  guarded.md: not carried: color, effort, hooks, model, permissionMode, permissions, tools\n" +
		"  guarded.md: not enforced: hooks.PreToolUse, permissionMode, permissions.allow, permissions.deny\n"
	notes = hooksNote + "\n" + permissionsNote + "\n"
	next  = "Next: each agent has no tools. Grant what it needs under Agents.\n"
)

func agentFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	putFile(t, dir, "guarded.md", guardedAgent)
	putFile(t, dir, "plain.md", plainAgent)
	return dir
}

func agentsJSON(t *testing.T, out string) agentsResult {
	t.Helper()
	var res agentsResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	return res
}

func TestImportAgentsNamesEachFieldItDoesNotCarry(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	out, err := runImport(t, "agents", agentFiles(t), "--provider", "anthropic", "--model", "claude-test", "--server", server.URL)
	want := "Created 2 agents on anthropic claude-test; 0 already there, 0 failed.\n" + guardedLines + notes + next
	if err != nil || out != want {
		t.Fatalf("err = %v\nout:\n%s\nwant:\n%s", err, out, want)
	}
	// The agent is sent only what it carries: no hook, rule or tool.
	spec := api.agents[0]["spec"].(map[string]any)
	if spec["name"] != "guarded" || len(spec) != 6 || len(spec["tools"].([]any)) != 0 || spec["model"] != "claude-test" {
		t.Fatalf("guarded = %v", spec)
	}
	if strings.Contains(out, "generated-") {
		t.Fatalf("the report holds a value:\n%s", out)
	}
	// The notes are short lines, and the hook note says what is true: a
	// check before a call has an equivalent, a hook after one does not.
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "  guarded.md") && len(line) > 80 {
			t.Errorf("a note line is %d columns: %q", len(line), line)
		}
	}
	if !strings.Contains(hooksNote, "does not run a file's hook commands") || !strings.Contains(hooksNote, "approvals") || !strings.Contains(hooksNote, "after a call or when a turn stops") {
		t.Errorf("hooks note: %s", hooksNote)
	}
}

func TestImportAgentsJSONNamesEachFieldItDoesNotCarry(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	out, err := runImport(t, "agents", agentFiles(t), "--provider", "p", "--model", "m", "--json", "--server", server.URL)
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	res := agentsJSON(t, out)
	if want := map[string][]string{"guarded.md": {"color", "effort", "hooks", "model", "permissionMode", "permissions", "tools"}}; !reflect.DeepEqual(res.Reduced, want) {
		t.Fatalf("reduced = %v", res.Reduced)
	}
	if want := map[string][]string{"guarded.md": {"hooks.PreToolUse", "permissionMode", "permissions.allow", "permissions.deny"}}; !reflect.DeepEqual(res.NotEnforced, want) {
		t.Fatalf("not_enforced = %v", res.NotEnforced)
	}
	if len(res.NotEnforcedOther) != 0 || len(res.Skipped) != 0 || strings.Contains(out, "generated-") {
		t.Fatalf("json:\n%s", out)
	}
}

func TestImportAgentsDryRunNamesWhatItWouldNotCarry(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	out, err := runImport(t, "agents", agentFiles(t), "--provider", "p", "--model", "m", "--dry-run", "--server", server.URL)
	want := "Would create 2 agents on p m; 0 already there, 0 failed.\n" + guardedLines + notes
	if err != nil || out != want {
		t.Fatalf("err = %v\nout:\n%s\nwant:\n%s", err, out, want)
	}
	if len(api.agents) != 0 {
		t.Fatalf("a dry run wrote %v", api.agents)
	}
}

// A second import of the same files writes nothing, and names again what the
// agents do not carry: whether an agent was made from its file as it is now
// is not known, so the list is repeated rather than called old news.
func TestImportAgentsAgainRepeatsWhatIsNotCarried(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	dir := agentFiles(t)
	if out, err := runImport(t, "agents", dir, "--provider", "p", "--model", "m", "--server", server.URL); err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	out, err := runImport(t, "agents", dir, "--provider", "p", "--model", "m", "--server", server.URL)
	want := "Created 0 agents on p m; 2 already there, 0 failed.\n" + guardedLines + notes
	if err != nil || out != want {
		t.Fatalf("err = %v\nout:\n%s\nwant:\n%s", err, out, want)
	}
	if len(api.agents) != 2 {
		t.Fatalf("agents = %v", api.agents)
	}
	out, err = runImport(t, "agents", dir, "--provider", "p", "--model", "m", "--json", "--server", server.URL)
	if res := agentsJSON(t, out); err != nil || len(res.Present) != 2 || len(res.Reduced["guarded.md"]) != 7 || len(res.NotEnforced["guarded.md"]) != 4 {
		t.Fatalf("err = %v\n%s", err, out)
	}
}

// Each explanation is printed once, however many files need it.
func TestImportAgentsExplainsOnceForManyFiles(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	dir := t.TempDir()
	for i := range 12 {
		putFile(t, dir, fmt.Sprintf("agent-%02d.md", i), strings.Replace(guardedAgent, "name: guarded", fmt.Sprintf("name: agent-%02d", i), 1))
	}
	out, err := runImport(t, "agents", dir, "--provider", "p", "--model", "m", "--server", server.URL)
	if err != nil || strings.Count(out, "Hooks are not enforced") != 1 || strings.Count(out, "Permission rules are not enforced") != 1 ||
		strings.Count(out, ": not carried: ") != 12 || strings.Count(out, ": not enforced: ") != 12 {
		t.Fatalf("err = %v\n%s", err, out)
	}
}

// Finding 1: a name from a file reaches the terminal quoted when it is not
// plain text, so an escape sequence cannot hide a line and a line break
// cannot forge one. JSON keeps the real name.
func TestImportAgentsQuotesNamesThatAreNotPlainText(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	dir := t.TempDir()
	putFile(t, dir, "conceal.md", "---\nname: conceal\n\"\\e[8mhidden\\e[0m\": 1\nhooks:\n  PreToolUse: []\n---\nBody.\n")
	putFile(t, dir, "newline.md", "---\nname: newline\n\"x\\n  victim.md: not enforced: nothing\": 1\n---\nBody.\n")
	putFile(t, dir, "title.md", "---\nname: title\n\"\\e]0;pwned\\a\": 1\n\"\\u202eevil\": 1\n\" spaced\": 1\n"+strings.Repeat("K", 200)+": 1\n---\nBody.\n")
	putFile(t, dir, "spoof.md", "---\nname: \"\\e[2K\\rspoof\"\n---\n")
	out, err := runImport(t, "agents", dir, "--provider", "p", "--model", "m", "--server", server.URL)
	if err == nil {
		t.Fatalf("the agent with no text was created\n%s", out)
	}
	for _, b := range []byte{0x1b, 0x07, '\r'} {
		if strings.IndexByte(out, b) >= 0 {
			t.Fatalf("raw byte %#x in the report:\n%q", b, out)
		}
	}
	for _, want := range []string{
		"  conceal.md: not carried: \"\\x1b[8mhidden\\x1b[0m\", hooks\n",
		"  newline.md: not carried: \"x\\n  victim.md: not enforced: nothing\"\n",
		"  title.md: not carried: \"\\x1b]0;pwned\\a\", \" spaced\", " + strings.Repeat("K", 64) + "..., \"\\u202eevil\"\n",
		"  failed \"\\x1b[2K\\rspoof\": spoof.md has no text to be the system prompt\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("out lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\n  victim.md") {
		t.Fatalf("a line was forged:\n%s", out)
	}
	js, _ := runImport(t, "agents", dir, "--provider", "p", "--model", "m", "--json", "--dry-run")
	if res := agentsJSON(t, js); !reflect.DeepEqual(res.Reduced["conceal.md"], []string{"\x1b[8mhidden\x1b[0m", "hooks"}) || strings.IndexByte(js, 0x1b) >= 0 {
		t.Fatalf("json:\n%s", js)
	}
}

// The skills import's line of fields left out quotes them the same way.
func TestImportSkillsQuotesFieldNamesThatAreNotPlainText(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	dir := t.TempDir()
	putFile(t, dir, "ship/SKILL.md", "---\nname: ship\ndescription: Ships.\n\"\\e[8mhidden\": true\n\"a\\nforged: line\": true\n---\n\n# Ship\n\nShip it.\n")
	out, err := runImport(t, "skills", dir, "--server", server.URL)
	if err != nil || strings.IndexByte(out, 0x1b) >= 0 ||
		!strings.Contains(out, "  ship: drafted without frontmatter the library does not read: \"\\x1b[8mhidden\", \"a\\nforged: line\"\n") {
		t.Fatalf("err = %v\n%q", err, out)
	}
}

// Finding 2: two files that name one agent are reported each under its own
// name: the first gives the agent and its fields, the second is skipped.
func TestImportAgentsReportsEachFileOfOneName(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	dir := t.TempDir()
	putFile(t, dir, "a.md", "---\nname: same\nhooks: {PreToolUse: []}\npermissions: {deny: [x]}\nmodel: opus\n---\nA.\n")
	putFile(t, dir, "b.md", "---\nname: same\ncolor: red\n---\nB.\n")
	out, err := runImport(t, "agents", dir, "--provider", "p", "--model", "m", "--server", server.URL)
	want := "Created 1 agents on p m; 0 already there, 1 skipped, 0 failed.\n" +
		"  skipped b.md: it names the agent same, which a.md gives\n" +
		"  a.md: not carried: hooks, model, permissions\n" +
		"  a.md: not enforced: hooks.PreToolUse, permissions.deny\n" + notes + next
	if err != nil || out != want {
		t.Fatalf("err = %v\nout:\n%s\nwant:\n%s", err, out, want)
	}
	if len(api.agents) != 1 || api.agents[0]["spec"].(map[string]any)["system_prompt"] != "A." {
		t.Fatalf("agents = %v", api.agents)
	}
	js, err := runImport(t, "agents", dir, "--provider", "p", "--model", "m", "--json", "--server", server.URL)
	res := agentsJSON(t, js)
	if err != nil || strings.Join(res.Present, ",") != "same" || !reflect.DeepEqual(res.Skipped, []agentSkipped{{File: "b.md", Name: "same", From: "a.md"}}) ||
		len(res.Reduced) != 1 || len(res.Reduced["a.md"]) != 3 || len(res.NotEnforced["a.md"]) != 2 {
		t.Fatalf("err = %v\n%s", err, js)
	}
}

// Findings 3 and 5: the frontmatter ends at a line that is --- alone; a
// document end, a second document, a repeated key and the like fail, each
// with a reason of its own that quotes nothing of the file.
func TestImportAgentsReportsMalformedFrontmatter(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	dir := t.TempDir()
	putFile(t, dir, "bare.md", "No frontmatter here.\n")
	putFile(t, dir, "broken.md", "---\nname: [broken\n---\nText.\n")
	putFile(t, dir, "docend.md", "---\nname: docend\n...\nhooks:\n  PreToolUse: []\n---\nText.\n")
	putFile(t, dir, "dashes.md", "---\nname: dashes\ndescription: |\n  line\n---x inside\nhooks: {Stop: []}\n---\nText.\n")
	putFile(t, dir, "dup.md", "---\nname: dup\ncolor: secret-red\ncolor: secret-blue\n---\nText.\n")
	putFile(t, dir, "empty.md", "---\n---\nText.\n")
	putFile(t, dir, "many.md", "---\nname: many\n"+manyKeys(maxAgentFields+1)+"---\nText.\n")
	putFile(t, dir, "open.md", "---\nname: open\nText with no closing line.\n")
	putFile(t, dir, "scalar.md", "---\njust a string\n---\nText.\n")
	putFile(t, dir, "typed.md", "---\nname: 7\ndescription: [a, b]\n---  \nText.\n")
	out, err := runImport(t, "agents", dir, "--provider", "p", "--model", "m", "--server", server.URL)
	if err == nil || err.Error() != "9 agents were not created" {
		t.Fatalf("err = %v\n%s", err, out)
	}
	for _, want := range []string{
		"Created 1 agents on p m; 0 already there, 9 failed.\n",
		"  failed bare: bare.md does not start with YAML frontmatter\n",
		"  failed broken: broken.md has frontmatter that is not valid YAML\n",
		"  failed dashes: dashes.md has frontmatter that is not valid YAML\n",
		"  failed docend: docend.md has more than one YAML document in its frontmatter\n",
		"  failed dup: dup.md has frontmatter that repeats a key\n",
		"  failed empty: empty.md has empty frontmatter\n",
		"  failed many: many.md has more than 1000 frontmatter fields\n",
		"  failed open: open.md has frontmatter with no closing --- line\n",
		"  failed scalar: scalar.md has frontmatter that is not a YAML mapping\n",
		// A closing line with trailing spaces closes. A name or description
		// that is not text is not carried: the agent is named for its file
		// and has no description.
		"  typed.md: not carried: description, name\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("out lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "secret-") || strings.Contains(out, "inside") {
		t.Fatalf("a reason quotes the file:\n%s", out)
	}
	if spec := api.agents[0]["spec"].(map[string]any); spec["name"] != "typed" || spec["description"] != "" || spec["system_prompt"] != "Text." {
		t.Fatalf("typed = %v", spec)
	}
}

func manyKeys(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "k%04d: v\n", i)
	}
	return b.String()
}

// Finding 6: an empty key is named (empty), a line lists at most 40 names,
// and JSON holds them all.
func TestImportAgentsBoundsTheNamesALineLists(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	dir := t.TempDir()
	putFile(t, dir, "keys.md", "---\nname: keys\n\"\": a\n~: b\n"+manyKeys(48)+"---\nText.\n")
	out, err := runImport(t, "agents", dir, "--provider", "p", "--model", "m", "--server", server.URL)
	if err != nil || !strings.Contains(out, "  keys.md: not carried: (empty), k0000, ") || !strings.HasSuffix(strings.Split(out, "\n")[1], ", k0038, and 9 more") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	js, _ := runImport(t, "agents", dir, "--provider", "p", "--model", "m", "--json", "--dry-run")
	if res := agentsJSON(t, js); len(res.Reduced["keys.md"]) != 49 || res.Reduced["keys.md"][0] != "" {
		t.Fatalf("json:\n%s", js)
	}
}

// Finding 7: under hooks and permissions only known names are shown; any
// other key, which may be a rule or a command, is counted.
func TestImportAgentsCountsEntriesItDoesNotName(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	dir := t.TempDir()
	putFile(t, dir, "rules.md", "---\nname: rules\npermissions:\n  \"Bash(rm -rf /generated)\": deny\n  deny: [x]\nhooks:\n  \"Pre\\e[2KToolUse\": []\n  Stop: []\n  1: []\n---\nText.\n")
	out, err := runImport(t, "agents", dir, "--provider", "p", "--model", "m", "--server", server.URL)
	if err != nil || !strings.Contains(out, "  rules.md: not enforced: hooks.Stop, permissions.deny, 2 other hooks entries, 1 other permissions entry\n") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	js, _ := runImport(t, "agents", dir, "--provider", "p", "--model", "m", "--json", "--dry-run")
	res := agentsJSON(t, js)
	if !reflect.DeepEqual(res.NotEnforcedOther["rules.md"], map[string]int{"hooks": 2, "permissions": 1}) {
		t.Fatalf("json:\n%s", js)
	}
	for _, text := range []string{out, js} {
		if strings.Contains(text, "rm -rf") || strings.Contains(text, "[2K") {
			t.Fatalf("a rule or event key is printed:\n%s", text)
		}
	}
}

func TestImportAgentsCarryingEverythingReportsNothingLeftOut(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	dir := t.TempDir()
	putFile(t, dir, "plain.md", plainAgent)
	out, err := runImport(t, "agents", dir, "--provider", "p", "--model", "m", "--server", server.URL)
	if want := "Created 1 agents on p m; 0 already there, 0 failed.\n" + next; err != nil || out != want {
		t.Fatalf("err = %v\n%s", err, out)
	}
	out, err = runImport(t, "agents", dir, "--provider", "p", "--model", "m", "--json", "--server", server.URL)
	if err != nil || !strings.Contains(out, `"reduced":{}`) || !strings.Contains(out, `"not_enforced":{}`) || !strings.Contains(out, `"skipped":[]`) {
		t.Fatalf("err = %v\n%s", err, out)
	}
}

// shown and inline are how every import line prints a name or a reason
// taken from a person's files, the sessions import's included.
func TestImportNamesArePrintedSafely(t *testing.T) {
	for in, want := range map[string]string{
		"plain":                        "plain",
		"":                             "(empty)",
		" lead":                        `" lead"`,
		"\x1b[8mx":                     `"\x1b[8mx"`,
		"a\nb":                         `"a\nb"`,
		"\xff":                         `"\xff"`,
		"é":                            "é",
		strings.Repeat("é", 70):        strings.Repeat("é", 64) + "...",
		"\r" + strings.Repeat("a", 70): `"\r` + strings.Repeat("a", 63) + `"...`,
	} {
		if got := shown(in); got != want {
			t.Errorf("shown(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"/work/app: refused":     "/work/app: refused",
		"/work/\x1b]0;x\a/app":   `/work/\x1b]0;x\a/app`,
		"line\nforged: \xff end": `line\nforged: \xff end`,
	} {
		if got := inline(in); got != want {
			t.Errorf("inline(%q) = %q, want %q", in, got, want)
		}
	}
}
