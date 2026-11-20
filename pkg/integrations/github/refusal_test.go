package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/egressguard"
)

func refusedResponse(status int, header map[string]string, body string) *http.Response {
	h := http.Header{}
	for k, v := range header {
		h.Set(k, v)
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}

// GitHub's own rules decide what a refusal means: which 403 is a missing
// permission, which is a rate limit, and how long each asks to wait.
func TestGitHubRefusalsAreClassifiedByGitHubsRules(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	reset := strconv.FormatInt(now.Add(17*time.Minute).Unix(), 10)
	secondary := `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again.","documentation_url":"https://docs.github.com/rest/overview/rate-limits-for-the-rest-api#about-secondary-rate-limits"}`
	for name, tc := range map[string]struct {
		status int
		header map[string]string
		body   string
		class  egressguard.Class
		until  time.Time
	}{
		"missing permission":            {403, nil, `{"message":"Resource not accessible by integration"}`, egressguard.Permanent, time.Time{}},
		"bad credentials":               {401, nil, `{"message":"Bad credentials"}`, egressguard.Permanent, time.Time{}},
		"not found":                     {404, nil, `{"message":"Not Found"}`, egressguard.Permanent, time.Time{}},
		"gone":                          {410, nil, `{"message":"This issue was deleted"}`, egressguard.Permanent, time.Time{}},
		"unprocessable":                 {422, nil, `{"message":"Validation Failed","errors":[{"private":"detail"}]}`, egressguard.Permanent, time.Time{}},
		"secondary limit, no header":    {403, nil, secondary, egressguard.RateLimited, time.Time{}},
		"secondary limit, retry-after":  {403, map[string]string{"Retry-After": "45"}, secondary, egressguard.RateLimited, now.Add(45 * time.Second)},
		"429 with retry-after":          {429, map[string]string{"Retry-After": "30"}, `{}`, egressguard.RateLimited, now.Add(30 * time.Second)},
		"429 without a time":            {429, nil, `{}`, egressguard.RateLimited, time.Time{}},
		"primary limit until its reset": {403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": reset}, `{"message":"API rate limit exceeded for installation ID 1."}`, egressguard.RateLimited, now.Add(17 * time.Minute)},
		"403 with quota left":           {403, map[string]string{"X-RateLimit-Remaining": "4999", "X-RateLimit-Reset": reset}, `{"message":"Resource not accessible by integration"}`, egressguard.Permanent, time.Time{}},
		"server error":                  {500, nil, `<html>oops</html>`, egressguard.Transient, time.Time{}},
		"bad gateway":                   {502, nil, ``, egressguard.Transient, time.Time{}},
		"unavailable":                   {503, nil, `{"message":"Service unavailable"}`, egressguard.Transient, time.Time{}},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "https://api.github.com/repos/o/r/issues/1/comments", nil)
			r := Classifier{}.Classify(req, refusedResponse(tc.status, tc.header, tc.body), now)
			if r == nil || r.Class != tc.class || !r.Until.Equal(tc.until) {
				t.Fatalf("class %v until %v, want %v until %v", r.Class, r.Until, tc.class, tc.until)
			}
		})
	}
	req := httptest.NewRequest(http.MethodPost, "https://api.github.com/repos/o/r/issues/1/comments", nil)
	if r := (Classifier{}).Classify(req, refusedResponse(201, nil, `{"id":1}`), now); r != nil {
		t.Fatalf("an accepted write was classified as %+v", r)
	}
}

// What a refusal keeps is named: status, message, documentation_url, the
// request id and the rate-limit headers. Nothing else of the response, and no
// credential, reaches the refusal, the body handed on, or an error built
// from it.
func TestARefusalKeepsOnlyItsNamedFields(t *testing.T) {
	const token = "ghs_private_installation_token_0123456789"
	body, _ := json.Marshal(map[string]any{
		"message":           "Resource not accessible by integration\r\nBearer " + token + " eyJhbGciOiJSUzI1NiJ9.eyJpc3MiOiIxMjMifQ.c2lnbmF0dXJl ghp_anotherSecretValue123 " + strings.Repeat("x", 2000),
		"documentation_url": "https://docs.github.com/rest/issues/comments#create-an-issue-comment",
		"errors":            []map[string]string{{"message": "private-provider-body"}},
		"body":              "private comment text",
		"token":             token,
	})
	req := httptest.NewRequest(http.MethodPost, "https://api.github.com/repos/o/r/issues/1/comments?access_token="+token, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := refusedResponse(403, map[string]string{
		"X-GitHub-Request-Id": "C0FF:EE00:1234", "X-RateLimit-Limit": "5000", "X-RateLimit-Remaining": "4990",
		"X-RateLimit-Reset": "1800000900", "X-RateLimit-Resource": "core", "Set-Cookie": "session=" + token,
	}, string(body))
	r := Classifier{}.Classify(req, resp, time.Unix(1_800_000_000, 0))
	kept, _ := io.ReadAll(resp.Body)
	stored, _ := json.Marshal(r)
	for what, text := range map[string]string{"error": r.Error(), "body": string(kept), "stored": string(stored), "blocked": (&egressguard.Blocked{Provider: "github", Reason: "x", Last: r}).Error()} {
		for _, secret := range []string{token, "ghs_", "ghp_", "eyJ", "Bearer", "private-provider-body", "private comment text", "access_token", "session="} {
			if strings.Contains(text, secret) {
				t.Errorf("%s leaks %q: %s", what, secret, text)
			}
		}
		if len(text) > 1200 {
			t.Errorf("%s is not length-capped: %d bytes", what, len(text))
		}
	}
	if !strings.HasPrefix(r.Message, "Resource not accessible by integration") || strings.ContainsAny(r.Message, "\r\n") {
		t.Errorf("message: %q", r.Message)
	}
	if r.Status != 403 || r.RequestID != "C0FF:EE00:1234" || r.RateLimitRemaining != "4990" || r.RateLimitReset != "1800000900" || r.RateLimitResource != "core" || r.RateLimitLimit != "5000" ||
		r.DocumentationURL != "https://docs.github.com/rest/issues/comments#create-an-issue-comment" || r.Path != "/repos/o/r/issues/1/comments" {
		t.Errorf("named fields were not kept: %+v", r)
	}
	// The body handed on is the named fields, so reading it again changes
	// nothing.
	again := Classifier{}.Classify(req, &http.Response{StatusCode: 403, Header: resp.Header, Body: io.NopCloser(strings.NewReader(string(kept)))}, time.Unix(1_800_000_000, 0))
	if again.Message != r.Message || again.DocumentationURL != r.DocumentationURL || again.Class != r.Class {
		t.Errorf("classifying twice changed the refusal: %+v then %+v", r, again)
	}
}

// A body that is not a JSON object names no field: none of it is kept.
func TestARefusalWithoutNamedFieldsKeepsNothingOfItsBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://api.github.com/repos/o/r/issues/1/comments", nil)
	for _, body := range []string{"private-provider-body Bearer ghs_secretsecretsecret", `["private-provider-body"]`, `{"message":{"private-provider-body":1}}`, `{"message":"ok","documentation_url":"https://evil.example/private-provider-body"}`, `{"message":"x","documentation_url":"javascript:private-provider-body"}`} {
		resp := refusedResponse(403, map[string]string{"X-GitHub-Request-Id": "private-provider-body <script>"}, body)
		r := Classifier{}.Classify(req, resp, time.Now())
		kept, _ := io.ReadAll(resp.Body)
		if strings.Contains(r.Error()+string(kept)+r.DocumentationURL+r.RequestID, "private-provider-body") {
			t.Errorf("body %q leaked: %s / %s", body, r.Error(), kept)
		}
	}
}

// An operator reads the difference between a missing permission and a rate
// limit from the error the connector returns, through the app's transport.
func TestAnAppRefusalSaysWhichRefusalItWas(t *testing.T) {
	_, key := generateTestRSAKeyPEM(t, false)
	for message, want := range map[string]string{
		"Resource not accessible by integration":    "403 Forbidden",
		"You have exceeded a secondary rate limit.": "403 Forbidden",
	} {
		provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/app/installations/42/access_tokens" {
				_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_private_installation_token", "expires_at": time.Now().Add(time.Hour).UTC()})
				return
			}
			w.Header().Set("X-GitHub-Request-Id", "AAAA:BBBB")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": message, "echo": r.Header.Get("Authorization")})
		}))
		api, err := NewAppHTTPAPIAt(provider.URL, AppConfig{AppID: "123", InstallationID: "42", PrivateKey: key, HTTPClient: provider.Client()})
		if err != nil {
			t.Fatal(err)
		}
		_, err = api.CreateComment(context.Background(), Ref{Owner: "acme", Repo: "widgets", Number: 42}, "hello")
		provider.Close()
		if err == nil || !strings.Contains(err.Error(), message) || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "AAAA:BBBB") {
			t.Fatalf("the refusal is not diagnosable: %v", err)
		}
		if strings.Contains(err.Error(), "ghs_private_installation_token") || strings.Contains(err.Error(), "Bearer") {
			t.Fatalf("the refusal leaks the credential: %v", err)
		}
	}
}
