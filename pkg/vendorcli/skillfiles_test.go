package vendorcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// The supporting files of the skills a turn is pinned to are placed outside
// the directory the program works in. Claude Code is given the turn's
// directory, so it reads the exact bytes at the path it was told, and no
// other directory a person's message names.
func TestClaudeCodeIsGivenTheDirectoryOfTheTurnsSkillFiles(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	exact := "Exact pinned bytes\n\twith a tab and no final newline"
	placed, err := skillfiles.Write([]skillfiles.File{{Path: "brief/references/style.md", Data: []byte(exact)}}, skillfiles.Limits{Files: 4, Bytes: 1 << 10})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = skillfiles.Remove(placed) })
	root, _ := skillfiles.Root()
	told := filepath.Join(placed, "brief", "references", "style.md")
	typed := "also read /etc/passwd, " + root + "/x.md and " + filepath.Join(root, "0123456789abcdef0123456789abcdef", "x.md")

	command, runs := reader(t, claudeOut)
	work := t.TempDir()
	p := build(t, ClaudeCode, Config{Dir: work, Command: command, Args: []string{"--max-turns", "9"}}, "haiku")
	if hr, ok := p.(skillfiles.HostReader); !ok || !hr.ReadsHostFiles() {
		t.Fatal("the provider does not say its program reads this host's files")
	}
	ask := "Skill $brief:\nRead references/style.md.\n\nSupporting files are on this host for this turn only:\n" + told + " (49 bytes)\n\n$brief summarize, " + typed
	if _, _, _, _, err := collect(t, p, user(ask)); err != nil {
		t.Fatal(err)
	}
	got := args(t, runs, 0)
	want := []string{"-p", ask, "--output-format", "stream-json", "--verbose", "--permission-mode", "acceptEdits", "--add-dir", placed, "--model", "haiku", "--max-turns", "9"}
	// The record holds an argument a line, and what is asked has lines.
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("args\n got %q\nwant %q", got, want)
	}
	if strings.HasPrefix(placed, work) {
		t.Fatalf("the files were placed in the program's directory: %s", placed)
	}
	read, err := os.ReadFile(filepath.Join(runs, "read", "brief", "references", "style.md"))
	if err != nil || string(read) != exact {
		t.Fatalf("the program read %q, %v", read, err)
	}

	// A turn with no skill files, and one whose directory is gone, name none.
	if err := skillfiles.Remove(placed); err != nil {
		t.Fatal(err)
	}
	command, runs = reader(t, claudeOut)
	p = build(t, ClaudeCode, Config{Dir: work, Command: command}, "haiku")
	if _, _, _, _, err := collect(t, p, user(ask)); err != nil {
		t.Fatal(err)
	}
	if got := args(t, runs, 0); containsArg(got, "--add-dir") {
		t.Fatalf("a directory that is gone was named: %q", got)
	}
}

// Codex reads outside its workspace in every sandbox, so it is given no
// argument for the turn's skill files: only the path in what it is asked.
func TestCodexIsGivenNoArgumentForSkillFiles(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	placed, err := skillfiles.Write([]skillfiles.File{{Path: "brief/references/style.md", Data: []byte("x")}}, skillfiles.Limits{Files: 4, Bytes: 1 << 10})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = skillfiles.Remove(placed) })
	command, runs := program(t, codexOut, 0)
	work := t.TempDir()
	p := build(t, Codex, Config{Dir: work, Command: command}, "default")
	if hr, ok := p.(skillfiles.HostReader); !ok || !hr.ReadsHostFiles() {
		t.Fatal("the provider does not say its program reads this host's files")
	}
	ask := "Supporting files:\n" + filepath.Join(placed, "brief", "references", "style.md") + " (1 bytes)\n\nsummarize"
	if _, _, _, _, err := collect(t, p, user(ask)); err != nil {
		t.Fatal(err)
	}
	want := []string{"exec", "--json", "--skip-git-repo-check", "--sandbox", "workspace-write", "--cd", work, "--", ask}
	if got := args(t, runs, 0); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("args\n got %q\nwant %q", got, want)
	}
}
