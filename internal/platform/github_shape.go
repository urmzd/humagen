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
// a review requested from the app, a rule whose shape is review, or a
// request whose words ask for one.
// Anything else, a question, a follow-up or anything on an issue, is a
// reply.
func githubShape(tag event.Envelope) string {
	_, kind, _, ok := channelRepository(tag.Channel)
	if !ok || kind != "pull" {
		return wire.ShapeReply
	}
	if shape := wire.ShapeOf(tag); shape != "" {
		// The host said so when it raised the run: an automatic review,
		// or an automation rule, whose shape is the rule's.
		return shape
	}
	if _, requested := tag.Data.(event.GitHubReviewRequested); requested {
		return wire.ShapeReview
	}
	if asksForReview(wire.TextOf(tag)) {
		return wire.ShapeReview
	}
	return wire.ShapeReply
}

// reviewRequest matches a message that is itself a request for a review,
// once mentions, quoted lines and code are gone: "review", "re-review",
// "please review", "review this PR", "could you review it?". A sentence
// about a review ("this review is wrong", "Review: why is this nil?",
// "don't review yet") is not one: after the request only a few words that
// name what to review may follow.
var reviewRequest = regexp.MustCompile(`^(?:(?:hey|hi|hello)\s+)?(?:(?:can|could|would|will)\s+you\s+)?(?:please\s+)?/?(?:re-?review|review|give\s+(?:it|this)\s+(?:a|another)\s+review|do\s+(?:a|another)\s+review)(?:\s+(?:this|it|again|the|my|pr|pull|request|change|changes|diff|now|please))*$`)

// mentionWord is an @mention, which says who is asked, not what.
var mentionWord = regexp.MustCompile(`@[A-Za-z0-9][A-Za-z0-9-]*(?:/[A-Za-z0-9-]+)?(?:\[bot\])?`)

// codeOrQuote is a fenced block, inline code, or a quoted line: what a
// person shows, not what they ask.
var codeOrQuote = regexp.MustCompile("(?m)```(?s:.*?)```|`[^`\n]*`|^[ \t]*>.*$")

// asksForReview reports whether text is a request for a review.
func asksForReview(text string) bool {
	text = codeOrQuote.ReplaceAllString(text, " ")
	text = mentionWord.ReplaceAllString(strings.ToLower(text), " ")
	text = strings.Join(strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ',' || r == '!' || r == '?' || r == '.'
	}), " ")
	return reviewRequest.MatchString(text)
}
