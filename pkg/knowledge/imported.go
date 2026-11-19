package knowledge

import (
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

// The bounds of one document's imported structure. A structure past any of
// them is refused, never cut by the store: the import cuts, and counts what
// it cut (Imported.Omitted). A source holds at most MaxImportDocuments
// documents, so its structures are at most MaxImportDocuments times
// MaxImportedBytes.
const (
	// MaxImportedTurns is the most turns of the main path one structure keeps.
	MaxImportedTurns = 200
	// MaxImportedBytes is the most a structure may be, as the JSON it is
	// kept as.
	MaxImportedBytes = 128 << 10
	// MaxImportedTurnBytes is the most text one turn keeps.
	MaxImportedTurnBytes = 16 << 10
	// MaxImportedID is the most an original conversation id may be.
	MaxImportedID = 256
	// maxImportedCount bounds a count a structure states.
	maxImportedCount = 1 << 30
)

// Imported is the structure of a session an import wrote a document from:
// what the server knows of the conversation without reading the document's
// text, title or URI, which a person may edit. It is kept with the digest of
// the revision of the document it was written with, and trusted only while
// the document's digest is that one (Store.OriginForScope).
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
	// path. None of their turns are kept.
	Branches int `json:"branches"`
	// Omitted counts what Turns leaves out of the main path.
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

// ImportedOmitted counts what an imported structure leaves out or cut. Each
// count is a number the import knows, or nil when it cannot know: nil is
// never zero.
type ImportedOmitted struct {
	// Turns is how many turns of the main path, other than tool results,
	// were left out to fit the bounds. They are the oldest.
	Turns *int `json:"turns"`
	// Results is how many tool results were left out to fit the bounds.
	// They go before any other turn does.
	Results *int `json:"results"`
	// CutTurns is how many of the kept turns hold only their opening: cut
	// when the export was read, as a long tool result is, or to fit.
	CutTurns *int `json:"cut_turns"`
	// Attachments is how many turns of the main path carried files. No
	// file is kept.
	Attachments *int `json:"attachments"`
}

// importedRecord is a structure as kept: with the digest of the document
// revision it was written with, in the one write that wrote that revision.
type importedRecord struct {
	Session  Imported `json:"session"`
	Document string   `json:"document"`
}

func importedKey(name string, seq int64) string { return "imported/" + name + "/" + state.Seq(seq) }

// StructureUnknown is why a document has no trusted structure.
type StructureUnknown string

const (
	// NotKept is a document no import kept a structure with: one written
	// before structures were kept, or one that is not an imported session.
	NotKept StructureUnknown = "not_kept"
	// Changed is a document whose title or text is no longer the one its
	// structure was written with: edited since its import.
	Changed StructureUnknown = "changed"
	// Malformed is a kept structure that is not one this version accepts.
	Malformed StructureUnknown = "malformed"
)

// Origin is one document and what is known of the session it was imported
// from. Session is nil when the structure is unknown, and Unknown says why:
// a caller says so, and never reads a structure out of the document's text.
type Origin struct {
	// Document is the revision the structure was read with. Its Digest
	// names that revision.
	Document Document
	Session  *Imported
	Unknown  StructureUnknown
}

func validTool(tool string) bool {
	return tool == ToolClaude || tool == ToolCodex || tool == ToolChatGPT || tool == ToolClaudeAI
}

func validSpeaker(s string) bool {
	return s == SpeakerUser || s == SpeakerAssistant || s == SpeakerTool || s == SpeakerResult
}

func validCount(n *int) bool { return n == nil || *n >= 0 && *n <= maxImportedCount }

// plain reports whether s is text a structure may hold: valid UTF-8 with no
// zero byte.
func plain(s string) bool { return utf8.ValidString(s) && !strings.ContainsRune(s, 0) }

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
	case im.Branches < 0 || im.Branches > maxImportedCount:
		return bad("branches is a count")
	case !validCount(im.Omitted.Turns) || !validCount(im.Omitted.Results) || !validCount(im.Omitted.CutTurns) || !validCount(im.Omitted.Attachments):
		return bad("an omitted count is a count, or null when it is not known")
	}
	for _, t := range im.Turns {
		switch {
		case !validSpeaker(t.Speaker):
			return bad("a turn's speaker is one of user, assistant, tool, result")
		case t.Text == "" || len(t.Text) > MaxImportedTurnBytes || !plain(t.Text):
			return bad(fmt.Sprintf("a turn's text is 1 to %d bytes of text", MaxImportedTurnBytes))
		}
	}
	raw, err := json.Marshal(im)
	if err != nil || len(raw) > MaxImportedBytes {
		return bad(fmt.Sprintf("at most %d bytes", MaxImportedBytes))
	}
	return nil
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	at := *t
	return &at
}

func cloneCount(n *int) *int {
	if n == nil {
		return nil
	}
	v := *n
	return &v
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
	o := &im.Omitted
	o.Turns, o.Results, o.CutTurns, o.Attachments = cloneCount(o.Turns), cloneCount(o.Results), cloneCount(o.CutTurns), cloneCount(o.Attachments)
	return im
}

// putImported adds to a batch the structure a write of one document's
// revision carries, kept with that revision's digest. A write that carries
// none leaves what is kept as it is: a structure of an earlier revision no
// longer matches the document's digest, and is not trusted (Changed).
func putImported(b *state.Batch[stored], tenant, name string, seq int64, session *Imported, digest string) {
	if session == nil {
		return
	}
	b.Put(tenant, importedKey(name, seq), stored{Imported: &importedRecord{Session: cloneImported(*session), Document: digest}})
}

// AddImportedForScope is AddForScope for a document an import writes from a
// session, with the session's structure. The document and its structure are
// one write: neither is kept without the other.
func (s *Store) AddImportedForScope(tenant, name, title, uri, text, by string, at time.Time, expected *SourceConfig, session Imported) (Document, error) {
	if err := CheckImported(session, uri); err != nil {
		return Document{}, err
	}
	return s.add(tenant, name, title, uri, text, by, at, expected, true, &session)
}

// ReplaceImportedForScope is ReplaceForScope for a document an import
// brings up to what its session now reads as, with the session's structure
// as it now is. The condition on the digest, the document and the structure
// are one write: a refused replacement leaves both as they were. The
// structure must be of the conversation the document's URI names.
func (s *Store) ReplaceImportedForScope(tenant, name, id, title, text, digest string, at time.Time, expected *SourceConfig, session Imported) (Document, error) {
	return s.replace(tenant, name, id, title, text, digest, at, expected, &session)
}

// OriginForScope reads one document and the structure of the session it was
// imported from, from a source whose configuration is still expected. The
// structure is given only when it was written with the revision of the
// document that is read: otherwise it is unknown, and Origin.Unknown says
// why. A provider's retained copy is not read here (ErrUpstreamRead), and an
// excluded document reads as missing.
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
		out := Origin{Document: row.doc}
		rec := kept.Value.Imported
		switch {
		case missing || rec == nil:
			out.Unknown = NotKept
		case rec.Document == "" || rec.Document != row.doc.Digest:
			out.Unknown = Changed
		case CheckImported(rec.Session, row.doc.URI) != nil:
			out.Unknown = Malformed
		default:
			session := cloneImported(rec.Session)
			out.Session = &session
		}
		return out, nil
	}
	return Origin{}, state.ErrBusy
}
