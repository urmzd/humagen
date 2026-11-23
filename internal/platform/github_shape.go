package platform

import (
	"regexp"
	"strings"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/saige/flow/event"
)

// githubShape is the shape a run on GitHub starts with, chosen by the host
// when the run starts and saved on its tag (wire.MetaAnswerShape). The host
// decides a review only on signals that cannot be misread, on a pull
// request: an automatic review, a review requested from the app, a rule
// whose shape is review, or the exact command "review", "re-review" or
// "/review". Anything else starts as a reply, and the agent turns it into a
// review with start_review when the person asked for one in other words.
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
	if isReviewCommand(wire.TextOf(tag)) {
		return wire.ShapeReview
	}
	return wire.ShapeReply
}

// reviewCommand is the whole message as a command: "review", "re-review"
// or "rereview", with trailing punctuation, or one that starts "/review".
var reviewCommand = regexp.MustCompile(`^(?:re-?review|review)[.!?]*$|^/review\b`)

// mentionWord is an @mention, which says who is asked, not what.
var mentionWord = regexp.MustCompile(`@[A-Za-z0-9][A-Za-z0-9-]*(?:/[A-Za-z0-9-]+)?(?:\[bot\])?`)

// quotedLine is a quoted line: what a person replies to, not what they ask.
var quotedLine = regexp.MustCompile(`(?m)^[ \t]*>.*$`)

// isReviewCommand reports whether text, the message after the trigger, is
// the review command. Nothing else is read for intent here: the agent reads
// what a person meant.
func isReviewCommand(text string) bool {
	text = quotedLine.ReplaceAllString(text, " ")
	text = strings.TrimSpace(mentionWord.ReplaceAllString(strings.ToLower(text), " "))
	return reviewCommand.MatchString(text)
}
