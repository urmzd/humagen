package github

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAppMintReportsOnlyConfirmedAuthenticationRejection(t *testing.T) {
	_, key := generateTestRSAKeyPEM(t, false)
	for _, code := range []int{0, 401, 403, 404, 429, 500} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			calls := 0
			var source *AppTokenSource
			var err error
			source, err = NewAppTokenSource("https://api.github.com", AppConfig{AppID: "123", InstallationID: "42", PrivateKey: key,
				AuthRevision: func(context.Context) string { return "before-request" },
				OnAuthRejected: func(_ context.Context, revision string) {
					if revision != "before-request" {
						t.Fatal("lost attempt identity")
					}
					// A callback must run after the token cache mutex is released.
					if !source.mu.TryLock() {
						t.Fatal("callback held token mutex")
					}
					source.mu.Unlock()
					calls++
				},
				HTTPClient: &http.Client{Transport: appRoundTripFunc(func(*http.Request) (*http.Response, error) {
					if code == 0 {
						return nil, errors.New("private network detail")
					}
					return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("private provider detail"))}, nil
				})},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = source.Token(t.Context()); err == nil || strings.Contains(err.Error(), "private") {
				t.Fatal(err)
			}
			if (calls == 1) != (code == 401) {
				t.Fatal(code, calls)
			}
		})
	}
}
