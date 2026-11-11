package slack

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/egressguard"
)

// Slack says most refusals in a 200. The classifier reads the code, keeps
// nothing else of the body, and leaves the body whole for the API's decoder.
func TestSlackAnswersAreClassifiedByTheirCode(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for name, tc := range map[string]struct {
		status int
		header map[string]string
		body   string
		class  egressguard.Class
		until  time.Time
		code   string
	}{
		"accepted":            {200, nil, `{"ok":true,"ts":"1.2"}`, "", time.Time{}, ""},
		"already reacted":     {200, nil, `{"ok":false,"error":"already_reacted"}`, "", time.Time{}, ""},
		"not in channel":      {200, nil, `{"ok":false,"error":"not_in_channel"}`, egressguard.Permanent, time.Time{}, "not_in_channel"},
		"revoked":             {200, nil, `{"ok":false,"error":"token_revoked"}`, egressguard.Permanent, time.Time{}, "token_revoked"},
		"missing scope":       {200, nil, `{"ok":false,"error":"missing_scope","needed":"chat:write"}`, egressguard.Permanent, time.Time{}, "missing_scope"},
		"rate limited":        {429, map[string]string{"Retry-After": "12"}, `{"ok":false,"error":"ratelimited"}`, egressguard.RateLimited, now.Add(12 * time.Second), "ratelimited"},
		"rate limited in 200": {200, nil, `{"ok":false,"error":"ratelimited"}`, egressguard.RateLimited, time.Time{}, "ratelimited"},
		"slack is down":       {503, nil, `<html>private-provider-body</html>`, egressguard.Transient, time.Time{}, ""},
		"internal error":      {200, nil, `{"ok":false,"error":"internal_error"}`, egressguard.Transient, time.Time{}, "internal_error"},
		"unknown code":        {200, nil, `{"ok":false,"error":"something_new"}`, egressguard.Transient, time.Time{}, "something_new"},
		"not a code":          {200, nil, `{"ok":false,"error":"private-provider-body <b>"}`, egressguard.Transient, time.Time{}, "unknown_error"},
	} {
		t.Run(name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range tc.header {
				h.Set(k, v)
			}
			resp := &http.Response{StatusCode: tc.status, Header: h, Body: io.NopCloser(strings.NewReader(tc.body))}
			req := httptest.NewRequest(http.MethodPost, "https://slack.com/api/chat.postMessage", nil)
			r := Classifier{}.Classify(req, resp, now)
			after, _ := io.ReadAll(resp.Body)
			if string(after) != tc.body {
				t.Fatalf("the body was not left whole: %q", after)
			}
			if tc.class == "" {
				if r != nil {
					t.Fatalf("classified as %+v", r)
				}
				return
			}
			if r == nil || r.Class != tc.class || !r.Until.Equal(tc.until) || r.Message != tc.code {
				t.Fatalf("got %+v, want class %v until %v code %q", r, tc.class, tc.until, tc.code)
			}
			if strings.Contains(r.Error(), "private-provider-body") {
				t.Fatalf("the refusal kept more than the code: %s", r.Error())
			}
		})
	}
}
