package bigquery

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type tokenTestTransport func(*http.Request) (*http.Response, error)

func (f tokenTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestOAuthRequestsRotateRefuseRedirectsAndFailClosed(t *testing.T) {
	current := "one"
	calls := 0
	seen := []string{}
	base := tokenTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		seen = append(seen, r.Header.Get("Authorization"))
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://attacker.test/steal"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	tr := &tokenTransport{base: base, source: func(context.Context) (string, error) {
		if current == "" {
			return "", errors.New("private token")
		}
		return current, nil
	}}
	client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, token := range []string{"one", "two"} {
		current = token
		r, err := client.Get("https://bigquery.googleapis.com/bigquery/v2/projects")
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
	}
	if strings.Join(seen, ",") != "Bearer one,Bearer two" || calls != 2 {
		t.Fatal(seen, calls)
	}
	current = ""
	if _, err := client.Get("https://bigquery.googleapis.com/bigquery/v2/projects"); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal(err)
	}
	current = "three"
	for _, target := range []string{"https://attacker.test/bigquery/v2/projects", "https://bigquery.googleapis.com:443/bigquery/v2/projects", "https://www.googleapis.com/drive/v3/files"} {
		if _, err := client.Get(target); err == nil {
			t.Fatal("accepted wrong audience")
		}
	}
	if calls != 2 {
		t.Fatal("unauthorized network request", calls)
	}
}

func TestCrawlRefusesUnavailableSelectedDataset(t *testing.T) {
	f := newFake()
	c, err := newWith(Config{Project: "acme-data", Datasets: []string{"missing"}}, func(context.Context, Config) (api, error) { return f, nil })
	if err != nil {
		t.Fatal(err)
	}
	entries, err := c.Crawl(context.Background())
	if !errors.Is(err, ErrOutOfScope) || len(entries) != 0 {
		t.Fatalf("unverified selection succeeded: %v %v", entries, err)
	}
}
