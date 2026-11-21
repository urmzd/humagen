package platform

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"

	"github.com/urmzd/humagen/pkg/call"

	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/humagen/pkg/usage"
)

// Read confinement: one rule decides whether a reader sees something that
// lives in a project, and every read path asks it. What a reader may not see
// answers as what does not exist, 404, so trying learns nothing.
//
// The rule is a pure function, seesPlace. A place is where an object lives:
// its project, and the thread it belongs to when it belongs to one. Runs,
// inbox messages and artifacts each name theirs.

// threadAccess is what reading a thread turns on: whether only its
// participants read it, and who they are, by subject.
type threadAccess struct {
	Private      bool
	Participants []string
	// Client is the client whose surface the thread is on, such as the Slack
	// app's: its machine reads it, as the Slack thread is its own.
	Client string
}

// seesPlace reports whether a reader sees what lives in a place. viewer is
// whether the reader's role in the place's project is at least viewer, and
// th is the place's thread, nil when it has none or none is recorded, which
// reads as shared. A reader holding projects:govern sees everything. Anyone
// else needs a role in the project, and in a private thread to take part in
// it. A reader with no subject takes part in nothing.
func seesPlace(viewer, govern bool, th *threadAccess, subject string) bool {
	switch {
	case govern:
		return true
	case !viewer:
		return false
	case th == nil || !th.Private:
		return true
	}
	return subject != "" && slices.Contains(th.Participants, subject)
}

// Where a housekeeping run lives: the turn it serves, whose words it is
// sent. Only the platform writes these, on runs of origin system.
const (
	metaPlaceProject = "warp.place.project"
	metaPlaceThread  = "warp.place.thread"
)

// placeOfRun is where a run lives: its conversation, in the project it is
// filed in. A run another run called has its root's, and a housekeeping run
// the turn's it serves.
func placeOfRun(r runs.Run) Place {
	if r.Origin == runs.OriginSystem {
		if meta := wire.Meta(r.Tag()); meta[metaPlaceThread] != "" {
			return Place{Project: meta[metaPlaceProject], Thread: meta[metaPlaceThread]}
		}
	}
	return Place{Project: r.Project, Thread: r.Thread}
}

// placeOfConversation is where a conversation lives: its project, from its
// record, in the conversation itself. A housekeeping run is a conversation of
// its own, with no record, and it lives where its run does: with the turn it
// serves, whose words it was sent.
func (p *Platform) placeOfConversation(tenant, id, project string) Place {
	first, _ := p.Runs.Get(tenant, id)
	return conversationPlace(id, project, first)
}

func conversationPlace(id, project string, first runs.Run) Place {
	if first.Origin == runs.OriginSystem && first.Thread == id {
		return placeOfRun(first)
	}
	return Place{Project: project, Thread: id}
}

// privateThread reports a conversation only its participants read.
func (p *Platform) privateThread(tenant, id string) bool {
	th, err := p.Threads.Get(tenant, id)
	return err == nil && th.Visibility == threads.Private
}

// placeOfSource is where an inbox message's subject lives.
func placeOfSource(s inbox.Source) Place { return Place{Project: s.Project, Thread: s.Thread} }

// placeOfArtifact is where a published file lives: the conversation that
// published it, in its project.
func placeOfArtifact(a artifacts.Artifact) Place { return Place{Project: a.Project, Thread: a.Thread} }

// Seer answers what one reader may read, from one snapshot of the tree: their
// role in every project, read once, and each private thread's participants,
// read once each. A read path takes a Seer per request and asks it of every
// object it would return.
type Seer struct {
	p       *Platform
	who     auth.Principal
	tree    *projects.Tree
	roles   map[string]projects.Access
	govern  bool
	own     map[string]projects.Access
	threads map[string]*threadAccess
	// gone is the reader's role in each deleted project asked about, read
	// from its tombstone once.
	gone map[string]projects.Access
	// hosts is, for each client asked about, whether the reader is the
	// machine that hosts its surface.
	hosts map[string]bool
	// records holds each thread record a snapshot has read, found or not, so
	// a listing reads a thread once however many of its rows live in it. Nil
	// reads the record each time it is asked for (Snapshot).
	records map[string]threadRead
	// runs holds where each run a snapshot has read lives, found or not
	// (ReadRuns).
	runs map[string]runPlace
	// held, when set, keeps back the records of reads made by exception
	// until what they granted is disclosed (Holding).
	held *heldReads
}

// runPlace is one read of where a run lives.
type runPlace struct {
	at Place
	ok bool
}

// heldReads is the reads a listing's reader was granted by exception
// (projects:govern), by the row each was granted for, and what recording
// each came to.
type heldReads struct {
	// row is the row being decided, "" between decisions.
	row string
	by  map[string][]readPlace
	// recorded is whether each place's record was written.
	recorded map[readPlace]bool
}

// readPlace is what a read record names: a project and a thread in it.
type readPlace struct{ project, thread string }

// threadRead is one read of a thread's record: the record, or why not.
type threadRead struct {
	th  threads.Thread
	err error
}

// Snapshot makes the seer read each thread's record once, as it already
// reads the tree and each private thread's participants once. It is for one
// listing in one request, which asks about many rows of the same threads:
// what it answers is as of the first read of each. A seer kept longer than a
// request, such as a stream's, is not a snapshot, and reads every time.
func (s *Seer) Snapshot() *Seer {
	if s.records == nil {
		s.records = map[string]threadRead{}
	}
	return s
}

// Holding makes the seer keep back the record of a read it grants by
// exception while it decides a row (deciding), until the row is disclosed
// (Disclose).
//
// The rule: a read by exception is recorded when the reader is shown what it
// granted. That is a row a response returns, or a fact of one a response
// states, such as a count that includes it. The platform examining a
// conversation to order, filter or page a listing is not the reader reading
// it: a session that is passed over, cut from the page, or left out by a
// filter after its turns were read is told to nobody, and no record of it is
// written. A row that is returned is recorded with every exceptional grant
// its decision rested on: its own conversation, and each session above it
// that the reader also sees only by exception, which the row names as its
// parent. Recording still comes first, and the records of one answer are
// written together or not at all: when they cannot be written the answer is
// refused whole and no record of it is kept.
//
// A seer that is not holding records each exceptional read as it grants it,
// which is right wherever everything decided is shown.
func (s *Seer) Holding() *Seer {
	if s.held == nil {
		s.held = &heldReads{by: map[string][]readPlace{}, recorded: map[readPlace]bool{}}
	}
	return s
}

// deciding runs one row's decision. On a holding seer the exceptional reads
// it grants are kept back under the row's name, and granted for now.
func (s *Seer) deciding(row string, decide func() bool) bool {
	if s.held == nil {
		return decide()
	}
	was := s.held.row
	s.held.row = row
	defer func() { s.held.row = was }()
	return decide()
}

// Disclose records the exceptional reads the decisions of rows rested on,
// now that the rows are to be shown, and reports whether they may be. The
// records are one answer's: a request's are kept back with the rest of its
// response's and written together before the response is sent
// (Platform.ResponseReader), and a reader with no request has them written
// here in one write. Either way all are written or none, and when none is
// nothing of the rows may be shown: the caller refuses its whole answer
// (disclosedSessions). A seer that is not holding recorded them as it
// decided.
func (s *Seer) Disclose(rows ...string) bool {
	if s.held == nil {
		return true
	}
	var unwritten []readPlace
	for _, row := range rows {
		for _, at := range s.held.by[row] {
			ok, done := s.held.recorded[at]
			if done && !ok {
				return false
			}
			if !done && !slices.Contains(unwritten, at) {
				unwritten = append(unwritten, at)
			}
		}
	}
	ok := true
	if s.who.AuditRead != nil {
		for _, at := range unwritten {
			ok = s.who.AuditRead(s.who.Subject, at.project, at.thread) && ok
		}
	} else if len(unwritten) > 0 {
		places := make([]Place, len(unwritten))
		for i, at := range unwritten {
			places[i] = Place{Project: at.project, Thread: at.thread}
		}
		ok = s.p.recordReads(s.who, places, "platform")
	}
	for _, at := range unwritten {
		s.held.recorded[at] = ok
	}
	return ok
}

// recordExceptional writes the record of a read granted by exception, and
// reports whether the read may go ahead.
func (s *Seer) recordExceptional(at Place) bool {
	if s.who.AuditRead != nil {
		return s.who.AuditRead(s.who.Subject, at.Project, at.Thread)
	}
	return s.p.recordRead(s.who, at, "platform")
}

// ReadRuns gives a snapshot where the runs a listing is about to ask of
// live, read together (runs.Index.Many) in place of a read a run. A run the
// index does not hold is asked of again when it is needed, since one being
// admitted is placed before it is indexed. A read that fails holds nothing.
// It does nothing on a seer that is not a snapshot.
func (s *Seer) ReadRuns(ids ...string) {
	if s.records == nil {
		return
	}
	if s.runs == nil {
		s.runs = map[string]runPlace{}
	}
	want := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, held := s.runs[id]; !held && id != "" && !slices.Contains(want, id) {
			want = append(want, id)
		}
	}
	if len(want) < 2 {
		return
	}
	found, err := s.p.Runs.Many(s.who.Tenant, want)
	if err != nil {
		return
	}
	threads := make([]string, 0, len(found))
	for id, r := range found {
		at := placeOfRun(r)
		s.runs[id] = runPlace{at: at, ok: true}
		threads = append(threads, at.Thread)
	}
	s.Read(threads...)
}

// placeOfRunID is Platform.placeOfRunID, from what a snapshot has read.
func (s *Seer) placeOfRunID(id string) (Place, bool) {
	if r, held := s.runs[id]; held {
		return r.at, r.ok
	}
	return s.p.placeOfRunID(s.who.Tenant, id)
}

// Hold gives a snapshot the records a listing has just read in one page, so
// asking about them reads nothing more. It does nothing on a seer that is
// not a snapshot.
func (s *Seer) Hold(recs ...threads.Thread) {
	if s.records == nil {
		return
	}
	for _, th := range recs {
		if _, held := s.records[th.ID]; !held {
			s.records[th.ID] = threadRead{th: th}
		}
	}
}

// Read gives a snapshot the records of the threads a listing is about to
// ask of, in one statement for every state.MaxList of them, in place of a
// read a thread. One with no record is remembered as having none. A read
// that fails holds nothing, and each thread is then read as it is asked of.
// It does nothing on a seer that is not a snapshot.
func (s *Seer) Read(ids ...string) {
	if s.records == nil {
		return
	}
	want := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, held := s.records[id]; !held && id != "" && !slices.Contains(want, id) {
			want = append(want, id)
		}
	}
	if len(want) < 2 {
		return
	}
	recs, err := s.p.Threads.Many(s.who.Tenant, want)
	if err != nil {
		return
	}
	s.Hold(recs...)
	for _, id := range want {
		if _, held := s.records[id]; !held {
			s.records[id] = threadRead{err: fmt.Errorf("%w: thread %q", threads.ErrNotFound, id)}
		}
	}
}

// record is a thread's record: read once by a snapshot, else read now.
func (s *Seer) record(id string) (threads.Thread, error) {
	if s.records == nil {
		return s.p.Threads.Get(s.who.Tenant, id)
	}
	if r, held := s.records[id]; held {
		return r.th, r.err
	}
	th, err := s.p.Threads.Get(s.who.Tenant, id)
	s.records[id] = threadRead{th: th, err: err}
	return th, err
}

// threadGone reports whether a conversation was deleted: no one sees it, or
// anything in it, again.
func (s *Seer) threadGone(id string) bool {
	if id == "" {
		return false
	}
	th, err := s.record(id)
	return err == nil && th.Archive != nil && th.Archive.Deleted != nil
}

// current is Platform.CurrentPlace, from the seer's read of the record.
func (s *Seer) current(at Place) Place {
	if at.Thread != "" {
		if th, err := s.record(at.Thread); err == nil {
			at.Project = th.Project
		}
	}
	return at
}

// Seer is a reader's view of their workspace, as the tree stands now.
func (p *Platform) Seer(who auth.Principal) *Seer {
	tree := p.Projects.Tree(who.Tenant)
	r := ReaderOf(who)
	ordinary := r
	ordinary.Govern = false
	return &Seer{own: tree.Roles(ordinary), p: p, who: who, tree: tree, roles: tree.Roles(r), govern: r.Govern && r.Subject != "", threads: map[string]*threadAccess{}, gone: map[string]projects.Access{}, hosts: map[string]bool{}}
}

// Sees reports whether a reader may read what lives in a place: a role of at
// least viewer in its project, and in a private thread to take part in it, or
// projects:govern. What a deleted project held is read, not continued, by
// whom it was read by when it was deleted, under its parent as the parent
// stands now (projects.Tree.DeletedRole); a name with no such record is seen
// only with projects:govern (fail closed).
func (p *Platform) Sees(who auth.Principal, at Place) bool { return p.Seer(who).Place(at) }

// Place reports whether the reader sees what lives in a place. Besides the
// rule, the machine a client names to host its surface reads the threads on
// that surface, wherever they are filed: the Slack thread is already its own.
func (s *Seer) Place(at Place) bool {
	if s.threadGone(at.Thread) {
		return false
	}
	if s.tree.PendingFork(s.current(at).Project) {
		return false
	}
	return s.sessionAncestorsVisible(at.Thread) && s.forkSourcesVisible(at, s.placeDirect) && s.placeDirect(at)
}

// Session descendants retain their original participants for continuity, but
// access to inherited context follows the current ancestor permissions. This
// lives at Place so compatibility thread/run routes and global streams enforce
// exactly the same rule as canonical session endpoints.
func (s *Seer) sessionAncestorsVisible(id string) bool {
	for depth := 0; depth <= 64; depth++ {
		th, err := s.record(id)
		if err != nil {
			return !strings.HasPrefix(id, "s_")
		}
		if th.Session == nil {
			return true
		}
		if !s.Agent(th.Session.Agent) {
			return false
		}
		parent := th.Session.Parent
		if !strings.HasPrefix(parent, "s_") {
			return true
		}
		p, err := s.record(parent)
		if err != nil {
			return false
		}
		at := Place{Project: p.Project, Thread: p.ID}
		if s.threadGone(parent) || !s.forkSourcesVisible(at, s.placeDirect) || !s.placeDirect(at) {
			return false
		}
		id = parent
	}
	return false
}

func (s *Seer) placeDirect(at Place) bool {
	at = s.current(at)
	if s.tree.PendingFork(at.Project) {
		return false
	}
	if pr, ok := s.tree.Get(at.Project); ok && pr.PersonalOwner != "" && s.who.Kind == auth.Machine {
		return false
	}

	if s.govern {
		ordinary := *s
		ordinary.govern = false
		ordinary.roles = s.own
		if ordinary.placeDirect(at) {
			return true
		}
		if s.held != nil && s.held.row != "" {
			// Granted for now; recorded when the row is disclosed.
			if read := (readPlace{at.Project, at.Thread}); !slices.Contains(s.held.by[s.held.row], read) {
				s.held.by[s.held.row] = append(s.held.by[s.held.row], read)
			}
			return true
		}
		return s.recordExceptional(at)
	}
	if th := s.thread(at.Thread); th != nil && th.Client != "" && s.hosting(th.Client) {
		return true
	}
	a, ok := s.roles[at.Project]
	if !ok {
		a, ok = s.deleted(at.Project)
	}
	return ok && seesPlace(a.Role.AtLeast(projects.Viewer), false, s.thread(at.Thread), s.who.Subject)
}

// deleted is the reader's role in a project the tree no longer holds, from
// its tombstone.
func (s *Seer) deleted(name string) (projects.Access, bool) {
	if a, known := s.gone[name]; known {
		return a, true
	}
	x, ok := s.p.Projects.Tombstone(s.who.Tenant, name)
	if !ok {
		return projects.Access{}, false
	}
	reader := ReaderOf(s.who)
	reader.Govern = s.govern
	a := s.tree.DeletedRole(x, reader, func(n string) (projects.Tombstone, bool) { return s.p.Projects.Tombstone(s.who.Tenant, n) })
	s.gone[name] = a
	return a, true
}

// Project reports whether the reader sees a project at all.
func (s *Seer) Project(name string) bool { return s.Place(Place{Project: name}) }

// thread is what reading a thread needs: nil for one that is shared, or has
// no record, which reads as shared.
func (s *Seer) thread(id string) *threadAccess {
	if id == "" {
		return nil
	}
	if th, seen := s.threads[id]; seen {
		return th
	}
	var out *threadAccess
	if th, err := s.record(id); err == nil && (th.Visibility == threads.Private || th.Surface != nil) {
		out = &threadAccess{Private: th.Visibility == threads.Private}
		if th.Surface != nil {
			out.Client = th.Surface.Client
		}
		for _, pt := range th.Participants {
			if pt.Kind == threads.Person {
				out.Participants = append(out.Participants, pt.Name)
			}
		}
	}
	s.threads[id] = out
	return out
}

// hosting reports whether the reader is the machine a client names to host
// its surface.
func (s *Seer) hosting(client string) bool {
	if v, known := s.hosts[client]; known {
		return v
	}
	c, err := s.p.Clients.Get(s.who.Tenant, client)
	v := err == nil && machineOf(c, s.who)
	s.hosts[client] = v
	return v
}

// Conversation reports whether the reader sees a conversation, by its id and
// the project its record files it in (ThreadRecord).
func (s *Seer) Conversation(id, project string) bool {
	return s.Place(s.p.placeOfConversation(s.who.Tenant, id, project))
}

// Session reports whether the reader sees a session: its thread and its
// agent, and the same of every session above it, so sharing a child never
// leaks context from a parent whose access has since been revoked.
func (s *Seer) Session(id string) bool {
	for depth := 0; depth <= 64; depth++ {
		th, err := s.record(id)
		if err != nil || th.Session == nil || !s.Place(Place{Project: th.Project, Thread: id}) || !s.Agent(th.Session.Agent) {
			return false
		}
		if !strings.HasPrefix(th.Session.Parent, "s_") {
			return true
		}
		id = th.Session.Parent
	}
	return false
}

// Run reports whether the reader sees a run: where its conversation lives.
func (s *Seer) Run(r runs.Run) bool { return s.Place(placeOfRun(r)) }

// RunID reports whether the reader sees the run an id names, one being
// admitted included. One that does not exist is not seen.
func (s *Seer) RunID(id string) bool {
	at, ok := s.placeOfRunID(id)
	return ok && s.Place(at)
}

// Message reports whether the reader sees what an inbox message is about: its
// run's place when it names a run the index holds, else the project and
// thread it names, and the agent it names. A thread is read where its record
// files it now, so a mention raised in a conversation before it was shared
// into another project is read where the conversation went. Who it is
// addressed to is the inbox's to decide besides.
func (s *Seer) Message(m inbox.Message) bool {
	if m.Source.Knowledge != "" && !s.p.SourceVisible(s.who, m.Source.Knowledge) {
		return false
	}
	if m.Kind == inbox.KindOperation {
		found := false
		for _, part := range m.Parts {
			if part.Operation == nil {
				continue
			}
			found = true
			op := part.Operation
			switch op.Kind {
			case "assignment":
				a, ok, err := s.p.Operations.Assignment(s.who.Tenant, op.ID)
				if err != nil || !ok || !s.Project(a.Project) || !s.Place(Place{Project: a.Project, Thread: a.Thread}) || !s.Agent(a.Agent) {
					return false
				}
			case "handoff":
				h, ok, err := s.p.Operations.Handoff(s.who.Tenant, op.ID)
				if err != nil || !ok || !s.p.seesHandoff(s, h) {
					return false
				}
			default:
				return false
			}
		}
		if !found {
			return false
		}
	}

	// Moving a conversation does not publish its original project settings,
	// memory proposals, limits or drafts. Those retain their own scope.
	for _, part := range m.Parts {
		if part.Memory != nil && !s.MemoryOwner(part.Memory.Owner) {
			return false
		}
		if part.Budget != nil && part.Budget.ScopeKind == "project" && !s.Project(part.Budget.ScopeName) {
			return false
		}
		if part.Draft != nil && !s.Project(part.Draft.Project) {
			return false
		}
		if part.Question != nil && m.Source.Run != "" {
			if run, err := s.p.Runs.Get(s.who.Tenant, m.Source.Run); err == nil && s.p.RunPlace(s.who.Tenant, run).Project != run.Project && !s.Project(run.Project) {
				return false
			}
		}
	}
	if m.Source.Run != "" {
		if r, err := s.p.Runs.Get(s.who.Tenant, m.Source.Run); err == nil {
			return s.Run(r)
		}
	}
	at := placeOfSource(m.Source)
	if at.Thread != "" {
		if th, err := s.p.Threads.Get(s.who.Tenant, at.Thread); err == nil {
			at.Project = th.Project
		}
	}
	return s.Place(at) && (m.Source.Agent == "" || s.Agent(m.Source.Agent))
}

// Scope reports whether the reader sees what a limit is set on: an agent's
// where they see the agent, a project's where they see the project.
func (s *Seer) Scope(sc policy.Scope) bool {
	switch sc.Kind {
	case policy.Agent:
		return s.Agent(sc.Name)
	case policy.Project:
		return s.Project(sc.Name)
	}
	return true
}

// Notice reports whether the reader sees a warning or refusal at a limit:
// the project its run was filed in, its agent, and the limit's scope.
func (s *Seer) Notice(e usage.Entry) bool {
	return s.Place(Place{Project: e.Project}) && (e.Agent == "" || s.Agent(e.Agent)) && (e.Scope == nil || s.Scope(*e.Scope))
}

// Suite reports whether the reader sees an eval suite: one that tests an
// agent where they see the agent.
func (s *Seer) Suite(suite evals.Suite) bool {
	if !s.p.EvalSuiteSourcesVisible(s.who, suite) {
		return false
	}
	if suite.Target == evals.Knowledge {
		return s.p.SourceVisible(s.who, suite.Subject)
	}
	if suite.Target == evals.Corpus {
		return s.p.CorpusReadable(s.who, suite.Subject)
	}
	return suite.Target != evals.Agent || s.Agent(suite.Subject)
}

// Alert reports whether the reader sees an alert: the run that raised it,
// where it lives, when the index holds it, else its project; and its agent.
// A run's alert in a private thread is its participants'.
func (s *Seer) Alert(a alerts.Alert) bool {
	if a.Session != "" && !s.Session(a.Session) {
		return false
	}
	if a.Agent != "" && !s.Agent(a.Agent) {
		return false
	}
	if a.RunID != "" {
		if at, ok := s.placeOfRunID(a.RunID); ok {
			return s.Place(at)
		}
	}
	return s.Project(a.Project)
}

// Rule reports whether the reader sees an alert rule: the project and the
// agent it watches.
func (s *Seer) Rule(r alerts.Rule) bool {
	return s.Project(r.Project) && (r.Agent == "" || s.Agent(r.Agent))
}

// Artifact reports whether the reader sees a published file.
func (s *Seer) Artifact(a artifacts.Artifact) bool { return s.Place(placeOfArtifact(a)) }

// Agent reports whether the reader sees an agent, by any spelling of its
// name: a platform agent always, an agent that lives in a project only where
// they see the project.
func (s *Seer) Agent(name string) bool {
	home, homed := s.tree.Home(name)
	return !homed || s.Project(home)
}

// MemoryOwner reports whether the reader sees notes kept under an owner: a
// project's where they see the project, an agent's where they see the agent.
func (s *Seer) MemoryOwner(owner string) bool {
	if subject, ok := memory.OwnerPerson(owner); ok {
		return subject != "" && s.who.Kind == auth.User && s.who.Subject == subject
	}

	if project, ok := memory.OwnerProject(owner); ok {
		return s.Project(project)
	}
	return s.Agent(owner)
}

// ProjectCounts is what each of the reader's workspace's projects holds that
// they may read, by name: the run index's counts, leaving out the private
// threads they take no part in and the conversations of housekeeping runs
// that serve a turn they may not read, so a count says nothing of a
// conversation the reader is not shown. A project the reader may not see is
// the caller's to leave out.
//
// What the reader may read is decided as of a mark taken first. Every
// conversation opened by then is decided: a private thread's record is
// written before its first run is recorded, and a housekeeping run's
// conversation is among SystemThreads from when its run is recorded. One
// opened after the mark is counted from a later read.
func (p *Platform) ProjectCounts(who auth.Principal) map[string]runs.ProjectCount {
	seer := p.Seer(who).Snapshot()
	mark := p.Runs.Opened(who.Tenant)
	hidden := map[string]bool{}
	recs, err := p.Threads.Records(who.Tenant, func(th threads.Thread) bool { return true })
	if err != nil {
		// Without the records, nothing says which conversations are hidden,
		// so nothing is counted.
		p.log.Warn("platform: project counts: threads", "err", err)
		return map[string]runs.ProjectCount{}
	}
	// The records just read answer for themselves: none is read again.
	seer.Hold(recs...)
	for _, th := range recs {
		if !seer.Place(Place{Project: th.Project, Thread: th.ID}) {
			hidden[th.ID] = true
		}
	}
	// A housekeeping run's conversation has no record and is counted at the
	// workspace, where its run is filed. It is read, as it is listed, where
	// the turn it serves is.
	for _, id := range p.Runs.SystemThreads(who.Tenant) {
		if !seer.Conversation(id, "") {
			hidden[id] = true
		}
	}
	var keep func(string) bool
	if len(hidden) > 0 {
		keep = func(thread string) bool { return !hidden[thread] }
	}
	return p.Runs.ProjectCountsPlaced(who.Tenant, mark, keep, func(thread, original string) string {
		return p.CurrentPlace(who.Tenant, Place{Project: original, Thread: thread}).Project
	})
}

// SeesThread reports whether a reader may read a thread: its project, and in
// a private one taking part. A thread that does not exist is not seen.
func (p *Platform) SeesThread(who auth.Principal, id string) bool {
	th, err := p.ThreadRecord(who.Tenant, id)
	return err == nil && p.Seer(who).Place(Place{Project: th.Project, Thread: id})
}

// placeAdmitted is where a run lives before the index records it: its
// root's conversation for a called run, else the conversation the API placed
// it in, or its own.
func (p *Platform) placeAdmitted(tag event.Envelope, edge call.Edge, runID, project string) Place {
	if wire.Origin(tag) == runs.OriginSystem && wire.Meta(tag)[metaPlaceThread] != "" {
		return Place{Project: wire.Meta(tag)[metaPlaceProject], Thread: wire.Meta(tag)[metaPlaceThread]}
	}
	if edge.Parent != "" {
		if root, err := p.Runs.Get(tag.Tenant, runtime.RunID(tag.Tenant, edge.Root)); err == nil {
			return placeOfRun(root)
		}
	}
	thread := runID
	if v := wire.Meta(tag)[runs.MetaThread]; v != "" && wire.Origin(tag) == runs.OriginAPI {
		thread = v
	}
	return Place{Project: project, Thread: thread}
}

// placeOfRunID is where the run an id names lives: from the index, or from
// its admission while it is not recorded yet.
// AwaitRunRecord reports whether a run is known here, its record read or
// its admission in this process, waiting up to d for it. A run's first
// events are published before its record is written, so a reader on another
// replica can hear of a run that this process cannot place yet.
func (p *Platform) AwaitRunRecord(ctx context.Context, tenant, id string, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	tick := time.NewTicker(runRecordPoll)
	defer tick.Stop()
	for {
		if _, ok := p.placeOfRunID(tenant, id); ok {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-t.C:
			_, ok := p.placeOfRunID(tenant, id)
			return ok
		case <-tick.C:
		}
	}
}

// runRecordPoll is how often AwaitRunRecord reads a run's record.
const runRecordPoll = 25 * time.Millisecond

func (p *Platform) placeOfRunID(tenant, id string) (Place, bool) {
	if r, err := p.Runs.Get(tenant, id); err == nil {
		return placeOfRun(r), true
	}
	if v, ok := p.admitting.Load(id); ok {
		if a := v.(admitted); a.tenant == tenant {
			return a.place, true
		}
	}
	return Place{}, false
}
