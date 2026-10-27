package join

import (
	"context"
	"slices"
	"strings"
	"unicode"
)

// Words is an Asker that asks no model: an agent joins a message that
// shares a word of three letters or more with its name, description or
// prompt, and the reason names the words. It is deterministic, so tests and
// offline deployments get the same answer every time.
var Words Asker = AskerFunc(func(_ context.Context, c Candidate, message string) (Decision, error) {
	about := map[string]bool{}
	for _, w := range words(c.Name + " " + c.Description + " " + c.Prompt) {
		about[w] = true
	}
	var matched []string
	for _, w := range words(message) {
		if about[w] && !slices.Contains(matched, w) {
			matched = append(matched, w)
		}
	}
	if len(matched) == 0 {
		return Decision{Reason: "Nothing in it is mine."}, nil
	}
	return Decision{Join: true, Reason: "matched " + strings.Join(matched, ", ") + "."}, nil
})

var stopWords = map[string]bool{"the": true, "and": true, "for": true, "you": true, "can": true, "what": true, "please": true, "with": true, "this": true, "that": true}

// words are the lowercase words of s worth matching, each with a trailing
// "ing" or "s" cut.
func words(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		for _, suffix := range []string{"ing", "s"} {
			if len(w)-len(suffix) >= 3 && strings.HasSuffix(w, suffix) {
				w = strings.TrimSuffix(w, suffix)
				break
			}
		}
		if len(w) >= 3 && !stopWords[w] {
			out = append(out, w)
		}
	}
	return out
}
