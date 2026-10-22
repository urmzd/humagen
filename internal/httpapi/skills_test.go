package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// A project's owners declare its skills, which a message posted there and
// below can name; whoever reads the project reads them, and a project a
// caller may not see answers 404 on every route that would list them.
func TestAProjectsSkillsAreDeclaredByOwnersAndListedWhereTheyAreSeen(t *testing.T) {
	f := newFixture(t)
	admin := f.machineAs(t, "svc-admin", "acme", everything...)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Field"}`, http.StatusCreated)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Globex","parent":"Field","members":[{"who":"*","role":"member"},{"who":"svc-admin","role":"owner"}]}`, http.StatusCreated)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Secret","members":[{"who":"svc-admin","role":"owner"}]}`, http.StatusCreated)
	skill := func(name string) string {
		return fmt.Sprintf(`{"name":"%s","description":"Does %s.","instructions":"Do %s."}`, name, name, name)
	}
	put := func(project string, names ...string) map[string]any {
		t.Helper()
		cur := want(t, f, admin, "GET", "/api/projects/"+project+"/skills", "", http.StatusOK)
		var ss []string
		for _, n := range names {
			ss = append(ss, skill(n))
		}
		return want(t, f, admin, "PUT", "/api/projects/"+project+"/skills", fmt.Sprintf(`{"version":%v,"skills":[%s]}`, cur["version"], strings.Join(ss, ",")), http.StatusOK)
	}
	put("Field", "review")
	got := put("Globex", "renewal")
	if fmt.Sprint(got["offered"]) != "[map[argument_hint: description:Does renewal. from:Globex name:renewal] map[argument_hint: description:Does review. from:Field name:review]]" {
		t.Fatalf("Globex offers %v", got["offered"])
	}
	put("Secret", "canary")

	// A member reads them; only an owner declares them.
	member := f.person(t, "acme", "00u-bo", "Bo", "humagen-operators")
	if listed := want(t, f, member, "GET", "/api/skills?project=Globex", "", http.StatusOK); fmt.Sprint(listed["skills"]) != fmt.Sprint(got["offered"]) {
		t.Fatalf("a member lists %v", listed["skills"])
	}
	if code, out := status(t, f, member, "PUT", "/api/projects/Globex/skills", fmt.Sprintf(`{"version":%v,"skills":[]}`, got["version"])); code != http.StatusForbidden || out["error"] != "you need to own Globex" {
		t.Fatalf("a member declaring Globex's skills = %d %v", code, out)
	}
	if items := want(t, f, member, "GET", "/api/mentions?project=Globex&kind=skill&q=re", "", http.StatusOK); fmt.Sprint(items["items"]) != "[map[description:Does renewal. name:renewal] map[description:Does review. name:review]]" {
		t.Fatalf("a member completes %v", items["items"])
	}
	// A project they may not see is not there, on every route.
	for _, path := range []string{"/api/projects/Secret/skills", "/api/skills?project=Secret", "/api/mentions?project=Secret", "/api/mentions?project=Secret&kind=skill"} {
		if code, body := get(t, f, member, path); code != http.StatusNotFound || strings.Contains(body, "canary") {
			t.Errorf("%s = %d %s", path, code, body)
		}
	}
	for _, path := range []string{"/api/skills", "/api/skills?project=Field", "/api/mentions?kind=skill"} {
		if _, body := get(t, f, member, path); strings.Contains(body, "canary") {
			t.Errorf("%s shows a skill of a project the caller may not see: %s", path, body)
		}
	}
	// A message names them where it is posted: Globex's in Globex, and
	// not in Field.
	want(t, f, member, "POST", "/api/threads", `{"project":"Globex","visibility":"private","text":"$renewal please","post_only":true}`, http.StatusCreated)
	if code, out := status(t, f, member, "POST", "/api/threads", `{"project":"Field","visibility":"private","text":"$renewal please","post_only":true}`); code != http.StatusBadRequest || out["error"] != "there is no skill called renewal" {
		t.Fatalf("a skill named outside its project = %d %v", code, out)
	}
}

// A project's view, its list and its history name its skills without their
// instructions, so 32 skills of 8,000 characters each keep them small; only
// the skills route reads the instructions.
func TestAProjectsViewAndHistoryStaySmallWithLargeSkills(t *testing.T) {
	f := newFixture(t)
	admin := f.machineAs(t, "svc-admin", "acme", everything...)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Field"}`, http.StatusCreated)
	var ss []string
	for i := range 32 {
		ss = append(ss, fmt.Sprintf(`{"name":"s%d","description":"Does %d.","argument_hint":"[x]","instructions":"%s"}`, i, i, strings.Repeat("i", 8000)))
	}
	cur := want(t, f, admin, "GET", "/api/projects/Field/skills", "", http.StatusOK)
	got := want(t, f, admin, "PUT", "/api/projects/Field/skills", fmt.Sprintf(`{"version":%v,"skills":[%s]}`, cur["version"], strings.Join(ss, ",")), http.StatusOK)
	if sk := sliceOf(got["skills"]); len(sk) != 32 || len(obj(sk[0])["instructions"].(string)) != 8000 {
		t.Fatalf("the skills route lost the instructions")
	}
	for _, path := range []string{"/api/projects/Field", "/api/projects", "/api/projects/Field/history"} {
		code, body := get(t, f, admin, path)
		if code != http.StatusOK || !strings.Contains(body, `"s31"`) {
			t.Fatalf("%s = %d, without the skills' names: %.300s", path, code, body)
		}
		if strings.Contains(body, strings.Repeat("i", 100)) || len(body) > 64<<10 {
			t.Errorf("%s holds the skills' instructions: %d bytes", path, len(body))
		}
	}
}
