package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"

	saigeskills "github.com/urmzd/saige/agent/skills"
	"github.com/urmzd/saige/flow/runtime"

	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
)

// skillResources is a generated package's supporting files, sorted by path
// as a snapshot gives them: n fillers under a/, and extra.
func skillResources(n int, extra ...string) []saigeskills.SkillResource {
	paths := slices.Clone(extra)
	for i := range n {
		paths = append(paths, fmt.Sprintf("a/f%03d.xsd", i))
	}
	slices.Sort(paths)
	out := make([]saigeskills.SkillResource, len(paths))
	for i, p := range paths {
		out[i] = saigeskills.SkillResource{Path: p, Size: 10}
	}
	return out
}

func listedPaths(instructions string, resources []saigeskills.SkillResource) []string {
	var out []string
	for _, r := range listedSkillFiles(instructions, resources) {
		out = append(out, r.Path)
	}
	return out
}

// Every path the instructions name is listed first, in the order named,
// however it is written: in backquotes or a link, after "./", before
// sentence punctuation, or holding a space, a bracket, a comma, a
// semicolon, an asterisk or a trailing full stop. A name that is no file of
// the package lists nothing, and the result is the same every time.
func TestTheFilesTheInstructionsNameAreListedFirst(t *testing.T) {
	named := []string{
		"z/plain.md", "z/tick.md", "z/link.md", "z/dot.md", "z/colon.md", "z/prefix", "z/prefix.md",
		"z/uni-é.md", "z/space name.md", "z/paren(1).md", "z/comma,x.md", "z/trailing.", "z/semi;x.md", "z/star*.md", "z/hash#.md",
		"z/ellipsis.md", "z/quoted.md",
	}
	resources := skillResources(100, named...)
	instructions := strings.Join([]string{
		"Read z/plain.md first.",
		"Then `z/tick.md`, and [the link](z/link.md).",
		"See ./z/dot.md. Also z/colon.md: done.",
		"Prefix z/prefix and z/prefix.md.",
		"Unicode z/uni-é.md.",
		"Space z/space name.md, paren z/paren(1).md, comma z/comma,x.md, trailing z/trailing. semi z/semi;x.md star z/star*.md hash z/hash#.md",
		"Ellipsis z/ellipsis.md... quoted \"z/quoted.md\" and z/missing.md which is not in the package.",
	}, "\n")
	got := listedPaths(instructions, resources)
	if len(got) != maxListedSkillFiles || !slices.Equal(got[:len(named)], named) || got[len(named)] != "a/f000.xsd" {
		t.Fatalf("listed %d, first %q", len(got), got[:min(len(got), len(named)+1)])
	}
	for range 5 {
		if !slices.Equal(got, listedPaths(instructions, resources)) {
			t.Fatal("the order differs between calls")
		}
	}
	// Every file named, in reverse: the first 63 named are listed, in the
	// order named.
	resources = skillResources(255)
	var names []string
	for i := 254; i >= 0; i-- {
		names = append(names, fmt.Sprintf("a/f%03d.xsd", i))
	}
	if got := listedPaths(strings.Join(names, " "), resources); got[0] != "a/f254.xsd" || got[62] != "a/f192.xsd" {
		t.Fatalf("named order not kept: first %s, last %s", got[0], got[62])
	}
}

// In the turn's text a named file over what skill_file_read returns is listed
// first and marked, and the remainder line counts the rest, for both the
// skill_file_read list and the host one.
func TestTheTurnTextMarksANamedLargeFileAndCountsTheRest(t *testing.T) {
	resources := skillResources(200, "z/big.xsd")
	for i := range resources {
		if resources[i].Path == "z/big.xsd" {
			resources[i].Size = 1 << 20
		}
	}
	raw, err := json.Marshal([]pinnedSkill{{Name: "many", PackageID: "sk-x", PackageHash: "h", Version: 1, Resources: resources, Instructions: "Validate with z/big.xsd."}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := withSkills(runtime.Request{Meta: map[string]string{runs.MetaSkills: string(raw)}}, func(runtime.Request, []pinnedSkill) (hostFiles, error) {
		return hostFiles{dir: "/tmp/hf", tool: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"not execute it:\nskill:many/z/big.xsd (1048576 bytes), over the 131072 bytes skill_file_read returns\nskill:many/a/f000.xsd (10 bytes)",
		"63 of 201 supporting files are listed: those the instructions name first, then by path. 138 more are not listed; skill_file_read reads any path the instructions name, as skill:many/<path>.",
		"not execute it:\n/tmp/hf/many/z/big.xsd (1048576 bytes)\n",
		"138 more are not listed; every file is under /tmp/hf/many/.",
	} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("the turn's text lacks %q:\n%.3000s", want, out.Text)
		}
	}
}

// A skill record this release cannot read is logged once per process, not at
// every completion that leaves it out.
func TestAnUnreadableSkillIsLoggedOnce(t *testing.T) {
	st := state.NewMemory()
	var logged bytes.Buffer
	p, err := New(Config{Workspace: t.TempDir(), Log: slog.New(slog.NewTextHandler(&logged, nil)), Backends: Backends{State: st}})
	if err != nil {
		t.Fatal(err)
	}
	own := library.Scope{Kind: "personal", Subject: "ada"}
	if err := st.Write(t.Context(), "library.skills", state.Put("acme", own.Key()+"/later", []byte(`{"revisions":"not a list"}`))); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		p.AvailableSkills("acme", "", "ada")
	}
	if n := strings.Count(logged.String(), "skills this release cannot read are left out"); n != 1 || !strings.Contains(logged.String(), "skills=[later]") {
		t.Fatalf("logged %d times:\n%s", n, logged.String())
	}
}
