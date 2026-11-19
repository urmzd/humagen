package platform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/threads"
)

// Continuing an imported conversation: a new private thread started from a
// knowledge document an import wrote from a session of another tool, with
// the newest turns of that session's main path as passive context
// (threads.Import), as a fork is started with its source's history.

// ContinueImportInput is what a person chose to continue, as they read it,
// and how the new conversation is answered.
type ContinueImportInput struct {
	// Source and Document name the document.
	Source   string `json:"source"`
	Document string `json:"document"`
	// DocumentDigest is the revision the person read (knowledge.Document's
	// Digest), and StructureDigest its structure's
	// (knowledge.Origin.StructureDigest), "" for a document whose
	// structure is not kept. Both must still be the document's.
	DocumentDigest  string `json:"document_digest"`
	StructureDigest string `json:"structure_digest,omitempty"`
	// Project is the project the person saw the source filed in, "~" or ""
	// for the workspace itself. Nil leaves it to the source: the project
	// it belongs to or is attached to, else the workspace itself.
	Project *string  `json:"project,omitempty"`
	Title   string   `json:"title,omitempty"`
	Agents  []string `json:"agents,omitempty"`
	Mode    string   `json:"mode,omitempty"`
}

// Why a continuation is refused. Each is also the class a route answers
// with: threads.ErrVersion for what changed since it was read, and
// threads.ErrInvalid for what cannot be continued.
var (
	// ErrImportChanged: the document, or its structure, is not the
	// revision the person read.
	ErrImportChanged = fmt.Errorf("%w: the imported document changed since it was read; read it again", threads.ErrVersion)
	// ErrImportDetached: the source is no longer filed in the project the
	// person saw it in.
	ErrImportDetached = fmt.Errorf("%w: the source is no longer filed in that project", threads.ErrVersion)
	// ErrImportUntrusted: the document's structure was kept, and is not one
	// that can be trusted now: edited since its import, or malformed.
	ErrImportUntrusted = fmt.Errorf("%w: the imported conversation's structure cannot be trusted", threads.ErrInvalid)
	// ErrImportNotSession: the document is not one an import wrote from a
	// session of a tool it knows.
	ErrImportNotSession = fmt.Errorf("%w: the document is not an imported conversation", threads.ErrInvalid)
)

// sessionURIs are the uri schemes an import writes a session's document
// with. Only a document of one of them is continued without a structure,
// as one block whose speakers are not known: its uri is a person's to edit,
// so it decides only that the document is offered, never what its text
// is said to be.
var sessionURIs = []string{threads.ImportClaude + ":", threads.ImportCodex + ":", threads.ImportChatGPT + ":", threads.ImportClaudeAI + ":"}

// ContinueImport starts, as who, a private conversation that continues an
// imported one: who's own, in the source's project, with the newest turns
// of the conversation's main path the agent is sent ahead of the thread's
// own (threads.SeedImport): at most half the workspace's history limit as
// the transcript is written, and at most a fork's bounds. It starts no
// agent and copies no action, and it is audited
// (conversation.continue_import).
//
// The document's structure must be one an import kept with the revision
// read (ImportedOrigin), or none: a document no import kept a structure
// with is continued as one block of its text, labelled as an imported
// document whose speakers are not known. A structure kept and since edited,
// or malformed, is refused (ErrImportUntrusted), as is a document that is
// not an imported session's (ErrImportNotSession).
//
// It is refused when the document or its structure is no longer the one
// the person read (ErrImportChanged), when the source is no longer filed in
// the project they saw it in (ErrImportDetached), when the document is
// excluded or gone, the source deleted or hidden from who, or another
// workspace's (threads.ErrNotFound), and when who may not start a run
// (runs:write) or post in the project (MayPost). The document and the source are read again just before
// the thread is written, and both checks made again.
func (p *Platform) ContinueImport(ctx context.Context, who auth.Principal, in ContinueImportInput) (threads.Thread, error) {
	// The route that will start one checks runs:write; it is checked here
	// too, as ImportDocument checks knowledge:write beside its route's
	// permission, and reading the source takes knowledge:read (SourceAs).
	if !who.Can(auth.RunsWrite) {
		return threads.Thread{}, fmt.Errorf("%w: you need runs:write", auth.ErrForbidden)
	}
	if !validDigestText(in.DocumentDigest) || (in.StructureDigest != "" && !validDigestText(in.StructureDigest)) {
		return threads.Thread{}, fmt.Errorf("%w: name the document's digest, and its structure's when it is kept, as read", threads.ErrInvalid)
	}
	if _, _, _, _, err := p.author(who, nil); err != nil {
		return threads.Thread{}, err
	}
	person, err := p.poster(who, nil)
	if err != nil {
		return threads.Thread{}, err
	}
	origin, project, err := p.pinnedOrigin(who, in)
	if err != nil {
		return threads.Thread{}, err
	}
	if err := p.MayPost(person, Place{Project: project}); err != nil {
		return threads.Thread{}, err
	}
	agents, mode, err := p.checkRouting(ctx, who.Tenant, project, in.Agents, in.Mode, p.Seer(who).Agent)
	if err != nil {
		return threads.Thread{}, err
	}
	record, err := p.importRecord(who, in, origin)
	if err != nil {
		return threads.Thread{}, err
	}
	title := in.Title
	if title == "" {
		title = boundedTitle(origin.Document.Title)
	}
	id, err := newThreadID()
	if err != nil {
		return threads.Thread{}, err
	}
	after := map[string]string{"source": in.Source, "document": in.Document, "document_digest": in.DocumentDigest, "structure": record.Structure}
	if record.StructureDigest != "" {
		after["structure_digest"] = record.StructureDigest
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "conversation.continue_import", Resource: "conversation", Scope: "personal", Subject: who.Subject, ID: id, Project: project, Thread: id, After: after})
	if err != nil {
		return threads.Thread{}, err
	}
	// Read again just before the write: what changed meanwhile refuses it.
	if _, again, err := p.pinnedOrigin(who, in); err == nil && again != project {
		err = ErrImportDetached
		_ = attempt.Finish(err)
		return threads.Thread{}, err
	} else if err != nil {
		_ = attempt.Finish(err)
		return threads.Thread{}, err
	}
	record.At = p.now()
	result, err := p.Threads.Create(who.Tenant, threads.Thread{ID: id, Project: project, Channel: projects.General, Title: title, Visibility: threads.Private, CreatedBy: person.Subject, Agents: agents, Mode: mode, Import: record}, record.At)
	_ = attempt.Finish(err)
	return result, err
}

// pinnedOrigin reads, as who, the document a continuation names, and where
// the continuation is filed, and checks both are what the person read.
func (p *Platform) pinnedOrigin(who auth.Principal, in ContinueImportInput) (knowledge.Origin, string, error) {
	src, err := p.SourceAs(who, in.Source)
	if err != nil {
		return knowledge.Origin{}, "", fmt.Errorf("%w: source %q", threads.ErrNotFound, in.Source)
	}
	origin, err := p.ImportedOrigin(who, in.Source, in.Document)
	switch {
	case errors.Is(err, knowledge.ErrNotFound):
		return knowledge.Origin{}, "", fmt.Errorf("%w: document %q", threads.ErrNotFound, in.Document)
	case errors.Is(err, knowledge.ErrUpstreamRead):
		return knowledge.Origin{}, "", fmt.Errorf("%w: a provider's copy holds no imported conversation", ErrImportNotSession)
	case err != nil:
		return knowledge.Origin{}, "", err
	}
	if origin.Document.Digest == "" || origin.Document.Digest != in.DocumentDigest {
		return knowledge.Origin{}, "", ErrImportChanged
	}
	switch {
	case origin.Session != nil:
		if in.StructureDigest != origin.StructureDigest {
			return knowledge.Origin{}, "", ErrImportChanged
		}
	case origin.Unknown == knowledge.NotKept:
		if in.StructureDigest != "" {
			return knowledge.Origin{}, "", ErrImportChanged
		}
		if !slices.ContainsFunc(sessionURIs, func(scheme string) bool { return strings.HasPrefix(origin.Document.URI, scheme) }) {
			return knowledge.Origin{}, "", ErrImportNotSession
		}
	default:
		return knowledge.Origin{}, "", fmt.Errorf("%w: %s", ErrImportUntrusted, origin.Unknown)
	}
	project, err := p.importLanding(who, src, in.Project)
	return origin, project, err
}

// importLanding is the project a continuation of a source's document is
// filed in: the project the source belongs to (its scope), else the one it
// is attached to, else the workspace itself. A source attached to more than
// one project needs the person to name one. A project the person names must
// be one of those, or the workspace itself for a source filed nowhere;
// otherwise the source was detached since they saw it there.
func (p *Platform) importLanding(who auth.Principal, src knowledge.Source, chosen *string) (string, error) {
	var places []string
	if src.Lifecycle != nil && src.Lifecycle.Project != "" {
		places = []string{src.Lifecycle.Project}
	} else {
		attached, err := p.SourceAttachmentsAs(who, src.Name)
		if err != nil {
			return "", err
		}
		for _, a := range attached {
			if name := projects.NameOf(a.Project); !slices.Contains(places, name) {
				places = append(places, name)
			}
		}
	}
	if chosen != nil {
		want := projects.NameOf(*chosen)
		if slices.Contains(places, want) || (len(places) == 0 && want == projects.Root) {
			return want, nil
		}
		return "", ErrImportDetached
	}
	switch len(places) {
	case 0:
		return projects.Root, nil
	case 1:
		return places[0], nil
	}
	return "", fmt.Errorf("%w: the source is attached to more than one project; name the one to continue in", threads.ErrInvalid)
}

// importRecord is the imported origin a continuation keeps: the pinned
// revision, and the newest turns that fit the workspace's history budget.
func (p *Platform) importRecord(who auth.Principal, in ContinueImportInput, origin knowledge.Origin) (*threads.Import, error) {
	budget := p.Settings(who.Tenant).HistoryLimit / 2
	doc := origin.Document
	record := &threads.Import{Source: in.Source, Document: doc.ID, URI: doc.URI, Title: strings.Join(strings.Fields(doc.Title), " "), DocumentDigest: doc.Digest, By: who.Subject}
	if len(record.Title) > 1024 {
		record.Title = boundedTitle(record.Title)
	}
	if origin.Session == nil {
		text, err := p.documentText(who, in.Source, doc, budget)
		if err != nil {
			return nil, err
		}
		context, omitted, err := threads.SeedImport("", false, []threads.ImportTurn{{Speaker: threads.ImportDocument, Text: text}}, budget)
		if err != nil {
			return nil, err
		}
		record.Structure, record.Context, record.Omitted = threads.StructureNotKept, context, omitted
		return record, nil
	}
	s := origin.Session
	turns := make([]threads.ImportTurn, 0, len(s.Turns))
	for _, t := range s.Turns {
		turns = append(turns, threads.ImportTurn{Speaker: t.Speaker, At: t.At, Text: t.Text, Cut: t.Cut})
	}
	context, omitted, err := threads.SeedImport(s.Tool, s.Child, turns, budget)
	if err != nil {
		return nil, err
	}
	branches := s.Branches
	record.Structure, record.Tool, record.Child = threads.StructureKept, s.Tool, s.Child
	record.StructureDigest, record.ImportedBy, record.ImportedAt = origin.StructureDigest, origin.ImportedBy, origin.ImportedAt
	record.StartedAt, record.EndedAt = s.StartedAt, s.EndedAt
	record.Context, record.Omitted, record.Branches = context, omitted, &branches
	record.ImportCut = &threads.ImportCut{Turns: s.Omitted.Turns, Results: s.Omitted.Results, CutTurns: s.Omitted.CutTurns, Attachments: s.Omitted.Attachments}
	return record, nil
}

// documentText is the text of a document whose structure is not kept, at
// the revision read, cut to at most limit bytes: no more of it is kept.
func (p *Platform) documentText(who auth.Principal, source string, doc knowledge.Document, limit int) (string, error) {
	src, err := p.SourceAs(who, source)
	if err != nil {
		return "", fmt.Errorf("%w: source %q", threads.ErrNotFound, source)
	}
	var expected *knowledge.SourceConfig
	if src.Lifecycle != nil {
		cfg := src.Lifecycle.SourceConfig
		expected = &cfg
	}
	read, texts, err := p.Knowledge.DocumentTextForScope(who.Tenant, source, doc.ID, expected)
	switch {
	case errors.Is(err, knowledge.ErrNotFound):
		return "", fmt.Errorf("%w: document %q", threads.ErrNotFound, doc.ID)
	case err != nil:
		return "", err
	case read.Digest != doc.Digest:
		return "", ErrImportChanged
	}
	text := strings.Join(texts, "\n\n")
	if len(text) > limit {
		text = text[:limit]
	}
	return text, nil
}

func validDigestText(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && strings.ToLower(s) == s
}

// newThreadID is a new thread's id, named before it is written so its audit
// record can name it.
func newThreadID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "th-" + hex.EncodeToString(b[:]), nil
}

// importSourceVisible reports whether the reader still sees the source a
// continuation was imported from: as SourceVisible decides, read through the
// seer, so a read it grants by exception is recorded as the seer records
// others. A source deleted since is seen by nobody.
func (s *Seer) importSourceVisible(name string) bool {
	if !s.who.Can(auth.KnowledgeRead) {
		return false
	}
	src, err := s.p.Knowledge.Get(s.who.Tenant, name)
	if err != nil {
		return false
	}
	return src.Lifecycle == nil || src.Lifecycle.Project == "" || s.placeDirect(Place{Project: src.Lifecycle.Project})
}
