package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/audit"

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

// A file that does not exist is created by writing it against the version on
// view, and one created by someone else since then is a conflict. Deleting a
// file takes what writing takes and follows the same conflict rule: a member
// who only reads is refused, a path outside the tree is refused, a file
// changed since the version read is a conflict that removes nothing, and the
// earlier versions still hold the file. The delete is audited without content.
func TestCreatingAndDeletingAMemoryFile(t *testing.T) {
	f := newFixture(t)
	admin := f.machineAs(t, "svc-admin", "acme", everything...)
	ana := f.machineAs(t, "ana", "acme", operator...)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Field","members":[{"who":"*","role":"member"},{"who":"svc-admin","role":"owner"}]}`, http.StatusCreated)

	const tree, file, at = "/api/memory/files?scope=project&project=Field", "/api/memory/files/content", "?scope=project&project=Field"
	write := func(path, content string, base int) string {
		return fmt.Sprintf(`{"scope":"project","project":"Field","file":%q,"content":%q,"base_version":%d}`, path, content, base)
	}
	remove := func(path string, base int) string {
		return fmt.Sprintf("%s%s&file=%s&base_version=%d", file, at, url.QueryEscape(path), base)
	}
	paths := func(view map[string]any) string {
		var out []string
		for _, e := range view["entries"].([]any) {
			out = append(out, e.(map[string]any)["path"].(string))
		}
		return strings.Join(out, " ")
	}

	// A new file is written beside the ones the tree holds, at the version on
	// view. The same path created again from the older version is a conflict.
	want(t, f, admin, "PUT", file, write("plan.md", "Plan.", 0), http.StatusOK)
	created := want(t, f, admin, "PUT", file, write("notes/new.md", "New.", 1), http.StatusOK)
	if created["version"].(float64) != 2 || paths(created) != "notes/new.md plan.md" {
		t.Fatalf("the created file left %v", created)
	}
	want(t, f, admin, "PUT", file, write("notes/new.md", "Mine.", 1), http.StatusConflict)
	want(t, f, ana, "PUT", file, write("notes/theirs.md", "Smuggled.", 2), http.StatusForbidden)
	want(t, f, admin, "PUT", file, write("../notes/new.md", "x", 2), http.StatusBadRequest)

	// The member is refused, and nothing is removed.
	want(t, f, ana, "DELETE", remove("plan.md", 2), "", http.StatusForbidden)
	// A path outside the tree, a base that is not a version and a file the
	// tree does not hold are refused.
	for _, path := range []string{"../plan.md", "/etc/plan.md", "notes/../../plan.md", ""} {
		want(t, f, admin, "DELETE", remove(path, 2), "", http.StatusBadRequest)
	}
	want(t, f, admin, "DELETE", file+at+"&file=plan.md&base_version=-1", "", http.StatusBadRequest)
	want(t, f, admin, "DELETE", remove("missing.md", 2), "", http.StatusNotFound)
	// A file rewritten since the version read is a conflict, as is one with no
	// version named.
	want(t, f, admin, "PUT", file, write("plan.md", "Second plan.", 2), http.StatusOK)
	want(t, f, admin, "DELETE", remove("plan.md", 2), "", http.StatusConflict)
	want(t, f, admin, "DELETE", file+at+"&file=plan.md", "", http.StatusConflict)
	if view := want(t, f, admin, "GET", tree, "", http.StatusOK); view["version"].(float64) != 3 || paths(view) != "notes/new.md plan.md" {
		t.Fatalf("after refused deletes the tree is %v", view)
	}

	// The owner deletes the file at the version on view. The version before
	// still holds it.
	gone := want(t, f, admin, "DELETE", remove("plan.md", 3), "", http.StatusOK)
	if gone["version"].(float64) != 4 || paths(gone) != "notes/new.md" || gone["can_write"] != true {
		t.Fatalf("the delete left %v", gone)
	}
	want(t, f, admin, "GET", file+at+"&file=plan.md", "", http.StatusNotFound)
	if was := want(t, f, admin, "GET", file+at+"&file=plan.md&version=3", "", http.StatusOK); was["content"] != "Second plan." {
		t.Fatalf("the version before the delete holds %v", was)
	}
	// Deleting it again from the version that held it is a conflict.
	want(t, f, admin, "DELETE", remove("plan.md", 3), "", http.StatusConflict)

	// The file's history is the versions that changed it, newest first, and
	// pages by version. A reader may list it; a path outside the tree may not
	// be asked for.
	const history = "/api/memory/files/history" + at
	versions := func(tok, query string) string {
		t.Helper()
		var out []string
		for _, v := range want(t, f, tok, "GET", history+query, "", http.StatusOK)["versions"].([]any) {
			row := v.(map[string]any)
			out = append(out, fmt.Sprintf("%v:%d", row["version"], len(row["entries"].([]any))))
		}
		return strings.Join(out, " ")
	}
	if got := versions(ana, "&file=plan.md"); got != "4:0 3:1 1:1" {
		t.Fatalf("the file's history = %s", got)
	}
	if got := versions(admin, "&file=plan.md&before=4&limit=1"); got != "3:1" {
		t.Fatalf("the page before version 4 = %s", got)
	}
	if got := versions(admin, "&file=notes/new.md"); got != "2:1" {
		t.Fatalf("the other file's history = %s", got)
	}
	want(t, f, admin, "GET", history+"&file=../plan.md", "", http.StatusBadRequest)

	// Every delete tried is in the audit trail, refused or not, with the file
	// and no content.
	records, err := f.p.Audit.List("acme", audit.Query{Resource: "memory", From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour), Limit: 500}, func(audit.Record) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	outcomes := map[audit.Outcome]int{}
	for _, r := range records {
		if r.Action != "memory.delete" {
			continue
		}
		outcomes[r.Outcome]++
		if r.Route != "DELETE /api/memory/files/content" || r.After["file"] == "" || strings.Contains(fmt.Sprint(r), "Second plan.") {
			t.Fatalf("the audit record = %+v", r)
		}
	}
	if len(outcomes) < 2 {
		t.Fatalf("the audited deletes = %v, want refused and done", outcomes)
	}
}
