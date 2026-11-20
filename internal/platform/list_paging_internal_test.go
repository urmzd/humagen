package platform

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/threads"
)

// Roots that last changed at the same instant are listed in one order, by
// id, whatever order they were read in, and a reader paging through them is
// shown each once.
func TestRootsThatChangedAtTheSameInstantAreListedAndPagedInOneOrder(t *testing.T) {
	instant := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	roots := func(ids ...string) []RootMessage {
		var out []RootMessage
		for _, id := range ids {
			at := instant
			if id == "s_newer" {
				at = instant.Add(time.Nanosecond)
			}
			// The same instant read in another zone is the same instant.
			if id == "s_b" {
				at = at.In(time.FixedZone("", -5*60*60))
			}
			out = append(out, RootMessage{ID: id, LatestAt: at})
		}
		return out
	}
	want := []string{"s_newer", "s_a", "s_b", "s_c", "s_d"}
	ids := func(page RootMessagePage) []string {
		var out []string
		for _, m := range page.Messages {
			out = append(out, m.ID)
		}
		return out
	}
	for _, read := range [][]string{
		{"s_a", "s_b", "s_c", "s_d", "s_newer"},
		{"s_d", "s_newer", "s_c", "s_b", "s_a"},
		{"s_c", "s_a", "s_d", "s_newer", "s_b"},
	} {
		all, err := pageRoots(roots(read...), 50, "")
		if err != nil || !slices.Equal(ids(all), want) || all.Next != "" {
			t.Fatalf("roots read as %v are listed %v, %v, want %v", read, ids(all), err, want)
		}
		for _, size := range []int{1, 2, 3} {
			var paged []string
			for after, pages := "", 0; ; pages++ {
				page, err := pageRoots(roots(read...), size, after)
				if err != nil || len(page.Messages) == 0 || pages == len(want) {
					t.Fatalf("page %d of %d = %+v, %v", pages, size, page, err)
				}
				paged = append(paged, ids(page)...)
				if after = page.Next; after == "" {
					break
				}
			}
			if !slices.Equal(paged, want) {
				t.Fatalf("roots read as %v and paged by %d = %v, want each once in order %v", read, size, paged, want)
			}
		}
	}
	// The cursor of the last root is the end of the listing, not an error,
	// and so is the cursor of a root that is no longer listed.
	last, err := pageRoots(roots("s_a", "s_b"), 1, "")
	if err != nil || last.Next == "" {
		t.Fatalf("the first of two = %+v, %v", last, err)
	}
	if rest, err := pageRoots(roots("s_b"), 1, last.Next); err != nil || !slices.Equal(ids(rest), []string{"s_b"}) || rest.Next != "" {
		t.Fatalf("after a root that is gone = %+v, %v", rest, err)
	}
	if rest, err := pageRoots(roots("s_a"), 1, encodeRootCursor(RootMessage{ID: "s_a", LatestAt: instant})); err != nil || len(rest.Messages) != 0 || rest.Next != "" {
		t.Fatalf("after the last root = %+v, %v", rest, err)
	}
	if _, err := pageRoots(roots("s_a"), 1, "s_a"); !errors.Is(err, threads.ErrCursor) {
		t.Fatalf("a root's id as a cursor = %v", err)
	}
}

// Sessions are paged by place as roots are: the sessions an alert raises
// first, then newest first, then by id. Sessions that changed at the same
// instant are each shown once, and a session that changes between pages
// repeats nothing already read.
func TestSessionsArePagedByPlaceThroughTiesAndChanges(t *testing.T) {
	instant := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	rows := func() []SessionSummary {
		return []SessionSummary{
			{ID: "s_alert", UpdatedAt: instant.Add(-time.Hour), rank: 3},
			{ID: "s_newer", UpdatedAt: instant.Add(time.Nanosecond)},
			{ID: "s_a", UpdatedAt: instant},
			{ID: "s_b", UpdatedAt: instant.In(time.FixedZone("", -5*60*60))},
			{ID: "s_c", UpdatedAt: instant},
		}
	}
	want := []string{"s_alert", "s_newer", "s_a", "s_b", "s_c"}
	ids := func(page []SessionSummary) []string {
		var out []string
		for _, v := range page {
			out = append(out, v.ID)
		}
		return out
	}
	for _, size := range []int{1, 2, 3, 5, 50} {
		var paged []string
		for after, pages := "", 0; ; pages++ {
			page, next, err := PageSessions(rows(), size, after)
			if err != nil || len(page) == 0 || pages == len(want) {
				t.Fatalf("page %d of %d = %v, %v", pages, size, ids(page), err)
			}
			paged = append(paged, ids(page)...)
			if after = next; after == "" {
				break
			}
		}
		if !slices.Equal(paged, want) {
			t.Fatalf("paged by %d = %v, want each once in order %v", size, paged, want)
		}
	}
	// The session a page ended on changes, and stands first: the page after
	// goes on from where that page ended.
	top, next, err := PageSessions(rows(), 3, "")
	if err != nil || !slices.Equal(ids(top), want[:3]) || next == "" {
		t.Fatalf("the first page of three = %v, %q, %v", ids(top), next, err)
	}
	moved := rows()
	moved[2].UpdatedAt = instant.Add(time.Minute)
	moved = []SessionSummary{moved[0], moved[2], moved[1], moved[3], moved[4]}
	if rest, next, err := PageSessions(moved, 3, next); err != nil || !slices.Equal(ids(rest), []string{"s_b", "s_c"}) || next != "" {
		t.Fatalf("the page after a change = %v, %q, %v, want s_b and s_c", ids(rest), next, err)
	}
	// A session that is gone leaves its cursor good, and the end of the
	// listing is an empty page.
	if rest, _, err := PageSessions(moved[:1], 3, next); err != nil || len(rest) != 0 {
		t.Fatalf("after the last session = %v, %v", ids(rest), err)
	}
	for _, bad := range []string{"s_a", "bm90IGEgY3Vyc29y", "MDpzb29uOnNfYQ"} {
		if _, _, err := PageSessions(rows(), 3, bad); !errors.Is(err, threads.ErrCursor) {
			t.Fatalf("a cursor no page returned (%q) = %v", bad, err)
		}
	}
}
