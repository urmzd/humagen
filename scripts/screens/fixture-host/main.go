// The screen fixture uses the normal command tree with a local GitHub API.
// Review provenance still comes from a real connector tool result.
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"slices"

	"github.com/urmzd/humagen"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/integrations/github"
)

const (
	fixtureTokenName  = "HUMAGEN_SCREENS_GITHUB_TOKEN"
	fixtureSecretName = "HUMAGEN_SCREENS_GITHUB_SECRET"
	fixtureToken      = "screens-local-github-token"
	fixtureSecret     = "screens-local-signing-secret"
)

//go:embed review-context.json
var reviewContextJSON []byte

type reviewFixture struct {
	Owner   string         `json:"owner"`
	Repo    string         `json:"repo"`
	Number  int            `json:"number"`
	Context github.Context `json:"context"`
}

func readReviewFixture() (reviewFixture, error) {
	var fixture reviewFixture
	if err := json.Unmarshal(reviewContextJSON, &fixture); err != nil {
		return fixture, fmt.Errorf("screen review context: %w", err)
	}
	if fixture.Context.Subject.Number != fixture.Number || fixture.Context.Subject.Head.SHA == "" {
		return fixture, fmt.Errorf("screen review context must name its pull request and head")
	}
	return fixture, nil
}

func fixtureConfig(cfg *platform.Config) error {
	fixture, err := readReviewFixture()
	if err != nil {
		return err
	}
	api := github.NewFake()
	if err := api.SeedContext(github.Ref{Owner: fixture.Owner, Repo: fixture.Repo, Number: fixture.Number}, fixture.Context); err != nil {
		return fmt.Errorf("screen GitHub fixture: %w", err)
	}
	// Every GitHub installation in this fixture process stays in memory, even
	// when the shell happens to contain an unrelated production credential.
	cfg.GitHubAPI = func(string) github.API { return api }
	previousEnv := cfg.Env
	cfg.Env = func(name string) (string, bool) {
		switch name {
		case fixtureTokenName:
			return fixtureToken, true
		case fixtureSecretName:
			return fixtureSecret, true
		default:
			if previousEnv != nil {
				return previousEnv(name)
			}
			return "", false
		}
	}
	refs := make(map[string][]string, len(cfg.SecretRefs)+1)
	for tenant, names := range cfg.SecretRefs {
		refs[tenant] = slices.Clone(names)
	}
	for _, name := range []string{fixtureTokenName, fixtureSecretName} {
		if !slices.Contains(refs["default"], name) {
			refs["default"] = append(refs["default"], name)
		}
	}
	cfg.SecretRefs = refs
	return nil
}

func main() {
	os.Exit(humagen.Main(humagen.Version{Version: "screen-fixture"}, humagen.WithPlatform(fixtureConfig)))
}
