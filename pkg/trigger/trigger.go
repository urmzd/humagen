// Package trigger reads the one word that asks Humagen on a surface,
// "@humagen", and the agent a person names right after it.
//
// On GitHub an agent's name is not safe to mention: "@github-reviewer" is
// somebody else's account, and GitHub tells them. So a comment asks only
// through the trigger, which names Humagen's own account, and the agent it is
// for is the word after it:
//
//	@humagen what changed?                     the default answers
//	@humagen github-reviewer review this       github-reviewer answers
//	@humagen @docs-bot summarize               docs-bot answers
//	@humagen ask research-bot about the cache  research-bot answers
//
// The trigger counts in any case, as a whole word, anywhere in the text, but
// not inside code or a quoted reply: "`@humagen help`" shows a command, and a
// line that starts with ">" repeats what someone else said.
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
// case, as a whole word, outside code spans, fenced code and quoted lines.
// start and end are the byte offsets of the "@" and one past the name.
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
	fence := ""
	for off := 0; off < len(text); {
		lineEnd, next := len(text), len(text)
		if nl := strings.IndexByte(text[off:], '\n'); nl >= 0 {
			lineEnd, next = off+nl, off+nl+1
		}
		line := strings.TrimSpace(text[off:lineEnd])
		switch {
		case fence != "":
			if strings.HasPrefix(line, fence) && strings.Trim(line, fence[:1]) == "" {
				fence = ""
			}
		case fenceOf(line) != "":
			fence = fenceOf(line)
		case strings.HasPrefix(line, ">"):
			// A quoted reply repeats someone else's words.
		default:
			if s, e, found := findInLine(text, lower, off, lineEnd, names); found {
				return s, e, true
			}
		}
		off = next
	}
	return 0, 0, false
}

// findInLine finds a trigger in text[from:to], one line outside fenced code,
// skipping inline code spans.
func findInLine(text, lower string, from, to int, names []string) (start, end int, ok bool) {
	for i := from; i < to; i++ {
		switch text[i] {
		case '`':
			// An inline code span runs to the next run of as many backticks.
			n := i
			for n < to && text[n] == '`' {
				n++
			}
			run := text[i:n]
			if close := strings.Index(text[n:to], run); close >= 0 {
				i = n + close + len(run) - 1
			} else {
				i = n - 1
			}
		case '@':
			if i > from && (nameByte(lower[i-1]) || lower[i-1] == '.' || lower[i-1] == '@') {
				continue
			}
			for _, name := range names {
				e := i + 1 + len(name)
				if e > to || lower[i+1:e] != name {
					continue
				}
				// "@humagen/reviewers" is a team, not the account.
				if e < to && (nameByte(lower[e]) || lower[e] == '/') {
					continue
				}
				// The app's login as its payloads spell it is the same account.
				if strings.HasPrefix(lower[e:to], "[bot]") {
					e += len("[bot]")
				}
				return i, e, true
			}
		}
	}
	return 0, 0, false
}

// Leading reads an agent named at the start of s: "docs-bot ...",
// "@docs-bot ...", "docs-bot: ..." or "ask docs-bot ...". n is how many
// bytes of s the name, its "ask" and the space after it take. A word lookup
// does not resolve names nobody, and n is 0.
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
	if word == "" {
		return "", 0
	}
	if a, ok := resolve(word, lookup); ok {
		return a, skipAddress(s, after)
	}
	return "", 0
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
