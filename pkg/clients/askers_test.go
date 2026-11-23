package clients

import (
	"encoding/json"
	"testing"
)

func asking(kind, action, login, association string) []byte {
	who := map[string]any{"user": map[string]string{"login": login}, "author_association": association}
	body := map[string]any{"action": action, "sender": map[string]string{"login": login}}
	switch kind {
	case "issue_comment", "pull_request_review_comment":
		body["comment"] = who
	case "pull_request_review":
		body["review"] = who
	case "issues":
		body["issue"] = who
	case "pull_request":
		body["pull_request"] = who
	}
	b, _ := json.Marshal(body)
	return b
}

// On a public repository anyone can comment: only people with write access,
// or people the client maps, ask, and named askers narrow that further.
func TestOnlyPeopleWhoMayWriteAsk(t *testing.T) {
	gh := githubOf("gh", "acme")
	gh.GitHub = &GitHubOptions{People: map[string]string{"guest": "ana"}}
	for _, tc := range []struct {
		name, kind, action, login, association string
		want                                   bool
	}{
		{"an owner's comment", "issue_comment", "created", "ada", "OWNER", true},
		{"a member's review", "pull_request_review", "submitted", "ada", "MEMBER", true},
		{"a collaborator's diff comment", "pull_request_review_comment", "created", "ada", "COLLABORATOR", true},
		{"a stranger's comment", "issue_comment", "created", "mallory", "NONE", false},
		{"a contributor's comment", "issue_comment", "created", "mallory", "CONTRIBUTOR", false},
		{"a stranger's fork pull request", "pull_request", "opened", "mallory", "FIRST_TIME_CONTRIBUTOR", false},
		{"a mapped guest", "issue_comment", "created", "guest", "NONE", true},
		{"a review request", "pull_request", "review_requested", "ada", "", true},
		{"a push", "push", "", "", "", true},
	} {
		_, ok, why := gh.GitHubAsker(tc.kind, asking(tc.kind, tc.action, tc.login, tc.association))
		if ok != tc.want {
			t.Errorf("%s: allowed = %v (%s)", tc.name, ok, why)
		}
	}
	gh.GitHub.Askers = []string{"Ada"}
	if _, ok, _ := gh.GitHubAsker("issue_comment", asking("issue_comment", "created", "ada", "OWNER")); !ok {
		t.Error("a named asker was refused")
	}
	if _, ok, why := gh.GitHubAsker("issue_comment", asking("issue_comment", "created", "bob", "COLLABORATOR")); ok || why != "bob is not one of the client's askers" {
		t.Errorf("a collaborator who is not a named asker: %v (%s)", ok, why)
	}
	if _, _, why := gh.GitHubAsker("issue_comment", asking("issue_comment", "created", "mallory", "NONE")); why != "mallory has no write access to the repository (none)" {
		t.Errorf("why = %q", why)
	}
}

func TestAskersAreLogins(t *testing.T) {
	gh := githubOf("gh", "acme")
	gh.GitHub = &GitHubOptions{Askers: []string{"not a login"}}
	if err := gh.Validate(); err == nil {
		t.Fatal("an asker that is not a login was accepted")
	}
}
