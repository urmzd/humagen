package platform

import (
	"regexp"
	"strings"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/trigger"
	"github.com/urmzd/saige/flow/event"
)

// githubShape is the shape a run on GitHub starts with, chosen by the host
// when the run starts and saved on its tag (wire.MetaAnswerShape). The host
// decides a review only on signals that cannot be misread, on a pull
// request: an automatic review, a review requested from the app, a rule
// whose shape is review, or the exact command "review", "re-review" or
// "/review". Anything else starts as a reply, and the agent turns it into a
// review with start_review when the person asked for one in other words.
// handles are the client's triggers (clients.Client.Triggers), the one
// mention that addresses a command.
func githubShape(tag event.Envelope, handles []string) string {
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
	if isReviewCommand(wire.TextOf(tag), handles) {
		return wire.ShapeReview
	}
	return wire.ShapeReply
}

// reviewCommand is the whole message as a command: "review", "re-review"
// or "rereview", with closing punctuation, or a message that starts with
// the word "/review".
var reviewCommand = regexp.MustCompile(`^(?:re-?review|review)[.!?]*$|^/review(?:\s|$)`)

// quotedLine is a quoted line: what a person replies to, not what they ask.
var quotedLine = regexp.MustCompile(`(?m)^[ \t]*>.*$`)

// isReviewCommand reports whether text is the review command and nothing
// else: the command alone, or addressed by a trigger that opens the
// message ("@humagen review"). Case, surrounding space and quoted lines do
// not count. A trigger inside a sentence, a mention of anyone else, and any
// other word make the message a reply. Nothing else is read for intent
// here: the agent reads what a person meant.
func isReviewCommand(text string, handles []string) bool {
	text = quotedLine.ReplaceAllString(text, "")
	if ask, ok := trigger.Read(text, handles, nil); ok {
		// A trigger that opens the message is its address and is taken
		// out; one inside a sentence stays, so the sentence is no command.
		text = ask.Text
	}
	return reviewCommand.MatchString(strings.ToLower(strings.TrimSpace(text)))
}
