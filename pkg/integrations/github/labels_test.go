package github_test

import (
	"context"
	"github.com/urmzd/humagen/pkg/integrations/github"
	"io"
	"net/http"
	"strings"
	"testing"
)

type labelTransport func(*http.Request) (*http.Response, error)

func (f labelTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestConsoleLabelsUseServerTokenAndBoundedRead(t *testing.T) {
	api := github.NewHTTPAPI("secret-token", &http.Client{Transport: labelTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer secret-token" || r.URL.Path != "/repos/acme/api/issues/12/labels" || r.URL.Query().Get("per_page") != "100" {
			t.Fatalf("unexpected label request: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[{"name":"bug"},{"name":"regression"}]`)), Header: http.Header{}}, nil
	})})
	labels, err := api.IssueLabels(context.Background(), github.Ref{Owner: "acme", Repo: "api", Number: 12})
	if err != nil || len(labels) != 2 || labels[0] != "bug" {
		t.Fatalf("labels=%v err=%v", labels, err)
	}
}
