package github_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/saige/flow/address"
)

// installedOnly grants what an installation covering acme/docs would.
func installedOnly(_ context.Context, owner, repo string) bool {
	return strings.EqualFold(owner+"/"+repo, "acme/docs")
}

func action(t *testing.T, c *github.Connector, name string) func(source, args string) (string, error) {
	t.Helper()
	for _, a := range c.Actions() {
		if a.Name == name {
			return func(source, args string) (string, error) {
				r, err := a.Invoke(context.Background(), address.MustParse(source), json.RawMessage(args))
				return r.Summary, err
			}
		}
	}
	t.Fatalf("no action %s", name)
	return nil
}

func TestCrossSourceReadsReachARepositoryTheInstallationCovers(t *testing.T) {
	fake := github.NewFake()
	for _, ref := range []github.Ref{{Owner: "acme", Repo: "docs", Number: 7}, {Owner: "acme", Repo: "secret", Number: 7}} {
		var evidence github.Context
		evidence.Subject.Title = ref.Repo + " issue"
		evidence.Subject.Head.SHA = sourceSHAForGrant
		if err := fake.SeedContext(ref, evidence); err != nil {
			t.Fatal(err)
		}
		if err := fake.SeedFiles(ref, map[string]string{"README.md": "hello\n"}); err != nil {
			t.Fatal(err)
		}
	}
	c, err := github.New(github.Config{API: fake, Owners: []string{"acme"}, MayRead: installedOnly})
	if err != nil {
		t.Fatal(err)
	}
	read := action(t, c, "github_read_context")
	for _, tc := range []struct {
		name, source, reference string
		want                    bool
	}{
		{"Slack, an installed repository", "slack://T01/C01", "https://github.com/acme/docs/issues/7", true},
		{"Slack, a github:// address", "slack://T01/C01", "github://acme/docs/issues/7", true},
		{"another repository's run", "github://acme/web/pull/3", "github://acme/docs/issues/7", true},
		{"Slack, a repository it does not cover", "slack://T01/C01", "github://acme/secret/issues/7", false},
		{"a lookalike", "slack://T01/C01", "github://acme/docs-other/issues/7", false},
		{"another host", "slack://T01/C01", "https://evil.test/acme/docs/issues/7", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			summary, err := read(tc.source, `{"address":"`+tc.reference+`"}`)
			if (err == nil) != tc.want {
				t.Fatalf("summary %q error %v", summary, err)
			}
			if tc.want && !strings.Contains(summary, "docs issue") {
				t.Fatalf("read the wrong subject: %s", summary)
			}
		})
	}
	file := action(t, c, "github_read_file")
	if summary, err := file("slack://T01/C01", `{"address":"github://acme/docs/issues/7","path":"README.md","ref":"`+sourceSHAForGrant+`"}`); err != nil || !strings.Contains(summary, "hello") {
		t.Fatalf("file of an installed repository: %q, %v", summary, err)
	}
	if _, err := file("slack://T01/C01", `{"address":"github://acme/secret/issues/7","path":"README.md","ref":"`+sourceSHAForGrant+`"}`); err == nil {
		t.Fatal("read a file of a repository the installation does not cover")
	}
}

func TestAnInstallationGrantNeverAuthorizesWrites(t *testing.T) {
	c, err := github.New(github.Config{API: github.NewFake(), Owners: []string{"acme"}, MayRead: installedOnly})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"github_comment", "github_add_labels", "github_request_review"} {
		if _, err := action(t, c, name)("slack://T01/C01", `{"body":"write","labels":["x"],"reviewers":["a"],"address":"github://acme/docs/issues/7"}`); err == nil {
			t.Fatalf("%s wrote to an installed repository from another surface", name)
		}
	}
}

// The transport checks the owners a client serves before any grant, so an
// installed repository of another owner stays refused to the read tools,
// though the console reads it (docs/reference/github.md).
func TestAnInstalledRepositoryOutsideTheOwnersIsRefusedToTheReadTools(t *testing.T) {
	fake := github.NewFake()
	ref := github.Ref{Owner: "partner", Repo: "tools", Number: 7}
	if err := fake.SeedContext(ref, github.Context{Subject: github.ContextSubject{Title: "tools issue"}}); err != nil {
		t.Fatal(err)
	}
	everything := func(context.Context, string, string) bool { return true }
	c, err := github.New(github.Config{API: fake, Owners: []string{"acme"}, MayRead: everything})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := action(t, c, "github_read_context")("slack://T01/C01", `{"address":"github://partner/tools/issues/7"}`)
	if err == nil || !strings.Contains(err.Error(), "does not serve") {
		t.Fatalf("summary %q error %v, want the owners refusal", summary, err)
	}
}

const sourceSHAForGrant = "316c56f81540c3eaca02184173df322a48aebdd9"
