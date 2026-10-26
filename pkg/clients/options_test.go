package clients

import (
	"errors"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestInstallationCapabilitiesAndPeopleAreClonedAndSurviveRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		store, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		c := githubOf("github", "acme")
		c.GitHub = &GitHubOptions{People: map[string]string{"octocat": "ada"}}
		c.Capabilities = &Capabilities{Events: []string{"issue_comment"}, Actions: []string{"github_comment"}, DeliverOutcomes: true}
		saved, err := store.Create("acme", c, DefaultCaps(), t0)
		if err != nil {
			t.Fatal(err)
		}
		c.GitHub.People["octocat"] = "eve"
		c.Capabilities.Events[0] = "issues"
		saved.GitHub.People["octocat"] = "mallory"
		saved.Capabilities.Actions[0] = "github_apply_changes"
		fresh, err := store.Get("acme", "github")
		if err != nil {
			t.Fatal(err)
		}
		if fresh.GitHub.People["octocat"] != "ada" || !fresh.AllowsEvent("issue_comment") || fresh.AllowsEvent("issues") || !fresh.AllowsAction("github_comment") || fresh.AllowsAction("github_apply_changes") {
			t.Fatal("caller widened saved installation", fresh)
		}
		reopened, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		fresh, err = reopened.Get("acme", "github")
		if err != nil || fresh.GitHub.People["octocat"] != "ada" || !fresh.AllowsOutcomes() || fresh.AllowsAction("github_apply_changes") {
			t.Fatal("installation did not replay", fresh, err)
		}
	})
}

func TestInstallationCapabilitiesRejectUnknownAndMalformedPeople(t *testing.T) {
	for _, change := range []func(*Client){
		func(c *Client) { c.Capabilities = &Capabilities{} },
		func(c *Client) { c.Capabilities = &Capabilities{Events: []string{"message"}, Actions: []string{}} },
		func(c *Client) {
			c.Capabilities = &Capabilities{Events: []string{}, Actions: []string{"github_comment", "github_comment"}}
		},
		func(c *Client) { c.GitHub = &GitHubOptions{People: map[string]string{"@octocat": "ada"}} },
		func(c *Client) { c.GitHub = &GitHubOptions{People: map[string]string{"octocat": ""}} },
	} {
		c := githubOf("github", "acme")
		change(&c)
		if err := c.Validate(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted malformed installation: %v", err)
		}
	}
}
