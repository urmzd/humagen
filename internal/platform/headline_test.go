package platform

import (
	"strings"
	"testing"
)

// A subject or a thread's name is the whole text on one line: a question
// asked below a line of context is kept, and one cut short says so.
func TestAHeadlineKeepsEveryLineAndSaysWhenItIsCut(t *testing.T) {
	for _, c := range []struct {
		name, text, want string
	}{
		{"one short line", "Draft the credit note.", "Draft the credit note"},
		{"lines that fit are joined", "INV-2041 bills twice.\nShould the note cover one line?", "INV-2041 bills twice. Should the note cover one line?"},
		{
			"lines that do not fit are joined, then cut at a word",
			"INV-2041 bills the March maintenance twice.\nShould the credit note cover only the duplicated line?",
			"INV-2041 bills the March maintenance twice. Should the credit note cover only…",
		},
		{"blank lines and runs of spaces are one space", "  \"Renewal   terms\n\n for Globex\"  ", "Renewal terms for Globex"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := headline(c.text, maxQuestionSubject)
			if got != c.want {
				t.Fatalf("headline(%q) = %q, want %q", c.text, got, c.want)
			}
			if len(got) > maxQuestionSubject {
				t.Fatalf("headline(%q) is %d bytes, over %d", c.text, len(got), maxQuestionSubject)
			}
			if strings.Contains(got, "\n") {
				t.Fatalf("headline(%q) = %q holds a line break", c.text, got)
			}
		})
	}
}
