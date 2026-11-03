package platform

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/drive"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/runs"
)

// What memory a conversation draws on, and what its session and each of its
// runs used and wrote (RFC 0004, "Where the files live"). Read only: memory
// is changed where it is kept (the Library's Memory, an agent's Memory tab),
// and a tree the reader may not read is counted, never named.

// MaxMemoryNotes is the most notes one run's memory view names.
const MaxMemoryNotes = 20

// MemoryTreeView is one memory tree as a reader may see it.
type MemoryTreeView struct {
	Kind     string `json:"kind"`
	Project  string `json:"project,omitempty"`
	Agent    string `json:"agent,omitempty"`
	Self     bool   `json:"self,omitempty"`
	Dir      string `json:"dir"`
	ReadOnly bool   `json:"read_only,omitempty"`
	Head     int64  `json:"head"`
	Files    int    `json:"files"`
}

// RunMemoryTree is a tree one run was given, and what it wrote there.
type RunMemoryTree struct {
	MemoryTreeView
	Installed bool  `json:"installed"`
	Version   int64 `json:"version,omitempty"`
	Finished  bool  `json:"finished,omitempty"`
	Committed int64 `json:"committed,omitempty"`
	Proposals int   `json:"proposals,omitempty"`
	Dropped   int   `json:"dropped,omitempty"`
}

// MemoryNoteView is a note a run drew from the conversation.
type MemoryNoteView struct {
	ID     string        `json:"id"`
	Kind   string        `json:"kind"`
	Owner  string        `json:"owner,omitempty"`
	Text   string        `json:"text"`
	Status memory.Status `json:"status"`
	At     time.Time     `json:"at"`
}

// RunMemory is what one run used and wrote.
type RunMemory struct {
	Run       string           `json:"run"`
	Agent     string           `json:"agent"`
	Depth     int              `json:"depth"`
	CreatedAt time.Time        `json:"created_at"`
	Trees     []RunMemoryTree  `json:"trees"`
	Notes     []MemoryNoteView `json:"notes"`
	Hidden    int              `json:"hidden"`
}

// SessionMemoryTree is a tree across the session's runs.
type SessionMemoryTree struct {
	MemoryTreeView
	Runs      int     `json:"runs"`
	Versions  []int64 `json:"versions"`
	Commits   []int64 `json:"commits"`
	Proposals int     `json:"proposals"`
	Notes     int     `json:"notes"`
}

// ConversationMemory is the Workspace's Memories: the trees the conversation
// draws on, the session's use of them, and each run's.
type ConversationMemory struct {
	Isolated     bool                `json:"isolated"`
	Conversation []MemoryTreeView    `json:"conversation"`
	Session      []SessionMemoryTree `json:"session"`
	Runs         []RunMemory         `json:"runs"`
	Hidden       int                 `json:"hidden"`
	More         bool                `json:"more"`
}

func treeOf(u runs.MemoryUse) drive.Tree {
	return drive.Tree{Kind: drive.Kind(u.Kind), Person: u.Person, Project: u.Project, Agent: u.Agent}
}

// ConversationMemories reads what memory a conversation draws on, newest
// run first and at most MaxSandboxRuns of them.
func (p *Platform) ConversationMemories(ctx context.Context, who auth.Principal, id string) (ConversationMemory, error) {
	if !who.Can(auth.RunsRead) {
		return ConversationMemory{}, auth.ErrForbidden
	}
	th, err := p.workspaceThread(who, id)
	if err != nil {
		return ConversationMemory{}, err
	}
	out := ConversationMemory{Isolated: p.cfg.Sandbox.Isolated(), Conversation: []MemoryTreeView{}, Session: []SessionMemoryTree{}, Runs: []RunMemory{}}
	actor := p.MemoryActor(who)
	readable := func(t drive.Tree) bool {
		if t.Kind == drive.PersonKind {
			return who.Kind == auth.User && t.Person == who.Subject
		}
		return actor.Grants != nil && actor.Grants(t).Read
	}
	heads := map[string]MemoryTreeView{}
	view := func(t drive.Tree, dir string, readOnly bool) MemoryTreeView {
		if v, ok := heads[t.Key()]; ok {
			v.Dir, v.ReadOnly = dir, readOnly
			return v
		}
		v := MemoryTreeView{Kind: string(t.Kind), Project: t.Project, Agent: t.Agent, Self: t.Kind == drive.PersonKind, Dir: "/" + dir, ReadOnly: readOnly}
		if head, err := p.Drive.Head(ctx, who.Tenant, actor, t); err == nil {
			v.Head, v.Files = head.N, len(head.Entries)
		}
		heads[t.Key()] = v
		return v
	}
	notes := map[string][]memory.Note{}
	notesOf := func(t drive.Tree) []memory.Note {
		owner, ok := memory.OwnerOf(t)
		if !ok {
			return nil
		}
		if n, ok := notes[owner]; ok {
			return n
		}
		n, _ := p.Memory.List(who.Tenant, owner)
		notes[owner] = n
		return n
	}
	conversation := map[string]bool{}
	session := map[string]int{}
	addConversation := func(t drive.Tree, dir string, readOnly bool) {
		if conversation[t.Key()] {
			return
		}
		conversation[t.Key()] = true
		if !readable(t) {
			out.Hidden++
			return
		}
		out.Conversation = append(out.Conversation, view(t, dir, readOnly))
	}
	// What the conversation's project draws on, even before any run: the
	// project, then its ancestors nearest first, read only above it.
	for i, pr := range p.Projects.Tree(who.Tenant).Chain(th.Project) {
		t := drive.Project(pr.Name)
		addConversation(t, t.Mount(), i != 0)
	}

	turns, err := p.Runs.Conversation(who.Tenant, id)
	if errors.Is(err, runs.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	var all []runs.Run
	for _, turn := range turns {
		tree, err := p.Runs.Tree(who.Tenant, turn.ID)
		if err != nil {
			return out, err
		}
		all = append(all, tree...)
		if len(all) > runs.MaxTree {
			break
		}
	}
	slices.SortStableFunc(all, func(a, b runs.Run) int { return b.CreatedAt.Compare(a.CreatedAt) })
	all = slices.CompactFunc(all, func(a, b runs.Run) bool { return a.ID == b.ID })
	if len(all) > MaxSandboxRuns {
		all, out.More = all[:MaxSandboxRuns], true
	}
	for _, r := range all {
		rm := RunMemory{Run: r.ID, Agent: r.Agent, Depth: r.Depth, CreatedAt: r.CreatedAt, Trees: []RunMemoryTree{}, Notes: []MemoryNoteView{}}
		recorded := map[string]runs.MemoryUse{}
		for _, u := range r.Memory {
			recorded[treeOf(u).Key()] = u
		}
		for _, m := range p.MemoryMounts(r) {
			addConversation(m.Tree, m.Dir, m.ReadOnly)
			if !readable(m.Tree) {
				rm.Hidden++
				continue
			}
			rt := RunMemoryTree{MemoryTreeView: view(m.Tree, m.Dir, m.ReadOnly)}
			if u, ok := recorded[m.Tree.Key()]; ok {
				rt.Installed, rt.Version, rt.Finished = true, u.Version, u.Finished
				rt.Committed, rt.Proposals, rt.Dropped = u.Committed, len(u.Proposals), u.Dropped
			}
			rm.Trees = append(rm.Trees, rt)
			// The session's use, by tree.
			i, ok := session[m.Tree.Key()]
			if !ok {
				i = len(out.Session)
				session[m.Tree.Key()] = i
				out.Session = append(out.Session, SessionMemoryTree{MemoryTreeView: rt.MemoryTreeView, Versions: []int64{}, Commits: []int64{}})
			}
			st := &out.Session[i]
			st.Runs++
			if rt.Installed && !slices.Contains(st.Versions, rt.Version) {
				st.Versions = append(st.Versions, rt.Version)
			}
			if rt.Committed > 0 && !slices.Contains(st.Commits, rt.Committed) {
				st.Commits = append(st.Commits, rt.Committed)
			}
			st.Proposals += rt.Proposals
			owner := m.Tree.Agent
			if m.Tree.Kind == drive.ProjectKind {
				owner = m.Tree.Project
			}
			for _, n := range notesOf(m.Tree) {
				if n.By != r.ID {
					continue
				}
				st.Notes++
				if len(rm.Notes) < MaxMemoryNotes {
					rm.Notes = append(rm.Notes, MemoryNoteView{ID: n.ID, Kind: string(m.Tree.Kind), Owner: owner, Text: n.Text, Status: n.Status, At: n.At})
				}
			}
		}
		out.Runs = append(out.Runs, rm)
	}
	for i := range out.Session {
		slices.Sort(out.Session[i].Versions)
		slices.Sort(out.Session[i].Commits)
	}
	return out, nil
}
