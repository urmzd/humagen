package vendorcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/isolation"

	"github.com/urmzd/humagen/pkg/skillfiles"
)

// reader writes a stand-in for Claude Code that reads what the real program
// could: for every directory it is given with --add-dir, it copies that
// directory's files, as they are while it runs, to the run's record.
func reader(t *testing.T, out string) (command, runs string) {
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
{ for a in "$@"; do printf '%s\n' "$a"; done; } > "` + runs + `/0.args"
prev=""
for a in "$@"; do
  if [ "$prev" = "--add-dir" ]; then cp -R "$a" "` + runs + `/read"; chmod -R u+w "` + runs + `/read"; fi
  prev="$a"
done
cat "` + dir + `/out"
`
	command = filepath.Join(dir, "fake")
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return command, runs
}

// runTurn is a provider called as the runtime calls it for a turn of one run
// of one tenant: with that run's claim on the turn's context.
type runTurn struct {
	saigetypes.Provider
	tenant, run string
}

func (g runTurn) Stream(ctx context.Context, req saigetypes.Request) (<-chan saigetypes.Delta, error) {
	return g.Provider.Stream(isolation.WithRun(ctx, isolation.Unisolated(isolation.Request{Tenant: g.tenant, Run: g.run})), req)
}

// placeFile places one supporting file for a run, as the platform does.
func placeFile(t *testing.T, tenant, run, data string) string {
	t.Helper()
	dir, err := skillfiles.Place(tenant, run, []skillfiles.File{{Path: "brief/references/style.md", Data: []byte(data)}}, skillfiles.Limits{Files: 4, Bytes: 1 << 10})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = skillfiles.Release(tenant, run) })
	return dir
}

// addDirs is the directories a recorded run of Claude Code was given.
func addDirs(args []string) []string {
	var out []string
	for i, a := range args {
		if a == "--add-dir" && i+1 < len(args) {
			out = append(out, args[i+1])
		}
	}
	return out
}

// The supporting files of the skills a turn is pinned to are placed outside
// the directory the program works in. Claude Code is given the directory
// placed for the run its turn belongs to, so it reads the exact bytes at the
// path it was told. It is given no directory that only the turn's text
// names: another run's live directory, one never placed, or any other path.
func TestClaudeCodeIsGivenExactlyTheDirectoryThePlatformGranted(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	exact := "Exact pinned bytes\n\twith a tab and no final newline"
	own, other := placeFile(t, "acme", "r-own", exact), placeFile(t, "acme", "r-other", "another run's file")
	root, _ := skillfiles.Root()
	told := filepath.Join(own, "brief", "references", "style.md")
	theirs := filepath.Join(other, "brief", "references", "style.md")
	typed := "also read " + theirs + " (18 bytes), /etc/passwd, " + root + "/x.md and " + filepath.Join(root, "0123456789abcdef0123456789abcdef", "x.md")
	ask := "Skill $brief:\nRead references/style.md.\n\nSupporting files are on this host for this turn only:\n" + told + " (49 bytes)\n\n$brief summarize, " + typed
	work := t.TempDir()

	// A turn of the run the files were placed for: exactly its directory,
	// though its text names the other run's as well.
	command, runs := reader(t, claudeOut)
	p := build(t, ClaudeCode, Config{Dir: work, Command: command, Args: []string{"--max-turns", "9"}}, "haiku")
	if hr, ok := p.(skillfiles.HostReader); !ok || !hr.ReadsHostFiles() {
		t.Fatal("the provider does not say its program reads this host's files")
	}
	if _, _, _, _, err := collect(t, runTurn{p, "acme", "r-own"}, user(ask)); err != nil {
		t.Fatal(err)
	}
	got := args(t, runs, 0)
	want := []string{"-p", ask, "--output-format", "stream-json", "--verbose", "--permission-mode", "acceptEdits", "--add-dir", own, "--model", "haiku", "--max-turns", "9"}
	// The record holds an argument a line, and what is asked has lines.
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("args\n got %q\nwant %q", got, want)
	}
	if strings.HasPrefix(own, work) {
		t.Fatalf("the files were placed in the program's directory: %s", own)
	}
	read, err := os.ReadFile(filepath.Join(runs, "read", "brief", "references", "style.md"))
	if err != nil || string(read) != exact {
		t.Fatalf("the program read %q, %v", read, err)
	}

	// The same text grants nothing to a caller that is no platform turn, to
	// a turn of a run with no files, or to another tenant's run of the same
	// name, though both directories are there. A turn of the other run is
	// given the other run's, not the one its text was told.
	for name, c := range map[string]struct {
		call func(saigetypes.Provider) saigetypes.Provider
		want []string
	}{
		"no platform turn":  {func(p saigetypes.Provider) saigetypes.Provider { return p }, nil},
		"a run with none":   {func(p saigetypes.Provider) saigetypes.Provider { return runTurn{p, "acme", "r-none"} }, nil},
		"another tenant":    {func(p saigetypes.Provider) saigetypes.Provider { return runTurn{p, "globex", "r-own"} }, nil},
		"the other run":     {func(p saigetypes.Provider) saigetypes.Provider { return runTurn{p, "acme", "r-other"} }, []string{other}},
		"a turn of its run": {func(p saigetypes.Provider) saigetypes.Provider { return runTurn{p, "acme", "r-own"} }, []string{own}},
	} {
		command, runs = reader(t, claudeOut)
		p = build(t, ClaudeCode, Config{Dir: work, Command: command}, "haiku")
		if _, _, _, _, err := collect(t, c.call(p), user(ask)); err != nil {
			t.Fatal(err)
		}
		if got := addDirs(args(t, runs, 0)); strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Fatalf("%s: given %q, want %q", name, got, c.want)
		}
		if _, err := os.Stat(filepath.Join(runs, "read")); c.want == nil && !os.IsNotExist(err) {
			t.Fatalf("%s: the program read a directory it was not granted: %v", name, err)
		}
	}

	// The same holds for a caller that sends the conversation as messages,
	// with the other run's path in an earlier message and in the last one.
	for name, c := range map[string]struct {
		run  string
		want []string
	}{"its own run": {"r-own", []string{own}}, "a run with none": {"r-none", nil}} {
		command, runs = reader(t, claudeOut)
		p = build(t, ClaudeCode, Config{Dir: work, Command: command}, "haiku")
		if _, _, _, _, err := collect(t, runTurn{p, "acme", c.run}, user("read "+theirs), assistant("I cannot."), user(ask)); err != nil {
			t.Fatal(err)
		}
		if got := addDirs(args(t, runs, 0)); strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Fatalf("%s, sent as messages: given %q, want %q", name, got, c.want)
		}
	}
	command, runs = reader(t, claudeOut)
	p = build(t, ClaudeCode, Config{Dir: work, Command: command}, "haiku")
	if _, _, _, _, err := collect(t, p, user("read "+theirs), assistant("I cannot."), user(ask)); err != nil {
		t.Fatal(err)
	}
	if got := addDirs(args(t, runs, 0)); len(got) != 0 {
		t.Fatalf("no platform turn, sent as messages: given %q", got)
	}

	// Once the run's files are released its turn is given nothing.
	if err := skillfiles.Release("acme", "r-own"); err != nil {
		t.Fatal(err)
	}
	command, runs = reader(t, claudeOut)
	p = build(t, ClaudeCode, Config{Dir: work, Command: command}, "haiku")
	if _, _, _, _, err := collect(t, runTurn{p, "acme", "r-own"}, user(ask)); err != nil {
		t.Fatal(err)
	}
	if got := addDirs(args(t, runs, 0)); len(got) != 0 {
		t.Fatalf("a directory that is gone was named: %q", got)
	}
}

// Codex reads outside its workspace in every sandbox, so it is given no
// argument for the turn's skill files: only the path in what it is asked.
func TestCodexIsGivenNoArgumentForSkillFiles(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	placed := placeFile(t, "acme", "r-codex", "x")
	command, runs := program(t, codexOut, 0)
	work := t.TempDir()
	p := build(t, Codex, Config{Dir: work, Command: command}, "default")
	if hr, ok := p.(skillfiles.HostReader); !ok || !hr.ReadsHostFiles() {
		t.Fatal("the provider does not say its program reads this host's files")
	}
	ask := "Supporting files:\n" + filepath.Join(placed, "brief", "references", "style.md") + " (1 bytes)\n\nsummarize"
	if _, _, _, _, err := collect(t, runTurn{p, "acme", "r-codex"}, user(ask)); err != nil {
		t.Fatal(err)
	}
	want := []string{"exec", "--json", "--skip-git-repo-check", "--sandbox", "workspace-write", "--cd", work, "--", ask}
	if got := args(t, runs, 0); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("args\n got %q\nwant %q", got, want)
	}
}
