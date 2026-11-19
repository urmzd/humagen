package app

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// guardedAgent is a generated agent file, not a real one: a frontmatter with
// a hook, a permissions block with a deny rule, tools, a model and other
// fields an agent here does not carry.
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

// plainAgent is a generated agent file whose every field is carried.
const plainAgent = "---\nname: plain\ndescription: Says hello.\n---\nSay hello.\n"

const (
	guardedReduced = "  guarded: not carried from guarded.md: color, effort, hooks, model, permissionMode, permissions, tools\n"
	guardedHooks   = "  guarded: not enforced by Humagen: hooks.PreToolUse. Humagen runs no hooks around an agent's tool calls, and has no equivalent.\n"
	guardedDeny    = "  guarded: not enforced by Humagen: permissionMode, permissions.allow, permissions.deny. No rule of the file is applied. The equivalent is set under Agents, on the agent (the tools it is granted, and which of them wait for a person's approval), and under Operations, Action Policies (Never or Ask for a kind of action). A rule on one command or path has no equivalent.\n"
)

func agentFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	putFile(t, dir, "guarded.md", guardedAgent)
	putFile(t, dir, "plain.md", plainAgent)
	return dir
}

func TestImportAgentsNamesEachFieldItDoesNotCarry(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	dir := agentFiles(t)
	out, err := runImport(t, "agents", dir, "--provider", "anthropic", "--model", "claude-test", "--server", server.URL)
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	want := "Created 2 agents on anthropic claude-test; 0 already there, 0 failed.\n" +
		guardedReduced + guardedHooks + guardedDeny +
		"Next: each agent has no tools. Grant what it needs under Agents.\n"
	if out != want {
		t.Fatalf("out:\n%s\nwant:\n%s", out, want)
	}
	// The agent is sent only what it carries: no hook, rule or tool.
	spec := api.agents[0]["spec"].(map[string]any)
	if spec["name"] != "guarded" || len(spec) != 6 || len(spec["tools"].([]any)) != 0 || spec["model"] != "claude-test" {
		t.Fatalf("guarded = %v", spec)
	}
	// Values never reach the report: they may hold commands and paths.
	if strings.Contains(out, "generated-check") || strings.Contains(out, "generated-rm") {
		t.Fatalf("the report holds a value:\n%s", out)
	}
}

func TestImportAgentsJSONNamesEachFieldItDoesNotCarry(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	out, err := runImport(t, "agents", agentFiles(t), "--provider", "p", "--model", "m", "--json", "--server", server.URL)
	var res agentsResult
	if jerr := json.Unmarshal([]byte(out), &res); jerr != nil || err != nil {
		t.Fatalf("err = %v, json = %v\n%s", err, jerr, out)
	}
	if want := map[string][]string{"guarded": {"color", "effort", "hooks", "model", "permissionMode", "permissions", "tools"}}; !reflect.DeepEqual(res.Reduced, want) {
		t.Fatalf("reduced = %v", res.Reduced)
	}
	if want := map[string][]string{"guarded": {"hooks.PreToolUse", "permissionMode", "permissions.allow", "permissions.deny"}}; !reflect.DeepEqual(res.NotEnforced, want) {
		t.Fatalf("not_enforced = %v", res.NotEnforced)
	}
	if strings.Contains(out, "generated-") {
		t.Fatalf("the report holds a value:\n%s", out)
	}
}

func TestImportAgentsDryRunNamesWhatItWouldNotCarry(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	out, err := runImport(t, "agents", agentFiles(t), "--provider", "p", "--model", "m", "--dry-run", "--server", server.URL)
	want := "Would create 2 agents on p m; 0 already there, 0 failed.\n" + guardedReduced + guardedHooks + guardedDeny
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
	want := "Created 0 agents on p m; 2 already there, 0 failed.\n" + guardedReduced + guardedHooks + guardedDeny
	if err != nil || out != want {
		t.Fatalf("err = %v\nout:\n%s\nwant:\n%s", err, out, want)
	}
	if len(api.agents) != 2 {
		t.Fatalf("agents = %v", api.agents)
	}
	out, err = runImport(t, "agents", dir, "--provider", "p", "--model", "m", "--json", "--server", server.URL)
	var res agentsResult
	if jerr := json.Unmarshal([]byte(out), &res); jerr != nil || err != nil || len(res.Present) != 2 || len(res.Reduced["guarded"]) != 7 || len(res.NotEnforced["guarded"]) != 4 {
		t.Fatalf("err = %v, json = %v\n%s", err, jerr, out)
	}
}

func TestImportAgentsReportsMalformedFrontmatter(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	dir := t.TempDir()
	// Generated files.
	putFile(t, dir, "broken.md", "---\nname: [broken\n---\nText.\n")
	putFile(t, dir, "open.md", "---\nname: open\nText with no closing line.\n")
	putFile(t, dir, "bare.md", "No frontmatter here.\n")
	putFile(t, dir, "typed.md", "---\nname: 7\ndescription: [a, b]\n---\nText.\n")
	out, err := runImport(t, "agents", dir, "--provider", "p", "--model", "m", "--server", server.URL)
	if err == nil || err.Error() != "3 agents were not created" {
		t.Fatalf("err = %v\n%s", err, out)
	}
	for _, want := range []string{
		"Created 1 agents on p m; 0 already there, 3 failed.\n",
		"  failed bare: bare.md does not start with YAML frontmatter\n",
		"  failed broken: broken.md has frontmatter that is not a YAML mapping, or that no closing --- line ends\n",
		"  failed open: open.md has frontmatter that is not a YAML mapping, or that no closing --- line ends\n",
		// A name or description that is not text is not carried: the agent
		// is named for its file and has no description.
		"  typed: not carried from typed.md: description, name\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("out lacks %q:\n%s", want, out)
		}
	}
	if spec := api.agents[0]["spec"].(map[string]any); spec["name"] != "typed" || spec["description"] != "" {
		t.Fatalf("typed = %v", spec)
	}
}

func TestImportAgentsCarryingEverythingReportsNothingLeftOut(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	dir := t.TempDir()
	putFile(t, dir, "plain.md", plainAgent)
	out, err := runImport(t, "agents", dir, "--provider", "p", "--model", "m", "--server", server.URL)
	if want := "Created 1 agents on p m; 0 already there, 0 failed.\nNext: each agent has no tools. Grant what it needs under Agents.\n"; err != nil || out != want {
		t.Fatalf("err = %v\n%s", err, out)
	}
	out, err = runImport(t, "agents", dir, "--provider", "p", "--model", "m", "--json", "--server", server.URL)
	if err != nil || !strings.Contains(out, `"reduced":{}`) || !strings.Contains(out, `"not_enforced":{}`) {
		t.Fatalf("err = %v\n%s", err, out)
	}
}
