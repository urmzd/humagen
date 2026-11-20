package github

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/urmzd/humagen/pkg/egressguard"
)

// A refused GitHub call, made diagnosable without echoing the provider.
//
// GitHub puts the reason for a refusal in the response body, and orchagent's
// HTTP client copies a failed response's body into its error, which is then
// logged and shown. A body can reflect what was sent, so none is passed on.
// What is kept is named: the status, the "message" and "documentation_url"
// fields of a JSON body, the request id and the rate-limit headers. That is
// enough to tell "Resource not accessible by integration" from "You have
// exceeded a secondary rate limit", and nothing else leaves this file.

const (
	// refusedMessage stands in for a body that is not JSON or names no
	// message.
	refusedMessage = "GitHub installation request failed"
	maxMessage     = 300
	maxDocURL      = 200
	maxHeader      = 80
	// maxRefusalBody bounds how much of a failed response is read.
	maxRefusalBody = 64 << 10
)

// secretShapes are credentials as GitHub mints them, and bearer values and
// JWTs in general. A message that holds one has it cut out, whoever's it is.
var secretShapes = regexp.MustCompile(`(?i)(gh[pousr]_[a-z0-9_]{8,}|github_pat_[a-z0-9_]{8,}|v1\.[0-9a-f]{40}|eyJ[a-z0-9_-]{8,}\.[a-z0-9_-]{8,}\.[a-z0-9_-]{8,}|bearer\s+\S+|-----BEGIN[^-]*-----)`)

// safeText is s on one line, without control characters or anything shaped
// like a credential, at most limit bytes. secrets are cut out wherever they
// appear.
func safeText(s string, limit int, secrets ...string) string {
	for _, secret := range secrets {
		if len(secret) >= 8 {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	s = secretShapes.ReplaceAllString(s, "[redacted]")
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsControl(r) || r == ' ' || r == ' ' }), " ")
	s = strings.TrimSpace(strings.ToValidUTF8(s, ""))
	if len(s) > limit {
		s = strings.ToValidUTF8(s[:limit], "") + "..."
	}
	return s
}

// safeDocURL keeps a documentation link only when it is a plain HTTPS URL on
// a GitHub documentation host.
func safeDocURL(s string) string {
	if len(s) > maxDocURL {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" {
		return ""
	}
	switch strings.ToLower(u.Host) {
	case "docs.github.com", "developer.github.com", "support.github.com":
		return u.String()
	}
	return ""
}

// safeHeader keeps a header value that is short and made of the characters
// GitHub's ids, counts and resource names are made of.
func safeHeader(h http.Header, name string) string {
	v := h.Get(name)
	if v == "" || len(v) > maxHeader {
		return ""
	}
	for _, r := range v {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune(":-_.", r) {
			return ""
		}
	}
	return v
}

// bearer is the credential a request carries, so it can be cut out of
// anything read back.
func bearer(req *http.Request) string {
	_, token, _ := strings.Cut(req.Header.Get("Authorization"), " ")
	return strings.TrimSpace(token)
}

// refusalOf reads a failed response into a refusal and replaces its body
// with the named fields alone. It is safe to run twice.
func refusalOf(req *http.Request, resp *http.Response, now time.Time, secrets ...string) *egressguard.Refusal {
	r := &egressguard.Refusal{
		Provider: Name, Method: req.Method, Path: req.URL.Path, Status: resp.StatusCode, At: now,
		RequestID:          safeHeader(resp.Header, "X-GitHub-Request-Id"),
		RetryAfter:         safeHeader(resp.Header, "Retry-After"),
		RateLimitLimit:     safeHeader(resp.Header, "X-RateLimit-Limit"),
		RateLimitRemaining: safeHeader(resp.Header, "X-RateLimit-Remaining"),
		RateLimitReset:     safeHeader(resp.Header, "X-RateLimit-Reset"),
		RateLimitResource:  safeHeader(resp.Header, "X-RateLimit-Resource"),
	}
	var body struct {
		Message          string `json:"message"`
		DocumentationURL string `json:"documentation_url"`
	}
	if resp.Body != nil {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxRefusalBody))
		_ = resp.Body.Close()
		// A body that is not one JSON object names no field, and none of it
		// is read as one.
		_ = json.Unmarshal(raw, &body)
	}
	secrets = append(secrets, bearer(req))
	r.Message = safeText(body.Message, maxMessage, secrets...)
	if r.Message == "" {
		r.Message = refusedMessage
	}
	r.DocumentationURL = safeDocURL(body.DocumentationURL)
	r.Class, r.Until = classify(r, now)

	kept, _ := json.Marshal(map[string]any{"message": r.Message, "documentation_url": r.DocumentationURL, "status": r.Status, "request_id": r.RequestID})
	resp.Body = io.NopCloser(bytes.NewReader(kept))
	resp.ContentLength = int64(len(kept))
	resp.Header.Del("Content-Encoding")
	resp.Header.Set("Content-Type", "application/json")
	resp.Uncompressed = false
	return r
}

// classify says what a refusal means, following GitHub's rules for its rate
// limits: a 403 or 429 with retry-after waits that long; one with
// x-ratelimit-remaining 0 waits until x-ratelimit-reset; one that says it is
// a secondary rate limit and names no time waits at least a minute, which
// the guard lengthens each time. Any other 403 is a missing permission.
func classify(r *egressguard.Refusal, now time.Time) (egressguard.Class, time.Time) {
	if r.Status == http.StatusForbidden || r.Status == http.StatusTooManyRequests {
		if seconds, err := strconv.Atoi(r.RetryAfter); err == nil && seconds >= 0 {
			return egressguard.RateLimited, now.Add(time.Duration(seconds) * time.Second)
		}
		if r.RateLimitRemaining == "0" {
			if reset, err := strconv.ParseInt(r.RateLimitReset, 10, 64); err == nil && reset > 0 {
				return egressguard.RateLimited, time.Unix(reset, 0)
			}
			return egressguard.RateLimited, time.Time{}
		}
		text := strings.ToLower(r.Message + " " + r.DocumentationURL)
		if r.Status == http.StatusTooManyRequests || strings.Contains(text, "rate limit") || strings.Contains(text, "rate-limit") || strings.Contains(text, "abuse detection") {
			return egressguard.RateLimited, time.Time{}
		}
		return egressguard.Permanent, time.Time{}
	}
	switch r.Status {
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusLocked, http.StatusTooEarly:
		return egressguard.Transient, time.Time{}
	}
	if r.Status >= 500 {
		return egressguard.Transient, time.Time{}
	}
	// 401, 404, 410, 422 and the rest of 3xx and 4xx: the same request gets
	// the same answer. Redirects are refused, so a 3xx is one too.
	return egressguard.Permanent, time.Time{}
}

// Classifier reads GitHub's answers for a guard.
type Classifier struct{}

var _ egressguard.Classifier = Classifier{}

// Provider implements egressguard.Classifier.
func (Classifier) Provider() string { return Name }

// Write implements egressguard.Classifier: everything but a read.
func (Classifier) Write(req *http.Request) bool {
	return req.Method != http.MethodGet && req.Method != http.MethodHead && req.Method != http.MethodOptions
}

// Classify implements egressguard.Classifier.
func (Classifier) Classify(req *http.Request, resp *http.Response, now time.Time) *egressguard.Refusal {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return refusalOf(req, resp, now)
}
