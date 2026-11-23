package platform

import (
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/saige/flow/event"
)

func TestAsksForReviewReadsTheRequestNotTheTopic(t *testing.T) {
	for text, want := range map[string]bool{
		"review":                              true,
		"Review this, please.":                true,
		"@humagen review":                     true,
		"@humagen can you re-review?":         true,
		"Hey @humagen, could you review it?":  true,
		"please give it another review":       true,
		"would you mind reviewing the retry?": true,
		"/review":                             true,
		"why did the review flag line 42?":    false,
		"what does Deliver return?":           false,
		"fix the second finding":              false,
		"the reviewer is wrong about retries": false,
		"":                                    false,
	} {
		if got := asksForReview(text); got != want {
			t.Errorf("asksForReview(%q) = %v, want %v", text, got, want)
		}
	}
}

// The host chooses the shape: a review only on a pull request, and only
// when one was asked for; anything else is a reply.
func TestGitHubShapeIsTheHostsChoice(t *testing.T) {
	pull := "github://acme/site/pull/7?head_sha=" + strings.Repeat("a", 40)
	issue := "github://acme/site/issues/7"
	raise := func(channel, text string, ctx map[string]string) event.Envelope {
		return wire.NewEvent(wire.Raise{ID: "e", Tenant: "t", Origin: "github", Channel: channel, Text: text, Context: ctx})
	}
	for _, tc := range []struct {
		name string
		tag  event.Envelope
		want string
	}{
		{"a review asked on a pull request", raise(pull, "review please", nil), wire.ShapeReview},
		{"a question on a pull request", raise(pull, "what does this change?", nil), wire.ShapeReply},
		{"a review asked on an issue", raise(issue, "review please", nil), wire.ShapeReply},
		{"an automatic review", raise(pull, "Review pull request #7.", map[string]string{wire.MetaAnswerShape: wire.ShapeReview}), wire.ShapeReview},
		{"a requested review", event.Envelope{Channel: pull, Data: event.GitHubReviewRequested{}}, wire.ShapeReview},
	} {
		if got := githubShape(tc.tag); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
