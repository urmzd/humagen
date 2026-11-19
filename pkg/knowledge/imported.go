package knowledge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/state"
)

// The tools an imported session comes from.
const (
	ToolClaude   = "claude"
	ToolCodex    = "codex"
	ToolChatGPT  = "chatgpt"
	ToolClaudeAI = "claude-ai"
)

// The speakers of an imported turn: the person, the assistant of the tool
// the session is from, one line naming a tool the assistant called, and what
// a tool gave back.
const (
	SpeakerUser      = "user"
	SpeakerAssistant = "assistant"
	SpeakerTool      = "tool"
	SpeakerResult    = "result"
)

// The bounds of imported structures. A structure past any of them is
// refused with its document, never cut by the store: the import cuts, and
// counts what it cut (Imported.Omitted).
const (
	// MaxImportedTurns is the most turns of the main path one structure keeps.
	MaxImportedTurns = 200
	// MaxImportedBytes is the most one structure may be, as the JSON it is
	// kept as.
	MaxImportedBytes = 128 << 10
	// MaxImportedTurnBytes is the most text one turn keeps.
	MaxImportedTurnBytes = 16 << 10
	// MaxImportedID is the most an original conversation id may be.
	MaxImportedID = 256
	// MaxImportedSourceBytes is the most the structures of one source's
	// documents may be together, as JSON: the same as the text a source
	// holds (MaxImportBytes). A write past it is refused, and an import
	// goes on in the next source, as it does when the text is full.
	MaxImportedSourceBytes = 8 << 20
	// maxImportedCount bounds a count a structure states.
	maxImportedCount = 1 << 30
)

// Imported is the structure of a session an import wrote a document from:
// what the server holds of the conversation without reading the document's
// text, title or URI, which a person may edit.
//
// It is asserted, not verified. The server cannot tell that a session came
// from the tool it names or says what the tool's files said: a structure is
// what a named importer, holding knowledge:import, sent through the import
// routes with the document. The store keeps who that was and when
// (Origin.ImportedBy, Origin.ImportedAt), and trusts the structure only for
// the revision of the document it was written with.
type Imported struct {
	// Tool is the tool the session is from, one of the Tool constants.
	Tool string `json:"tool"`
	// ID is the conversation's id in that tool.
	ID string `json:"id"`
	// Child is set on a session another session started. A person did not
	// write what its user turns say.
	Child bool `json:"child,omitempty"`
	// StartedAt and EndedAt are when the conversation began and when its
	// main path was last added to, as the export states them. Nil is an
	// export that does not say.
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	// Turns are the turns of the main path that were kept, in order.
	Turns []ImportedTurn `json:"turns"`
	// Branches is how many branches of the conversation are not on the main
	// path, every one counted. None of their turns are kept.
	Branches int `json:"branches"`
	// Omitted counts what Turns leaves out of the main path or cut.
	Omitted ImportedOmitted `json:"omitted"`
}

// ImportedTurn is one turn of an imported session's main path.
type ImportedTurn struct {
	// Speaker is one of the Speaker constants.
	Speaker string `json:"speaker"`
	// At is when the export says it was said. Nil is an export that does
	// not say.
	At   *time.Time `json:"at,omitempty"`
	Text string     `json:"text"`
	// Cut is set when Text is the opening of a longer turn.
	Cut bool `json:"cut,omitempty"`
}

// ImportedOmitted counts what an imported structure leaves out or cut. Every
// count is exact: an import counts what it cuts when it reads the export and
// what it leaves out to fit, so none is unknown.
type ImportedOmitted struct {
	// Turns is how many turns of the main path, other than tool results,
	// were left out to fit the bounds. They are the oldest.
	Turns int `json:"turns"`
	// Results is how many tool results were left out to fit the bounds.
	// They go before any other turn does.
	Results int `json:"results"`
	// CutTurns is how many of the kept turns hold only their opening: cut
	// when the export was read, as a long tool result or tool line is, or
	// to fit.
	CutTurns int `json:"cut_turns"`
	// Attachments is how many turns of the main path carried files. No
	// file is kept.
	Attachments int `json:"attachments"`
}

// importedRecord is a structure as kept: with the digest of the document
// revision it was written with, in the one write that wrote that revision,
// and who sent it and when. An ordinary edit of the document marks it
// Edited and drops the structure, so it is never trusted again: only an
// import writes a trusted one.
type importedRecord struct {
	Session  *Imported `json:"session,omitempty"`
	Document string    `json:"document"`
	By       string    `json:"by"`
	At       time.Time `json:"at"`
	Edited   bool      `json:"edited,omitempty"`
}

func importedKey(name string, seq int64) string { return "imported/" + name + "/" + state.Seq(seq) }

// StructureUnknown is why a document has no trusted structure.
type StructureUnknown string

const (
	// NotKept is a document no import kept a structure with: one written
	// before structures were kept, or one written by an ordinary add.
	NotKept StructureUnknown = "not_kept"
	// Changed is a document whose title or text was changed by an ordinary
	// edit since its import, or is no longer the one its structure was
	// written with.
	Changed StructureUnknown = "changed"
	// Malformed is a kept structure that is not one this version accepts:
	// one that names no importer, or is past the bounds.
	Malformed StructureUnknown = "malformed"
)

// Origin is one document and what is known of the session it was imported
// from. Session is nil when the structure is unknown, and Unknown says why:
// a caller says so, and never reads a structure out of the document's text.
type Origin struct {
	// Document is the revision the structure was read with. Its Digest
	// names that revision.
	Document Document
	// Session is the structure, asserted by ImportedBy at ImportedAt.
	Session *Imported
	Unknown StructureUnknown
	// ImportedBy is the subject that sent the structure through an import
	// route, and ImportedAt when. They are set whenever a structure was
	// kept, so a caller can name its importer even once it is Changed.
	ImportedBy string
	ImportedAt *time.Time
}

func validTool(tool string) bool {
	return tool == ToolClaude || tool == ToolCodex || tool == ToolChatGPT || tool == ToolClaudeAI
}

func validSpeaker(s string) bool {
	return s == SpeakerUser || s == SpeakerAssistant || s == SpeakerTool || s == SpeakerResult
}

func validCount(n int) bool { return n >= 0 && n <= maxImportedCount }

// plain reports whether s is text a structure may hold: valid UTF-8 with no
// zero byte.
func plain(s string) bool { return utf8.ValidString(s) && !strings.ContainsRune(s, 0) }

// importedSize is a structure's size as JSON, which the bounds measure.
func importedSize(im Imported) int {
	raw, err := json.Marshal(im)
	if err != nil {
		return MaxImportedBytes + 1
	}
	return len(raw)
}

// CheckImported reports whether a structure is one a document of this URI
// may be kept with: of a known tool, of the conversation the URI names, and
// within the bounds.
func CheckImported(im Imported, uri string) error {
	bad := func(what string) error { return fmt.Errorf("%w: imported session: %s", ErrInvalid, what) }
	switch {
	case !validTool(im.Tool):
		return bad("tool is one of claude, codex, chatgpt, claude-ai")
	case im.ID == "" || len(im.ID) > MaxImportedID || !plain(im.ID) || strings.ContainsFunc(im.ID, func(r rune) bool { return r < 0x20 || r == 0x7f }):
		return bad(fmt.Sprintf("id is 1 to %d bytes with no control characters", MaxImportedID))
	case uri != im.Tool+":"+im.ID:
		return bad("the document's uri is the tool, a colon and the id")
	case len(im.Turns) > MaxImportedTurns:
		return bad(fmt.Sprintf("at most %d turns", MaxImportedTurns))
	case !validCount(im.Branches):
		return bad("branches is a count")
	case !validCount(im.Omitted.Turns) || !validCount(im.Omitted.Results) || !validCount(im.Omitted.CutTurns) || !validCount(im.Omitted.Attachments):
		return bad("each omitted count is a count")
	}
	for _, t := range im.Turns {
		switch {
		case !validSpeaker(t.Speaker):
			return bad("a turn's speaker is one of user, assistant, tool, result")
		case t.Text == "" || len(t.Text) > MaxImportedTurnBytes || !plain(t.Text):
			return bad(fmt.Sprintf("a turn's text is 1 to %d bytes of text", MaxImportedTurnBytes))
		}
	}
	if importedSize(im) > MaxImportedBytes {
		return bad(fmt.Sprintf("at most %d bytes", MaxImportedBytes))
	}
	return nil
}

// ParseImported reads a structure as an import route takes it: every field
// the contract requires is there and not null (tool, id, turns, branches,
// omitted and each of its counts, and each turn's speaker and text), and no
// field it does not name. A thin structure is refused, never read as zeros.
func ParseImported(raw []byte) (Imported, error) {
	bad := func(what string) error { return fmt.Errorf("%w: imported session: %s", ErrInvalid, what) }
	var w struct {
		Tool      *string    `json:"tool"`
		ID        *string    `json:"id"`
		Child     *bool      `json:"child"`
		StartedAt *time.Time `json:"started_at"`
		EndedAt   *time.Time `json:"ended_at"`
		Turns     *[]struct {
			Speaker *string    `json:"speaker"`
			At      *time.Time `json:"at"`
			Text    *string    `json:"text"`
			Cut     *bool      `json:"cut"`
		} `json:"turns"`
		Branches *int `json:"branches"`
		Omitted  *struct {
			Turns       *int `json:"turns"`
			Results     *int `json:"results"`
			CutTurns    *int `json:"cut_turns"`
			Attachments *int `json:"attachments"`
		} `json:"omitted"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w); err != nil {
		return Imported{}, bad("not the expected JSON: " + err.Error())
	}
	switch {
	case w.Tool == nil || w.ID == nil:
		return Imported{}, bad("tool and id are required")
	case w.Turns == nil:
		return Imported{}, bad("turns is required")
	case w.Branches == nil:
		return Imported{}, bad("branches is required")
	case w.Omitted == nil || w.Omitted.Turns == nil || w.Omitted.Results == nil || w.Omitted.CutTurns == nil || w.Omitted.Attachments == nil:
		return Imported{}, bad("omitted and each of its counts (turns, results, cut_turns, attachments) are required")
	}
	o := w.Omitted
	im := Imported{Tool: *w.Tool, ID: *w.ID, StartedAt: w.StartedAt, EndedAt: w.EndedAt, Turns: make([]ImportedTurn, 0, len(*w.Turns)), Branches: *w.Branches,
		Omitted: ImportedOmitted{Turns: *o.Turns, Results: *o.Results, CutTurns: *o.CutTurns, Attachments: *o.Attachments}}
	if w.Child != nil {
		im.Child = *w.Child
	}
	for _, t := range *w.Turns {
		if t.Speaker == nil || t.Text == nil {
			return Imported{}, bad("each turn's speaker and text are required")
		}
		turn := ImportedTurn{Speaker: *t.Speaker, At: t.At, Text: *t.Text}
		if t.Cut != nil {
			turn.Cut = *t.Cut
		}
		im.Turns = append(im.Turns, turn)
	}
	return im, nil
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	at := *t
	return &at
}

func cloneImported(im Imported) Imported {
	im.StartedAt, im.EndedAt = cloneTime(im.StartedAt), cloneTime(im.EndedAt)
	im.Turns = slices.Clone(im.Turns)
	if im.Turns == nil {
		im.Turns = []ImportedTurn{}
	}
	for i := range im.Turns {
		im.Turns[i].At = cloneTime(im.Turns[i].At)
	}
	return im
}

// structureBytes is what the structures of a source's documents come to,
// with one document's counted as size in place of what it was.
func structureBytes(docs []docRow, seq int64, size int) int {
	total := size
	for _, d := range docs {
		if d.seq != seq {
			total += d.doc.StructureBytes
		}
	}
	return total
}

func sourceStructuresFull() error {
	return fmt.Errorf("%w: source limit reached for imported session structures: at most %d bytes in one source", ErrInvalid, MaxImportedSourceBytes)
}

// AddImportedForScope is AddForScope for a document an import writes from a
// session, with the session's structure, as by asserts it. The document and
// its structure are one write: neither is kept without the other. Only an
// import route calls it.
func (s *Store) AddImportedForScope(tenant, name, title, uri, text, by string, at time.Time, expected *SourceConfig, session Imported) (Document, error) {
	if err := CheckImported(session, uri); err != nil {
		return Document{}, err
	}
	if strings.TrimSpace(by) == "" {
		return Document{}, fmt.Errorf("%w: imported session: it names no importer", ErrInvalid)
	}
	return s.add(tenant, name, title, uri, text, by, at, expected, true, &session)
}

// ReplaceImportedForScope is ReplaceForScope for a document an import
// brings up to what its session now reads as, with the session's structure
// as it now is, as by asserts it. The condition on the digest, the document
// and the structure are one write: a refused replacement leaves both as
// they were. The structure must be of the conversation the document's URI
// names. Only an import route calls it.
func (s *Store) ReplaceImportedForScope(tenant, name, id, title, text, digest, by string, at time.Time, expected *SourceConfig, session Imported) (Document, error) {
	if strings.TrimSpace(by) == "" {
		return Document{}, fmt.Errorf("%w: imported session: it names no importer", ErrInvalid)
	}
	return s.replace(tenant, name, id, title, text, digest, at, expected, &session, by)
}

// OriginForScope reads one document and the structure of the session it was
// imported from, from a source whose configuration is still expected. The
// structure is given only when an import wrote it with the revision of the
// document that is read and no ordinary edit came after: otherwise it is
// unknown, and Origin.Unknown says why. A provider's retained copy is not
// read here (ErrUpstreamRead), and an excluded document reads as missing.
//
// The document and the structure are read at one version of the source's
// record, which every write to either moves, so they are of one revision or
// the read is made again.
func (s *Store) OriginForScope(tenant, name, id string, expected *SourceConfig) (Origin, error) {
	for range state.MaxTries {
		meta, at, err := s.meta(tenant, name)
		if err != nil {
			return Origin{}, err
		}
		if !sameConfiguration(meta, expected) {
			return Origin{}, ErrNotFound
		}
		if meta.RequiresUpstreamRead() {
			return Origin{}, ErrUpstreamRead
		}
		docs, err := s.docs(tenant, name)
		if err != nil {
			return Origin{}, err
		}
		i := slices.IndexFunc(docs, func(d docRow) bool { return d.doc.ID == id })
		if i < 0 || docs[i].doc.Excluded {
			return Origin{}, fmt.Errorf("%w: document %q", ErrNotFound, id)
		}
		row := docs[i]
		kept, err := s.kept.Lookup(tenant, importedKey(name, row.seq))
		missing := errors.Is(err, state.ErrNotFound)
		if err != nil && !missing {
			return Origin{}, err
		}
		if _, again, err := s.meta(tenant, name); err != nil {
			return Origin{}, err
		} else if again != at {
			continue
		}
		return originOf(row.doc, kept.Value.Imported, missing), nil
	}
	return Origin{}, state.ErrBusy
}

// originOf decides what is known of a document's session from the record
// kept with it.
func originOf(doc Document, rec *importedRecord, missing bool) Origin {
	out := Origin{Document: doc}
	if missing || rec == nil {
		out.Unknown = NotKept
		return out
	}
	out.ImportedBy = rec.By
	if !rec.At.IsZero() {
		out.ImportedAt = cloneTime(&rec.At)
	}
	switch {
	case rec.Edited || rec.Document != doc.Digest:
		out.Unknown = Changed
	case rec.Session == nil || rec.By == "" || rec.At.IsZero() || CheckImported(*rec.Session, doc.URI) != nil:
		out.Unknown = Malformed
	default:
		session := cloneImported(*rec.Session)
		out.Session = &session
	}
	return out
}
