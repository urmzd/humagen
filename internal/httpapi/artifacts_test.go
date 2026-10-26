package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/nativefiles"
	"github.com/urmzd/humagen/pkg/policy"
)

func (f *fixture) raw(t *testing.T, path, bearer string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest("GET", f.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

// A published file is listed, versioned and downloaded only by callers in its
// tenant who hold the right, and is never served in a form a browser would run.
func TestArtifactsAreServedSafely(t *testing.T) {
	f := newFixture(t)
	admin := f.machine(t, "acme", everything...)
	reader := f.machine(t, "acme", "humagen:artifacts:read")
	none := f.machine(t, "acme", "humagen:runs:read")
	globex := f.machine(t, "globex", everything...)
	// What is published is read where it lives: Launch, which everyone sees.
	if res, _ := f.do(t, nil, "POST", "/api/projects", admin, `{"name":"Launch"}`); res.StatusCode != http.StatusCreated {
		t.Fatalf("create Launch = %d", res.StatusCode)
	}

	publish := func(thread, name, content string) string {
		t.Helper()
		if err := f.p.PutFile("acme", thread, name, content); err != nil {
			t.Fatal(err)
		}
		a, err := f.p.Publish(t.Context(), platform.Publication{
			Tenant: "acme", Thread: thread, Run: thread, Agent: "helper", Project: "Launch", By: "agent:helper", Path: name,
		})
		if err != nil {
			t.Fatal(err)
		}
		return a.ID
	}
	page := publish("t1", "page.html", "<html><body><script>alert(document.cookie)</script></body></html>")
	svg := publish("t1", "logo.svg", `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	notes := publish("t1", "notes.txt", "Refunds fell.")
	publish("t1", "notes.txt", "Refunds fell by a third.")
	publish("t2", `we"ird name.md`, "# Hi")

	// Listed newest first, latest version of each, with filters and paging.
	_, out := f.do(t, nil, "GET", "/api/artifacts", admin, "")
	list := out["artifacts"].([]any)
	if len(list) != 4 || out["next"] != nil {
		t.Fatalf("list = %d artifacts, next %v", len(list), out["next"])
	}
	latest := list[1].(map[string]any)
	if latest["title"] != "notes.txt" || latest["version"].(float64) != 2 || latest["mime"] != "text/plain" || latest["kind"] != "document" {
		t.Fatalf("latest notes = %v", latest)
	}
	if _, has := latest["key"]; has {
		t.Fatal("where the bytes are stored is exposed")
	}
	_, first := f.do(t, nil, "GET", "/api/artifacts?limit=3", admin, "")
	_, second := f.do(t, nil, "GET", "/api/artifacts?limit=3&before="+first["next"].(string), admin, "")
	if len(first["artifacts"].([]any)) != 3 || len(second["artifacts"].([]any)) != 1 || second["next"] != nil {
		t.Fatalf("pages = %d then %d", len(first["artifacts"].([]any)), len(second["artifacts"].([]any)))
	}
	if _, out := f.do(t, nil, "GET", "/api/artifacts?thread=t2", admin, ""); len(out["artifacts"].([]any)) != 1 {
		t.Fatal("the thread filter did not narrow the list")
	}
	if _, out := f.do(t, nil, "GET", "/api/artifacts?kind=html", admin, ""); len(out["artifacts"].([]any)) != 1 {
		t.Fatal("the kind filter did not narrow the list")
	}
	if _, out := f.do(t, nil, "GET", "/api/artifacts?q=NOTES", admin, ""); len(out["artifacts"].([]any)) != 1 {
		t.Fatal("searching by title did not find the one file")
	}
	for _, bad := range []string{"?kind=hologram", "?limit=0", "?before=not-a-cursor!"} {
		if res, _ := f.do(t, nil, "GET", "/api/artifacts"+bad, admin, ""); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %d", bad, res.StatusCode)
		}
	}
	_, versions := f.do(t, nil, "GET", "/api/artifacts/"+notes+"/versions", admin, "")
	if v := versions["versions"].([]any); len(v) != 2 || v[0].(map[string]any)["version"].(float64) != 2 {
		t.Fatalf("versions = %v", versions)
	}

	// Every download is an attachment of an opaque type unless it is asked
	// for inline and is a type that cannot carry script.
	for name, c := range map[string]struct {
		path, disposition, ctype string
	}{
		"html":                  {page + "/content", "attachment", "application/octet-stream"},
		"html asked for inline": {page + "/content?inline=1", "attachment", "application/octet-stream"},
		"svg asked for inline":  {svg + "/content?inline=1", "attachment", "application/octet-stream"},
		"text":                  {notes + "/content", "attachment", "application/octet-stream"},
		"text asked for inline": {notes + "/content?inline=1", "inline", "text/plain; charset=utf-8"},
	} {
		res, body := f.raw(t, "/api/artifacts/"+c.path, reader)
		h := res.Header
		if res.StatusCode != http.StatusOK || !strings.HasPrefix(h.Get("Content-Disposition"), c.disposition+";") || h.Get("Content-Type") != c.ctype {
			t.Errorf("%s: %d, %q, %q", name, res.StatusCode, h.Get("Content-Disposition"), h.Get("Content-Type"))
		}
		if h.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(h.Get("Content-Security-Policy"), "sandbox") || h.Get("Cache-Control") != "private, no-store" {
			t.Errorf("%s: headers %v", name, h)
		}
		if body == "" {
			t.Errorf("%s: no body", name)
		}
	}
	if _, body := f.raw(t, "/api/artifacts/"+notes+"/content", reader); body != "Refunds fell." {
		t.Fatalf("the first version reads %q", body)
	}
	// A title cannot break out of the header it is quoted in.
	odd := list[0].(map[string]any)["id"].(string)
	if res, _ := f.raw(t, "/api/artifacts/"+odd+"/content", reader); strings.ContainsAny(res.Header.Get("Content-Disposition"), "\r\n") || !strings.Contains(res.Header.Get("Content-Disposition"), "weird name.md") {
		t.Fatalf("disposition = %q", res.Header.Get("Content-Disposition"))
	}

	// Without the right, or from another tenant, there is nothing there.
	for _, path := range []string{"", "/" + notes, "/" + notes + "/content", "/" + notes + "/versions"} {
		if res, _ := f.raw(t, "/api/artifacts"+path, none); res.StatusCode != http.StatusForbidden {
			t.Errorf("without artifacts:read, %q = %d", path, res.StatusCode)
		}
	}
	if _, out := f.do(t, nil, "GET", "/api/artifacts", globex, ""); len(out["artifacts"].([]any)) != 0 {
		t.Fatal("another tenant lists the files")
	}
	for _, path := range []string{"/" + notes, "/" + notes + "/content", "/" + notes + "/versions"} {
		if res, body := f.raw(t, "/api/artifacts"+path, globex); res.StatusCode != http.StatusNotFound || strings.Contains(body, "Refunds") {
			t.Errorf("another tenant, %q = %d", path, res.StatusCode)
		}
	}
	if res, _ := f.do(t, nil, "DELETE", "/api/artifacts/"+notes, globex, ""); res.StatusCode != http.StatusNotFound {
		t.Fatalf("another tenant deleting = %d", res.StatusCode)
	}
	if res, _ := f.do(t, nil, "DELETE", "/api/artifacts/"+notes, reader, ""); res.StatusCode != http.StatusForbidden {
		t.Fatalf("a reader deleting = %d", res.StatusCode)
	}

	_, storage := f.do(t, nil, "GET", "/api/storage", admin, "")
	if storage["count"].(float64) != 5 || storage["limit_bytes"] != nil || storage["by_project"].([]any)[0].(map[string]any)["name"] != "Launch" {
		t.Fatalf("storage = %v", storage)
	}
	if _, other := f.do(t, nil, "GET", "/api/storage", globex, ""); other["used_bytes"].(float64) != 0 {
		t.Fatal("another tenant is charged for the files")
	}

	if res, _ := f.do(t, nil, "DELETE", "/api/artifacts/"+notes, admin, ""); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete = %d", res.StatusCode)
	}
	if res, _ := f.raw(t, "/api/artifacts/"+notes+"/content", reader); res.StatusCode != http.StatusNotFound {
		t.Fatalf("a deleted file still downloads: %d", res.StatusCode)
	}
	if _, after := f.do(t, nil, "GET", "/api/storage", admin, ""); after["count"].(float64) != 4 {
		t.Fatalf("after deleting one version there are %v", after["count"])
	}

	// The platform's own agent cannot be edited or impersonated.
	spec := `{"name":"humagen-writer","model":"offline","provider":"offline","system_prompt":"x"}`
	if res, _ := f.do(t, nil, "POST", "/api/agents", admin, spec); res.StatusCode != http.StatusForbidden {
		t.Fatalf("creating an agent under a built-in's name = %d", res.StatusCode)
	}
	if res, _ := f.do(t, nil, "PUT", "/api/agents/humagen-writer", admin, `{"spec":`+spec+`,"expected_rev":1}`); res.StatusCode != http.StatusForbidden {
		t.Fatalf("revising a built-in = %d", res.StatusCode)
	}
}

// What the storage page does, through the API: preview an image, delete a
// version, show a workspace over its limit, and page with See more. These are
// the server's half; the console's half is in apps/web/src/storage.test.tsx.
// Neither has been checked in a browser.
func TestStoragePageFlows(t *testing.T) {
	f := newFixture(t)
	admin := f.machine(t, "acme", everything...)
	publish := func(thread, name, content string) string {
		t.Helper()
		if err := f.p.PutFile("acme", thread, name, content); err != nil {
			t.Fatal(err)
		}
		a, err := f.p.Publish(t.Context(), platform.Publication{Tenant: "acme", Thread: thread, Run: thread, Agent: "helper", By: "agent:helper", Path: name})
		if err != nil {
			t.Fatal(err)
		}
		return a.ID
	}

	// Image preview: a PNG is served inline as itself when asked, and is an
	// image to the index, so the console shows it.
	png := "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89"
	chart := publish("t1", "chart.png", png)
	_, got := f.do(t, nil, "GET", "/api/artifacts/"+chart, admin, "")
	if got["mime"] != "image/png" || got["kind"] != "image" {
		t.Fatalf("a png is indexed as %v, %v", got["mime"], got["kind"])
	}
	res, body := f.raw(t, "/api/artifacts/"+chart+"/content?inline=1", admin)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/png" || !strings.HasPrefix(res.Header.Get("Content-Disposition"), "inline;") || body != png {
		t.Fatalf("inline png = %d, %q, %q, %d bytes", res.StatusCode, res.Header.Get("Content-Type"), res.Header.Get("Content-Disposition"), len(body))
	}
	if res.Header.Get("Content-Security-Policy") == "" || res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("inline png headers = %v", res.Header)
	}

	// Delete: removing the latest version lists the one before it again,
	// and the totals drop by what was removed.
	v1 := publish("t1", "plan.md", "one")
	v2 := publish("t1", "plan.md", "version two")
	_, before := f.do(t, nil, "GET", "/api/storage", admin, "")
	if res, _ := f.do(t, nil, "DELETE", "/api/artifacts/"+v2, admin, ""); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete = %d", res.StatusCode)
	}
	_, after := f.do(t, nil, "GET", "/api/storage", admin, "")
	if before["used_bytes"].(float64)-after["used_bytes"].(float64) != float64(len("version two")) || after["count"].(float64) != before["count"].(float64)-1 {
		t.Fatalf("storage %v then %v", before, after)
	}
	_, list := f.do(t, nil, "GET", "/api/artifacts?q=plan", admin, "")
	if l := list["artifacts"].([]any); len(l) != 1 || l[0].(map[string]any)["id"] != v1 {
		t.Fatalf("after deleting the latest, the list shows %v", l)
	}
	if res, _ := f.do(t, nil, "GET", "/api/artifacts/"+v2+"/versions", admin, ""); res.StatusCode != http.StatusNotFound {
		t.Fatalf("versions of a deleted file = %d", res.StatusCode)
	}
	if res, _ := f.do(t, nil, "DELETE", "/api/artifacts/"+v2, admin, ""); res.StatusCode != http.StatusNotFound {
		t.Fatalf("deleting twice = %d", res.StatusCode)
	}

	// Over quota: with a limit below what is stored, the storage read says
	// so, a publish is refused, and deleting brings it back under.
	_, now := f.do(t, nil, "GET", "/api/storage", admin, "")
	used := int64(now["used_bytes"].(float64))
	if _, err := f.p.Policies.Put("acme", policy.Policy{Scope: policy.Scope{Kind: policy.Tenant}, Period: policy.Day, MaxStorage: used - 1, Action: policy.Block}, "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	_, over := f.do(t, nil, "GET", "/api/storage", admin, "")
	if over["limit_bytes"] == nil || over["used_bytes"].(float64) <= over["limit_bytes"].(float64) {
		t.Fatalf("over quota reads %v", over)
	}
	if err := f.p.PutFile("acme", "t1", "more.md", "more"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Publish(t.Context(), platform.Publication{Tenant: "acme", Thread: "t1", Run: "t1", Agent: "helper", By: "agent:helper", Path: "more.md"}); !errors.Is(err, platform.ErrStorage) {
		t.Fatalf("a publish over quota = %v", err)
	}
	if res, _ := f.do(t, nil, "DELETE", "/api/artifacts/"+v1, admin, ""); res.StatusCode != http.StatusNoContent {
		t.Fatal("a delete over quota was refused")
	}
	if _, under := f.do(t, nil, "GET", "/api/storage", admin, ""); under["used_bytes"].(float64) > under["limit_bytes"].(float64) {
		t.Fatalf("after deleting, still over: %v", under)
	}
	if _, err := f.p.Policies.Put("acme", policy.Policy{Scope: policy.Scope{Kind: policy.Tenant}, Period: policy.Day, MaxStorage: 1 << 30, Action: policy.Block}, "test", time.Now()); err != nil {
		t.Fatal(err)
	}

	// See more: the console asks for 30 at a time and follows next. Every
	// file comes once, newest first, even when one is published and one
	// deleted between pages.
	var all []string
	for i := range 64 {
		all = append(all, publish("t2", fmt.Sprintf("f%02d.md", i), "x"))
	}
	var seen []string
	next, pages := "", 0
	for {
		path := "/api/artifacts?thread=t2&limit=30"
		if next != "" {
			path += "&before=" + next
		}
		_, out := f.do(t, nil, "GET", path, admin, "")
		for _, a := range out["artifacts"].([]any) {
			seen = append(seen, a.(map[string]any)["id"].(string))
		}
		pages++
		if pages == 1 {
			publish("t2", "late.md", "x")
			if res, _ := f.do(t, nil, "DELETE", "/api/artifacts/"+all[10], admin, ""); res.StatusCode != http.StatusNoContent {
				t.Fatal("delete between pages")
			}
		}
		if out["next"] == nil {
			break
		}
		next = out["next"].(string)
	}
	var want []string
	for i := len(all) - 1; i >= 0; i-- {
		if i != 10 {
			want = append(want, all[i])
		}
	}
	if pages != 3 || strings.Join(seen, " ") != strings.Join(want, " ") {
		t.Fatalf("%d pages, %d files: %v", pages, len(seen), seen)
	}
}

// A run's whole record can be read back after it has stopped streaming: its
// events as published, and one manifest for it and every run under it.
func TestRunManifest(t *testing.T) {
	f := newFixture(t)
	acme, globex := f.machine(t, "acme", everything...), f.machine(t, "globex", everything...)
	f.do(t, nil, "POST", "/api/agents", acme, helperSpec)
	_, run := f.do(t, nil, "POST", "/api/runs", acme, `{"agent":"helper","text":"Say hello"}`)
	id := run["id"].(string)

	var events []any
	for range 200 {
		_, out := f.do(t, nil, "GET", "/api/runs/"+id+"/events", acme, "")
		events, _ = out["events"].([]any)
		if n := len(events); n > 0 && events[n-1].(map[string]any)["kind"] == "lifecycle.completed" {
			if out["complete"] != true || events[0].(map[string]any)["kind"] != "lifecycle.accepted" {
				t.Fatalf("events = %v", out)
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(events) < 2 {
		t.Fatalf("the run's events never completed: %v", events)
	}

	res, m := f.do(t, nil, "GET", "/api/runs/"+id+"/manifest", acme, "")
	if res.StatusCode != http.StatusOK || m["root"] != id || m["version"].(float64) != 1 {
		t.Fatalf("manifest = %d %v", res.StatusCode, m)
	}
	entry := m["runs"].([]any)[0].(map[string]any)
	if entry["state"] != "done" || entry["complete"] != true || len(entry["events"].([]any)) != len(events) {
		t.Fatalf("entry = %v", entry)
	}
	def := entry["definition"].(map[string]any)
	if def["rev"].(float64) != 1 || def["hash"] == "" || def["spec"].(map[string]any)["name"] != "helper" {
		t.Fatalf("definition = %v", def)
	}
	// Bodies are decoded, not base64: the document reads without a tool.
	last := entry["events"].([]any)[len(events)-1].(map[string]any)
	if body, isObject := last["body"].(map[string]any); !isObject || body["text"] == "" {
		t.Fatalf("the last event's body = %v", last["body"])
	}
	if u := m["usage"].(map[string]any); u["metered"] != true || u["total"].(map[string]any)["model_calls"].(float64) < 1 {
		t.Fatalf("the manifest's usage = %v", u)
	}
	for _, path := range []string{"/events", "/manifest"} {
		if res, _ := f.do(t, nil, "GET", "/api/runs/"+id+path, globex, ""); res.StatusCode != http.StatusNotFound {
			t.Errorf("another tenant reading %s = %d", path, res.StatusCode)
		}
	}
}

func TestNativePDFExportReadsTheExactConfinedVersion(t *testing.T) {
	f := newFixture(t)
	admin := f.machine(t, "acme", everything...)
	reader := f.machine(t, "acme", "humagen:artifacts:read")
	foreign := f.machine(t, "other", "humagen:artifacts:read")
	if res, _ := f.do(t, nil, "POST", "/api/projects", admin, `{"name":"Exports"}`); res.StatusCode != http.StatusCreated {
		t.Fatalf("create project: %d", res.StatusCode)
	}
	publish := func(text string) string {
		bytes, err := nativefiles.Encode("plan.docx", `{"paragraphs":["`+text+`"]}`)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.p.PutFile("acme", "export-thread", "plan.docx", string(bytes)); err != nil {
			t.Fatal(err)
		}
		file, err := f.p.Publish(t.Context(), platform.Publication{Tenant: "acme", Thread: "export-thread", Project: "Exports", Agent: "helper", Path: "plan.docx"})
		if err != nil {
			t.Fatal(err)
		}
		return file.ID
	}
	original := publish("Original decision")
	publish("Revised decision")
	response, body := f.raw(t, "/api/artifacts/"+original+"/pdf", reader)
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/pdf" || !strings.HasPrefix(body, "%PDF-") || !strings.Contains(body, "Original decision") || strings.Contains(body, "Revised decision") {
		t.Fatalf("export = %d %q", response.StatusCode, body)
	}
	if response.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatal("export cache is not private")
	}
	for _, path := range []string{"/api/artifacts/" + original + "/pdf", "/api/artifacts/not-real/pdf"} {
		response, _ := f.raw(t, path, foreign)
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("foreign export = %d", response.StatusCode)
		}
	}
}
