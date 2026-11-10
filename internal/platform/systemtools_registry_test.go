package platform_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
)

// registryTable is the registry as one line per tool: its key (and its name
// when the key differs), who it is offered to, its category (and the second
// one it declares), what a call does, and who approves a call.
func registryTable(t *testing.T, p *platform.Platform) string {
	t.Helper()
	all, err := p.SystemTools()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, tl := range all {
		offered := []string{}
		if tl.Exposure.MCP() {
			offered = append(offered, "mcp")
		}
		if tl.Exposure.Runs() {
			offered = append(offered, "runs")
		}
		category := string(tl.Category)
		if tl.Also != "" {
			category += "+" + string(tl.Also)
			if tl.BothAllow {
				category += "(both)"
			}
		}
		does := []string{}
		for name, set := range map[string]bool{"read-only": tl.ReadOnly, "destructive": tl.Destructive, "idempotent": tl.Idempotent, "parallel": tl.Parallel, "acts-for-person": tl.ActsForPerson, "upstream": tl.Upstream} {
			if set {
				does = append(does, name)
			}
		}
		sort.Strings(does)
		approval := "policy"
		switch {
		case tl.Approval.PerCall != nil:
			approval = "policy, and per call"
		case tl.Approval.Mode == platform.ApprovalNone:
			approval = "none"
		case tl.Approval.Mode == platform.ApprovalAlways:
			approval = fmt.Sprintf("always %s self=%v expires=%s", strings.Join(tl.Approval.Approvers, ","), tl.Approval.SelfApproval, tl.Approval.Expires)
		}
		name := tl.Name
		if tl.Key != "" {
			name = tl.Key + " (" + tl.Name + ")"
		}
		fmt.Fprintf(&b, "%s | %s | %s | %s | %s\n", name, strings.Join(offered, ","), category, strings.Join(does, ","), approval)
	}
	return b.String()
}

// Every tool is declared once in the registry, with its category, what a
// call does, who approves it and who it is offered to. The whole table is
// pinned: a tool that is added, or one whose declaration changes, shows here
// as a diff to review.
func TestTheRegistryDeclaresEveryTool(t *testing.T) {
	p, err := platform.New(platform.Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	got := registryTable(t, p)
	golden := filepath.Join("testdata", "system_tools.golden")
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got == string(want) {
		return
	}
	have := map[string]bool{}
	for line := range strings.SplitSeq(string(want), "\n") {
		have[line] = true
	}
	for line := range strings.SplitSeq(got, "\n") {
		if !have[line] {
			t.Errorf("declared, not pinned: %s", line)
		}
		delete(have, line)
	}
	for line := range have {
		t.Errorf("pinned, not declared: %s", line)
	}
	if !t.Failed() {
		t.Error("the registry lists its tools in another order than the pinned table")
	}
}
