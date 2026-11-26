package github

import (
	"regexp"
	"strings"
)

// hidden is every HTML comment: machine markers a person never sees.
var hidden = regexp.MustCompile(`(?s)<!--.*?-->`)

// linkDest is a Markdown link's destination, which a person does not read.
var linkDest = regexp.MustCompile(`\]\([^)]*\)`)

// codeText is inline code and fenced blocks, which GitHub shows as typed.
var codeText = regexp.MustCompile("(?s)```.*?```|`[^`\n]*`")

// internalName is an id a person should not see: a run, a session, a tool
// step, a tool name, or the report's own JSON.
var internalName = regexp.MustCompile(`\b(?:s|run|tc|step)(?:_|&#95;)[A-Za-z0-9]+\b|github_read_context|github_read_file|file_read|file_list|session_read|"format"\s*:|\{"|humagen\.review\.v2|humagen\.github\.review|found_by|#[a-z]\b`)

// pings are what GitHub turns into a mention or a reference: @name,
// @org/team, #12, owner/repo#12 and GH-12.
var pings = regexp.MustCompile(`(?i)@[a-z0-9]|#[0-9]|\bgh-[0-9]`)

// fillers open an answer without saying anything.
var fillers = []string{"great question", "i'd be happy to", "i would be happy to", "happy to help", "certainly!", "absolutely!", "let me know if", "hope this helps", "as an ai"}

// VoiceProblems lists what in a rendered GitHub body does not read as a
// colleague wrote it: an em dash, a filler, an internal id, a tool name or
// JSON, a mention or reference GitHub would act on, a wall of emoji, or a
// heading. Hidden markers and link destinations are not read; code is
// read only for ids.
func VoiceProblems(body string) []string {
	visible := linkDest.ReplaceAllString(hidden.ReplaceAllString(body, ""), "]")
	var out []string
	if strings.Contains(visible, "—") {
		out = append(out, "an em dash")
	}
	lower := strings.ToLower(visible)
	for _, f := range fillers {
		if strings.Contains(lower, f) {
			out = append(out, "the filler "+f)
		}
	}
	if m := internalName.FindString(visible); m != "" {
		out = append(out, "the internal name "+m)
	}
	if m := pings.FindString(codeText.ReplaceAllString(visible, "")); m != "" {
		out = append(out, "a mention or reference "+m)
	}
	for _, emoji := range []string{"🔴", "🟡", "🔵", "⚠️", "👍", "👎"} {
		if strings.Contains(visible, emoji) {
			out = append(out, "the emoji "+emoji)
		}
	}
	for _, line := range strings.Split(visible, "\n") {
		if strings.HasPrefix(line, "#") {
			out = append(out, "a heading")
			break
		}
	}
	return out
}
