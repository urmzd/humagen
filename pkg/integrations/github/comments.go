package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// CommentsAPI reads an issue's or pull request's latest conversation
// comments without reading the rest of its context again.
type CommentsAPI interface {
	// LatestComments is the last page of the issue's comments, oldest
	// first, and the page before it when the last holds fewer than want
	// comments that keep reports true for. It makes at most three requests.
	LatestComments(ctx context.Context, ref Ref, want int, keep func(ContextComment) bool) ([]ContextComment, error)
}

// commentsPage is how many comments one page holds, as ReadContext asks.
const commentsPage = 30

// maxCommentsBody bounds one page's answer.
const maxCommentsBody = 4 << 20

// maxCommentsPages bounds the last page a Link header may name.
const maxCommentsPages = 10000

var commentsRepo = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100}$`)

// LatestComments reads GET /repos/{owner}/{repo}/issues/{n}/comments, which
// GitHub lists oldest first: page 1, then the page its Link header names
// rel="last", then the one before that when the last page is short of want
// kept comments. The Link header gives only a page number; the request
// is always built here, never taken from the header.
func (a *HTTPAPI) LatestComments(ctx context.Context, ref Ref, want int, keep func(ContextComment) bool) ([]ContextComment, error) {
	if !commentsRepo.MatchString(ref.Owner+"/"+ref.Repo) || ref.Repo == "." || ref.Repo == ".." || ref.Number <= 0 {
		return nil, fmt.Errorf("github comments: invalid reference %s", ref)
	}
	first, last, err := a.commentsPage(ctx, ref, 1)
	if err != nil || last <= 1 {
		return first, err
	}
	tail, _, err := a.commentsPage(ctx, ref, last)
	if err != nil {
		return nil, err
	}
	kept := 0
	for _, c := range tail {
		if keep == nil || keep(c) {
			kept++
		}
	}
	if kept >= want || last == 1 {
		return tail, nil
	}
	before := first
	if last-1 > 1 {
		if before, _, err = a.commentsPage(ctx, ref, last-1); err != nil {
			return nil, err
		}
	}
	return append(before, tail...), nil
}

// commentsPage is one page of an issue's comments and the last page its
// Link header names, 0 when it names none.
func (a *HTTPAPI) commentsPage(ctx context.Context, ref Ref, page int) ([]ContextComment, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments?per_page=%d&page=%d", a.base, ref.Owner, ref.Repo, ref.Number, commentsPage, page), nil)
	if err != nil {
		return nil, 0, err
	}
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("github comments: status %d", resp.StatusCode)
	}
	var out []ContextComment
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxCommentsBody)).Decode(&out); err != nil {
		return nil, 0, fmt.Errorf("github comments: %w", err)
	}
	return out[:min(len(out), commentsPage)], lastPage(resp.Header.Get("Link")), nil
}

// lastPage is the page number a Link header names rel="last", 0 when it
// names none or one out of bounds.
func lastPage(link string) int {
	for _, part := range strings.Split(link, ",") {
		target, params, ok := strings.Cut(strings.TrimSpace(part), ";")
		if !ok || !strings.Contains(params, `rel="last"`) {
			continue
		}
		u, err := url.Parse(strings.Trim(strings.TrimSpace(target), "<>"))
		if err != nil {
			return 0
		}
		n, err := strconv.Atoi(u.Query().Get("page"))
		if err != nil || n < 1 || n > maxCommentsPages {
			return 0
		}
		return n
	}
	return 0
}
