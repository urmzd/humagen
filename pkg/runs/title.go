package runs

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxDerivedTitle bounds a title the system derives, in runes.
const maxDerivedTitle = 60

// DeriveTitle is the name the system gives a conversation from its first
// message until someone, or a title agent, names it: the first line, without
// the agents it tags at the start, capitalized, cut at a word under 60
// characters. It is a pure function of the text, so every surface shows the
// same title at once.
func DeriveTitle(first string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(first), "\n")
	words := strings.Fields(line)
	for len(words) > 1 && strings.HasPrefix(words[0], "@") {
		words = words[1:]
	}
	if len(words) == 0 {
		return ""
	}
	if len(words) == 1 && strings.HasPrefix(words[0], "@") {
		return words[0]
	}
	title := strings.TrimRight(strings.Join(words, " "), " ,;:-")
	if utf8.RuneCountInString(title) > maxDerivedTitle {
		cut := []rune(title)[:maxDerivedTitle]
		if i := strings.LastIndexByte(string(cut), ' '); i > maxDerivedTitle/2 {
			cut = []rune(string(cut)[:i])
		}
		title = strings.TrimRight(string(cut), " ,;:-.") + "…"
	}
	r, size := utf8.DecodeRuneInString(title)
	return string(unicode.ToUpper(r)) + title[size:]
}
