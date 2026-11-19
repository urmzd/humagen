package sessionimport

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits bound a session's structure. Each is what the server the structure
// is sent to accepts.
type Limits struct {
	// Bytes is the most the structure may be as JSON.
	Bytes int
	// Turns is the most turns it may keep.
	Turns int
	// TurnBytes is the most text one turn may keep.
	TurnBytes int
	// ID is the most the session's id may be.
	ID int
}

// Structure is a session's main path as data, for a server to keep with the
// document Markdown makes of the same session: who said what and when,
// without anything a reader would have to find in the document's text.
type Structure struct {
	Tool      string           `json:"tool"`
	ID        string           `json:"id"`
	Child     bool             `json:"child,omitempty"`
	StartedAt *time.Time       `json:"started_at,omitempty"`
	EndedAt   *time.Time       `json:"ended_at,omitempty"`
	Turns     []StructureTurn  `json:"turns"`
	Branches  int              `json:"branches"`
	Omitted   StructureOmitted `json:"omitted"`
}

// StructureTurn is one turn of the main path. Speaker is a Turn's Role.
type StructureTurn struct {
	Speaker string     `json:"speaker"`
	At      *time.Time `json:"at,omitempty"`
	Text    string     `json:"text"`
	// Cut is set when Text is the opening of a longer turn.
	Cut bool `json:"cut,omitempty"`
}

// StructureOmitted counts what a structure leaves out of the main path or
// cut. A nil count is one that cannot be known, never zero. A structure this
// package makes knows every count; a nil one is for a reader that cannot.
type StructureOmitted struct {
	// Turns is how many turns other than tool results were left out to fit.
	Turns *int `json:"turns"`
	// Results is how many tool results were left out to fit.
	Results *int `json:"results"`
	// CutTurns is how many kept turns hold only their opening: a tool
	// result cut to MaxResult when the session was read, or a turn cut to
	// fit.
	CutTurns *int `json:"cut_turns"`
	// Attachments is how many turns of the main path carried files. No file
	// is kept.
	Attachments *int `json:"attachments"`
}

// stamped is a time as a structure keeps it: nil for one the export does
// not state.
func stamped(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	at := t.UTC()
	return &at
}

// Structure is the session's main path within limits, credentials redacted
// as Markdown redacts them. Branches are counted and not kept. A session
// too long for the limits loses, in this order and counted in Omitted: the
// end of any turn longer than TurnBytes, then its tool results, oldest
// first, then its other turns, oldest first. What is kept is the newest of
// the conversation.
//
// ok is false for a session no structure can name within the limits: one
// whose id is too long or holds control characters. Its document is then
// kept with no structure.
func (s Session) Structure(lim Limits) (out Structure, ok bool) {
	if s.ID == "" || len(s.ID) > lim.ID || !utf8.ValidString(s.ID) || strings.ContainsFunc(s.ID, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return Structure{}, false
	}
	out = Structure{Tool: s.Tool, ID: s.ID, Child: s.Child, StartedAt: stamped(s.Started), Turns: []StructureTurn{}, Branches: len(s.Side) + s.Branches}
	var last time.Time
	attachments := 0
	for _, t := range s.Turns {
		if t.At.After(last) {
			last = t.At
		}
		if t.Attached != "" {
			attachments++
		}
		text := strings.TrimSpace(strings.ReplaceAll(strings.ToValidUTF8(Redact(t.Text), ""), "\x00", ""))
		if text == "" {
			continue
		}
		turn := StructureTurn{Speaker: t.Role, At: stamped(t.At), Text: text, Cut: t.Cut}
		if len(text) > lim.TurnBytes {
			n := lim.TurnBytes
			for n > 0 && !utf8.RuneStart(text[n]) {
				n--
			}
			turn.Text, turn.Cut = strings.TrimSpace(text[:n]), true
		}
		out.Turns = append(out.Turns, turn)
	}
	out.EndedAt = stamped(last)
	// The counts are in place before anything is measured, so the size
	// measured is the size sent.
	turns, results, cut := 0, 0, 0
	out.Omitted = StructureOmitted{Turns: &turns, Results: &results, CutTurns: &cut, Attachments: &attachments}
	// Each turn's size as JSON, with the comma after it. Counts are
	// measured at their widest, so dropping a turn never grows the whole.
	const widest = 3 * len("1073741824")
	all := out.Turns
	out.Turns = []StructureTurn{}
	head, _ := json.Marshal(out)
	size, sizes := len(head)+widest, make([]int, len(all))
	for i, t := range all {
		raw, _ := json.Marshal(t)
		sizes[i] = len(raw) + 1
		size += sizes[i]
	}
	keep := make([]bool, len(all))
	kept := len(keep)
	for i := range keep {
		keep[i] = true
	}
	over := func() bool { return size > lim.Bytes || kept > lim.Turns }
	drop := func(only string) {
		for i, t := range all {
			if !over() {
				return
			}
			if !keep[i] || only != "" && t.Speaker != only {
				continue
			}
			keep[i], kept, size = false, kept-1, size-sizes[i]
			if t.Speaker == "result" {
				results++
			} else {
				turns++
			}
		}
	}
	drop("result")
	drop("")
	out.Turns = make([]StructureTurn, 0, kept)
	for i, t := range all {
		if !keep[i] {
			continue
		}
		if t.Cut {
			cut++
		}
		out.Turns = append(out.Turns, t)
	}
	return out, true
}
