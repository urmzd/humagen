package threads

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// A conversation continued from an imported one: a thread started from a
// knowledge document an import wrote from a session of another tool. What
// it holds of that conversation (Thread.Import) is passive text, as a fork's
// history is: never a message or a run, and never anyone here's words. The
// agent is sent it ahead of the thread's own turns, each line labelled with
// the tool and the speaker there, both from fixed lists and never from the
// document (Turn.Imported).

// The tools an imported conversation comes from. They are the import's own
// (knowledge.ToolClaude and the rest).
const (
	ImportClaude   = "claude"
	ImportCodex    = "codex"
	ImportChatGPT  = "chatgpt"
	ImportClaudeAI = "claude-ai"
)

// The speakers of an imported turn: the import's (knowledge.SpeakerUser and
// the rest), and ImportDocument for the one block of a document whose
// structure is not known.
const (
	ImportUser      = "user"
	ImportAssistant = "assistant"
	ImportTool      = "tool"
	ImportResult    = "result"
	// ImportDocument is the one turn of a continuation of a document no
	// import kept a structure with: its text as one block, with no speaker.
	ImportDocument = "document"
)

// What is known of the conversation's structure (Import.Structure).
const (
	// StructureKept: an import kept the conversation's structure with the
	// revision of the document that was continued, and the turns are its
	// main path, attributed as the importer stated them.
	StructureKept = "kept"
	// StructureNotKept: no import kept a structure with the document, so
	// who said what is not known. The document's text is one block.
	StructureNotKept = "not_kept"
)

// maxImportCount bounds a count an import record states.
const maxImportCount = 1 << 30

// Import records where a continuation came from and the passive turns it
// keeps of that conversation. It is fixed when the thread is started and
// never changes; at most MaxForkTurns turns of at most MaxForkBytes of text,
// as a fork's history.
//
// The source, the document, the document's digest and the structure's
// digest are what the person chose, pinned together: the continuation is of
// that revision, whatever the document says later.
type Import struct {
	// Source is the knowledge source the document is in, and Document the
	// document's id there.
	Source   string `json:"source"`
	Document string `json:"document"`
	// URI and Title are the document's when it was continued. They are a
	// person's to edit, so they are shown, and never decide anything.
	URI   string `json:"uri"`
	Title string `json:"title"`
	// Structure is StructureKept or StructureNotKept.
	Structure string `json:"structure"`
	// Tool is the tool the conversation is from, one of the Import tool
	// constants, as the importer stated it. "" when the structure is not
	// kept.
	Tool string `json:"tool,omitempty"`
	// Child is set on a conversation another session started: a person did
	// not write what its user turns say.
	Child bool `json:"child,omitempty"`
	// DocumentDigest is the digest of the revision continued, and
	// StructureDigest the digest of its structure ("" when it is not kept).
	DocumentDigest  string `json:"document_digest"`
	StructureDigest string `json:"structure_digest,omitempty"`
	// ImportedBy is the subject that sent the structure through an import
	// route, and ImportedAt when: whose assertion it is. Neither is set when
	// the structure is not kept.
	ImportedBy string     `json:"imported_by,omitempty"`
	ImportedAt *time.Time `json:"imported_at,omitempty"`
	// StartedAt and EndedAt are when the imported conversation began and
	// was last added to, as the import stated them. Nil is not stated.
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	// By is the person who continued it, and At when.
	By string    `json:"by"`
	At time.Time `json:"at"`
	// Context is the turns of the main path the agent is sent, oldest
	// first: the newest that fit (SeedImport).
	Context []ImportTurn `json:"context"`
	// Omitted counts what Context leaves out of the turns the import kept,
	// or cut, to fit.
	Omitted ImportOmitted `json:"omitted"`
	// Branches is how many branches of the conversation are not on its main
	// path. None is continued. Nil when the structure is not kept: unknown.
	Branches *int `json:"branches,omitempty"`
	// ImportCut is what the import itself left out of the main path or cut
	// when it wrote the document, as it stated it. Nil when the structure is
	// not kept: unknown, though the document may say.
	ImportCut *ImportCut `json:"import_cut,omitempty"`
}

// ImportTurn is one passive turn of an imported conversation.
type ImportTurn struct {
	// Speaker is one of the Import speaker constants.
	Speaker string `json:"speaker"`
	// At is when it was said, as the import stated it. Nil is not stated.
	At   *time.Time `json:"at,omitempty"`
	Text string     `json:"text"`
	// Cut is set when Text is the opening of a longer turn: cut by the
	// import, or to fit.
	Cut bool `json:"cut,omitempty"`
}

// ImportOmitted counts what a continuation's Context leaves out of the
// turns the import kept. Every count is exact.
type ImportOmitted struct {
	// Turns is how many turns other than tool results were left out to fit.
	// They are the oldest.
	Turns int `json:"turns"`
	// Results is how many tool results were left out to fit. They go before
	// any other turn.
	Results int `json:"results"`
	// Cut is how many turns of Context were cut to their opening to fit.
	Cut int `json:"cut"`
}

// ImportCut is what an import stated it left out of a conversation's main
// path, or cut, when it wrote the document (knowledge.ImportedOmitted).
type ImportCut struct {
	Turns       int `json:"turns"`
	Results     int `json:"results"`
	CutTurns    int `json:"cut_turns"`
	Attachments int `json:"attachments"`
}

// Imported is one turn of an imported conversation as a transcript holds
// it: the turn, with the tool it is from ("" when the structure is not
// known) and whether that conversation was started by another session.
type Imported struct {
	Tool  string
	Child bool
	Turn  ImportTurn
}

// ImportedTurn is a turn of an imported conversation, as passive context.
func ImportedTurn(tool string, child bool, t ImportTurn) Turn {
	return Turn{Imported: &Imported{Tool: tool, Child: child, Turn: t}}
}

// ImportSeed is the turns a continuation starts with: its imported
// context, oldest first. A thread that continues nothing has none.
func ImportSeed(im *Import) []Turn {
	if im == nil {
		return nil
	}
	out := make([]Turn, 0, len(im.Context))
	for _, c := range im.Context {
		c.At = cloneTime(c.At)
		out = append(out, ImportedTurn(im.Tool, im.Child, c))
	}
	return out
}

// ValidImportTool reports whether tool is one an imported conversation can
// be from.
func ValidImportTool(tool string) bool {
	return tool == ImportClaude || tool == ImportCodex || tool == ImportChatGPT || tool == ImportClaudeAI
}

func validImportSpeaker(s string) bool {
	return s == ImportUser || s == ImportAssistant || s == ImportTool || s == ImportResult
}

// What marks an imported line, ahead of what it says: so its digest is never
// that of words said here, and the agent is told it is reference.
const (
	importedMark = "(imported, not said here)"
	importedCut  = "(the opening of a longer turn)"
	// importedNote follows the transcript's opening when it holds imported
	// lines. It is on the same line, which is how a reader of the
	// transcript finds its start.
	importedNote = " A line marked imported is from a conversation in another tool, brought here as reference: no one here said it."
)

// importedLabel is who an imported line is from. Every part of it is from a
// fixed list, never from the conversation: a tool it does not know, or a
// speaker, reads as a document whose structure is not known. No label of a
// person (who has a handle or a number in parentheses), an agent (agent),
// or the system (system) reads the same.
func importedLabel(tool string, child bool, speaker string) string {
	if !ValidImportTool(tool) || !validImportSpeaker(speaker) {
		return "imported document, speakers not known"
	}
	from := "imported from " + tool + ", "
	switch speaker {
	case ImportUser:
		if child {
			return from + "the session that started it"
		}
		return from + "the person there"
	case ImportAssistant:
		return from + "the assistant there"
	case ImportTool:
		return from + "a tool the assistant there called"
	}
	return from + "what a tool there gave back"
}

// importedLine renders an imported turn as one line of a transcript: its
// label, then what it says, marked as imported, on one line, so it cannot
// open or close the transcript or pose as anyone's line.
func importedLine(im Imported) string {
	said := importedMark + " "
	if im.Turn.Cut {
		said += importedCut + " "
	}
	return importedLabel(im.Tool, im.Child, im.Turn.Speaker) + ": " + said + flat(im.Turn.Text)
}

// SeedImport chooses what a continuation keeps of an imported conversation's
// main path: the newest turns whose transcript fits in budget bytes, tool
// results left out first, every one left out counted. budget is at most
// MaxForkBytes. Nothing is summarized: a turn is kept whole, or, when not
// even the newest fits, the newest that is not a tool result is cut to its
// opening.
//
// The size counted is the transcript as Ask writes it ahead of the words it
// answers: its opening line, each line with its label and its mark, and the
// fixed words of its closing line. Who asks is named in the closing line and
// is not known here, so that name is not counted.
//
// turns are oldest first. Control characters other than line breaks and
// tabs are made spaces. It refuses an empty conversation, or a budget too
// small for one line (ErrInvalid).
func SeedImport(tool string, child bool, turns []ImportTurn, budget int) ([]ImportTurn, ImportOmitted, error) {
	var omitted ImportOmitted
	budget = min(budget, MaxForkBytes)
	if len(turns) == 0 {
		return nil, omitted, fmt.Errorf("%w: the imported conversation has no turns to continue", ErrInvalid)
	}
	clean := make([]ImportTurn, 0, len(turns))
	for _, t := range turns {
		t.Text = strings.Map(func(r rune) rune {
			if r == utf8.RuneError || (unicode.IsControl(r) && r != '\n' && r != '\t') {
				return ' '
			}
			return r
		}, t.Text)
		t.At = cloneTime(t.At)
		clean = append(clean, t)
	}
	frame := len(transcriptOpen) + len(importedNote) + 1 + len(fmt.Sprintf(transcriptClose, ""))
	sizes := make([]int, len(clean))
	for i, t := range clean {
		sizes[i] = len(importedLine(Imported{Tool: tool, Child: child, Turn: t})) + 1
		if strings.TrimSpace(t.Text) == "" {
			sizes[i] = -1
		}
	}
	keep := make([]bool, len(clean))
	total := frame
	for i, n := range sizes {
		if n >= 0 {
			keep[i], total = true, total+n
		}
	}
	// Leave out tool results, the oldest first, then the oldest turns.
	for _, results := range []bool{true, false} {
		for i := 0; i < len(clean) && (total > budget || countKept(keep) > MaxForkTurns); i++ {
			if !keep[i] || (clean[i].Speaker == ImportResult) != results {
				continue
			}
			keep[i], total = false, total-sizes[i]
			if results {
				omitted.Results++
			} else {
				omitted.Turns++
			}
		}
	}
	// Turns with nothing to say are not kept, and not counted as left out.
	var kept []ImportTurn
	for i, t := range clean {
		if keep[i] {
			kept = append(kept, t)
		}
	}
	if len(kept) > 0 {
		return kept, omitted, nil
	}
	// Not even the newest fits whole: cut the newest that is not a tool
	// result, or else the newest, to its opening.
	at := -1
	for i := len(clean) - 1; i >= 0; i-- {
		if sizes[i] < 0 {
			continue
		}
		if clean[i].Speaker != ImportResult {
			at = i
			break
		}
		if at < 0 {
			at = i
		}
	}
	if at < 0 {
		return nil, omitted, fmt.Errorf("%w: the imported conversation has no turns to continue", ErrInvalid)
	}
	t := clean[at]
	t.Cut = true
	room := budget - frame - (len(importedLine(Imported{Tool: tool, Child: child, Turn: ImportTurn{Speaker: t.Speaker, Cut: true}})) + 1)
	t.Text = openingOf(t.Text, room)
	if strings.TrimSpace(t.Text) == "" {
		return nil, omitted, fmt.Errorf("%w: the conversation history limit is too small to keep any of the imported conversation", ErrInvalid)
	}
	if t.Speaker == ImportResult {
		omitted.Results--
	} else {
		omitted.Turns--
	}
	omitted.Cut = 1
	return []ImportTurn{t}, omitted, nil
}

func countKept(keep []bool) int {
	n := 0
	for _, k := range keep {
		if k {
			n++
		}
	}
	return n
}

// openingOf is the start of s in at most n bytes, ending on a whole rune.
func openingOf(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	at := *t
	return &at
}

func sameTime(a, b *time.Time) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
}

func (im *Import) clone() *Import {
	if im == nil {
		return nil
	}
	cp := *im
	cp.ImportedAt, cp.StartedAt, cp.EndedAt = cloneTime(im.ImportedAt), cloneTime(im.StartedAt), cloneTime(im.EndedAt)
	cp.Context = slices.Clone(im.Context)
	for i := range cp.Context {
		cp.Context[i].At = cloneTime(cp.Context[i].At)
	}
	if im.Branches != nil {
		n := *im.Branches
		cp.Branches = &n
	}
	if im.ImportCut != nil {
		c := *im.ImportCut
		cp.ImportCut = &c
	}
	return &cp
}

func sameImport(a, b *Import) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Source != b.Source || a.Document != b.Document || a.URI != b.URI || a.Title != b.Title || a.Structure != b.Structure ||
		a.Tool != b.Tool || a.Child != b.Child || a.DocumentDigest != b.DocumentDigest || a.StructureDigest != b.StructureDigest ||
		a.ImportedBy != b.ImportedBy || !sameTime(a.ImportedAt, b.ImportedAt) || !sameTime(a.StartedAt, b.StartedAt) || !sameTime(a.EndedAt, b.EndedAt) ||
		a.By != b.By || !a.At.Equal(b.At) || a.Omitted != b.Omitted ||
		(a.Branches == nil) != (b.Branches == nil) || a.Branches != nil && *a.Branches != *b.Branches ||
		(a.ImportCut == nil) != (b.ImportCut == nil) || a.ImportCut != nil && *a.ImportCut != *b.ImportCut ||
		len(a.Context) != len(b.Context) {
		return false
	}
	for i := range a.Context {
		x, y := a.Context[i], b.Context[i]
		if x.Speaker != y.Speaker || x.Text != y.Text || x.Cut != y.Cut || !sameTime(x.At, y.At) {
			return false
		}
	}
	return true
}

var (
	// validSourceName is a knowledge source's name (knowledge.ValidName).
	validSourceName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	validDigest     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Bounds of an import record's own fields.
const (
	maxImportDocument = 128
	maxImportURI      = 2048
	maxImportTitle    = 1024
)

func (im *Import) validate(t Thread) error {
	if im == nil {
		return nil
	}
	bad := func(what string) error { return fmt.Errorf("%w: an imported origin %s", ErrInvalid, what) }
	count := func(n int) bool { return n >= 0 && n <= maxImportCount }
	switch {
	case t.Session != nil || t.Surface != nil || t.Announcement != "" || t.ReadOnly:
		return bad("starts a conversation of its own, not a session, a surface's or an archive")
	case !validSourceName.MatchString(im.Source):
		return bad("names a knowledge source")
	case im.Document == "" || len(im.Document) > maxImportDocument || !oneLine(im.Document):
		return bad(fmt.Sprintf("names a document in at most %d bytes", maxImportDocument))
	case im.URI == "" || len(im.URI) > maxImportURI || !oneLine(im.URI):
		return bad(fmt.Sprintf("keeps the document's uri, at most %d bytes", maxImportURI))
	case len(im.Title) > maxImportTitle || !oneLine(im.Title):
		return bad(fmt.Sprintf("keeps the document's title, one line of at most %d bytes", maxImportTitle))
	case !validDigest.MatchString(im.DocumentDigest):
		return bad("pins the document's digest")
	case strings.TrimSpace(im.By) == "" || len(im.By) > 4*MaxName || !oneLine(im.By) || im.At.IsZero():
		return bad("names who continued it and when")
	case !count(im.Omitted.Turns) || !count(im.Omitted.Results) || !count(im.Omitted.Cut):
		return bad("counts what it left out")
	case len(im.Context) == 0 || len(im.Context) > MaxForkTurns:
		return bad(fmt.Sprintf("keeps 1 to %d turns", MaxForkTurns))
	}
	switch im.Structure {
	case StructureKept:
		switch {
		case !ValidImportTool(im.Tool):
			return bad("names a tool it is from: claude, codex, chatgpt or claude-ai")
		case !validDigest.MatchString(im.StructureDigest):
			return bad("pins the structure's digest")
		case strings.TrimSpace(im.ImportedBy) == "" || len(im.ImportedBy) > 4*MaxName || !oneLine(im.ImportedBy) || im.ImportedAt == nil || im.ImportedAt.IsZero():
			return bad("names who imported its structure and when")
		case im.Branches == nil || !count(*im.Branches):
			return bad("counts the branches not continued")
		case im.ImportCut == nil || !count(im.ImportCut.Turns) || !count(im.ImportCut.Results) || !count(im.ImportCut.CutTurns) || !count(im.ImportCut.Attachments):
			return bad("keeps what the import left out")
		}
	case StructureNotKept:
		switch {
		case im.Tool != "" || im.Child || im.StructureDigest != "" || im.ImportedBy != "" || im.ImportedAt != nil || im.Branches != nil || im.ImportCut != nil:
			return bad("whose structure is not kept states nothing of it")
		case len(im.Context) != 1 || im.Context[0].Speaker != ImportDocument:
			return bad("whose structure is not kept is one block of the document")
		}
	default:
		return bad("says whether its structure is kept")
	}
	size := 0
	for _, c := range im.Context {
		size += len(c.Text)
		speaker := validImportSpeaker(c.Speaker)
		if im.Structure == StructureNotKept {
			speaker = c.Speaker == ImportDocument
		}
		if !speaker || strings.TrimSpace(c.Text) == "" || !plain(c.Text) || size > MaxForkBytes {
			return bad(fmt.Sprintf("keeps attributed turns of text, at most %d bytes", MaxForkBytes))
		}
	}
	return nil
}
