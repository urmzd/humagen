package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/drive"
)

// Writing a memory file takes what changing the tree's notes takes, and
// can_write says so before the write is tried. A member who reads a
// project's memory is refused, an owner writes, a stale base version is a
// conflict that leaves the tree as it was, and a path outside the tree, a
// binary body and a body over the file bound are refused.
func TestWritingAMemoryFileTakesTheRightToChangeTheTree(t *testing.T) {
	f := newFixture(t)
	admin := f.machineAs(t, "svc-admin", "acme", everything...)
	ana := f.machineAs(t, "ana", "acme", operator...)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Field","members":[{"who":"*","role":"member"},{"who":"svc-admin","role":"owner"}]}`, http.StatusCreated)

	const tree, file = "/api/memory/files?scope=project&project=Field", "/api/memory/files/content"
	write := func(path, content string, base int) string {
		return fmt.Sprintf(`{"scope":"project","project":"Field","file":%q,"content":%q,"base_version":%d}`, path, content, base)
	}
	version := func(tok string) int {
		t.Helper()
		return int(want(t, f, tok, "GET", tree, "", http.StatusOK)["version"].(float64))
	}
	text := func() string {
		t.Helper()
		return want(t, f, admin, "GET", file+"?scope=project&project=Field&file=plan.md", "", http.StatusOK)["content"].(string)
	}

	// The member reads the tree, is told they may not write it, and is
	// refused when they try.
	if view := want(t, f, ana, "GET", tree, "", http.StatusOK); view["can_write"] != false {
		t.Fatalf("a member's view of the tree = %v", view)
	}
	want(t, f, ana, "PUT", file, write("plan.md", "Smuggled.", 0), http.StatusForbidden)
	if n := version(admin); n != 0 {
		t.Fatalf("a refused write left version %d", n)
	}

	// The owner is told they may write, and does.
	if view := want(t, f, admin, "GET", tree, "", http.StatusOK); view["can_write"] != true {
		t.Fatalf("an owner's view of the tree = %v", view)
	}
	first := want(t, f, admin, "PUT", file, write("plan.md", "First plan.", 0), http.StatusOK)
	if first["version"].(float64) != 1 || first["can_write"] != true || text() != "First plan." {
		t.Fatalf("the first write = %v, holding %q", first, text())
	}
	want(t, f, admin, "PUT", file, write("plan.md", "Second plan.", 1), http.StatusOK)

	// An edit made against the version before the last write is a conflict,
	// and the tree is as the last write left it.
	stale := want(t, f, admin, "PUT", file, write("plan.md", "Stale plan.", 1), http.StatusConflict)
	if stale["error"] != "the file changed since it was read" {
		t.Fatalf("a stale write = %v", stale)
	}
	if n, got := version(admin), text(); n != 2 || got != "Second plan." {
		t.Fatalf("after a stale write the tree is version %d holding %q", n, got)
	}

	// A path outside the tree, a binary body, a body over the bound and a
	// base that would skip the check are refused, and write nothing.
	for _, path := range []string{"../plan.md", "/etc/plan.md", "notes/../../plan.md", ""} {
		want(t, f, admin, "PUT", file, write(path, "x", 2), http.StatusBadRequest)
	}
	want(t, f, admin, "PUT", file, write("plan.md", "a\x00b", 2), http.StatusBadRequest)
	want(t, f, admin, "PUT", file, write("plan.md", strings.Repeat("a", drive.MaxFileBytes+1), 2), http.StatusBadRequest)
	want(t, f, admin, "PUT", file, write("plan.md", "x", -1), http.StatusBadRequest)
	if n, got := version(admin), text(); n != 2 || got != "Second plan." {
		t.Fatalf("after refused writes the tree is version %d holding %q", n, got)
	}
}

// An agent that lives in a project is that project's to change, as its notes
// are: agents:write alone does not write its memory files.
func TestWritingAnAgentsMemoryFileTakesOwningItsHome(t *testing.T) {
	f := newFixture(t)
	admin := f.machineAs(t, "svc-admin", "acme", everything...)
	manager := f.machineAs(t, "bo", "acme", append([]string{"humagen:agents:write"}, operator...)...)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Field","members":[{"who":"*","role":"member"},{"who":"svc-admin","role":"owner"}]}`, http.StatusCreated)
	want(t, f, admin, "POST", "/api/agents", `{"name":"scout","model":"offline","provider":"offline","system_prompt":"x"}`, http.StatusCreated)
	want(t, f, admin, "PUT", "/api/agents/scout/home", `{"project":"Field"}`, http.StatusOK)

	const tree, file = "/api/memory/files?scope=agent&agent=scout", "/api/memory/files/content"
	body := `{"scope":"agent","agent":"scout","file":"plan.md","content":"Plan.","base_version":0}`
	if view := want(t, f, manager, "GET", tree, "", http.StatusOK); view["can_write"] != false {
		t.Fatalf("a member's view of the agent's tree = %v", view)
	}
	want(t, f, manager, "PUT", file, body, http.StatusForbidden)
	if view := want(t, f, admin, "GET", tree, "", http.StatusOK); view["can_write"] != true {
		t.Fatalf("the home's owner's view of the agent's tree = %v", view)
	}
	want(t, f, admin, "PUT", file, body, http.StatusOK)
	// An agent that does not exist has no memory to write.
	want(t, f, admin, "PUT", file, `{"scope":"agent","agent":"ghost","file":"plan.md","content":"Plan.","base_version":0}`, http.StatusForbidden)
}
