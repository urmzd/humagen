package platform

import (
	"regexp"
	"strings"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/saige/flow/event"
)

// githubShape is the shape a run on GitHub answers with, chosen by the
// host when the run starts and saved on its tag (wire.MetaAnswerShape). A
// review is one that was asked for on a pull request: an automatic review,
// a review requested from the app, or a request whose words ask for one.
// Anything else, a question, a follow-up or anything on an issue, is a
// reply.
func githubShape(tag event.Envelope) string {
	_, kind, _, ok := channelRepository(tag.Channel)
	if !ok || kind != "pull" {
		return wire.ShapeReply
	}
	if wire.ShapeOf(tag) == wire.ShapeReview {
		// The host's own automatic review said so when it raised the run.
		return wire.ShapeReview
	}
	if _, requested := tag.Data.(event.GitHubReviewRequested); requested {
		return wire.ShapeReview
	}
	if asksForReview(wire.TextOf(tag)) {
		return wire.ShapeReview
	}
	return wire.ShapeReply
}

// reviewRequest matches a request that starts by asking for a review, after a
// greeting, mentions and polite words: "review", "please review this",
// "@humagen can you re-review?", "give it another review". A question
// about a review ("why did the review flag this?") is not one.
var reviewRequest = regexp.MustCompile(`^(?:(?:hey|hi|hello|ok|okay|please|pls|kindly|now|can|could|would|will|you|go|ahead|and|to|do|give|this|it|me|us|a|an|another|mind|full|quick|new|fresh|code|pr)\s+)*/?(?:re-?review|review)(?:ing)?\b`)

// mentionWord is an @mention, which says who is asked, not what.
var mentionWord = regexp.MustCompile(`@[A-Za-z0-9][A-Za-z0-9-]*(?:\[bot\])?`)

// asksForReview reports whether text asks for a review.
func asksForReview(text string) bool {
	text = mentionWord.ReplaceAllString(strings.ToLower(text), " ")
	text = strings.Join(strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ',' || r == ':' || r == '!' || r == '?' || r == '.'
	}), " ")
	return reviewRequest.MatchString(text)
}
