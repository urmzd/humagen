package knowledge

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/urmzd/fiber/pkg/mcptools"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDriveBoundaryRejectsPartialAndRedactsEchoedCredential(t *testing.T) {
	secret := mcptools.NewSecret("sensitive-token")
	requests := 0
	provider := HTTPProvider{Kind: "drive", Client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Scheme != "https" || r.URL.Host != "www.googleapis.com" || r.Header.Get("Authorization") != "Bearer sensitive-token" {
			t.Fatalf("unexpected boundary %s", r.URL)
		}
		body := `{"files":[{"id":"doc1","name":"Runbook","mimeType":"text/plain"}]}`
		if requests == 2 {
			body = "deploy sensitive-token safely"
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	docs, err := provider.Fetch(context.Background(), "folder1", secret)
	if err != nil || len(docs) != 1 || strings.Contains(docs[0].Text, "sensitive-token") || !strings.Contains(docs[0].Text, mcptools.Redacted) {
		t.Fatalf("docs=%+v err=%v", docs, err)
	}
	provider.Client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"nextPageToken":"more","files":[]}`))}, nil
	})
	if _, err = provider.Fetch(context.Background(), "folder1", secret); FailureCause(err) != Unsupported {
		t.Fatalf("partial copy accepted: %v", err)
	}
	if _, err = provider.Fetch(context.Background(), "https://internal/secret", secret); FailureCause(err) != Unsupported {
		t.Fatal("arbitrary origin accepted")
	}
}

func TestProviderFailureDoesNotExposeRemoteBodyOrTransportError(t *testing.T) {
	for _, status := range []int{401, 403, 404, 429, 500, 302} {
		provider := HTTPProvider{Kind: "notion", Client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Host != "api.notion.com" {
				t.Fatal(r.URL)
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("sensitive-body"))}, nil
		})}}
		_, err := provider.Fetch(context.Background(), "page1", mcptools.NewSecret("token"))
		if err == nil || strings.Contains(err.Error(), "sensitive") {
			t.Fatalf("status %d: %v", status, err)
		}
	}
	provider := HTTPProvider{Kind: "drive", Client: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("token in error") })}}
	_, err := provider.Fetch(context.Background(), "folder1", mcptools.NewSecret("token"))
	if err == nil || strings.Contains(err.Error(), "token") {
		t.Fatal(err)
	}
}

func TestNotionNestedContentAndUnsupportedBlocks(t *testing.T) {
	requests := 0
	provider := HTTPProvider{Kind: "notion", Client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Header.Get("Notion-Version") == "" {
			t.Fatal("unversioned API")
		}
		body := `{"results":[{"id":"child","type":"paragraph","has_children":true,"paragraph":{"rich_text":[{"plain_text":"Release"}]}}]}`
		if requests == 2 {
			body = `{"results":[{"type":"paragraph","paragraph":{"rich_text":[{"plain_text":"safely"}]}}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	docs, err := provider.Fetch(context.Background(), "page1", mcptools.NewSecret("token"))
	if err != nil || len(docs) != 1 || !strings.Contains(docs[0].Text, "safely") {
		t.Fatalf("%+v %v", docs, err)
	}
	provider.Client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"results":[{"type":"image","image":{}}]}`))}, nil
	})
	if _, err := provider.Fetch(context.Background(), "page1", mcptools.NewSecret("token")); FailureCause(err) != Unsupported {
		t.Fatal(err)
	}
}
