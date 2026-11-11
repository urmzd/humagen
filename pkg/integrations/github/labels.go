package github

import (
	"context"
	"encoding/json"
	"fmt"
	transport "github.com/urmzd/saige/flow/connectors/github"
	"io"
	"net/http"
)

// LabelAPI supplements the upstream context reader with issue labels.
type LabelAPI interface {
	IssueLabels(context.Context, Ref) ([]string, error)
}

// HTTPAPI composes the shared transport with the console's label read.
// token is empty when the client's transport authenticates every request
// itself, as a GitHub App installation's does.
type HTTPAPI struct {
	*transport.HTTPAPI
	base  string
	token string
	http  *http.Client
}

func NewHTTPAPI(token string, client *http.Client) *HTTPAPI {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPAPI{HTTPAPI: transport.NewHTTPAPI(token, client), base: transport.GitHubBaseURL, token: token, http: client}
}
func (a *HTTPAPI) IssueLabels(ctx context.Context, ref Ref) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s/issues/%d/labels?per_page=100", a.base, ref.Owner, ref.Repo, ref.Number), nil)
	if err != nil {
		return nil, err
	}
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github labels: status %d", resp.StatusCode)
	}
	var labels []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&labels); err != nil {
		return nil, err
	}
	out := make([]string, 0, min(100, len(labels)))
	for _, label := range labels[:min(100, len(labels))] {
		out = append(out, label.Name)
	}
	return out, nil
}
