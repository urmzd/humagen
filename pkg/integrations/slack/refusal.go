package slack

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/egressguard"
)

// Classifier reads Slack's answers for a client's egress guard.
//
// Slack reports most refusals as HTTP 200 with ok:false and an error code,
// and a rate limit as HTTP 429 with Retry-After. Only the code is kept from
// a body: it is one of Slack's own identifiers, and nothing else is read.
type Classifier struct{}

var _ egressguard.Classifier = Classifier{}

// Provider implements egressguard.Classifier.
func (Classifier) Provider() string { return "slack" }

// Write implements egressguard.Classifier. Slack's write methods are POSTs.
func (Classifier) Write(req *http.Request) bool {
	return req.Method != http.MethodGet && req.Method != http.MethodHead && req.Method != http.MethodOptions
}

// permanent are codes a retry of the same call cannot change: the app is not
// allowed, the place is gone, or the message is not one Slack will take.
var permanent = map[string]bool{
	"account_inactive": true, "invalid_auth": true, "not_authed": true, "token_revoked": true, "token_expired": true,
	"missing_scope": true, "no_permission": true, "not_allowed_token_type": true, "access_denied": true,
	"ekm_access_denied": true, "team_access_not_granted": true, "org_login_required": true, "two_factor_setup_required": true,
	"enterprise_is_restricted": true, "restricted_action": true, "restricted_action_read_only_channel": true,
	"restricted_action_thread_only_channel": true, "restricted_action_non_threadable_channel": true,
	"restricted_action_thread_locked": true, "channel_not_found": true, "not_in_channel": true, "is_archived": true,
	"thread_not_found": true, "message_not_found": true, "cant_update_message": true, "edit_window_closed": true,
	"msg_too_long": true, "no_text": true, "invalid_blocks": true, "invalid_blocks_format": true,
	"invalid_arguments": true, "invalid_arg_name": true, "invalid_name": true, "too_many_attachments": true,
	"too_many_reactions": true, "too_many_emoji": true, "user_not_found": true, "file_not_found": true,
	"method_deprecated": true, "unknown_method": true, "deprecated_endpoint": true,
}

// expected are codes the connector itself treats as done.
var expected = map[string]bool{"already_reacted": true, "no_reaction": true}

const maxSlackBody = 64 << 10

// safeCode keeps a Slack error code that looks like one.
func safeCode(code string) string {
	if code == "" || len(code) > 64 {
		return ""
	}
	for _, r := range code {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return ""
		}
	}
	return code
}

// Classify implements egressguard.Classifier.
func (Classifier) Classify(req *http.Request, resp *http.Response, now time.Time) *egressguard.Refusal {
	r := &egressguard.Refusal{Provider: "slack", Method: req.Method, Path: req.URL.Path, Status: resp.StatusCode, At: now}
	if v := resp.Header.Get("Retry-After"); len(v) <= 10 {
		if _, err := strconv.Atoi(v); err == nil {
			r.RetryAfter = v
		}
	}
	if v := resp.Header.Get("X-Slack-Req-Id"); len(v) <= 80 && !strings.ContainsFunc(v, func(c rune) bool {
		return (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' && c != '.'
	}) {
		r.RequestID = v
	}
	code := ""
	if resp.Body != nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusTooManyRequests) {
		// Inspect a bounded prefix and give every byte back to the decoder.
		body := resp.Body
		raw, _ := io.ReadAll(io.LimitReader(body, maxSlackBody))
		resp.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(raw), body), body}
		var envelope struct {
			OK    *bool  `json:"ok"`
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &envelope) == nil && envelope.OK != nil && !*envelope.OK {
			code = safeCode(envelope.Error)
			if code == "" {
				code = "unknown_error"
			}
		}
	}
	r.Message = code
	switch {
	case resp.StatusCode == http.StatusTooManyRequests || code == "ratelimited" || code == "rate_limited":
		r.Class = egressguard.RateLimited
		if seconds, err := strconv.Atoi(r.RetryAfter); err == nil && seconds >= 0 {
			r.Until = now.Add(time.Duration(seconds) * time.Second)
		}
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout:
		r.Class = egressguard.Transient
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		r.Class = egressguard.Permanent
	case code == "" || expected[code]:
		return nil
	case permanent[code]:
		r.Class = egressguard.Permanent
	default:
		// A code this list does not know is retried with backoff, and stops
		// the message when its attempts run out.
		r.Class = egressguard.Transient
	}
	return r
}
