package platform

import (
	"slices"

	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"

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
		if meta := r.Tag().Meta; meta[metaPlaceThread] != "" {
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
	if r, err := p.Runs.Get(tenant, id); err == nil && r.Origin == runs.OriginSystem && r.Thread == id {
		return placeOfRun(r)
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
	return s.forkSourcesVisible(at, s.placeDirect) && s.placeDirect(at)
}

func (s *Seer) placeDirect(at Place) bool {
	if s.govern {
		ordinary := *s
		ordinary.govern = false
		ordinary.roles = s.own
		if ordinary.placeDirect(at) {
			return true
		}
		if s.who.AuditRead != nil {
			return s.who.AuditRead(s.who.Subject, at.Project, at.Thread)
		}
		return s.p.recordRead(s.who, at, "platform")
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
	if th, err := s.p.Threads.Get(s.who.Tenant, id); err == nil && (th.Visibility == threads.Private || th.Surface != nil) {
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

// Run reports whether the reader sees a run: where its conversation lives.
func (s *Seer) Run(r runs.Run) bool { return s.Place(placeOfRun(r)) }

// RunID reports whether the reader sees the run an id names, one being
// admitted included. One that does not exist is not seen.
func (s *Seer) RunID(id string) bool {
	at, ok := s.p.placeOfRunID(s.who.Tenant, id)
	return ok && s.Place(at)
}

// Message reports whether the reader sees what an inbox message is about: its
// run's place when it names a run the index holds, else the project and
// thread it names, and the agent it names. A thread is read where its record
// files it now, so a mention raised in a conversation before it was shared
// into another project is read where the conversation went. Who it is
// addressed to is the inbox's to decide besides.
func (s *Seer) Message(m inbox.Message) bool {
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
	return suite.Target != evals.Agent || s.Agent(suite.Subject)
}

// Alert reports whether the reader sees an alert: the run that raised it,
// where it lives, when the index holds it, else its project; and its agent.
// A run's alert in a private thread is its participants'.
func (s *Seer) Alert(a alerts.Alert) bool {
	if a.Agent != "" && !s.Agent(a.Agent) {
		return false
	}
	if a.RunID != "" {
		if at, ok := s.p.placeOfRunID(s.who.Tenant, a.RunID); ok {
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
	seer := p.Seer(who)
	mark := p.Runs.Opened(who.Tenant)
	hidden := map[string]bool{}
	for _, th := range p.Threads.Records(who.Tenant, func(th threads.Thread) bool { return th.Visibility == threads.Private }) {
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
	return p.Runs.ProjectCountsWhere(who.Tenant, mark, keep)
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
func (p *Platform) placeAdmitted(tag envelope.Tag, edge call.Edge, runID, project string) Place {
	if tag.Origin == runs.OriginSystem && tag.Meta[metaPlaceThread] != "" {
		return Place{Project: tag.Meta[metaPlaceProject], Thread: tag.Meta[metaPlaceThread]}
	}
	if edge.Parent != "" {
		if root, err := p.Runs.Get(tag.Tenant, runtime.RunID(tag.Tenant, edge.Root)); err == nil {
			return placeOfRun(root)
		}
	}
	thread := runID
	if v := tag.Meta[runs.MetaThread]; v != "" && tag.Origin == runs.OriginAPI {
		thread = v
	}
	return Place{Project: project, Thread: thread}
}

// placeOfRunID is where the run an id names lives: from the index, or from
// its admission while it is not recorded yet.
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
