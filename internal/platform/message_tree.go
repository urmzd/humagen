package platform

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// A message tree reads a session as its first message and what hangs
// beneath it. Beneath a person's message are the agents' messages that
// follow it in its session, up to the next person's message (the answers,
// and the entry agent's pick with each call it made), and the child
// sessions started for it, each standing as its own first message. A
// session's first message stands for the session, so beneath it are also
// the session's later people's messages, each with its own answers. Every
// node says how many children it has and when anything beneath it last
// changed, so a reader asks for more only where it wants to look. A child
// session the reader may not read is left out, and not counted.

// Bounds on one read of a message tree or of the roots.
const (
	// MaxMessageDepth is how many levels beneath a message one read goes.
	MaxMessageDepth = 5
	// MaxRootPage is how many roots one page of the listing holds.
	MaxRootPage = 200
	// maxNodeChildren is how many children one node shows: its latest.
	maxNodeChildren = 200
	// maxTreeNodes is how many nodes beneath the message one read holds.
	maxTreeNodes = 1000
)

// MessageAudience is who a root's place was asked in front of, by kind
// alone: public, private, group or direct. Who was there is never shown.
type MessageAudience struct {
	Kind string `json:"kind"`
}

// MessageNode is one message of a tree: who said it, its content (text,
// or a pick and its calls), its status (that of the runs beneath it, done
// when there are none), how many children it has, when anything beneath it
// last changed, and its children in time order as deep as the read went.
// Audience is on the node read, when its root's place reported one.
type MessageNode struct {
	ID            string           `json:"id"`
	Session       string           `json:"session"`
	Role          string           `json:"role"`
	Author        SessionAuthor    `json:"author"`
	Status        string           `json:"status"`
	Content       []SessionPart    `json:"content"`
	At            time.Time        `json:"at"`
	ChildrenCount int              `json:"children_count"`
	LatestAt      time.Time        `json:"latest_at"`
	Audience      *MessageAudience `json:"audience,omitempty"`
	Children      []MessageNode    `json:"children"`
}

// RootMessage is one root session in the listing: its place (Parent), who
// started it, the agent that owns it, and its node's status, count and
// latest change.
type RootMessage struct {
	ID            string           `json:"id"`
	Title         string           `json:"title"`
	Status        string           `json:"status"`
	Agent         string           `json:"agent"`
	Author        SessionAuthor    `json:"author"`
	Parent        string           `json:"parent"`
	Project       string           `json:"project"`
	LatestAt      time.Time        `json:"latest_at"`
	ChildrenCount int              `json:"children_count"`
	Audience      *MessageAudience `json:"audience,omitempty"`
}

// RootMessagePage is a page of roots, newest first, and the cursor of the
// next page ("" on the last).
type RootMessagePage struct {
	Messages []RootMessage `json:"messages"`
	Next     string        `json:"next"`
}

// ReadMessage is the message id names as a tree depth levels deep (0 to
// MaxMessageDepth). id is a session's, read as its first message, or a
// message's (session:message); a session's first message read by its own
// id is the session's node.
func (p *Platform) ReadMessage(ctx context.Context, who auth.Principal, id string, depth int) (MessageNode, error) {
	if depth < 0 || depth > MaxMessageDepth {
		return MessageNode{}, fmt.Errorf("%w: depth is 0 to %d", threads.ErrInvalid, MaxMessageDepth)
	}
	session, message := SplitMessageID(id)
	th, err := p.SessionRecord(ctx, who, session)
	if err != nil {
		return MessageNode{}, err
	}
	r, _, err := p.newTreeReader(who)
	if err != nil {
		return MessageNode{}, err
	}
	node, err := r.read(th, message, depth)
	if err != nil {
		return MessageNode{}, err
	}
	node.Audience = p.messageAudience(who.Tenant, th.ID)
	return node, nil
}

// ListRootMessages is a page of the root sessions who may read, newest
// first by when anything beneath them last changed, at most limit (1 to
// MaxRootPage), after the root the previous page's cursor names.
func (p *Platform) ListRootMessages(ctx context.Context, who auth.Principal, limit int, after string) (RootMessagePage, error) {
	if limit < 1 || limit > MaxRootPage {
		return RootMessagePage{}, fmt.Errorf("%w: limit is 1 to %d", threads.ErrInvalid, MaxRootPage)
	}
	r, records, err := p.newTreeReader(who)
	if err != nil {
		return RootMessagePage{}, err
	}
	all := []RootMessage{}
	for _, th := range records {
		if strings.HasPrefix(th.Session.Parent, "s_") || !r.see(th.ID) {
			continue
		}
		n := r.sessionNode(th, 0)
		all = append(all, RootMessage{ID: th.ID, Title: th.Title, Status: n.Status, Agent: th.Session.Agent, Author: n.Author, Parent: th.Session.Parent, Project: th.Project, LatestAt: n.LatestAt, ChildrenCount: n.ChildrenCount, Audience: p.messageAudience(who.Tenant, th.ID)})
	}
	slices.SortFunc(all, func(a, b RootMessage) int {
		if n := b.LatestAt.Compare(a.LatestAt); n != 0 {
			return n
		}
		return strings.Compare(a.ID, b.ID)
	})
	start := 0
	if after != "" {
		i := slices.IndexFunc(all, func(m RootMessage) bool { return m.ID == after })
		if i < 0 {
			return RootMessagePage{}, fmt.Errorf("%w: after is not a root in this listing", threads.ErrCursor)
		}
		start = i + 1
	}
	end := min(start+limit, len(all))
	out := RootMessagePage{Messages: all[start:end]}
	if end < len(all) {
		out.Next = all[end-1].ID
	}
	return out, nil
}

// messageAudience is the kind of audience session's root was asked in
// front of, or nil when no place reported one.
func (p *Platform) messageAudience(tenant, session string) *MessageAudience {
	if aud, ok := p.SessionAudience(tenant, session); ok && aud.Kind != "" {
		return &MessageAudience{Kind: aud.Kind}
	}
	return nil
}

// treeReader reads message trees for one reader: the workspace's child
// sessions by parent, read once, and what it has worked out about each
// session, kept for the read.
type treeReader struct {
	p      *Platform
	who    auth.Principal
	seer   *Seer
	kids   map[string][]threads.Thread
	sees   map[string]bool
	own    map[string][]runs.Run
	runs   map[string][]runs.Run
	latest map[string]time.Time
	// left is how many more nodes the read may hold beneath the message.
	left int
}

// newTreeReader reads who's workspace's sessions once, and answers them
// with the reader.
func (p *Platform) newTreeReader(who auth.Principal) (*treeReader, []threads.Thread, error) {
	records, err := p.Threads.Records(who.Tenant, func(th threads.Thread) bool {
		return th.Session != nil && th.Archive == nil
	})
	if err != nil {
		return nil, nil, err
	}
	r := &treeReader{p: p, who: who, seer: p.Seer(who), kids: map[string][]threads.Thread{}, sees: map[string]bool{}, own: map[string][]runs.Run{}, runs: map[string][]runs.Run{}, latest: map[string]time.Time{}, left: maxTreeNodes}
	for _, th := range records {
		if strings.HasPrefix(th.Session.Parent, "s_") {
			r.kids[th.Session.Parent] = append(r.kids[th.Session.Parent], th)
		}
	}
	for _, kids := range r.kids {
		slices.SortFunc(kids, func(a, b threads.Thread) int {
			if n := a.CreatedAt.Compare(b.CreatedAt); n != 0 {
				return n
			}
			return strings.Compare(a.ID, b.ID)
		})
	}
	return r, records, nil
}

// see reports whether the reader may read a session.
func (r *treeReader) see(id string) bool {
	seen, ok := r.sees[id]
	if !ok {
		seen = r.p.SeesSession(r.who, id)
		r.sees[id] = seen
	}
	return seen
}

// ownRuns are the runs in a session itself.
func (r *treeReader) ownRuns(id string) []runs.Run {
	got, ok := r.own[id]
	if !ok {
		got, _ = r.p.Runs.Conversation(r.who.Tenant, id)
		r.own[id] = got
	}
	return got
}

// children are the child sessions of a session the reader may read, oldest
// first.
func (r *treeReader) children(id string) []threads.Thread {
	var out []threads.Thread
	for _, c := range r.kids[id] {
		if r.see(c.ID) {
			out = append(out, c)
		}
	}
	return out
}

// runsBeneath are a session's runs and those of every child session beneath
// it the reader may read.
func (r *treeReader) runsBeneath(th threads.Thread) []runs.Run {
	if got, ok := r.runs[th.ID]; ok {
		return got
	}
	r.runs[th.ID] = nil // a cycle reads as nothing more
	out := slices.Clone(r.ownRuns(th.ID))
	for _, c := range r.children(th.ID) {
		out = append(out, r.runsBeneath(c)...)
	}
	r.runs[th.ID] = out
	return out
}

// latestBeneath is when anything in a session, or in a child session beneath
// it the reader may read, last changed: a record, a message or a run.
func (r *treeReader) latestBeneath(th threads.Thread) time.Time {
	if got, ok := r.latest[th.ID]; ok {
		return got
	}
	r.latest[th.ID] = th.UpdatedAt
	at := th.UpdatedAt
	for _, run := range r.ownRuns(th.ID) {
		at = later(at, run.UpdatedAt)
	}
	for _, c := range r.children(th.ID) {
		at = later(at, r.latestBeneath(c))
	}
	r.latest[th.ID] = at
	return at
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// treeItem is one message of a session as the reader is shown it, when it
// last changed, and the runs beneath it: its own, or those its pick's calls
// led to.
type treeItem struct {
	msg    SessionMessage
	latest time.Time
	person bool
	runs   []runs.Run
}

// treeTurn is a person's message and what follows it in its session: the
// agents' messages up to the next person's message, and the child sessions
// started for it. A session that starts with an agent's message has that
// message head its first turn.
type treeTurn struct {
	head  treeItem
	after []treeItem
	kids  []threads.Thread
}

// turns lays a session out as turns, each child session the reader may
// read under the message it was started for: the first a pick called it
// for, else the last person's message posted before it.
func (r *treeReader) turns(th threads.Thread) []*treeTurn {
	tenant := r.who.Tenant
	seen := r.seer.Turns(th, r.p.turnsOf(tenant, th))
	_, seen = r.p.ShowTurns(tenant, r.seer.Thread(th), seen)
	var out []*treeTurn
	calledFor := map[string]int{}
	for _, t := range seen {
		m, at, ok := sessionMessageOf(th.ID, t)
		if !ok {
			continue
		}
		it := treeItem{msg: m, latest: at, person: t.Run == nil}
		if t.Run != nil {
			it.runs = []runs.Run{*t.Run}
		}
		if it.person || len(out) == 0 {
			out = append(out, &treeTurn{head: it})
		} else {
			out[len(out)-1].after = append(out[len(out)-1].after, it)
		}
		if !it.person {
			continue
		}
		pm, called, ok := r.p.pickFor(tenant, th, *t.Message, r.see)
		if !ok {
			continue
		}
		pick := treeItem{msg: pm, latest: pm.At, runs: called}
		for _, run := range called {
			pick.latest = later(pick.latest, run.UpdatedAt)
		}
		for _, part := range pm.Parts {
			if _, done := calledFor[part.Session]; part.Session != "" && !done {
				calledFor[part.Session] = len(out) - 1
			}
		}
		out[len(out)-1].after = append(out[len(out)-1].after, pick)
	}
	kids := r.children(th.ID)
	if len(out) == 0 {
		out = []*treeTurn{{head: r.standIn(th)}}
	}
	for _, c := range kids {
		i, ok := calledFor[c.ID]
		if !ok {
			i = 0
			for j, turn := range out {
				if turn.head.person && !turn.head.msg.At.After(c.CreatedAt) {
					i = j
				}
			}
		}
		out[i].kids = append(out[i].kids, c)
	}
	return out
}

// standIn heads a session no message is shown in yet: its title, said by
// whoever started it, else by its agent.
func (r *treeReader) standIn(th threads.Thread) treeItem {
	author := SessionAuthor{Kind: "agent", Name: th.Session.Agent}
	if th.CreatedBy != "" {
		author = SessionAuthor{Kind: "person", Name: r.p.shower(r.who.Tenant).name(th.CreatedBy)}
	}
	return treeItem{msg: SessionMessage{ID: "", Session: th.ID, Author: author, Status: "done", Parts: []SessionPart{{Type: "text", Text: th.Title}}, At: th.CreatedAt}, latest: th.CreatedAt, person: author.Kind == "person"}
}

// read is the node message names in th ("" or its first message for the
// session's own), depth levels deep.
func (r *treeReader) read(th threads.Thread, message string, depth int) (MessageNode, error) {
	turns := r.turns(th)
	if message == "" || message == turns[0].head.msg.ID {
		return r.sessionNodeOf(th, turns, depth), nil
	}
	for i, turn := range turns {
		if i > 0 && turn.head.msg.ID == message {
			return r.turnNode(th, turn, depth), nil
		}
		for _, it := range turn.after {
			if it.msg.ID == message {
				return r.itemNode(th, it), nil
			}
		}
	}
	return MessageNode{}, threads.ErrNotFound
}

// sessionNode is a session as its first message, depth levels deep.
func (r *treeReader) sessionNode(th threads.Thread, depth int) MessageNode {
	return r.sessionNodeOf(th, r.turns(th), depth)
}

// sessionNodeOf is a session laid out as turns, standing as its first
// message: beneath it what follows that message and the child sessions
// started for it, then the session's later people's messages.
func (r *treeReader) sessionNodeOf(th threads.Thread, turns []*treeTurn, depth int) MessageNode {
	first := turns[0]
	n := r.itemNode(th, first.head)
	n.ID = th.ID
	n.Status = sessionStatus(r.runsBeneath(th))
	n.LatestAt = later(n.LatestAt, r.latestBeneath(th))
	kids := r.pending(th, first)
	for _, turn := range turns[1:] {
		kids = append(kids, pendingNode{at: turn.head.msg.At, latest: r.turnLatest(turn), build: func(d int) MessageNode { return r.turnNode(th, turn, d) }})
	}
	return r.grow(n, kids, depth)
}

// turnNode is a person's message beneath its session's first, with what
// follows it and the child sessions started for it.
func (r *treeReader) turnNode(th threads.Thread, turn *treeTurn, depth int) MessageNode {
	n := r.itemNode(th, turn.head)
	var beneath []runs.Run
	for _, it := range turn.after {
		beneath = append(beneath, it.runs...)
	}
	for _, c := range turn.kids {
		beneath = append(beneath, r.runsBeneath(c)...)
	}
	n.Status = sessionStatus(beneath)
	return r.grow(n, r.pending(th, turn), depth)
}

// turnLatest is when anything beneath a turn last changed.
func (r *treeReader) turnLatest(turn *treeTurn) time.Time {
	at := later(turn.head.msg.At, turn.head.latest)
	for _, it := range turn.after {
		at = later(at, it.latest)
	}
	for _, c := range turn.kids {
		at = later(at, r.latestBeneath(c))
	}
	return at
}

// itemNode is one message with nothing grown beneath it.
func (r *treeReader) itemNode(th threads.Thread, it treeItem) MessageNode {
	role := "assistant"
	if it.person {
		role = "user"
	}
	parts := it.msg.Parts
	if parts == nil {
		parts = []SessionPart{}
	}
	return MessageNode{ID: messageID(th.ID, it.msg.ID), Session: th.ID, Role: role, Author: it.msg.Author, Status: it.msg.Status, Content: parts, At: it.msg.At, LatestAt: later(it.msg.At, it.latest), Children: []MessageNode{}}
}

// pendingNode is a child not yet grown: when it was said, when anything
// beneath it last changed, and how to grow it.
type pendingNode struct {
	at     time.Time
	latest time.Time
	build  func(depth int) MessageNode
}

// pending are the children of a turn's message: what follows it, and the
// child sessions started for it.
func (r *treeReader) pending(th threads.Thread, turn *treeTurn) []pendingNode {
	var out []pendingNode
	for _, it := range turn.after {
		out = append(out, pendingNode{at: it.msg.At, latest: later(it.msg.At, it.latest), build: func(int) MessageNode { return r.itemNode(th, it) }})
	}
	for _, c := range turn.kids {
		out = append(out, pendingNode{at: c.CreatedAt, latest: r.latestBeneath(c), build: func(d int) MessageNode { return r.sessionNode(c, d) }})
	}
	return out
}

// grow counts a node's children and when anything beneath it last changed,
// and with depth left grows its latest children, in time order, while the
// read has room for them.
func (r *treeReader) grow(n MessageNode, kids []pendingNode, depth int) MessageNode {
	n.ChildrenCount = len(kids)
	for _, k := range kids {
		n.LatestAt = later(n.LatestAt, k.latest)
	}
	if depth == 0 {
		return n
	}
	slices.SortStableFunc(kids, func(a, b pendingNode) int { return a.at.Compare(b.at) })
	if len(kids) > maxNodeChildren {
		kids = kids[len(kids)-maxNodeChildren:]
	}
	for _, k := range kids {
		if r.left <= 0 {
			break
		}
		r.left--
		n.Children = append(n.Children, k.build(depth-1))
	}
	return n
}
