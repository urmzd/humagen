package artifacts_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/artifacts"
)

// put reserves an id and publishes under it, the way the platform does.
func put(t *testing.T, x *artifacts.Index, tenant string, a artifacts.Artifact) artifacts.Artifact {
	t.Helper()
	a.ID = x.NextID(tenant)
	if a.Key == "" {
		a.Key = "artifacts/" + a.ID
	}
	if a.Kind == "" {
		a.Kind = artifacts.Document
	}
	got, err := x.Publish(tenant, a)
	if err != nil {
		t.Fatalf("publish %+v: %v", a, err)
	}
	return got
}

func ids(list []artifacts.Artifact) string {
	out := make([]string, len(list))
	for i, a := range list {
		out[i] = a.ID
	}
	return strings.Join(out, " ")
}

func TestCleanTitle(t *testing.T) {
	for in, want := range map[string]string{
		"report.md":            "report.md",
		"  report.md  ":        "report.md",
		"../../etc/passwd":     "passwd",
		`C:\Users\ada\q3.xlsx`: "q3.xlsx",
		"a/b/c.csv":            "c.csv",
		".env":                 "env",
		"...hidden":            "hidden",
		"bad\x00na\nme.txt":    "badname.txt",
		`we"ird<name>?.md`:     "weirdname.md",
		"tab\there.md":         "tabhere.md",
	} {
		got, err := artifacts.CleanTitle(in)
		if err != nil || got != want {
			t.Errorf("CleanTitle(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "   ", "/", "..", ".", "\x00\x01", `"<>|`} {
		if got, err := artifacts.CleanTitle(in); !errors.Is(err, artifacts.ErrInvalid) {
			t.Errorf("CleanTitle(%q) = %q, %v; want invalid", in, got, err)
		}
	}
	long, err := artifacts.CleanTitle(strings.Repeat("a", 300) + ".xlsx")
	if err != nil || len(long) != artifacts.MaxTitle || !strings.HasSuffix(long, ".xlsx") {
		t.Fatalf("a long title = %q (%d), %v", long, len(long), err)
	}
	// Cut on a character boundary, never inside one.
	wide, err := artifacts.CleanTitle(strings.Repeat("é", 100) + ".md")
	if err != nil || len(wide) > artifacts.MaxTitle || !utf8.ValidString(wide) || !strings.HasSuffix(wide, ".md") {
		t.Fatalf("a long title of wide characters = %q, %v", wide, err)
	}
	// An extension too long to be one is not kept.
	odd, _ := artifacts.CleanTitle(strings.Repeat("b", 130) + "." + strings.Repeat("x", 20))
	if len(odd) != artifacts.MaxTitle {
		t.Fatalf("a title with a long suffix = %d bytes", len(odd))
	}
}

func TestIDsAreOpaqueAndReservationsAreUnique(t *testing.T) {
	x := artifacts.NewIndex()
	a, b, c := x.NextID("acme"), x.NextID("acme"), x.NextID("globex")
	for _, id := range []string{a, b, c} {
		opaque(t, id)
	}
	if a == b || b == c || a == c {
		t.Fatal("reservations collided")
	}
	got := put(t, x, "acme", artifacts.Artifact{Thread: "t", Title: "a.md"})
	if got.ID == a || got.ID == b {
		t.Fatal("abandoned reservation reused")
	}
}

func TestPublishChecksWhatItRecords(t *testing.T) {
	x := artifacts.NewIndex()
	ok := artifacts.Artifact{ID: x.NextID("acme"), Key: "k", Thread: "t", Title: "a.md", Kind: artifacts.Document}
	for name, mutate := range map[string]func(*artifacts.Artifact){
		"no id":           func(a *artifacts.Artifact) { a.ID = "" },
		"no key":          func(a *artifacts.Artifact) { a.Key = "" },
		"no conversation": func(a *artifacts.Artifact) { a.Thread = "" },
		"no title":        func(a *artifacts.Artifact) { a.Title = "" },
		"unknown kind":    func(a *artifacts.Artifact) { a.Kind = "hologram" },
		"no kind":         func(a *artifacts.Artifact) { a.Kind = "" },
	} {
		a := ok
		mutate(&a)
		if _, err := x.Publish("acme", a); !errors.Is(err, artifacts.ErrInvalid) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}
	for _, k := range artifacts.Kinds() {
		a := ok
		a.ID, a.Kind, a.Title = x.NextID("acme"), k, string(k)+".file"
		if _, err := x.Publish("acme", a); err != nil {
			t.Errorf("kind %s: %v", k, err)
		}
	}
	if _, err := x.Publish("acme", ok); err != nil {
		t.Fatal(err)
	}
	if _, err := x.Publish("acme", ok); !errors.Is(err, artifacts.ErrInvalid) {
		t.Fatalf("publishing one id twice = %v", err)
	}
	// What a caller passes as a version is not what is recorded.
	b := ok
	b.ID, b.Title, b.Version = x.NextID("acme"), "b.md", 41
	if got, _ := x.Publish("acme", b); got.Version != 1 {
		t.Fatalf("version = %d", got.Version)
	}
}

func TestVersionsArePerTitlePerConversation(t *testing.T) {
	x := artifacts.NewIndex()
	v1 := put(t, x, "acme", artifacts.Artifact{Thread: "t1", Title: "plan.md", Size: 1})
	other := put(t, x, "acme", artifacts.Artifact{Thread: "t2", Title: "plan.md", Size: 1})
	v2 := put(t, x, "acme", artifacts.Artifact{Thread: "t1", Title: "plan.md", Size: 2})
	elsewhere := put(t, x, "globex", artifacts.Artifact{Thread: "t1", Title: "plan.md"})
	if v1.Version != 1 || v2.Version != 2 || other.Version != 1 || elsewhere.Version != 1 {
		t.Fatalf("versions = %d %d %d %d", v1.Version, v2.Version, other.Version, elsewhere.Version)
	}
	got, err := x.Versions("acme", v1.ID)
	if err != nil || ids(got) != v2.ID+" "+v1.ID {
		t.Fatalf("versions of v1 = %s, %v", ids(got), err)
	}
	// Deleting the latest leaves the earlier one, and the next is after the highest left.
	if _, err := x.Delete("acme", v2.ID); err != nil {
		t.Fatal(err)
	}
	if v := put(t, x, "acme", artifacts.Artifact{Thread: "t1", Title: "plan.md"}); v.Version != 2 {
		t.Fatalf("after deleting version 2, the next is %d", v.Version)
	}
	// Deleting the first does not renumber the rest.
	if _, err := x.Delete("acme", v1.ID); err != nil {
		t.Fatal(err)
	}
	if v := put(t, x, "acme", artifacts.Artifact{Thread: "t1", Title: "plan.md"}); v.Version != 3 {
		t.Fatalf("after deleting version 1, the next is %d", v.Version)
	}
}

func TestTenantsDoNotSeeEachOther(t *testing.T) {
	x := artifacts.NewIndex()
	a := put(t, x, "acme", artifacts.Artifact{Thread: "t", Title: "a.md", Size: 10, Project: "p", Agent: "lead"})
	if _, err := x.Get("acme", a.ID); err != nil {
		t.Fatal(err)
	}
	// globex has an artifact with the same id, which is not acme's.
	if _, err := x.Publish("globex", artifacts.Artifact{ID: a.ID, Key: "other-tenant-key", Thread: "t", Title: "b.md", Kind: artifacts.Document, Size: 3}); err != nil {
		t.Fatal(err)
	}
	if got, _ := x.Get("globex", a.ID); got.Title != "b.md" {
		t.Fatalf("globex reads %q", got.Title)
	}
	for _, tenant := range []string{"initech", ""} {
		if _, err := x.Get(tenant, a.ID); !errors.Is(err, artifacts.ErrNotFound) {
			t.Errorf("%q get = %v", tenant, err)
		}
		if _, err := x.Versions(tenant, a.ID); !errors.Is(err, artifacts.ErrNotFound) {
			t.Errorf("%q versions = %v", tenant, err)
		}
		if _, err := x.Delete(tenant, a.ID); !errors.Is(err, artifacts.ErrNotFound) {
			t.Errorf("%q delete = %v", tenant, err)
		}
		if page, next, err := x.Page(tenant, artifacts.Query{}); err != nil || len(page) != 0 || next != "" {
			t.Errorf("%q page = %v, %q, %v", tenant, page, next, err)
		}
		if u := x.Usage(tenant); u.Bytes != 0 || u.Count != 0 || len(u.ByProject) != 0 {
			t.Errorf("%q usage = %+v", tenant, u)
		}
	}
	if _, err := x.Get("acme", a.ID); err != nil {
		t.Fatal("reading another tenant changed acme")
	}
	if x.Used("acme", "") != 10 || x.Used("globex", "") != 3 {
		t.Fatalf("used = %d, %d", x.Used("acme", ""), x.Used("globex", ""))
	}
}

func TestDelete(t *testing.T) {
	x := artifacts.NewIndex()
	a := put(t, x, "acme", artifacts.Artifact{Thread: "t", Title: "a.md", Size: 5})
	got, err := x.Delete("acme", a.ID)
	if err != nil || got.Key != a.Key {
		t.Fatalf("delete = %+v, %v", got, err)
	}
	if _, err := x.Get("acme", a.ID); !errors.Is(err, artifacts.ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	if _, err := x.Delete("acme", a.ID); !errors.Is(err, artifacts.ErrNotFound) {
		t.Fatalf("a second delete = %v", err)
	}
	if u := x.Usage("acme"); u.Count != 0 || u.Bytes != 0 {
		t.Fatalf("usage after delete = %+v", u)
	}
}

func TestPageListsTheLatestOfEachFile(t *testing.T) {
	x := artifacts.NewIndex()
	plan1 := put(t, x, "acme", artifacts.Artifact{Thread: "t1", Title: "plan.md", Agent: "lead", Project: "Launch"})
	chart := put(t, x, "acme", artifacts.Artifact{Thread: "t1", Title: "chart.png", Kind: artifacts.Image, Agent: "lead", Run: "r1"})
	plan2 := put(t, x, "acme", artifacts.Artifact{Thread: "t1", Title: "plan.md", Agent: "lead", Project: "Launch"})
	notes := put(t, x, "acme", artifacts.Artifact{Thread: "t2", Title: "Notes.TXT", Agent: "helper"})

	page, next, err := x.Page("acme", artifacts.Query{})
	if err != nil || next != "" || ids(page) != notes.ID+" "+plan2.ID+" "+chart.ID {
		t.Fatalf("page = %s, %q, %v", ids(page), next, err)
	}
	for name, c := range map[string]struct {
		q    artifacts.Query
		want string
	}{
		"project":                {artifacts.Query{Project: "Launch"}, plan2.ID},
		"agent":                  {artifacts.Query{Agent: "lead"}, plan2.ID + " " + chart.ID},
		"run":                    {artifacts.Query{Run: "r1"}, chart.ID},
		"thread":                 {artifacts.Query{Thread: "t2"}, notes.ID},
		"kind":                   {artifacts.Query{Kind: artifacts.Image}, chart.ID},
		"title in any case":      {artifacts.Query{Title: "notes"}, notes.ID},
		"title part":             {artifacts.Query{Title: "AN."}, plan2.ID},
		"nothing matches":        {artifacts.Query{Agent: "nobody"}, ""},
		"filters combine":        {artifacts.Query{Agent: "lead", Kind: artifacts.Document}, plan2.ID},
		"an old version is gone": {artifacts.Query{Project: "Launch", Title: "plan"}, plan2.ID},
	} {
		got, _, err := x.Page("acme", c.q)
		if err != nil || ids(got) != c.want {
			t.Errorf("%s: %s, %v; want %s", name, ids(got), err, c.want)
		}
	}
	// Deleting the latest version brings the one before it back to the list.
	if _, err := x.Delete("acme", plan2.ID); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := x.Page("acme", artifacts.Query{Project: "Launch"}); ids(got) != plan1.ID {
		t.Fatalf("after deleting the latest = %s", ids(got))
	}
}

func TestPagingIsStable(t *testing.T) {
	x := artifacts.NewIndex()
	issued := []artifacts.Artifact{}
	for i := range 7 {
		issued = append(issued, put(t, x, "acme", artifacts.Artifact{Thread: "t", Title: fmt.Sprintf("f%d.md", i)}))
	}
	first, next, err := x.Page("acme", artifacts.Query{Limit: 3})
	if err != nil || ids(first) != issued[6].ID+" "+issued[5].ID+" "+issued[4].ID || next == "" {
		t.Fatalf("first = %s, %q, %v", ids(first), next, err)
	}
	// Something published and something deleted between pages moves nothing.
	put(t, x, "acme", artifacts.Artifact{Thread: "t", Title: "new.md"})
	if _, err := x.Delete("acme", issued[3].ID); err != nil {
		t.Fatal(err)
	}
	second, next, err := x.Page("acme", artifacts.Query{Limit: 3, Before: next})
	if err != nil || ids(second) != issued[2].ID+" "+issued[1].ID+" "+issued[0].ID {
		t.Fatalf("second = %s, %v", ids(second), err)
	}
	if next != "" {
		third, _, _ := x.Page("acme", artifacts.Query{Limit: 3, Before: next})
		if len(third) != 0 {
			t.Fatalf("a third page = %s", ids(third))
		}
	}
	// A cursor names an id, so it carries to a narrowed list too.
	if got, _, _ := x.Page("acme", artifacts.Query{Title: "f1", Before: next}); next != "" && len(got) != 0 {
		t.Fatalf("narrowed after the end = %s", ids(got))
	}
	for _, bad := range []string{"not base64!", "YXJ0LXg", "Zm9v"} {
		if _, _, err := x.Page("acme", artifacts.Query{Before: bad}); !errors.Is(err, artifacts.ErrCursor) {
			t.Errorf("cursor %q = %v", bad, err)
		}
	}
}

func TestPageIsBounded(t *testing.T) {
	x := artifacts.NewIndex()
	for i := range artifacts.MaxPage + 10 {
		put(t, x, "acme", artifacts.Artifact{Thread: "t", Title: fmt.Sprintf("f%d.md", i)})
	}
	if page, next, _ := x.Page("acme", artifacts.Query{}); len(page) != artifacts.DefaultPage || next == "" {
		t.Fatalf("default page = %d, %q", len(page), next)
	}
	if page, _, _ := x.Page("acme", artifacts.Query{Limit: 10_000}); len(page) != artifacts.MaxPage {
		t.Fatalf("a page asked for 10000 = %d", len(page))
	}
	if page, _, _ := x.Page("acme", artifacts.Query{Limit: -1}); len(page) != artifacts.DefaultPage {
		t.Fatalf("a negative limit = %d", len(page))
	}
}

func TestUsageCountsEveryVersion(t *testing.T) {
	x := artifacts.NewIndex()
	put(t, x, "acme", artifacts.Artifact{Thread: "t", Title: "a.md", Size: 100, Project: "Launch", Agent: "lead"})
	put(t, x, "acme", artifacts.Artifact{Thread: "t", Title: "a.md", Size: 150, Project: "Launch", Agent: "lead"})
	put(t, x, "acme", artifacts.Artifact{Thread: "t", Title: "b.md", Size: 250, Project: "Ops", Agent: "helper"})
	put(t, x, "acme", artifacts.Artifact{Thread: "t", Title: "c.md", Size: 7})
	u := x.Usage("acme")
	if u.Bytes != 507 || u.Count != 4 {
		t.Fatalf("usage = %+v", u)
	}
	// Largest first, ties by name, and a file with no project or agent is in no share.
	if fmt.Sprint(u.ByProject) != "[{Launch 250 2} {Ops 250 1}]" || fmt.Sprint(u.ByAgent) != "[{helper 250 1} {lead 250 2}]" {
		t.Fatalf("shares = %v, %v", u.ByProject, u.ByAgent)
	}
	if x.Used("acme", "") != 507 || x.Used("acme", "Launch") != 250 || x.Used("acme", "nowhere") != 0 {
		t.Fatalf("used = %d, %d", x.Used("acme", ""), x.Used("acme", "Launch"))
	}
}

// Readers and writers in many tenants at once, under the race detector.
// Reading a tenant nobody has written to must not add it.
func TestConcurrentUse(t *testing.T) {
	x := artifacts.NewIndex()
	var wg sync.WaitGroup
	for i := range 8 {
		tenant := fmt.Sprint("t", i%3)
		wg.Go(func() {
			for j := range 20 {
				id := x.NextID(tenant)
				if _, err := x.Publish(tenant, artifacts.Artifact{ID: id, Key: id, Thread: "c", Title: fmt.Sprint(j%4, ".md"), Kind: artifacts.Data, Size: 1}); err != nil {
					t.Error(err)
				}
			}
		})
		wg.Go(func() {
			reader := fmt.Sprint("reader", i)
			for range 20 {
				_, _ = x.Get(reader, "art-1")
				_, _, _ = x.Page(reader, artifacts.Query{})
				_ = x.Usage(reader)
				_ = x.Used(tenant, "")
				_, _, _ = x.Page(tenant, artifacts.Query{Limit: 2})
			}
		})
	}
	wg.Wait()
	total := 0
	for i := range 3 {
		total += x.Usage(fmt.Sprint("t", i)).Count
		page, _, _ := x.Page(fmt.Sprint("t", i), artifacts.Query{})
		if len(page) != 4 {
			t.Errorf("tenant %d lists %d files, want the latest of 4 titles", i, len(page))
		}
	}
	if total != 160 {
		t.Fatalf("%d of 160 publishes landed", total)
	}
}
