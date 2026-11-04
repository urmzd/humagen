package platform_test

import (
	"errors"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/integrations/github"
	"testing"
)

func TestGitHubPullEvidenceAndTenantBounds(t *testing.T) {
	p, fake := sourceStage(t)
	ref := github.Ref{Owner: "acme", Repo: "api", Number: 42}
	var c github.Context
	c.Subject.Number = 42
	c.Subject.Title = "Fix leases"
	c.Subject.Head.SHA = sourceSHA
	c.Subject.PullRequest = &struct {
		URL string `json:"url"`
	}{URL: "https://api.github.com/repos/acme/api/pulls/42"}
	for range 40 {
		c.Files = append(c.Files, github.ContextFile{Filename: "a.go", Patch: "@@ -1 +1 @@\n-old\n+new"})
		c.Comments = append(c.Comments, github.ContextComment{Body: "evidence"})
	}
	if err := fake.SeedContext(ref, c); err != nil {
		t.Fatal(err)
	}
	if err := fake.SeedWorkflow(ref, github.WorkflowRun{ID: 1, HeadSHA: sourceSHA, Attempt: 1, Status: "completed", Conclusion: "success"}); err != nil {
		t.Fatal(err)
	}
	out, err := p.GitHubPull(t.Context(), tenant, "acme/api", 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Files) > 30 || len(out.Comments) > 30 || out.Subject.Head.SHA != sourceSHA || len(out.Workflows) != 1 || out.Workflows[0].Conclusion != "success" {
		t.Fatalf("evidence: %+v", out)
	}
	for _, tc := range []struct {
		tenant, repo string
		number, page int
		want         error
	}{
		{"other", "acme/api", 42, 1, platform.ErrSourceNotFound},
		{tenant, "stranger/secret", 42, 1, platform.ErrSourceNotFound},
		{tenant, "acme/api", 0, 1, platform.ErrSourceInvalid},
		{tenant, "acme/api", 42, 11, platform.ErrSourceInvalid},
		{tenant, "acme/api", 42, 0, platform.ErrSourceInvalid},
		{tenant, "acme/api/extra", 42, 1, platform.ErrSourceInvalid},
	} {
		_, err := p.GitHubPullPage(t.Context(), tc.tenant, tc.repo, tc.number, tc.page)
		if !errors.Is(err, tc.want) {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
}
