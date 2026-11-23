// Package trigger reads the one word that asks Humagen on a surface,
// "@humagen", and the agent a person names right after it.
//
// On GitHub an agent's name is not safe to mention: "@github-reviewer" is
// somebody else's account, and GitHub tells them. So a comment asks only
// through the trigger, which names Humagen's own account, and the agent it is
// for is the word after it:
//
//	@humagen what changed?                     the default answers
//	@humagen ask research-bot about the cache  research-bot answers
//	@humagen github-reviewer: review this      github-reviewer answers
//	@humagen @docs-bot summarize               docs-bot answers
//	@humagen docs are stale                    the default answers
//
// The trigger counts in any case, as a whole word, anywhere in the text, but
// not where text is shown or quoted rather than said: code, HTML comments
// (a pull request template can hold one) and quoted replies.
//
// This package is a leaf: stdlib only.
package trigger

import "strings"

// Name is the trigger's word: a person writes "@humagen".
const Name = "humagen"

// Ask is what a message that holds the trigger asks.
type Ask struct {
	// Agent is the agent named right after the trigger, "" for none.
	Agent string
	// Text is what the agent reads. The whole message is kept as the person
	// wrote it, except that a trigger that opens the message is its address
	// and is taken out, with the agent's name after it.
	Text string
}

// Lookup resolves a word to the agent it names, in the agent's own spelling.
type Lookup func(word string) (agent string, ok bool)

// Read reports whether text holds the trigger, spelled as any of handles,
// and what it asks. lookup decides which words name an agent; nil names
// none.
func Read(text string, handles []string, lookup Lookup) (Ask, bool) {
	start, end, ok := Find(text, handles...)
	if !ok {
		return Ask{}, false
	}
	after := text[end:]
	agent, n := Leading(after, lookup)
	if strings.TrimSpace(text[:start]) != "" {
		// Inside a sentence the trigger is part of what was said.
		return Ask{Agent: agent, Text: strings.TrimSpace(text)}, true
	}
	return Ask{Agent: agent, Text: trimAddress(after[n:])}, true
}

// Find locates the first trigger in text: "@" and one of handles, in any
// case, as a whole word, outside anything that quotes or shows text rather
// than says it (prose). start and end are the byte offsets of the "@" and one
// past the name.
//
// A trigger is a whole word: the "@" follows the start of the text or a rune
// that is neither a name character nor "/", "." or "@" (so not
// "https://github.com/@humagen" or "ops@humagen"), and the name is followed
// by the end of the text, a space, or punctuation other than "/" and a "."
// that runs on into a word (so not "@humagen.dev" or "@humagen/reviewers").
func Find(text string, handles ...string) (start, end int, ok bool) {
	names := make([]string, 0, len(handles))
	for _, h := range handles {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			names = append(names, h)
		}
	}
	if len(names) == 0 {
		return 0, 0, false
	}
	lower := strings.ToLower(text)
	shown := hidden(text)
	for i := 0; i < len(text); i++ {
		if text[i] != '@' || shown[i] {
			continue
		}
		if i > 0 && (nameByte(lower[i-1]) || lower[i-1] == '.' || lower[i-1] == '@' || lower[i-1] == '/') {
			continue
		}
		for _, name := range names {
			e := i + 1 + len(name)
			if e > len(text) || lower[i+1:e] != name {
				continue
			}
			// The app's login as its payloads spell it is the same account.
			if strings.HasPrefix(lower[e:], "[bot]") {
				e += len("[bot]")
			}
			if e < len(text) && !endsWord(lower, e) {
				continue
			}
			return i, e, true
		}
	}
	return 0, 0, false
}

// endsWord reports whether a name may end at text[e:]: a space, or
// punctuation other than "/" and a "." followed by more of a word.
func endsWord(text string, e int) bool {
	switch b := text[e]; {
	case nameByte(b), b == '/', b == '@':
		return false
	case b == '.':
		return e+1 == len(text) || isSpace(text[e+1])
	}
	return true
}

// hidden marks the bytes of text that are not prose: what a person shows or
// quotes rather than says. They are fenced and indented code blocks, inline
// code spans (across lines, within a paragraph), <code> and <pre> elements,
// HTML comments, and quoted lines with the lines a quote runs on to. A line
// indented under a list item is the item's paragraph, not code.
func hidden(text string) []bool {
	out := make([]bool, len(text))
	mark := func(from, to int) {
		for i := from; i < to && i < len(text); i++ {
			out[i] = true
		}
	}
	lower := strings.ToLower(text)
	// Elements and comments, wherever they sit.
	for _, pair := range [][2]string{{"<!--", "-->"}, {"<code", "</code>"}, {"<pre", "</pre>"}} {
		for from := 0; ; {
			i := strings.Index(lower[from:], pair[0])
			if i < 0 {
				break
			}
			i += from
			// A tag is its whole name: "<pre>" and "<code class=x>", not
			// "<pre-commit>" or "<codebase>".
			if after := i + len(pair[0]); pair[0] != "<!--" && (after >= len(text) || text[after] != '>' && !isSpace(text[after])) {
				from = after
				continue
			}
			j := strings.Index(lower[i+len(pair[0]):], pair[1])
			end := len(text)
			if j >= 0 {
				end = i + len(pair[0]) + j + len(pair[1])
			}
			mark(i, end)
			from = end
		}
	}
	// Blocks, line by line.
	fence, quoted, prevBlank, inCode, inList := "", false, true, false, false
	for off := 0; off < len(text); {
		lineEnd, next := len(text), len(text)
		if nl := strings.IndexByte(text[off:], '\n'); nl >= 0 {
			lineEnd, next = off+nl, off+nl+1
		}
		raw := text[off:lineEnd]
		line := strings.TrimSpace(raw)
		blank := line == ""
		indented := !blank && indentOf(raw) >= 4
		wasCode := inCode
		inCode = false
		// A list item runs on through blank lines and its indented
		// paragraphs, until a line that is neither starts at the margin.
		switch {
		case !blank && !indented && listItem(line):
			inList = true
		case !blank && indentOf(raw) == 0 && prevBlank:
			inList = false
		}
		switch {
		case fence != "":
			mark(off, next)
			if strings.HasPrefix(line, fence) && strings.Trim(line, fence[:1]) == "" {
				fence = ""
			}
		case !indented && fenceOf(line) != "":
			fence = fenceOf(line)
			quoted = false
			mark(off, next)
		case blank:
			quoted = false
			// A blank line inside an indented block does not end it.
			inCode = wasCode
		case indented && !quoted && !inList && (prevBlank || wasCode):
			// An indented code block starts after a blank line.
			mark(off, next)
			inCode = true
		case strings.HasPrefix(line, ">") && indentOf(raw) < 4:
			quoted = true
			mark(off, next)
		case quoted:
			// A line that runs on from a quote is part of it.
			mark(off, next)
		}
		prevBlank = blank
		off = next
	}
	// Inline code spans in what is left, which may cross lines but not a
	// blank one.
	for i := 0; i < len(text); i++ {
		if text[i] != '`' || out[i] {
			continue
		}
		n := i
		for n < len(text) && text[n] == '`' {
			n++
		}
		run := text[i:n]
		close := -1
		for k := n; k < len(text); k++ {
			if out[k] || text[k] == '\n' && blankAfter(text, k) {
				break
			}
			if text[k] != '`' {
				continue
			}
			m := k
			for m < len(text) && text[m] == '`' {
				m++
			}
			if m-k == len(run) {
				close = m
				break
			}
			k = m - 1
		}
		if close < 0 {
			i = n - 1
			continue
		}
		mark(i, close)
		i = close - 1
	}
	return out
}

// Said is what a person said in text once the lines they quote are taken
// out, and whether that is all the text held besides. plain is false when
// the text also shows something rather than saying it: code, an HTML
// comment, or a line a quote runs on to. A caller that reads a whole
// message as one command reads said, and only when plain is true.
func Said(text string) (said string, plain bool) {
	mask := hidden(text)
	var b strings.Builder
	plain = true
	for off := 0; off < len(text); {
		lineEnd, next := len(text), len(text)
		if nl := strings.IndexByte(text[off:], '\n'); nl >= 0 {
			lineEnd, next = off+nl, off+nl+1
		}
		shown := 0
		for i := off; i < lineEnd; i++ {
			if mask[i] {
				shown++
			}
		}
		switch {
		case shown == 0:
			b.WriteString(text[off:next])
		case shown == lineEnd-off && strings.HasPrefix(strings.TrimSpace(text[off:lineEnd]), ">"):
			// A quoted line: what the person replies to.
		default:
			plain = false
		}
		off = next
	}
	return b.String(), plain
}

// listItem reports whether a line starts a list item: "- ", "* ", "+ ",
// "1. " or "1) ".
func listItem(line string) bool {
	if len(line) >= 2 && strings.ContainsRune("-*+", rune(line[0])) && isSpace(line[1]) {
		return true
	}
	i := 0
	for i < len(line) && i < 9 && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	return i > 0 && i+1 < len(line) && (line[i] == '.' || line[i] == ')') && isSpace(line[i+1])
}

// blankAfter reports whether the line after the newline at text[nl] is
// blank: the end of a paragraph.
func blankAfter(text string, nl int) bool {
	for k := nl + 1; k < len(text); k++ {
		switch text[k] {
		case '\n':
			return true
		case ' ', '\t', '\r':
			continue
		}
		return false
	}
	return true
}

// indentOf is how far a line is indented, a tab counting as four.
func indentOf(line string) int {
	n := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

func isSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

// Leading reads an agent named at the start of s, written so that it can
// only be a name: "@docs-bot ...", "ask docs-bot ...", or "docs-bot: ..." and
// "docs-bot, ..." with the punctuation that sets a name apart. A bare word
// with no such punctuation is part of the sentence ("docs are stale"), even
// when an agent has that name. n is how many bytes of s the name, its "ask"
// and the space after it take. With no name, agent is "" and n is 0.
func Leading(s string, lookup Lookup) (agent string, n int) {
	if lookup == nil {
		return "", 0
	}
	off := skipAddress(s, 0)
	word, after := wordAt(s, off)
	if strings.EqualFold(word, "ask") {
		if next, end := wordAt(s, skipSpace(s, after)); next != "" {
			if a, ok := resolve(next, lookup); ok {
				return a, skipAddress(s, end)
			}
		}
	}
	if word == "" || !strings.HasPrefix(word, "@") && !setApart(s, after) {
		return "", 0
	}
	if a, ok := resolve(word, lookup); ok {
		return a, skipAddress(s, after)
	}
	return "", 0
}

// setApart reports whether the word ending at s[at] is followed by the
// punctuation people put after a name they address: ":" or ",".
func setApart(s string, at int) bool {
	return at < len(s) && (s[at] == ':' || s[at] == ',')
}

// resolve looks a word up, with or without the "@" a person may type.
func resolve(word string, lookup Lookup) (string, bool) {
	name := strings.TrimPrefix(word, "@")
	if name == "" || !validName(name) {
		return "", false
	}
	return lookup(name)
}

// wordAt is the word that starts at off, an optional "@" and name
// characters, and the offset just past it.
func wordAt(s string, off int) (string, int) {
	end := off
	if end < len(s) && s[end] == '@' {
		end++
	}
	for end < len(s) && nameByte(lowerByte(s[end])) {
		end++
	}
	if end == off || end == off+1 && s[off] == '@' {
		return "", off
	}
	if end < len(s) && !boundary(s[end]) {
		return "", off
	}
	return s[off:end], end
}

// boundary reports whether b may end a word that names an agent.
func boundary(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', ':', ',', ';', '.', '!', '?':
		return true
	}
	return false
}

// skipAddress skips the spaces and the punctuation people put after a name:
// "@humagen: go" and "@humagen, docs-bot - go".
func skipAddress(s string, off int) int {
	for off < len(s) {
		switch s[off] {
		case ' ', '\t', '\n', '\r', ':', ',', ';', '-':
			off++
		default:
			return off
		}
	}
	return off
}

func skipSpace(s string, off int) int {
	for off < len(s) && (s[off] == ' ' || s[off] == '\t') {
		off++
	}
	return off
}

// trimAddress is what follows an address: the text with the punctuation
// left by the name taken off its front.
func trimAddress(s string) string {
	return strings.TrimSpace(s[skipAddress(s, 0):])
}

// fenceOf is the fence a line opens, "" when it opens none.
func fenceOf(line string) string {
	if line == "" || (line[0] != '`' && line[0] != '~') {
		return ""
	}
	n := 0
	for n < len(line) && line[n] == line[0] {
		n++
	}
	if n < 3 {
		return ""
	}
	return line[:n]
}

func validName(name string) bool {
	for i := 0; i < len(name); i++ {
		if !nameByte(lowerByte(name[i])) {
			return false
		}
	}
	return name != ""
}

func nameByte(b byte) bool {
	return b == '-' || b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z'
}

func lowerByte(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}
