package github_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/integrations/github"
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

// Labels are read for the repository named and follow no redirect, as the
// transport's cross-source reads do.
func TestConsoleLabelsFollowNoRedirect(t *testing.T) {
	asked := 0
	api := github.NewHTTPAPI("secret-token", &http.Client{Transport: labelTransport(func(r *http.Request) (*http.Response, error) {
		asked++
		if r.URL.Path != "/repos/acme/api/issues/12/labels" {
			t.Fatalf("followed a redirect to %s", r.URL)
		}
		h := http.Header{}
		h.Set("Location", "https://api.github.com/repos/acme/kept/issues/7/labels?per_page=100")
		return &http.Response{StatusCode: http.StatusMovedPermanently, Body: io.NopCloser(strings.NewReader("")), Header: h, Request: r}, nil
	})})
	if labels, err := api.IssueLabels(context.Background(), github.Ref{Owner: "acme", Repo: "api", Number: 12}); !errors.Is(err, github.ErrReadRedirected) || asked != 1 {
		t.Fatalf("labels=%v err=%v after %d requests", labels, err, asked)
	}
}
