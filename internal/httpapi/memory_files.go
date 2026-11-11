package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/drive"
)

// memoryTree is a tree's version as the files routes answer it.
type memoryTree struct {
	Tree      drive.Tree       `json:"tree"`
	Mount     string           `json:"mount"`
	Version   int64            `json:"version"`
	By        string           `json:"by,omitempty"`
	At        time.Time        `json:"at,omitzero"`
	Entries   []drive.Entry    `json:"entries"`
	Proposals []drive.Proposal `json:"proposals"`
	// CanWrite says whether the caller may write a file in this tree, by the
	// check the write route makes.
	CanWrite bool `json:"can_write"`
}

type memoryFile struct {
	Path    string `json:"path"`
	Hash    string `json:"hash"`
	Size    int64  `json:"size"`
	Version int64  `json:"version"`
	// Content is the file as text, or empty for one that is not text.
	Content string `json:"content"`
	Binary  bool   `json:"binary,omitempty"`
}

type memoryRestore struct {
	Scope   string `json:"scope"`
	Project string `json:"project,omitempty"`
	Agent   string `json:"agent,omitempty"`
	Version int64  `json:"version"`
}

// memoryWriteBody bounds a file write's request. A UTF-8 file can expand to
// six JSON bytes per input byte.
const memoryWriteBody = 6*drive.MaxFileBytes + 4096

// memoryWrite is one file's new content, and the tree version the edit was
// made against.
type memoryWrite struct {
	Scope       string `json:"scope"`
	Project     string `json:"project,omitempty"`
	Agent       string `json:"agent,omitempty"`
	File        string `json:"file"`
	Content     string `json:"content"`
	BaseVersion int64  `json:"base_version"`
}

// driveProblem answers a drive error.
func (s *Server) driveProblem(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, drive.ErrNotFound):
		notFound(w)
	case errors.Is(err, drive.ErrForbidden):
		fail(w, http.StatusForbidden, "you may not change this memory")
	case errors.Is(err, drive.ErrConflict):
		fail(w, http.StatusConflict, "the file changed since it was read")
	case errors.Is(err, drive.ErrInvalid), errors.Is(err, drive.ErrTooLarge), errors.Is(err, drive.ErrRefused):
		fail(w, http.StatusBadRequest, err.Error())
	default:
		s.problem(w, err)
	}
}

func queryInt(r *http.Request, name string) (int64, bool) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return 0, true
	}
	n, err := strconv.ParseInt(v, 10, 64)
	return n, err == nil && n >= 0
}

func (s *Server) memoryTreeOf(w http.ResponseWriter, r *http.Request, who auth.Principal) (drive.Tree, bool) {
	q := r.URL.Query()
	t, err := platform.MemoryTree(who, q.Get("scope"), q.Get("project"), q.Get("agent"))
	if err != nil {
		s.problem(w, err)
		return drive.Tree{}, false
	}
	return t, true
}

func (s *Server) viewMemoryTree(w http.ResponseWriter, r *http.Request, who auth.Principal, t drive.Tree, version int64) {
	actor := s.P.MemoryActor(who)
	var v drive.Version
	var err error
	if version == 0 {
		v, err = s.P.Drive.Head(r.Context(), who.Tenant, actor, t)
	} else {
		v, err = s.P.Drive.Version(r.Context(), who.Tenant, actor, t, version)
	}
	if err != nil {
		s.driveProblem(w, err)
		return
	}
	proposals, err := s.P.Drive.Proposals(who.Tenant, actor, t)
	if err != nil {
		s.driveProblem(w, err)
		return
	}
	ok(w, 200, memoryTree{Tree: t, Mount: "/" + t.Mount(), Version: v.N, By: v.By, At: v.At, Entries: v.Entries, Proposals: proposals, CanWrite: s.P.MayWriteMemory(r.Context(), who, t)})
}

// listMemoryFiles answers a memory tree's manifest at a version, the head by
// default, and its open proposals.
func (s *Server) listMemoryFiles(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	t, good := s.memoryTreeOf(w, r, who)
	if !good {
		return
	}
	version, valid := queryInt(r, "version")
	if !valid {
		fail(w, 400, "version is a whole number")
		return
	}
	s.viewMemoryTree(w, r, who, t, version)
}

func (s *Server) readMemoryFile(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	t, good := s.memoryTreeOf(w, r, who)
	if !good {
		return
	}
	version, valid := queryInt(r, "version")
	if !valid {
		fail(w, 400, "version is a whole number")
		return
	}
	b, e, err := s.P.Drive.ReadFile(r.Context(), who.Tenant, s.P.MemoryActor(who), t, version, r.URL.Query().Get("file"))
	if err != nil {
		s.driveProblem(w, err)
		return
	}
	out := memoryFile{Path: e.Path, Hash: e.Hash, Size: e.Size, Version: version}
	if utf8.Valid(b) {
		out.Content = string(b)
	} else {
		out.Binary = true
	}
	ok(w, 200, out)
}

func (s *Server) memoryHistory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	t, good := s.memoryTreeOf(w, r, who)
	if !good {
		return
	}
	before, okBefore := queryInt(r, "before")
	limit, okLimit := queryInt(r, "limit")
	if !okBefore || !okLimit || limit > drive.MaxPage {
		fail(w, 400, "before is a version and limit at most 100")
		return
	}
	versions, err := s.P.Drive.History(r.Context(), who.Tenant, s.P.MemoryActor(who), t, before, int(limit))
	if err != nil {
		s.driveProblem(w, err)
		return
	}
	ok(w, 200, map[string]any{"versions": versions})
}

// writeMemoryFile writes one text file as the tree's newest version. Only
// whoever may write the tree directly is answered: nothing is proposed here.
// A file that changed since the version the edit was made against is a
// conflict, and nothing is written.
func (s *Server) writeMemoryFile(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body memoryWrite
	if !decode(w, r, &body) {
		return
	}
	t, err := platform.MemoryTree(who, body.Scope, body.Project, body.Agent)
	if err != nil {
		s.problem(w, err)
		return
	}
	file, err := drive.CleanPath(body.File)
	if err != nil {
		fail(w, 400, "file is a path inside the tree")
		return
	}
	// A negative base would skip the check against the version read.
	if body.BaseVersion < 0 {
		fail(w, 400, "base_version is the version the file was read at")
		return
	}
	if len(body.Content) > drive.MaxFileBytes {
		fail(w, 400, "content is at most "+strconv.Itoa(drive.MaxFileBytes)+" bytes")
		return
	}
	if !utf8.ValidString(body.Content) || strings.ContainsRune(body.Content, 0) {
		fail(w, 400, "content is text")
		return
	}
	actor := s.P.MemoryActor(who)
	// A tree the caller may not read is 404, before anything is recorded.
	if _, err := s.P.Drive.Head(r.Context(), who.Tenant, actor, t); err != nil {
		s.driveProblem(w, err)
		return
	}
	attempt, err := s.P.BeginAudit(who, audit.Change{Action: "memory.write", Resource: "memory", ID: t.Key(), Subject: who.Subject, Project: t.Project, Agent: t.Agent, Route: "PUT /api/memory/files/content", Rule: string(auth.RunsWrite), After: map[string]string{"file": file, "base_version": strconv.FormatInt(body.BaseVersion, 10)}})
	if err != nil {
		s.problem(w, err)
		return
	}
	if s.P.MayWriteMemory(r.Context(), who, t) {
		_, err = s.P.Drive.Commit(r.Context(), who.Tenant, actor, t, drive.CommitInput{Base: body.BaseVersion, Changes: []drive.Change{{Path: file, Data: []byte(body.Content)}}, By: who.Subject, At: time.Now(), Origin: "api"})
	} else {
		err = drive.ErrForbidden
	}
	if auditErr := attempt.Finish(err); auditErr != nil {
		s.P.LogAuditCompletionFailure(auditErr)
	}
	if err != nil {
		s.driveProblem(w, err)
		return
	}
	s.viewMemoryTree(w, r, who, t, 0)
}

// restoreMemory checks out an earlier version as the tree's newest.
func (s *Server) restoreMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body memoryRestore
	if !decode(w, r, &body) {
		return
	}
	t, err := platform.MemoryTree(who, body.Scope, body.Project, body.Agent)
	if err != nil {
		s.problem(w, err)
		return
	}
	if body.Version <= 0 {
		fail(w, 400, "version names an earlier version")
		return
	}
	actor := s.P.MemoryActor(who)
	if _, err := s.P.Drive.Head(r.Context(), who.Tenant, actor, t); err != nil {
		s.driveProblem(w, err)
		return
	}
	attempt, err := s.P.BeginAudit(who, audit.Change{Action: "memory.restore", Resource: "memory", ID: t.Key(), Subject: who.Subject, Project: t.Project, Agent: t.Agent, Route: "POST /api/memory/files/restore", Rule: string(auth.RunsWrite), After: map[string]string{"version": strconv.FormatInt(body.Version, 10)}})
	if err != nil {
		s.problem(w, err)
		return
	}
	_, err = s.P.Drive.Restore(r.Context(), who.Tenant, actor, t, body.Version, who.Subject, time.Now())
	if auditErr := attempt.Finish(err); auditErr != nil {
		s.P.LogAuditCompletionFailure(auditErr)
	}
	if err != nil {
		s.driveProblem(w, err)
		return
	}
	s.viewMemoryTree(w, r, who, t, 0)
}

// searchMemory finds files in the memory the caller may read.
func (s *Server) searchMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	q := r.URL.Query().Get("q")
	limit, valid := queryInt(r, "limit")
	if q == "" || !valid || limit > drive.MaxPage {
		fail(w, 400, "q is required and limit at most 100")
		return
	}
	hits, err := s.P.SearchMemory(r.Context(), who, q, int(limit))
	if err != nil {
		s.driveProblem(w, err)
		return
	}
	ok(w, 200, map[string]any{"hits": hits})
}

type memoryDecision struct {
	Scope    string `json:"scope"`
	Project  string `json:"project,omitempty"`
	Agent    string `json:"agent,omitempty"`
	Decision string `json:"decision"`
}

// decideMemoryProposal approves or discards a proposal to a memory tree:
// what a run changed in memory it may only propose to.
func (s *Server) decideMemoryProposal(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body memoryDecision
	if !decode(w, r, &body) {
		return
	}
	t, err := platform.MemoryTree(who, body.Scope, body.Project, body.Agent)
	if err != nil {
		s.problem(w, err)
		return
	}
	if body.Decision != "approve" && body.Decision != "discard" {
		fail(w, 400, "decision is approve or discard")
		return
	}
	actor := s.P.MemoryActor(who)
	id := r.PathValue("id")
	attempt, err := s.P.BeginAudit(who, audit.Change{Action: "memory.proposal." + body.Decision, Resource: "memory", ID: id, Subject: who.Subject, Project: t.Project, Agent: t.Agent, Route: "POST /api/memory/files/proposals/{id}", Rule: string(auth.RunsWrite)})
	if err != nil {
		s.problem(w, err)
		return
	}
	if body.Decision == "approve" {
		_, err = s.P.Drive.Approve(r.Context(), who.Tenant, actor, t, id, who.Subject, time.Now())
	} else {
		err = s.P.Drive.Discard(r.Context(), who.Tenant, actor, t, id, "api")
	}
	if auditErr := attempt.Finish(err); auditErr != nil {
		s.P.LogAuditCompletionFailure(auditErr)
	}
	if err != nil {
		s.driveProblem(w, err)
		return
	}
	s.viewMemoryTree(w, r, who, t, 0)
}
