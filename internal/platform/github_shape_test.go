package platform

import (
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/saige/flow/event"
)

func TestAsksForReviewReadsTheRequestNotTheTopic(t *testing.T) {
	for text, want := range map[string]bool{
		"review":                             true,
		"@humagen review":                    true,
		"@humagen re-review":                 true,
		"Please review.":                     true,
		"please review this PR":              true,
		"review this pull request":           true,
		"review the change please":           true,
		"@humagen can you re-review?":        true,
		"Hey @humagen, could you review it?": true,
		"give it another review":             true,
		"/review":                            true,
		"> earlier\n@humagen review":         true,
		"this review is wrong about retries": false,
		"review comments are noise, can you explain finding 2?": false,
		"Review: why is this nil?":                              false,
		"review the retry changes and also fix them":            false,
		"why did the review flag line 42?":                      false,
		"don't review yet":                                      false,
		"please do not review":                                  false,
		"> review\nwhat does Deliver return?":                   false,
		"what does `review` return?":                            false,
		"```\nreview\n```":                                      false,
		"what does Deliver return?":                             false,
		"fix the second finding":                                false,
		"":                                                      false,
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
		{"a rule that replies on a pull request", raise(pull, "review please", map[string]string{wire.MetaAnswerShape: wire.ShapeReply}), wire.ShapeReply},
		{"a requested review", event.Envelope{Channel: pull, Data: event.GitHubReviewRequested{}}, wire.ShapeReview},
	} {
		if got := githubShape(tc.tag); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
