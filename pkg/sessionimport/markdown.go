package sessionimport

import (
	"fmt"
	"regexp"
	"strings"
	"time"
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

// cut is how much of a session a rendering leaves out to fit a limit.
type cut struct {
	// answer is the most bytes kept of one answer. 0 keeps all of it, with
	// every tool result and every tool call; anything else leaves the
	// results out and keeps six calls of a run of them.
	answer int
	// noSide leaves the branches out, and names the turns they fork after.
	noSide bool
}

// Markdown is the session as one document: a heading with where it came
// from, then each turn of the main path with its time and original ids,
// then each branch in a section of its own, credentials redacted. It is at
// most limit bytes. A longer session keeps everything the person asked for
// as long as it fits, and cuts in this order, saying so in its heading: the
// tool results and each answer to its opening, then the branches, then the
// middle of the session.
func (s Session) Markdown(limit int) string {
	if limit < MinMarkdown {
		limit = MinMarkdown
	}
	var out string
	for _, c := range []cut{{}, {answer: 4000}, {answer: 1200}, {answer: 400}, {answer: 400, noSide: true}} {
		if out = s.render(c); len(out) <= limit {
			return out
		}
	}
	// Still too long: keep the start and the end, where a session says what
	// it set out to do and where it got to.
	const mark = "\n\n[… the middle of this session is left out …]\n\n"
	head := (limit - len(mark)) * 2 / 3
	tail := limit - len(mark) - head
	return strings.ToValidUTF8(out[:head], "") + mark + strings.ToValidUTF8(out[len(out)-tail:], "")
}

// stamp is a turn's time and original ids as its heading shows them: the
// time the export states, the message's id, and for a tool call or result
// the call's id. It is "" for a turn whose export has none of them.
func stamp(t Turn) string {
	var parts []string
	if !t.At.IsZero() {
		parts = append(parts, t.At.UTC().Format(time.RFC3339))
	}
	if t.ID != "" {
		parts = append(parts, "id="+ident(t.ID))
	}
	if t.Call != "" {
		parts = append(parts, "call="+ident(t.Call))
	}
	if len(parts) == 0 {
		return ""
	}
	return " [" + strings.Join(parts, " ") + "]"
}

// order says how the turns are ordered, and what their times do not say.
func (s Session) order() string {
	total, untimed, early := 0, 0, 0
	see := func(ts []Turn) {
		var last time.Time
		for _, t := range ts {
			total++
			switch {
			case t.At.IsZero():
				untimed++
			case t.At.Before(last):
				early++
			default:
				last = t.At
			}
		}
	}
	see(s.Turns)
	for _, b := range s.Side {
		see(b.Turns)
	}
	var out string
	switch {
	case s.Causal:
		out = "each turn follows the turn it replies to"
		if early > 0 {
			out += fmt.Sprintf("; %d stamped earlier than a turn before them keep that place and show the time the export states", early)
		}
	case untimed == total:
		return "as in the export, which gives no times"
	default:
		out = "by time, a tool's result after its call"
	}
	switch {
	case untimed == 0:
	case s.Causal:
		out += fmt.Sprintf("; %d have no time in the export", untimed)
	default:
		out += fmt.Sprintf("; %d with no time follow the turn before them in the export", untimed)
	}
	return out
}

// render writes the session with what c leaves out.
func (s Session) render(c cut) string {
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
	fmt.Fprintf(&b, "- Order: %s\n", s.order())
	switch {
	case len(s.Side) == 0:
	case c.noSide:
		var from []string
		for i, br := range s.Side {
			if i == 10 {
				from = append(from, fmt.Sprintf("and %d more", len(s.Side)-i))
				break
			}
			from = append(from, forksAfter(br))
		}
		fmt.Fprintf(&b, "- Branches: %d, left out to fit the size limit (%s)\n", len(s.Side), strings.Join(from, "; "))
	default:
		fmt.Fprintf(&b, "- Branches: %d, each in its own section after the main path\n", len(s.Side))
	}
	if s.Branches > 0 {
		fmt.Fprintf(&b, "- Branches not shown: %d, past the %d kept\n", s.Branches, MaxBranches)
	}
	if c.answer > 0 {
		fmt.Fprintf(&b, "- Shortened to fit the size limit: tool results left out, answers cut to %d bytes, runs of tool calls to 6\n", c.answer)
	}
	turnsTo(&b, s.Turns, "##", c)
	if c.noSide {
		return b.String()
	}
	for i, br := range s.Side {
		fmt.Fprintf(&b, "\n## Branch %d: %s\n", i+1, forksAfter(br))
		turnsTo(&b, br.Turns, "###", c)
	}
	return b.String()
}

// forksAfter says where a branch leaves the conversation.
func forksAfter(br Branch) string {
	switch {
	case br.From == "":
		return "starts the conversation again"
	case br.In > 0:
		return fmt.Sprintf("forks after id=%s in branch %d", br.From, br.In)
	}
	return "forks after id=" + br.From
}

// turnsTo writes turns under headings of the level h.
func turnsTo(b *strings.Builder, ts []Turn, h string, c cut) {
	for i := 0; i < len(ts); i++ {
		t := ts[i]
		text := Redact(t.Text)
		files := Redact(t.Attached)
		if files != "" && text != "" {
			files = "\n\n" + files
		}
		switch t.Role {
		case "user":
			fmt.Fprintf(b, "\n%s Asked%s\n\n%s%s\n", h, stamp(t), text, files)
		case "assistant":
			if c.answer > 0 {
				text = clip(text, c.answer)
			}
			fmt.Fprintf(b, "\n%s Answered%s\n\n%s%s\n", h, stamp(t), text, files)
		case "tool":
			// Calls made one after another are one list.
			end := i
			for end+1 < len(ts) && ts[end+1].Role == "tool" {
				end++
			}
			b.WriteString("\nTools used:\n")
			for n, call := range ts[i : end+1] {
				if c.answer > 0 && n == 6 {
					fmt.Fprintf(b, "- and %d more\n", end+1-i-n)
					break
				}
				for _, line := range strings.Split(Redact(call.Text), "\n") {
					fmt.Fprintf(b, "- %s%s\n", line, stamp(call))
				}
			}
			i = end
		case "result":
			// A result is the first thing cut from a session that is too
			// long: what was asked and answered matters more.
			if c.answer > 0 {
				continue
			}
			fmt.Fprintf(b, "\nTool result%s:\n\n%s\n", stamp(t), indent(text))
		}
	}
}

// indent sets a tool's output apart from the session's own headings, which
// it may itself hold.
func indent(s string) string {
	return "    " + strings.ReplaceAll(s, "\n", "\n    ")
}
