package github_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/integrations/github"
	transport "github.com/urmzd/saige/flow/connectors/github"
)

// The repositories of the reach table, as a client might see them: the run's
// own (acme/web), one named by read_repositories (acme/listed), one the App
// installation covers (acme/docs), one kept to its own runs that is also
// named and covered (acme/kept), one the installation does not cover
// (acme/secret), and an installed one of an owner the client does not serve
// (partner/tools).
var reachTargets = []string{"acme/web", "acme/listed", "acme/docs", "acme/kept", "acme/secret", "partner/tools"}

func reachInstalled(_ context.Context, owner, repo string) bool {
	switch strings.ToLower(owner + "/" + repo) {
	case "acme/docs", "acme/kept", "partner/tools":
		return true
	}
	return false
}

func reachKept(_ context.Context, owner, repo string) bool {
	return strings.EqualFold(owner+"/"+repo, "acme/kept")
}

func reachConfig(api github.API) github.Config {
	return github.Config{
		API: api, Owners: []string{"acme"}, ReadRepositories: []string{"acme/listed", "acme/kept"},
		MayRead: reachInstalled, ReadRefused: reachKept,
		FixPullRequests: true, AuthorizeFixPull: func(context.Context, transport.Ref) error { return nil },
	}
}

// reachFake is a fake GitHub holding issue 7, files, a default branch and
// the repository itself for every repository of the table.
func reachFake(t *testing.T) *github.Fake {
	t.Helper()
	fake := github.NewFake()
	for _, full := range reachTargets {
		owner, repo, _ := strings.Cut(full, "/")
		ref := github.Ref{Owner: owner, Repo: repo, Number: 7}
		var evidence github.Context
		evidence.Subject.Title = full + " issue"
		evidence.Subject.Head.SHA = sourceSHAForGrant
		if err := fake.SeedContext(ref, evidence); err != nil {
			t.Fatal(err)
		}
		if err := fake.SeedFiles(ref, map[string]string{"README.md": full + " readme\n"}); err != nil {
			t.Fatal(err)
		}
		if err := fake.SeedRepository(owner, repo, github.RepositoryContext{Description: full + " repository", DefaultBranch: "main"}); err != nil {
			t.Fatal(err)
		}
		fake.SeedDefaultBranch(ref, transport.RepositoryBase{Name: "main", SHA: sourceSHAForGrant})
	}
	return fake
}

// redirectingGitHub is GitHub's REST API for the table's repositories over
// HTTP: issue 7 of every repository, and issue 100+i of every repository
// answering with a redirect to issue 7 of reachTargets[i], as GitHub answers
// for an issue transferred to another repository. Requests for
// api.github.com reach it through the returned client.
func redirectingGitHub(t *testing.T) *http.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/repos/"), "/")
		if len(parts) < 4 || parts[2] != "issues" {
			http.NotFound(w, r)
			return
		}
		var n int
		if _, err := fmt.Sscan(parts[3], &n); err != nil {
			http.NotFound(w, r)
			return
		}
		if i := n - 100; i >= 0 && i < len(reachTargets) {
			rest := strings.Join(append([]string{"7"}, parts[4:]...), "/")
			target := "https://api.github.com/repos/" + reachTargets[i] + "/issues/" + rest
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
			return
		}
		if n != 7 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if len(parts) == 5 && parts[4] == "comments" {
			_, _ = w.Write([]byte(`[{"id":1,"body":"` + parts[0] + "/" + parts[1] + ` comment","user":{"login":"ana"}}]`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "title": parts[0] + "/" + parts[1] + " issue", "state": "open"})
	}))
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)
	return &http.Client{Transport: rewriteHost{to: base}}
}

type rewriteHost struct{ to *url.URL }

func (h rewriteHost) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "api.github.com" {
		return nil, fmt.Errorf("request for %s", r.URL.Host)
	}
	out := r.Clone(r.Context())
	out.URL.Scheme, out.URL.Host, out.Host = h.to.Scheme, h.to.Host, h.to.Host
	return http.DefaultTransport.RoundTrip(out)
}

// reachOnMain says the table is checked against the main branch's column.
// It is false here; the comparison is made by running this file on main
// with it set to true.
var reachOnMain = false

// reachRow is one read: from a run raised on a pull request of acme/web or
// from a Slack run, of a target named directly, or reached by a redirect.
// main and branch say whether the read was answered, before and after the
// connector was given the read hooks.
type reachRow struct {
	run, how, target string
	main, branch     bool
}

func (r reachRow) want() bool {
	if reachOnMain {
		return r.main
	}
	return r.branch
}

// Who may read what does not widen: every read of the table is answered
// after the change only where it was answered before, or where it reaches a
// repository the same run reads directly in both (a redirect from the run's
// own repository to one the installation covers, and github_fix_base of
// one).
func TestReadReachIsUnchangedOrNarrower(t *testing.T) {
	const pr, slack = "github://acme/web/pull/3", "slack://T01/C01"
	direct := []reachRow{
		{pr, "direct", "acme/web", true, true},
		{pr, "direct", "acme/listed", true, true},
		{pr, "direct", "acme/docs", true, true},
		{pr, "direct", "acme/kept", false, false},
		{pr, "direct", "acme/secret", false, false},
		{pr, "direct", "partner/tools", false, false},
		{slack, "direct", "acme/listed", true, true},
		{slack, "direct", "acme/docs", true, true},
		{slack, "direct", "acme/kept", false, false},
		{slack, "direct", "acme/secret", false, false},
		{slack, "direct", "partner/tools", false, false},
	}
	redirects := []reachRow{
		// From the run's own repository: acme/web issue 100+i moved to target.
		{pr, "redirect from own", "acme/web", true, true},
		{pr, "redirect from own", "acme/listed", true, true},
		{pr, "redirect from own", "acme/docs", false, true},
		{pr, "redirect from own", "acme/kept", true, false},
		{pr, "redirect from own", "acme/secret", false, false},
		{pr, "redirect from own", "partner/tools", false, false},
		// From a repository read across sources: no redirect is followed.
		{pr, "redirect from installed", "acme/listed", true, false},
		{pr, "redirect from installed", "acme/kept", true, false},
		{pr, "redirect from installed", "acme/secret", false, false},
		{slack, "redirect from listed", "acme/docs", false, false},
		{slack, "redirect from listed", "acme/kept", false, false},
		{slack, "redirect from installed", "acme/listed", true, false},
		{slack, "redirect from installed", "acme/kept", true, false},
		{slack, "redirect from installed", "acme/secret", false, false},
	}

	fake, err := github.New(reachConfig(reachFake(t)))
	if err != nil {
		t.Fatal(err)
	}
	reads := map[string]func(target string) string{
		"github_read_context":            func(target string) string { return `{"address":"github://` + target + `/issues/7"}` },
		"github_read_context repository": func(target string) string { return `{"address":"` + target + `"}` },
		"github_read_file": func(target string) string {
			return `{"address":"github://` + target + `/issues/7","path":"README.md","ref":"` + sourceSHAForGrant + `"}`
		},
		"github_fix_base": func(target string) string { return `{"address":"github://` + target + `/issues/7"}` },
		"github_workflow_status": func(target string) string {
			return `{"address":"github://` + target + `/issues/7","ref":"` + sourceSHAForGrant + `"}`
		},
	}
	for name, args := range reads {
		for _, row := range direct {
			row := fixBaseBefore(name, row)
			t.Run(name+"/"+row.run+"/"+row.target, func(t *testing.T) {
				summary, err := action(t, fake, strings.TrimSuffix(name, " repository"))(row.run, args(row.target))
				if (err == nil) != row.want() {
					t.Fatalf("answered %v, want %v: %q, %v", err == nil, row.want(), summary, err)
				}
			})
		}
	}

	httpAPI, err := github.New(reachConfig(github.NewHTTPAPI("token", redirectingGitHub(t))))
	if err != nil {
		t.Fatal(err)
	}
	read := action(t, httpAPI, "github_read_context")
	for _, row := range redirects {
		t.Run("redirect/"+row.run+"/"+row.how+"/"+row.target, func(t *testing.T) {
			from := "acme/web"
			switch row.how {
			case "redirect from installed":
				from = "acme/docs"
			case "redirect from listed":
				from = "acme/listed"
			}
			number := 100
			for i, r := range reachTargets {
				if r == row.target {
					number += i
				}
			}
			summary, err := read(row.run, fmt.Sprintf(`{"address":"github://%s/issues/%d"}`, from, number))
			if (err == nil) != row.want() {
				t.Fatalf("answered %v, want %v: %q, %v", err == nil, row.want(), summary, err)
			}
			if err == nil && !strings.Contains(summary, row.target+" issue") {
				t.Fatalf("answered by the wrong repository: %s", summary)
			}
		})
	}
	// A read answered now that was not before reaches a repository the same
	// run reads directly with the other read tools in both: a redirect from
	// the run's own repository, or github_fix_base, which the hooks grant
	// with the other reads.
	rows := append([]reachRow{}, redirects...)
	for _, row := range direct {
		rows = append(rows, fixBaseBefore("github_fix_base", row))
	}
	for _, row := range rows {
		if row.branch && !row.main && !directly(direct, row.run, row.target) {
			t.Errorf("%s %s %s is answered after the change and was not before", row.run, row.how, row.target)
		}
	}
}

// fixBaseBefore is row for read as it was before the change, when
// github_fix_base was granted by ReadRepositories alone: it read a named
// repository kept to its own runs, and not one the installation covers.
func fixBaseBefore(read string, row reachRow) reachRow {
	if read != "github_fix_base" || row.how != "direct" {
		return row
	}
	switch row.target {
	case "acme/docs":
		row.main = false
	case "acme/kept":
		row.main = true
	}
	row.how = "direct github_fix_base"
	return row
}

func directly(rows []reachRow, run, target string) bool {
	for _, r := range rows {
		if r.run == run && r.target == target && r.branch && r.main {
			return true
		}
	}
	return false
}
