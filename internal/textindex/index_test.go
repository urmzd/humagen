package textindex

import (
	"strings"
	"testing"
)

func TestSearchUpdatesWholeWordsAndAppliesBounds(t *testing.T) {
	var x Index
	x.Put("a", "Café launch plan")
	x.Put("b", "café plan with release")
	if got := x.Search("CAFÉ plan", 1, nil); len(got) != 1 || got[0] != "a" {
		t.Fatal(got)
	}
	x.Put("a", "replacement")
	if got := x.Search("café plan", 10, nil); len(got) != 1 || got[0] != "b" {
		t.Fatal(got)
	}
	if got := x.Search("plan", 10, func(id string) bool { return id == "a" }); len(got) != 0 {
		t.Fatal(got)
	}
	x.Delete("b")
	if got := x.Search("plan", 10, nil); len(got) != 0 {
		t.Fatal(got)
	}
	if got := x.Search("?!", 10, nil); len(got) != 0 {
		t.Fatal(got)
	}
	excerpt := Snippet(strings.Repeat("prefix ", 80)+"needle evidence", "needle")
	if !strings.Contains(excerpt, "needle") || len([]rune(excerpt)) > 242 {
		t.Fatal(excerpt)
	}
}
