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

// reviewLead is a message that opens with the imperative that asks for a
// review: "review", "re-review", "please review", "can you re-review",
// "could you review", "give it another review", after a greeting. "review"
// must be the whole word: "reviews" and "reviewer" are about one.
var reviewLead = regexp.MustCompile(`^(?:(?:hey|hi|hello)[\s,!.]+)?(?:(?:can|could|would|will)\s+you\s+)?(?:please\s+)?/?(?:re-?review|review|give\s+(?:it|this)\s+(?:a|another)\s+review|do\s+(?:a|another)\s+review)\b`)

// questionWords start a question about a review ("Review: why is this
// nil?"), not an instruction for one.
var questionWords = map[string]bool{"why": true, "what": true, "how": true, "where": true, "when": true, "who": true, "which": true, "whose": true, "is": true, "are": true, "was": true, "were": true, "do": true, "does": true, "did": true, "can": true, "could": true, "should": true, "would": true, "will": true}

// focusWords start what to review or how: an object ("the retry loop",
// "this", "it"), a focus ("for security", "focus on auth") or a manner.
var focusWords = map[string]bool{"this": true, "that": true, "it": true, "the": true, "my": true, "our": true, "these": true, "those": true, "again": true, "for": true, "focus": true, "focusing": true, "with": true, "only": true, "just": true, "carefully": true, "please": true, "now": true, "pr": true, "pull": true, "change": true, "changes": true, "diff": true, "code": true, "commit": true, "commits": true, "file": true, "files": true, "against": true, "around": true, "on": true, "in": true, "from": true, "since": true, "after": true, "everything": true, "all": true, "security": true, "performance": true, "correctness": true, "tests": true}

// alsoChange is a request that asks for changes beside the review: it is
// work to do, not a review to post.
var alsoChange = regexp.MustCompile(`\band\s+(?:also\s+|then\s+)?(?:fix|change|update|write|implement|apply|push|commit|merge)\b`)

// asksForReview reports whether text, the message after the trigger, is a
// request for a review. It is when it opens with the imperative and what
// follows is nothing, a new line, a colon or comma with an instruction, or
// what to review: "review", "review the retry loop", "review\n\nfocus on
// security", "review: focus on auth", "review internal/retry/retry.go". It
// is not when "review" is what a sentence is about ("this review is wrong",
// "review comments are noise", "the review said"), when a question follows
// ("Review: why is this nil?"), or when it asks for changes too. Quoted
// lines and code are what a person shows, not what they ask.
func asksForReview(text string) bool {
	text = codeOrQuote.ReplaceAllString(text, " ")
	text = mentionWord.ReplaceAllString(strings.ToLower(text), " ")
	text = strings.TrimSpace(text)
	lead := reviewLead.FindStringIndex(text)
	if lead == nil {
		return false
	}
	rest := text[lead[1]:]
	if alsoChange.MatchString(rest) {
		return false
	}
	inline := strings.TrimLeft(rest, " \t")
	switch {
	case strings.Trim(inline, " \t\r\n.!?") == "":
		return true
	case strings.HasPrefix(inline, "\n") || strings.HasPrefix(inline, "\r"):
		// The instruction is on the lines after the request.
		return true
	case strings.HasPrefix(inline, ":") || strings.HasPrefix(inline, ",") || strings.HasPrefix(inline, "-"):
		after := strings.Fields(inline[1:])
		return len(after) == 0 || !questionWords[strings.Trim(after[0], ".,:;!?")]
	}
	first := strings.Trim(strings.Fields(inline)[0], ".,:;!?")
	return focusWords[first] || strings.ContainsAny(first, "/.") && len(first) > 1
}

// mentionWord is an @mention, which says who is asked, not what.
var mentionWord = regexp.MustCompile(`@[A-Za-z0-9][A-Za-z0-9-]*(?:/[A-Za-z0-9-]+)?(?:\[bot\])?`)

// codeOrQuote is a fenced block, inline code, or a quoted line: what a
// person shows, not what they ask.
var codeOrQuote = regexp.MustCompile("(?m)```(?s:.*?)```|`[^`\n]*`|^[ \t]*>.*$")
