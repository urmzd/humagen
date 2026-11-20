package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
)

// packaged is a skill folder as the library keeps it: SKILL.md and one
// supporting file that alone holds fact.
func packaged(name, fact string) library.Definition {
	md := fmt.Sprintf("---\nname: %s\ndescription: Answers from the facts sheet.\n---\n\nRead references/facts.md and answer from it.\n", name)
	return library.Definition{Skill: projects.Skill{Name: name}, Tools: []string{}, Files: []library.File{
		{Path: "SKILL.md", Data: []byte(md)}, {Path: "references/facts.md", Data: []byte(fact)},
	}}
}

// pinned is the skills a run's tag was pinned with, as the platform wrote
// them.
func pinned(t *testing.T, f *fixture, tenant, run string) []map[string]any {
	t.Helper()
	r, err := f.p.Runs.Get(tenant, run)
	if err != nil {
		t.Fatal(err)
	}
	raw := wire.Meta(r.Tag())[runs.MetaSkills]
	if raw == "" {
		return nil
	}
	var out []map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// A run started through POST /api/runs is given the skills its message
// names, as a message posted in a thread is: the published revision, pinned
// with its supporting files, never a newer draft, and a name that is no
// published skill where the run is filed refuses it before anything starts.
func TestARunIsGivenTheSkillsItsMessageNamesAsAThreadMessageIs(t *testing.T) {
	f := newFixture(t)
	admin := f.machineAs(t, "svc-admin", "acme", everything...)
	ada := f.person(t, "acme", "00u-ada", "Ada", "humagen-admins")
	bo := f.person(t, "acme", "00u-bo", "Bo", "humagen-operators")
	want(t, f, admin, "POST", "/api/agents", `{"name":"helper","model":"offline","provider":"offline","system_prompt":"Answer"}`, http.StatusCreated)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Vault","members":[{"who":"svc-admin","role":"owner"},{"who":"00u-ada","role":"member"}]}`, http.StatusCreated)

	now := time.Now()
	publish := func(tenant string, scope library.Scope, d library.Definition) library.Revision {
		t.Helper()
		r, err := f.p.SkillLibrary.Draft(tenant, "svc-admin", scope, d, 0, nil, now)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.p.SkillLibrary.Tested(tenant, scope, r.Name, 1, "trusted-fixture-test"); err != nil {
			t.Fatal(err)
		}
		if r, err = f.p.SkillLibrary.Publish(tenant, "svc-admin", scope, r.Name, 1, now); err != nil {
			t.Fatal(err)
		}
		return r
	}
	workspace := library.Scope{Kind: "workspace"}
	v1 := publish("acme", workspace, packaged("depot", "The code word is HERON."))
	// A newer draft, tested and not published, is never the one used.
	if _, err := f.p.SkillLibrary.Draft("acme", "svc-admin", workspace, packaged("depot", "The code word is OSPREY."), 1, nil, now); err != nil {
		t.Fatal(err)
	}
	if err := f.p.SkillLibrary.Tested("acme", workspace, "depot", 2, "trusted-fixture-test"); err != nil {
		t.Fatal(err)
	}
	// Skills that are there, and not for this run: never published, another
	// project's, another person's own, another workspace's.
	if _, err := f.p.SkillLibrary.Draft("acme", "svc-admin", workspace, packaged("unpublished", "x"), 0, nil, now); err != nil {
		t.Fatal(err)
	}
	publish("acme", library.Scope{Kind: "project", Project: "Vault"}, packaged("vaulted", "x"))
	publish("acme", library.Scope{Kind: "personal", Subject: "00u-ada"}, packaged("adas-own", "x"))
	publish("globex", workspace, packaged("elsewhere", "x"))

	hash := func() string {
		pkg, err := v1.Package()
		if err != nil {
			t.Fatal(err)
		}
		return pkg.Hash
	}()
	check := func(run string) {
		t.Helper()
		got := pinned(t, f, "acme", run)
		if len(got) != 1 || got[0]["name"] != "depot" || got[0]["version"] != float64(1) || got[0]["package_id"] != v1.ID || got[0]["package_hash"] != hash ||
			!strings.Contains(fmt.Sprint(got[0]["resources"]), "references/facts.md") || strings.Contains(fmt.Sprint(got), "OSPREY") {
			t.Fatalf("the run was pinned with %v", got)
		}
	}
	// The same words pin the same revision either way.
	for _, text := range []string{"$depot what is the code word?", "@skill:depot what is the code word?", "/depot what is the code word?", "a price of $5, then $depot and $depot again"} {
		run := want(t, f, bo, "POST", "/api/runs", fmt.Sprintf(`{"agent":"helper","text":%q}`, text), http.StatusAccepted)["id"].(string)
		check(run)
	}
	posted := want(t, f, bo, "POST", "/api/threads", `{"text":"@helper $depot what is the code word?"}`, http.StatusCreated)
	threadRun := obj(sliceOf(posted["runs"])[0])["id"].(string)
	check(threadRun)
	// A follow-up names it where its conversation is filed.
	first := want(t, f, bo, "POST", "/api/runs", `{"agent":"helper","text":"hello"}`, http.StatusAccepted)["id"].(string)
	if got := pinned(t, f, "acme", first); got != nil {
		t.Fatalf("a run that names no skill was pinned with %v", got)
	}
	waitRun(t, f, bo, first)
	check(want(t, f, bo, "POST", "/api/runs", fmt.Sprintf(`{"agent":"helper","text":"now $depot","after":%q}`, first), http.StatusAccepted)["id"].(string))

	// Tags of people and agents are text in a run's message, however many.
	many := "ask"
	for i := range 14 {
		many += fmt.Sprintf(" @nobody%d", i)
	}
	if got := pinned(t, f, "acme", want(t, f, bo, "POST", "/api/runs", fmt.Sprintf(`{"agent":"helper","text":%q}`, many+" about $PATH and `$unpublished`"), http.StatusAccepted)["id"].(string)); got != nil {
		t.Fatalf("a run that names no skill was pinned with %v", got)
	}

	// A name that is no published skill for this person here refuses the
	// run, in the words that refuse the message, and starts nothing.
	before := len(f.p.Runs.List("acme", 200))
	for _, name := range []string{"missing", "unpublished", "vaulted", "adas-own", "elsewhere"} {
		code, out := status(t, f, bo, "POST", "/api/runs", fmt.Sprintf(`{"agent":"helper","text":"$%s what is the code word?"}`, name))
		tcode, tout := status(t, f, bo, "POST", "/api/threads", fmt.Sprintf(`{"text":"@helper $%s what is the code word?"}`, name))
		if code != http.StatusBadRequest || out["error"] != "there is no skill called "+name || tcode != code || tout["error"] != out["error"] {
			t.Errorf("$%s: a run = %d %v, a thread = %d %v", name, code, out, tcode, tout)
		}
	}
	if code, out := status(t, f, bo, "POST", "/api/runs", `{"agent":"helper","text":"$depot and $missing"}`); code != http.StatusBadRequest || out["error"] != "there is no skill called missing" {
		t.Errorf("a known and an unknown skill = %d %v", code, out)
	}
	if after := len(f.p.Runs.List("acme", 200)); after != before {
		t.Errorf("refused runs started %d runs", after-before)
	}
	// Where they are for the person, they are used: their own, and their
	// project's in that project only.
	if got := pinned(t, f, "acme", want(t, f, ada, "POST", "/api/runs", `{"agent":"helper","text":"$adas-own go"}`, http.StatusAccepted)["id"].(string)); len(got) != 1 || got[0]["name"] != "adas-own" {
		t.Errorf("a person's own skill pinned %v", got)
	}
	if got := pinned(t, f, "acme", want(t, f, ada, "POST", "/api/runs", `{"agent":"helper","text":"$vaulted go","project":"Vault"}`, http.StatusAccepted)["id"].(string)); len(got) != 1 || got[0]["name"] != "vaulted" || got[0]["from"] != "Vault" {
		t.Errorf("a project's skill pinned %v", got)
	}
	if code, out := status(t, f, ada, "POST", "/api/runs", `{"agent":"helper","text":"$vaulted go"}`); code != http.StatusBadRequest || out["error"] != "there is no skill called vaulted" {
		t.Errorf("a project's skill named outside it = %d %v", code, out)
	}
	// More than a message may name is refused.
	if code, _ := status(t, f, bo, "POST", "/api/runs", `{"agent":"helper","text":"$a1 $a2 $a3 $a4 $a5 $a6 $a7 $a8 $a9 $a10 $a11"}`); code != http.StatusBadRequest {
		t.Errorf("eleven skills = %d", code)
	}
}

// A draft read from a skill folder records that it was imported, and every
// answer carries the hash of the revision's package, the list too, so an
// import tells a changed folder from one the library holds without reading
// its files.
func TestASkillRevisionSaysItWasImportedAndCarriesItsPackageHash(t *testing.T) {
	f := newFixture(t)
	admin := f.machineAs(t, "svc-admin", "acme", everything...)
	draft := func(d library.Definition, version int, via string) (int, map[string]any) {
		t.Helper()
		body := map[string]any{"scope": map[string]string{"kind": "workspace"}, "definition": d, "version": version}
		if via != "" {
			body["via"] = via
		}
		return status(t, f, admin, "POST", "/api/library/skills", mustJSON(t, body))
	}
	d := packaged("depot", "The code word is HERON.")
	pkg, err := d.Package()
	if err != nil {
		t.Fatal(err)
	}
	code, first := draft(d, 0, "import")
	if code != http.StatusCreated || first["via"] != "import" || first["package_hash"] != pkg.Hash || first["published_at"] != nil {
		t.Fatalf("an imported draft = %d %v", code, first)
	}
	if code, out := draft(d, 0, "import"); code != http.StatusConflict {
		t.Fatalf("a second first revision = %d %v", code, out)
	}
	if code, out := draft(d, 1, "console"); code != http.StatusBadRequest {
		t.Fatalf("an unknown via = %d %v", code, out)
	}
	code, second := draft(packaged("depot", "The code word is OSPREY."), 1, "")
	if _, has := second["via"]; code != http.StatusCreated || has || second["package_hash"] == pkg.Hash || second["package_hash"] == "" {
		t.Fatalf("a draft saved in the editor = %d %v", code, second)
	}
	listed := sliceOf(want(t, f, admin, "GET", "/api/library/skills?scope=workspace", "", http.StatusOK)["skills"])
	if row := obj(listed[0]); len(listed) != 1 || row["package_hash"] != second["package_hash"] || row["files"] != nil || row["version"] != float64(2) {
		t.Fatalf("listed = %v", listed)
	}
	revisions := sliceOf(want(t, f, admin, "GET", "/api/library/skills/"+first["id"].(string), "", http.StatusOK)["revisions"])
	if len(revisions) != 2 || obj(revisions[0])["via"] != "import" || obj(revisions[0])["package_hash"] != pkg.Hash || obj(revisions[1])["via"] != nil {
		t.Fatalf("revisions = %v", revisions)
	}
}
