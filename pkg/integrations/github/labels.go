package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	transport "github.com/urmzd/saige/flow/connectors/github"
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
	// app is the App's token source, nil for a client that is no App.
	app *AppTokenSource
}

// AppLogin is the login of the App's bot account as GitHub names it, read
// with the App's own key. A client that is no App has none.
func (a *HTTPAPI) AppLogin(ctx context.Context) (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("github: the client is no GitHub App")
	}
	return a.app.AppLogin(ctx)
}

func NewHTTPAPI(token string, client *http.Client) *HTTPAPI {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPAPI{HTTPAPI: transport.NewHTTPAPI(token, client), base: transport.GitHubBaseURL, token: token, http: client}
}

// IssueLabels reads an issue's labels. It is read for the repository the
// caller names, and, as the transport's cross-source reads do, it follows no
// redirect: a renamed or transferred repository answers ErrReadRedirected,
// so the labels are only ever the named repository's.
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
	client := *a.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return ErrReadRedirected }
	resp, err := client.Do(req)
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
