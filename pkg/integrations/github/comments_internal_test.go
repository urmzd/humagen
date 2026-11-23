package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
)

// commentsServer serves acme/repo#7's n comments, 30 to a page, oldest first,
// with GitHub's Link header, and counts the requests.
func commentsServer(t *testing.T, n int) (*HTTPAPI, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/repo/issues/7/comments" || r.URL.Query().Get("per_page") != "30" || r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		mu.Lock()
		asked = append(asked, r.URL.Query().Get("page"))
		mu.Unlock()
		last := max(1, (n+29)/30)
		if last > 1 {
			// GitHub's own host, which must never be followed.
			w.Header().Set("Link", fmt.Sprintf(`<https://evil.example/repositories/1/issues/7/comments?per_page=30&page=2>; rel="next", <https://evil.example/repositories/1/issues/7/comments?per_page=30&page=%d>; rel="last"`, last))
		}
		var out []map[string]any
		for i := (page-1)*30 + 1; i <= min(n, page*30); i++ {
			out = append(out, map[string]any{"id": i, "body": fmt.Sprintf("note-%03d", i), "user": map[string]any{"login": "ana"}})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	api := &HTTPAPI{base: srv.URL, token: "tok", http: srv.Client()}
	return api, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), asked...)
	}
}

// The latest comments are read in at most three requests, whatever the
// number of comments: page 1 for the Link header, the last page, and the one
// before it only when the last is short. Exactly 300 comments is ten full
// pages, read as page 1 and page 10.
func TestTheLatestCommentsAreReadThroughTheLastPage(t *testing.T) {
	for _, c := range []struct {
		n         int
		pages     string
		first     int
		lastShown int
	}{
		{0, "[1]", 0, 0},
		{3, "[1]", 1, 3},
		{30, "[1]", 1, 30},
		{33, "[1 2]", 1, 33},
		{62, "[1 3 2]", 31, 62},
		{90, "[1 3]", 61, 90},
		{300, "[1 10]", 271, 300},
		{301, "[1 11 10]", 271, 301},
	} {
		api, asked := commentsServer(t, c.n)
		got, err := api.LatestComments(context.Background(), Ref{Owner: "acme", Repo: "repo", Number: 7}, 5, func(ContextComment) bool { return true })
		if err != nil {
			t.Fatalf("%d comments: %v", c.n, err)
		}
		if fmt.Sprint(asked()) != c.pages {
			t.Errorf("%d comments read pages %v, want %s", c.n, asked(), c.pages)
		}
		if c.n == 0 {
			if len(got) != 0 {
				t.Errorf("no comments read %d", len(got))
			}
			continue
		}
		if got[0].ID != int64(c.first) || got[len(got)-1].ID != int64(c.lastShown) {
			t.Errorf("%d comments gave %d..%d, want %d..%d", c.n, got[0].ID, got[len(got)-1].ID, c.first, c.lastShown)
		}
	}
}

// A last page with enough comments kept needs no page before it.
func TestAShortLastPageOfPeopleReadsThePageBefore(t *testing.T) {
	api, asked := commentsServer(t, 64)
	if _, err := api.LatestComments(context.Background(), Ref{Owner: "acme", Repo: "repo", Number: 7}, 3, func(ContextComment) bool { return true }); err != nil || fmt.Sprint(asked()) != "[1 3]" {
		t.Fatalf("pages %v, %v", asked(), err)
	}
	api, asked = commentsServer(t, 64)
	if _, err := api.LatestComments(context.Background(), Ref{Owner: "acme", Repo: "repo", Number: 7}, 3, func(c ContextComment) bool { return c.ID < 60 }); err != nil || fmt.Sprint(asked()) != "[1 3 2]" {
		t.Fatalf("pages %v, %v", asked(), err)
	}
}

func TestLastPageReadsOnlyAPageNumber(t *testing.T) {
	for link, want := range map[string]int{
		`<https://api.github.com/x?page=2>; rel="next", <https://api.github.com/x?page=9>; rel="last"`: 9,
		`<https://api.github.com/x?page=2>; rel="next"`:                                                0,
		`<https://api.github.com/x?page=99999999>; rel="last"`:                                         0,
		`<https://api.github.com/x?page=-1>; rel="last"`:                                               0,
		``: 0,
	} {
		if got := lastPage(link); got != want {
			t.Errorf("lastPage(%q) = %d, want %d", link, got, want)
		}
	}
}

// Comments deleted between page 1 and the last page leave the pages apart:
// an empty last page, or a short page before it, is ErrCommentsChanged, never
// a read of no comments or of too few.
func TestCommentsDeletedWhileReadAreAChange(t *testing.T) {
	for name, sizes := range map[string]map[int]int{
		"an empty last page":        {1: 30, 4: 0},
		"a short page before it":    {1: 30, 4: 2, 3: 10},
		"a last page gone entirely": {1: 30},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			// Page 1 was read when there were four pages.
			w.Header().Set("Link", `<https://api.github.com/x?per_page=30&page=4>; rel="last"`)
			out := []map[string]any{}
			for i := 0; i < sizes[page]; i++ {
				out = append(out, map[string]any{"id": page*100 + i, "body": "x", "user": map[string]any{"login": "ana"}})
			}
			_ = json.NewEncoder(w).Encode(out)
		}))
		api := &HTTPAPI{base: srv.URL, http: srv.Client()}
		got, err := api.LatestComments(context.Background(), Ref{Owner: "acme", Repo: "repo", Number: 7}, 5, func(ContextComment) bool { return true })
		srv.Close()
		if !errors.Is(err, ErrCommentsChanged) || got != nil {
			t.Errorf("%s: %d comments, %v; want ErrCommentsChanged", name, len(got), err)
		}
	}
}
