package sessionimport

import (
	"fmt"
	"regexp"
	"strings"
)

// secrets match credentials in the shapes their issuers give them. A session
// is a record of a terminal, and a key pasted into one should not travel on
// with it.
var secrets = []*regexp.Regexp{
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{30,}`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{30,}`),
	regexp.MustCompile(`\bxox[abeprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`\bxapp-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`),
	regexp.MustCompile(`\bAIza[A-Za-z0-9_-]{35}\b`),
	regexp.MustCompile(`\bops_[A-Za-z0-9_-]{40,}`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`),
}

// Redact replaces what looks like a credential with a mark.
func Redact(s string) string {
	for _, re := range secrets {
		s = re.ReplaceAllString(s, "[redacted]")
	}
	return s
}

// MinMarkdown is the least a Markdown limit may be.
const MinMarkdown = 2048

// Markdown is the session as one document: a heading with where it came
// from, then each turn in order, credentials redacted. It is at most limit
// bytes. A longer session keeps everything the person asked for as long as
// it fits, and cuts the assistant's answers and its tool calls first: each
// to its opening, then the middle of the session.
func (s Session) Markdown(limit int) string {
	if limit < MinMarkdown {
		limit = MinMarkdown
	}
	for _, answer := range []int{0, 4000, 1200, 400} {
		if out := s.render(answer); len(out) <= limit || answer == 400 {
			if len(out) <= limit {
				return out
			}
			// Still too long: keep the start and the end, where a session
			// says what it set out to do and where it got to.
			const mark = "\n\n[… the middle of this session is left out …]\n\n"
			head := (limit - len(mark)) * 2 / 3
			tail := limit - len(mark) - head
			return strings.ToValidUTF8(out[:head], "") + mark + strings.ToValidUTF8(out[len(out)-tail:], "")
		}
	}
	return ""
}

// render writes the session with each answer cut to answer bytes; 0 cuts
// nothing.
func (s Session) render(answer int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", s.Title)
	fmt.Fprintf(&b, "- Source: %s session %s\n", s.Tool, s.ID)
	if s.Dir != "" {
		fmt.Fprintf(&b, "- Directory: %s\n", s.Dir)
	}
	if !s.Started.IsZero() {
		fmt.Fprintf(&b, "- Started: %s\n", s.Started.UTC().Format("2006-01-02 15:04 UTC"))
	}
	if s.Parent != "" {
		fmt.Fprintf(&b, "- Started by: %s\n", s.Parent)
	}
	if s.Branches > 0 {
		fmt.Fprintf(&b, "- Branches not shown: %d\n", s.Branches)
	}
	for _, t := range s.Turns {
		text := Redact(t.Text)
		switch t.Role {
		case "user":
			fmt.Fprintf(&b, "\n## Asked\n\n%s\n", text)
		case "assistant":
			if answer > 0 {
				text = clip(text, answer)
			}
			fmt.Fprintf(&b, "\n## Answered\n\n%s\n", text)
		case "tool":
			calls := strings.Split(text, "\n")
			if answer > 0 && len(calls) > 6 {
				calls = append(calls[:6], fmt.Sprintf("and %d more", len(calls)-6))
			}
			b.WriteString("\nTools used:\n")
			for _, c := range calls {
				fmt.Fprintf(&b, "- %s\n", c)
			}
		case "result":
			// A result is the first thing cut from a session that is too
			// long: what was asked and answered matters more.
			if answer > 0 {
				continue
			}
			fmt.Fprintf(&b, "\nTool result:\n\n%s\n", indent(text))
		}
	}
	return b.String()
}

// indent sets a tool's output apart from the session's own headings, which
// it may itself hold.
func indent(s string) string {
	return "    " + strings.ReplaceAll(s, "\n", "\n    ")
}
