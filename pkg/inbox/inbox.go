// Package inbox is the one place things that need a person land, and the place
// they are resolved from.
//
// A message is like an email with typed parts. A part is something to read or
// something to decide: acknowledge an alert, retry a failed run, choose which
// proposed memories to keep, raise a limit. Alert rules decide what to watch
// for. The inbox is where what they found gets dealt with.
//
// The store is a projection. Everything that happens to a message is a Change,
// Apply is the only thing that alters state, and Rebuild replays changes into
// the same state. So what the inbox says can always be accounted for.
//
// The inbox does not decide who may do what, and it does not carry out a
// decision. The platform checks the caller's rights and acts on the source
// (the alert, the run, the notes), then records the outcome here.
//
// Built with Open, the store writes each change through a state.Store before
// it takes effect, and replays them when it is built again.
//
// # Retention and counts
//
// What was settled for everyone longer ago than a retention period is
// dropped (Retain): the message, every change about it, and its records in
// the state.Store, so the record and a replay hold only what is kept. A
// count (Count) is added up from counters kept as changes apply, so it never
// reads what is settled.
//
// # Broadcasts
//
// An announcement is one message many people read as their own: a
// broadcast. Who it reaches is decided when it is read, through a
// "project:<name>" audience entry and the projects a reader belongs to, so
// a post is never copied per person. Dismissing or snoozing a broadcast
// hides it for that reader alone; withdrawing it closes it for everyone.
package inbox

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/urmzd/warp/pkg/state"
)

// Errors a caller can match.
var (
	ErrNotFound = errors.New("inbox: not found")
	ErrCursor   = errors.New("inbox: invalid cursor")
	ErrInvalid  = errors.New("inbox: invalid")
	// ErrLimit reports a project or a tenant that has posted as many
	// announcements today as it may.
	ErrLimit = errors.New("inbox: limit reached")
)

// Kind says what a message is about.
type Kind string

const (
	KindAlert         Kind = "alert"
	KindFailure       Kind = "failure"
	KindMemory        Kind = "memory"
	KindBudget        Kind = "budget"
	KindApproval      Kind = "approval"
	KindClarification Kind = "clarification"
	// KindResult is the answer of a run nobody was waiting on.
	KindResult Kind = "result"
	// KindArtifact says a file was published to storage, or a new version of
	// one was. It is there to be read: nothing waits on it.
	KindArtifact Kind = "artifact"
	// KindMention is a message in a shared thread that tags the person it is
	// to, by name or as one of the experts it tags.
	KindMention Kind = "mention"
	// KindAnnouncement is news posted in a project's inbox, by a person or
	// an agent. It is a broadcast, only there to be read, and never needs
	// anyone.
	KindAnnouncement Kind = "announcement"
)

// State is where a message stands.
type State string

const (
	Open      State = "open"
	Resolved  State = "resolved"
	Dismissed State = "dismissed"
	Snoozed   State = "snoozed"
)

// Severity orders messages by how much they matter.
type Severity string

const (
	Info     Severity = "info"
	Warning  Severity = "warning"
	Critical Severity = "critical"
)

// PartType says what a part holds, and so what can be done with it.
type PartType string

const (
	PartText    PartType = "text"
	PartAlert   PartType = "alert"
	PartFailure PartType = "failure"
	PartMemory  PartType = "memory"
	PartBudget  PartType = "budget"
	// PartApproval, PartChoice and PartReply are a question a run is parked
	// on: approve or deny a tool call, choose one of some options, or reply
	// in words. Each carries the question in Question.
	PartApproval PartType = "approval"
	PartChoice   PartType = "choice"
	PartReply    PartType = "reply"
	// PartArtifact is a published file, to open or download.
	PartArtifact PartType = "artifact"
	// PartMention is a thread's message that tags the reader, to reply to
	// or set aside.
	PartMention PartType = "mention"
)

// PartState says whether a part still needs someone.
type PartState string

const (
	// PartOpen needs a decision.
	PartOpen PartState = "open"
	// PartResolved has one.
	PartResolved PartState = "resolved"
	// PartInfo is only there to be read.
	PartInfo PartState = "info"
)

// From is who a message is from.
type From struct {
	// Kind is "rule", "agent", "system" or "person".
	Kind string `json:"kind"`
	// Name is the rule, the agent or the part of the system, and a person's
	// subject. A mention keeps no subject: the store keeps the name its
	// poster went by when they posted, and it is handed out naming them as
	// threads do, by handle.
	Name string `json:"name"`
	// Display is what a person who sent it is called: their name, else their
	// handle, never a subject that holds an address; for a mention, what its
	// message is called now. The store leaves it empty: it is filled in when
	// the message is read.
	Display string `json:"display,omitempty"`
	// ApprovedBy is the person who published an agent's draft, by subject.
	ApprovedBy string `json:"approved_by,omitempty"`
}

// Source links a message to what it is about.
type Source struct {
	Alert   string `json:"alert,omitempty"`
	Rule    string `json:"rule,omitempty"`
	Run     string `json:"run,omitempty"`
	Thread  string `json:"thread,omitempty"`
	Agent   string `json:"agent,omitempty"`
	Project string `json:"project,omitempty"`
	// Target is the agent a question's tool call would call, when the
	// question is about calling one: the callee whose queue it is in.
	Target string `json:"target,omitempty"`
	// ThreadTitle is what the conversation it is about is called, Thread
	// or else Run's, for a reader who may read it. It is filled in when the
	// message is shown, and never stored.
	ThreadTitle string `json:"thread_title,omitempty"`
}

// AlertPart is an alert to acknowledge.
type AlertPart struct {
	ID       string `json:"id"`
	Rule     string `json:"rule"`
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Body     string `json:"body,omitempty"`
}

// FailurePart is a run that failed.
type FailurePart struct {
	Run       string `json:"run"`
	Agent     string `json:"agent"`
	Error     string `json:"error"`
	Retryable bool   `json:"retryable"`
}

// NoteRef is one proposed memory.
type NoteRef struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// MemoryPart is a set of proposed memories to choose from.
type MemoryPart struct {
	// Owner is who would remember them, as the memory store names it.
	Owner   string    `json:"owner"`
	Agent   string    `json:"agent,omitempty"`
	Project string    `json:"project,omitempty"`
	Notes   []NoteRef `json:"notes"`
}

// Limits are the numbers a billing policy sets.
type Limits struct {
	MaxRuns       int   `json:"max_runs,omitempty"`
	MaxConcurrent int   `json:"max_concurrent,omitempty"`
	SpendLimit    int64 `json:"spend_limit,omitempty"`
	MaxTokens     int64 `json:"max_tokens,omitempty"`
}

// BudgetPart is a limit that was reached.
type BudgetPart struct {
	ScopeKind string `json:"scope_kind"`
	ScopeName string `json:"scope_name,omitempty"`
	// Limit is which one: "runs", "concurrent", "spend", "tokens" or "lease".
	Limit   string `json:"limit"`
	Period  string `json:"period"`
	Version int    `json:"version"`
	// Used and Max are where the scope stood against that limit when it was
	// reached: runs, runs at once, micro-USD, or tokens.
	Used int64 `json:"used"`
	// Refused is true when a run was turned away or stopped at the limit,
	// false for a warning.
	Refused bool   `json:"refused"`
	Max     int64  `json:"max"`
	Current Limits `json:"current"`
}

// ArtifactPart is one published version of a file.
type ArtifactPart struct {
	// ID is the artifact version's id, as the artifacts routes name it.
	ID          string `json:"id"`
	Title       string `json:"title"`
	Kind        string `json:"kind"`
	Mime        string `json:"mime,omitempty"`
	Size        int64  `json:"size"`
	Version     int    `json:"version"`
	Description string `json:"description,omitempty"`
	// Previous is the id of the version this one replaces, when it is not the first.
	Previous string `json:"previous,omitempty"`
}

// MaxExcerpt bounds how much of a message a mention carries, in characters.
const MaxExcerpt = 300

// MentionPart is a message in a shared thread that tags the reader.
type MentionPart struct {
	Thread  string `json:"thread"`
	Message string `json:"message"`
	// Project is the thread's project, "" the root, and Channel its channel.
	Project string `json:"project"`
	Channel string `json:"channel"`
	// By is the handle of the person who posted it, as the thread shows
	// them: never a subject, which can hold an address.
	By string `json:"by"`
	// Excerpt is the start of what they wrote, at most MaxExcerpt.
	Excerpt string `json:"excerpt"`
}

// Excerpt is the start of a message's text, as a mention carries it: at most
// MaxExcerpt characters, ending in an ellipsis when it was cut.
func Excerpt(text string) string {
	r := []rune(strings.TrimSpace(text))
	if len(r) <= MaxExcerpt {
		return string(r)
	}
	return strings.TrimRight(string(r[:MaxExcerpt-1]), " \t\r\n") + "…"
}

// MentionKey names the mention of one person in one message, so the same
// tag is raised once.
func MentionKey(thread, message, subject string) string {
	return "mention:" + thread + ":" + message + ":" + subject
}

// QuestionTool is the call an approval is about.
type QuestionTool struct {
	Name string `json:"name"`
	// CallID is the model's id for the call.
	CallID string `json:"call_id,omitempty"`
	// Arguments are what the model asked to call it with, as a JSON object.
	Arguments json.RawMessage `json:"arguments,omitempty"`
	// Schema is the tool's parameter schema. Edited arguments must satisfy it.
	Schema json.RawMessage `json:"schema,omitempty"`
	// ActsForPerson reports that the call runs with the credential of the
	// person the run acts for, a tool that acts as them. Anyone who may
	// approve it may approve or deny it as asked, but only that person may
	// approve it with edited arguments, so a surface offers nobody else an
	// edit.
	ActsForPerson bool `json:"acts_for_person,omitempty"`
}

// QuestionPart is a question a run is parked on.
type QuestionPart struct {
	// Run is the run that asked, and ID the question within it.
	Run string `json:"run"`
	ID  string `json:"id"`
	// Kind is approval, choice or reply.
	Kind   string `json:"kind"`
	Agent  string `json:"agent,omitempty"`
	Prompt string `json:"prompt"`
	// Options are what a choice offers.
	Options []string      `json:"options,omitempty"`
	Tool    *QuestionTool `json:"tool,omitempty"`
	// AskedOf is who the agent asked, as a hint for routing.
	AskedOf string `json:"asked_of,omitempty"`
	// ExpiresAt is when the question takes its default if nobody answers.
	ExpiresAt time.Time `json:"expires_at,omitzero"`
	// Default is what the run carries on with if it expires: "deny" for an
	// approval, the stated default for a choice or a reply, or empty when
	// the agent is told nobody answered.
	Default string `json:"default,omitempty"`
	// AskedAt is when the run asked.
	AskedAt time.Time `json:"asked_at,omitzero"`
	// Via is the run the question was raised through, when a run waiting on
	// another shows its question: the caller of the run that asked.
	Via string `json:"via,omitempty"`
	// Approvers, when set, are who may answer this one approval in place of
	// the spec's: the callee's call policy's approvers, for a call to an
	// agent. SelfApproval goes with them.
	Approvers    []string `json:"approvers,omitempty"`
	SelfApproval bool     `json:"self_approval,omitempty"`
	// SteeredFor is the person whose own credentials an answer may reach,
	// when there is one, as it stood when the question was raised: the run,
	// or a run that called it, acts for them with a tool that takes their
	// own credential. Only they reply, choose, or approve with edited
	// arguments or a note; anyone else approves or denies as asked. It is a
	// hint for surfaces: answering checks again.
	SteeredFor string `json:"steered_for,omitempty"`
	// SteeredForName is what that person is called (see From.Display). The
	// store leaves it empty: it is filled in when the message is read.
	SteeredForName string `json:"steered_for_name,omitempty"`
}

// Outcome is what was decided about a part, and by whom.
type Outcome struct {
	Action string `json:"action"`
	// By is who decided, by subject, or system.
	By string    `json:"by"`
	At time.Time `json:"at"`
	// ByName is what whoever decided is called (see From.Display). The store
	// leaves it empty: it is filled in when the message is read.
	ByName string `json:"by_name,omitempty"`
	Note   string `json:"note,omitempty"`
}

// Part is one piece of a message.
type Part struct {
	ID    string    `json:"id"`
	Type  PartType  `json:"type"`
	State PartState `json:"state"`
	// Actions is what the reader may do with the part. The store leaves it
	// empty: it depends on who is reading.
	Actions []string `json:"actions"`
	Outcome *Outcome `json:"outcome,omitempty"`

	Text    string       `json:"text,omitempty"`
	Alert   *AlertPart   `json:"alert,omitempty"`
	Failure *FailurePart `json:"failure,omitempty"`
	Memory  *MemoryPart  `json:"memory,omitempty"`
	Budget  *BudgetPart  `json:"budget,omitempty"`
	// Artifact is set on an artifact part.
	Artifact *ArtifactPart `json:"artifact,omitempty"`
	// Question is set on approval, choice and reply parts.
	Question *QuestionPart `json:"question,omitempty"`
	// Draft is set on a draft part.
	Draft *DraftPart `json:"draft,omitempty"`
	// Mention is set on a mention part.
	Mention *MentionPart `json:"mention,omitempty"`
}

// Message is one thing in the inbox.
type Message struct {
	ID string `json:"id"`
	// Key names what the message is about, so the same thing is not raised
	// twice. It is not shown.
	Key    string `json:"-"`
	Tenant string `json:"-"`
	// To is the one person it is for. Empty means everyone who may see its kind.
	To string `json:"to,omitempty"`
	// Audience, when set, is who the message is for: principals by subject,
	// and roles as "role:<name>". A reader sees it only if they are named or
	// hold a role named. Empty means everyone who may see its kind.
	Audience []string `json:"-"`
	// Excluded are principals the message is never shown to, whatever else
	// admits them: the person whose request raised a call they may not
	// approve.
	Excluded []string `json:"-"`
	// Broadcast is a message each reader keeps as their own: dismissing or
	// snoozing it hides it for that reader alone. Announcements are.
	Broadcast bool `json:"-"`
	// Once is an announcement posted under a key its poster named
	// (PostOnce), which is never posted again, even once retention has
	// dropped it.
	Once         bool      `json:"-"`
	Kind         Kind      `json:"kind"`
	Subject      string    `json:"subject"`
	From         From      `json:"from"`
	At           time.Time `json:"at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Severity     Severity  `json:"severity"`
	State        State     `json:"state"`
	SnoozedUntil time.Time `json:"snoozed_until,omitzero"`
	// Unread is filled in for whoever is reading.
	Unread bool `json:"unread"`
	// Hidden is filled in for whoever is reading: they hid or snoozed this
	// broadcast for themselves, so State is theirs alone and everyone else
	// still has it. Unhide brings it back.
	Hidden bool   `json:"hidden,omitempty"`
	Source Source `json:"source"`
	Parts  []Part `json:"parts"`
	// Deliveries are where clients posted the message, by client id.
	Deliveries map[string]Delivery `json:"deliveries,omitempty"`
}

func (m Message) clone() Message {
	m.Parts = slices.Clone(m.Parts)
	for i := range m.Parts {
		p := &m.Parts[i]
		if p.Outcome != nil {
			o := *p.Outcome
			p.Outcome = &o
		}
		if p.Question != nil {
			q := *p.Question
			q.Options = slices.Clone(q.Options)
			if q.Tool != nil {
				t := *q.Tool
				q.Tool = &t
			}
			p.Question = &q
		}
		if p.Memory != nil {
			mem := *p.Memory
			mem.Notes = slices.Clone(mem.Notes)
			p.Memory = &mem
		}
		if p.Mention != nil {
			mention := *p.Mention
			p.Mention = &mention
		}
		p.Actions = slices.Clone(p.Actions)
		p.Draft = p.Draft.clone()
	}
	m.Audience = slices.Clone(m.Audience)
	m.Excluded = slices.Clone(m.Excluded)
	m.Deliveries = cloneDeliveries(m.Deliveries)
	return m
}

// Reader is who is reading: their subject, the roles they hold, which
// decide whether a message addressed to a role is theirs, and the projects
// whose announcements reach them.
type Reader struct {
	Subject string
	Roles   []string
	// Projects are where a "project:<name>" audience entry reaches the
	// reader: the projects they are a member of and every ancestor of
	// those, as the platform reads them from the tree.
	Projects []string
	// Owns are where an "owners:<name>" audience entry reaches the reader:
	// the projects they own, as the platform reads them from the tree.
	Owns []string
}

// audienceProject starts an audience entry that names a project.
const audienceProject = "project:"

// ProjectAudience is the audience entry of a project's members and of the
// members of every project below it. The root's is "project:".
func ProjectAudience(project string) string { return audienceProject + project }

// audienceOwners starts an audience entry that names a project's owners.
const audienceOwners = "owners:"

// OwnersAudience is the audience entry of whoever owns a project when the
// message is read, through an entry there or above it. The root's is
// "owners:".
func OwnersAudience(project string) string { return audienceOwners + project }

// For reports whether a message is addressed to a reader. Who may see its
// kind at all is the caller's to decide. A project entry reaches only a
// reader with a subject.
func (m Message) For(r Reader) bool {
	if m.To != "" && m.To != r.Subject {
		return false
	}
	if slices.Contains(m.Excluded, r.Subject) {
		return false
	}
	if len(m.Audience) == 0 {
		return true
	}
	if r.Subject != "" && slices.Contains(m.Audience, r.Subject) {
		return true
	}
	for _, role := range r.Roles {
		if role != "" && slices.Contains(m.Audience, "role:"+role) {
			return true
		}
	}
	for _, p := range r.Projects {
		if r.Subject != "" && slices.Contains(m.Audience, ProjectAudience(p)) {
			return true
		}
	}
	for _, p := range r.Owns {
		if r.Subject != "" && slices.Contains(m.Audience, OwnersAudience(p)) {
			return true
		}
	}
	return false
}

// What names a change.
type What string

const (
	Raised      What = "raised"
	MarkRead    What = "read"
	MarkUnread  What = "unread"
	DidResolve  What = "resolved"
	DidDismiss  What = "dismissed"
	DidSnooze   What = "snoozed"
	DidWithdraw What = "withdrawn"
	// DidUnhide is a reader bringing back a broadcast they hid or snoozed
	// for themselves.
	DidUnhide What = "unhidden"
)

// Change is one thing that happened to a message. The list of them is the
// record of the inbox, and enough to rebuild it.
type Change struct {
	Tenant  string    `json:"-"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
	By      string    `json:"by,omitempty"`
	What    What      `json:"what"`
	Part    string    `json:"part,omitempty"`
	Action  string    `json:"action,omitempty"`
	Note    string    `json:"note,omitempty"`
	Until   time.Time `json:"until,omitzero"`
	// Raised is the message as it was raised, on a Raised change.
	Raised *Message `json:"-"`
	// Delivery is where the client in By posted the message, on a
	// Delivered change.
	Delivery *Delivery `json:"-"`
}

// Notice says a message changed. It carries no content: a reader fetches the
// message, and is shown only what they may see.
type Notice struct {
	ID        string    `json:"id"`
	Kind      Kind      `json:"kind"`
	State     State     `json:"state"`
	UpdatedAt time.Time `json:"updated_at"`
}

type tenant struct {
	messages []*Message // oldest first
	byKey    map[string]*Message
	byID     map[string]*Message
	// byPost finds a message by a client's receipt (postKey), as the
	// Delivered changes recorded it.
	byPost map[string]*Message
	read   map[string]map[string]bool // message id -> subject
	// hidden is how each reader set a broadcast aside for themselves:
	// message id -> subject -> how.
	hidden map[string]map[string]hide
	posted posted
	log    []Change
	// seqs are the keys log's changes are kept under, one each, in order,
	// and seq the key the next change is kept under. Retention leaves gaps.
	seqs []int64
	seq  int64
	// next is the number of the last message id issued.
	next int
	// cut is what retention keeps of what it dropped.
	cut      cut
	tally    tally
	watchers map[int]chan Notice
	nextW    int
}

// hide is one reader's own state of a broadcast: dismissed, or snoozed
// until a time.
type hide struct {
	state State
	until time.Time
}

// posted counts the announcements raised on the latest day one was, in UTC,
// for Post's caps. It is rebuilt with everything else when changes are
// replayed.
type posted struct {
	day   time.Time
	total int
	by    map[string]int
}

func dayOf(at time.Time) time.Time {
	at = at.UTC()
	return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
}

func (p *posted) add(project string, at time.Time) {
	switch day := dayOf(at); {
	case day.After(p.day):
		*p = posted{day: day, total: 1, by: map[string]int{project: 1}}
	case day.Equal(p.day):
		p.total++
		p.by[project]++
	}
}

// on is how many were posted on at's day: in the project, and in all.
func (p posted) on(project string, at time.Time) (int, int) {
	if !dayOf(at).Equal(p.day) {
		return 0, 0
	}
	return p.by[project], p.total
}

// Store holds every tenant's inbox.
type Store struct {
	mu      sync.Mutex
	tenants map[string]*tenant
	// kept is where changes are written, one record each, in the order they
	// were made. Nil keeps them only in memory.
	kept *state.Table[storedChange]
}

// storedChange is a change as it is written, with what the wire forms of a
// change and a message leave out.
type storedChange struct {
	Change
	Raised   *storedMessage `json:"raised,omitempty"`
	Delivery *Delivery      `json:"delivery,omitempty"`
	// Cut is set on the one record kept under cutKey, which is no change:
	// what retention keeps of what it dropped.
	Cut *cut `json:"cut,omitempty"`
}

type storedMessage struct {
	Message
	Key       string   `json:"key"`
	Audience  []string `json:"audience,omitempty"`
	Excluded  []string `json:"excluded,omitempty"`
	Broadcast bool     `json:"broadcast,omitempty"`
	Once      bool     `json:"once,omitempty"`
}

func storedOf(c Change) storedChange {
	out := storedChange{Change: c, Delivery: c.Delivery}
	if c.Raised != nil {
		out.Raised = &storedMessage{Message: *c.Raised, Key: c.Raised.Key, Audience: c.Raised.Audience, Excluded: c.Raised.Excluded, Broadcast: c.Raised.Broadcast, Once: c.Raised.Once}
	}
	return out
}

func (sc storedChange) change(tenant string) Change {
	c := sc.Change
	c.Tenant, c.Delivery = tenant, sc.Delivery
	if sc.Raised != nil {
		m := sc.Raised.Message
		m.Key, m.Audience, m.Excluded, m.Broadcast, m.Once = sc.Raised.Key, sc.Raised.Audience, sc.Raised.Excluded, sc.Raised.Broadcast, sc.Raised.Once
		// Records kept before Once was recorded name it by their key: Post
		// keys every announcement "announce:...", and any other key on a
		// broadcast announcement is one its poster named (PostOnce).
		if m.Broadcast && m.Kind == KindAnnouncement && m.Key != "" && !strings.HasPrefix(m.Key, "announce:") {
			m.Once = true
		}
		c.Raised = &m
	}
	return c
}

// NewStore returns an empty inbox in memory.
func NewStore() *Store { return &Store{tenants: map[string]*tenant{}} }

// OpenStore returns an inbox that keeps its changes in st, rebuilt from what st
// already has. A nil st is NewStore.
func OpenStore(st state.Store) (*Store, error) {
	s := NewStore()
	s.kept = state.NewTable[storedChange](st, "inbox")
	err := s.kept.Load(func(name, key string, sc storedChange) error {
		t := s.of(name)
		if key == cutKey {
			if sc.Cut != nil {
				t.restore(*sc.Cut)
			}
			return nil
		}
		seq, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			return fmt.Errorf("inbox: %s: a change kept under %q", name, key)
		}
		t.applyAt(sc.change(name), seq)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("inbox: load: %w", err)
	}
	return s, nil
}

// applyAt applies a change kept under a key, as a replay does.
func (t *tenant) applyAt(c Change, seq int64) {
	t.seq = seq
	t.apply(c)
	t.seq = max(t.seq, seq+1)
}

// replay puts a tenant back to what the first n changes of its record say,
// keeping who is watching it. It is how a change that could not be written is
// undone.
func (s *Store) replay(name string, n int) {
	old := s.tenants[name]
	delete(s.tenants, name)
	t := s.of(name)
	for i, c := range old.log[:n] {
		t.applyAt(c, old.seqs[i])
	}
	t.restore(old.cut)
	t.watchers, t.nextW = old.watchers, old.nextW
}

func (s *Store) of(name string) *tenant {
	t, ok := s.tenants[name]
	if !ok {
		t = &tenant{
			byKey: map[string]*Message{}, byID: map[string]*Message{}, byPost: map[string]*Message{}, read: map[string]map[string]bool{},
			hidden: map[string]map[string]hide{}, tally: newTally(), watchers: map[int]chan Notice{},
		}
		s.tenants[name] = t
	}
	return t
}

// record adds a change that took effect to the tenant's record.
func (t *tenant) record(c Change) {
	t.log = append(t.log, c)
	t.seqs = append(t.seqs, t.seq)
	t.seq++
}

// issue is the id a raised message is given: the one its record names,
// when it is replayed, else the next.
func (t *tenant) issue(c Change) string {
	if n, ok := strings.CutPrefix(c.Message, "m-"); ok {
		if i, err := strconv.Atoi(n); err == nil && i > 0 {
			t.next = max(t.next, i)
			return c.Message
		}
	}
	t.next++
	return "m-" + strconv.Itoa(t.next)
}

// apply is the only thing that alters state. It must depend on nothing but
// the state and the change, so that replaying changes rebuilds the state.
func (t *tenant) apply(c Change) (*Message, bool) {
	if c.What == Raised {
		if prior := t.byKey[c.Raised.Key]; c.Raised == nil || prior != nil && prior.State != Resolved && prior.State != Dismissed {
			return nil, false
		}
		m := c.Raised.clone()
		m.ID = t.issue(c)
		m.Tenant, m.At, m.UpdatedAt, m.State, m.Unread, m.Deliveries = c.Tenant, c.At, c.At, Open, false, nil
		for i := range m.Parts {
			m.Parts[i].ID = "p" + strconv.Itoa(i+1)
			m.Parts[i].Actions, m.Parts[i].Outcome = nil, nil
			if m.Parts[i].State == "" {
				m.Parts[i].State = PartOpen
			}
		}
		t.messages = append(t.messages, &m)
		t.byKey[m.Key], t.byID[m.ID] = &m, &m
		if m.Kind == KindAnnouncement && m.Broadcast {
			t.posted.add(m.Source.Project, c.At)
		}
		t.join(&m)
		c.Message = m.ID
		t.record(c)
		return &m, true
	}

	m := t.byID[c.Message]
	if m == nil {
		return nil, false
	}
	if m.Broadcast && (c.What == DidDismiss || c.What == DidSnooze || c.What == DidUnhide) {
		return m, t.hideFor(m, c)
	}
	switch c.What {
	case MarkRead, MarkUnread:
		was := t.read[m.ID][c.By]
		if was == (c.What == MarkRead) {
			return m, false
		}
		wasOut := t.isOut(m, c.By)
		if t.read[m.ID] == nil {
			t.read[m.ID] = map[string]bool{}
		}
		t.read[m.ID][c.By] = c.What == MarkRead
		t.remark(m, c.By, wasOut)
		t.record(c)
		// Reading changes nothing anyone else sees.
		return m, false
	case Delivered:
		was, had := m.Deliveries[c.By]
		if !m.delivered(c) {
			return m, false
		}
		if had && t.byPost[postKey(c.By, was)] == m {
			delete(t.byPost, postKey(c.By, was))
		}
		t.byPost[postKey(c.By, *c.Delivery)] = m
		t.record(c)
		return m, false
	case DidResolve:
		i := slices.IndexFunc(m.Parts, func(p Part) bool { return p.ID == c.Part })
		if i < 0 || m.Parts[i].State != PartOpen {
			return m, false
		}
		m.Parts[i].State = PartResolved
		m.Parts[i].Outcome = &Outcome{Action: c.Action, By: c.By, At: c.At, Note: c.Note}
		if !slices.ContainsFunc(m.Parts, func(p Part) bool { return p.State == PartOpen }) {
			t.restate(m, Resolved, time.Time{})
		}
	case DidDismiss, DidWithdraw:
		if m.State == Resolved || m.State == Dismissed {
			return m, false
		}
		// A withdrawal from before it recorded who reads as the system's.
		if c.What == DidWithdraw && c.By == "" {
			c.By = "system"
		}
		t.restate(m, Dismissed, time.Time{})
	case DidSnooze:
		if m.State == Resolved || m.State == Dismissed {
			return m, false
		}
		t.restate(m, Snoozed, c.Until)
	default:
		return m, false
	}
	m.UpdatedAt = c.At
	t.record(c)
	return m, true
}

// restate moves a message to a state everyone sees, and its count with it.
func (t *tenant) restate(m *Message, st State, until time.Time) {
	t.leave(m)
	m.State, m.SnoozedUntil = st, until
	t.join(m)
}

// hideFor records one reader dismissing, snoozing or unhiding a broadcast
// for themselves. It changes nothing anyone else sees, so it reports no
// change to tell watchers about. A broadcast already closed for everyone, a
// dismiss or snooze of one the reader dismissed, a snooze to the time it is
// already snoozed until, an unhide of one the reader never set aside, or a
// change with no reader, is not recorded.
func (t *tenant) hideFor(m *Message, c Change) bool {
	if c.By == "" || m.State == Resolved || m.State == Dismissed {
		return false
	}
	wasOut := t.isOut(m, c.By)
	if c.What == DidUnhide {
		if _, ok := t.hidden[m.ID][c.By]; !ok {
			return false
		}
		delete(t.hidden[m.ID], c.By)
		t.remark(m, c.By, wasOut)
		t.record(c)
		return false
	}
	if t.hidden[m.ID][c.By].state == Dismissed {
		return false
	}
	h := hide{state: Dismissed}
	if c.What == DidSnooze {
		h = hide{state: Snoozed, until: c.Until}
	}
	if was, ok := t.hidden[m.ID][c.By]; ok && was.state == h.state && was.until.Equal(h.until) {
		return false
	}
	if t.hidden[m.ID] == nil {
		t.hidden[m.ID] = map[string]hide{}
	}
	t.hidden[m.ID][c.By] = h
	t.remark(m, c.By, wasOut)
	t.record(c)
	return false
}

// stateFor is a message's state for one reader at a moment: a broadcast as
// the reader set it aside, and a snooze that has run out open. A zero now
// leaves a snooze as it is. own reports that the state is the reader's own
// hide or snooze of a broadcast, which nobody else has.
func (t *tenant) stateFor(m *Message, subject string, now time.Time) (st State, until time.Time, own bool) {
	st, until = m.State, m.SnoozedUntil
	if h, ok := t.hidden[m.ID][subject]; ok && m.Broadcast && (st == Open || st == Snoozed) {
		st, until, own = h.state, h.until, true
	}
	if st == Snoozed && !now.IsZero() && !now.Before(until) {
		return Open, time.Time{}, false
	}
	return st, until, own
}

// view is a message as one reader sees it.
func (t *tenant) view(m *Message, subject string, now time.Time) Message {
	out := m.clone()
	out.State, out.SnoozedUntil, out.Hidden = t.stateFor(m, subject, now)
	out.Unread = !t.read[m.ID][subject]
	return out
}

func (t *tenant) tell(m *Message) {
	n := Notice{ID: m.ID, Kind: m.Kind, State: m.State, UpdatedAt: m.UpdatedAt}
	for _, ch := range t.watchers {
		// A watcher that is not keeping up misses a notice, never blocks the
		// inbox. It refetches on the next one.
		select {
		case ch <- n:
		default:
		}
	}
}

func (s *Store) do(c Change) (Message, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.doLocked(c)
}

// doLocked is do with the lock held.
func (s *Store) doLocked(c Change) (Message, bool, error) {
	t := s.of(c.Tenant)
	if c.What != Raised && t.byID[c.Message] == nil {
		return Message{}, false, fmt.Errorf("%w: message %q", ErrNotFound, c.Message)
	}
	n := len(t.log)
	m, changed := t.apply(c)
	if err := s.commitLocked(c.Tenant, n); err != nil {
		return Message{}, false, err
	}
	if m == nil {
		return Message{}, false, nil
	}
	if changed {
		t.tell(m)
	}
	return t.view(m, c.By, time.Time{}), changed, nil
}

// commitLocked writes what a tenant's record gained after its first n
// changes, in one batch, or puts the tenant back to those n when it cannot
// be written. The lock is held.
func (s *Store) commitLocked(name string, n int) error {
	t := s.tenants[name]
	if len(t.log) == n || !s.kept.Enabled() {
		return nil
	}
	b := s.kept.Batch()
	for i := n; i < len(t.log); i++ {
		b.Put(name, state.Seq(t.seqs[i]), storedOf(t.log[i]))
	}
	if err := b.Commit(); err != nil {
		s.replay(name, n)
		return fmt.Errorf("inbox: write: %w", err)
	}
	return nil
}

// Raise puts a message in the inbox. key names what it is about: while a
// message with that key is open, a second is not raised and the first is
// returned.
func (s *Store) Raise(tenantName, key string, m Message, at time.Time) (Message, bool, error) {
	if existing, ok := s.ByKey(tenantName, key); ok && existing.State != Resolved && existing.State != Dismissed {
		return existing, false, nil
	}
	m.Key = key
	return s.do(Change{Tenant: tenantName, What: Raised, At: at, By: m.From.Name, Raised: &m})
}

// ByID returns one message as it is, for the platform's own use: who may
// read it is the caller's to decide.
func (s *Store) ByID(tenantName, id string) (Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.of(tenantName).byID[id]
	if m == nil {
		return Message{}, false
	}
	return m.clone(), true
}

// ByKey returns the latest message raised under a key.
func (s *Store) ByKey(tenantName, key string) (Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.of(tenantName).byKey[key]
	if m == nil {
		return Message{}, false
	}
	return m.clone(), true
}

// Get returns one message as a reader known only by their subject sees it.
func (s *Store) Get(tenantName, id, reader string) (Message, error) {
	return s.GetAs(tenantName, id, Reader{Subject: reader})
}

// GetAs returns one message as a reader sees it. A message not addressed to
// them does not exist for them.
func (s *Store) GetAs(tenantName, id string, r Reader) (Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.of(tenantName)
	m := t.byID[id]
	if m == nil || !m.For(r) {
		return Message{}, fmt.Errorf("%w: message %q", ErrNotFound, id)
	}
	return t.view(m, r.Subject, time.Time{}), nil
}

// Read marks a message read or unread for one person.
func (s *Store) Read(tenantName, id, by string, read bool, at time.Time) (Message, error) {
	what := MarkUnread
	if read {
		what = MarkRead
	}
	m, _, err := s.do(Change{Tenant: tenantName, Message: id, What: what, By: by, At: at})
	return m, err
}

// Resolve records what was decided about a part. Resolving a part that has an
// outcome changes nothing and returns the message with the first outcome.
func (s *Store) Resolve(tenantName, id, part string, o Outcome) (Message, bool, error) {
	return s.do(Change{Tenant: tenantName, Message: id, What: DidResolve, Part: part, Action: o.Action, By: o.By, At: o.At, Note: o.Note})
}

// Dismiss sets a message aside without a decision. A broadcast is set aside
// for the one who dismisses it; everyone else still has it.
func (s *Store) Dismiss(tenantName, id, by string, at time.Time) (Message, bool, error) {
	return s.do(Change{Tenant: tenantName, Message: id, What: DidDismiss, By: by, At: at})
}

// Withdraw closes a message for everyone because what it was about is gone,
// or because who posted it took it back. by is who withdrew it, or "system"
// for the platform; "" reads as "system".
func (s *Store) Withdraw(tenantName, id, by, note string, at time.Time) (Message, bool, error) {
	if by == "" {
		by = "system"
	}
	return s.do(Change{Tenant: tenantName, Message: id, What: DidWithdraw, By: by, Note: note, At: at})
}

// Unhide brings back a broadcast one reader hid or snoozed for themselves,
// as everyone else has it. It changes nothing for anyone else, and nothing
// at all for a message that is not a broadcast or that the reader never set
// aside.
func (s *Store) Unhide(tenantName, id, by string, at time.Time) (Message, bool, error) {
	return s.do(Change{Tenant: tenantName, Message: id, What: DidUnhide, By: by, At: at})
}

// Snooze hides a message until a time, when it is open again. A broadcast is
// hidden for the one who snoozes it alone.
func (s *Store) Snooze(tenantName, id, by string, until, at time.Time) (Message, bool, error) {
	if !until.After(at) {
		return Message{}, false, fmt.Errorf("%w: snooze until a time that is still to come", ErrInvalid)
	}
	return s.do(Change{Tenant: tenantName, Message: id, What: DidSnooze, By: by, Until: until, At: at})
}

// History returns everything that happened to one message, oldest first:
// the record, every reader's own marks included.
func (s *Store) History(tenantName, id string) []Change {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Change
	for _, c := range s.of(tenantName).log {
		if c.Message == id {
			out = append(out, c)
		}
	}
	return out
}

// MaxHistory is the most changes HistoryFor returns.
const MaxHistory = 100

// HistoryFor returns what happened to one message as one reader is shown it,
// oldest first: at most MaxHistory changes, the newest kept. A reader's own
// marks are theirs alone, so another reader's reads and unreads, and on a
// broadcast another reader's hiding or snoozing of their own copy, are left
// out; the record keeps them for replay. What changed the message for
// everyone is shown to everyone.
func (s *Store) HistoryFor(tenantName, id, subject string) []Change {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.of(tenantName)
	broadcast := t.byID[id] != nil && t.byID[id].Broadcast
	out := []Change{}
	for _, c := range t.log {
		if c.Message != id || (c.By != subject && ownMark(c.What, broadcast)) {
			continue
		}
		out = append(out, c)
	}
	if len(out) > MaxHistory {
		out = slices.Clone(out[len(out)-MaxHistory:])
	}
	return out
}

// ownMark reports whether a change is one reader's mark on their own copy of
// a message: a read or an unread, and on a broadcast a hide, a snooze or an
// unhide.
func ownMark(what What, broadcast bool) bool {
	switch what {
	case MarkRead, MarkUnread:
		return true
	case DidDismiss, DidSnooze, DidUnhide:
		return broadcast
	}
	return false
}

// Changes returns a tenant's whole record, oldest first.
func (s *Store) Changes(tenantName string) []Change {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.of(tenantName).log)
}

// Rebuild returns the inbox a record describes. A record retention has cut
// rebuilds what it kept; the ids and keys its cut keeps are OpenStore's.
func Rebuild(changes []Change) *Store {
	s := NewStore()
	for _, c := range changes {
		s.of(c.Tenant).apply(c)
	}
	return s
}

// Snapshot returns every message in a tenant with who has read it, for
// comparing a rebuilt inbox with a live one.
func (s *Store) Snapshot(tenantName string) ([]Message, map[string][]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.of(tenantName)
	msgs := make([]Message, 0, len(t.messages))
	read := map[string][]string{}
	for _, m := range t.messages {
		msgs = append(msgs, m.clone())
		for who, on := range t.read[m.ID] {
			if on {
				read[m.ID] = append(read[m.ID], who)
			}
		}
		slices.Sort(read[m.ID])
	}
	return msgs, read
}

// Paging bounds for Page.
const (
	DefaultPage = 50
	MaxPage     = 200
)

// Query narrows a page of the inbox.
type Query struct {
	// Reader is whose inbox this is: it decides unread, and which messages
	// addressed to one person are included.
	Reader string
	// Roles are the roles the reader holds, for messages addressed to a role.
	Roles []string
	// ReaderProjects are the projects whose announcements reach the reader
	// (Reader.Projects), and ReaderOwns the projects they own (Reader.Owns).
	ReaderProjects []string
	ReaderOwns     []string
	// State keeps messages in one state. Empty means open. "all" means any.
	// A broadcast is in the state the reader left it in.
	State State
	Kind  Kind
	// Kinds keeps messages of any of these kinds.
	Kinds []Kind
	// Projects keeps messages about these projects (Source.Project), the
	// root's "" too when it is given.
	Projects []string
	// Shared reads Projects as the projects' own inbox, which everyone who
	// can see them reads: there a project audience entry naming one of them
	// reaches any reader. Without it, the reader's own inbox is narrowed.
	Shared bool
	// Alert keeps the messages about one alert.
	Alert string
	// Agent keeps the messages about one agent: the one that raised them,
	// or the one a call they ask about would call.
	Agent string
	// Unread keeps only what the reader has not read.
	Unread bool
	// Visible reports whether the reader may see a kind. Nil means all.
	Visible func(Kind) bool
	// Sees reports whether the reader may read what a message is about:
	// the project and thread it lives in. Nil means all. It reads only the
	// message's kind, addressing and Source's run, project, thread and
	// agent: Count asks it once for all the open messages alike in those.
	Sees   func(Message) bool
	Now    time.Time
	Limit  int
	Before string
}

// All is the State that matches every message.
const All State = "all"

// filter is a Query made ready to test many messages against.
type filter struct {
	Query
	reader   Reader
	projects map[string]bool
}

func (q Query) filter() filter {
	f := filter{Query: q, reader: Reader{Subject: q.Reader, Roles: q.Roles, Projects: q.ReaderProjects, Owns: q.ReaderOwns}}
	if len(q.Projects) > 0 {
		f.projects = make(map[string]bool, len(q.Projects))
		for _, p := range q.Projects {
			f.projects[p] = true
		}
		if q.Shared {
			f.reader.Projects = append(slices.Clone(q.ReaderProjects), q.Projects...)
		}
	}
	return f
}

// admits reports whether a message is the reader's to see at all: addressed
// to them, of a kind they may see, about what they may read. It reads only
// what a count's buckets share.
func (q filter) admits(m *Message) bool {
	if !m.For(q.reader) {
		return false
	}
	if q.Visible != nil && !q.Visible(m.Kind) {
		return false
	}
	return q.Sees == nil || q.Sees(*m)
}

func (t *tenant) match(m *Message, q filter) bool {
	if q.projects != nil && !q.projects[m.Source.Project] {
		return false
	}
	if !q.admits(m) {
		return false
	}
	if q.Kind != "" && m.Kind != q.Kind {
		return false
	}
	if len(q.Kinds) > 0 && !slices.Contains(q.Kinds, m.Kind) {
		return false
	}
	if q.Alert != "" && m.Source.Alert != q.Alert {
		return false
	}
	if q.Agent != "" && m.Source.Agent != q.Agent && m.Source.Target != q.Agent {
		return false
	}
	want := q.State
	if want == "" {
		want = Open
	}
	if st, _, _ := t.stateFor(m, q.Reader, q.Now); want != All && st != want {
		return false
	}
	return !q.Unread || !t.read[m.ID][q.Reader]
}

func encodeCursor(m *Message) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(m.At.UnixNano(), 10) + ":" + m.ID))
}

func decodeCursor(c string) (int64, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return 0, "", ErrCursor
	}
	stamp, id, ok := strings.Cut(string(raw), ":")
	if !ok || id == "" {
		return 0, "", ErrCursor
	}
	at, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil {
		return 0, "", ErrCursor
	}
	return at, id, nil
}

// Page returns one page of an inbox, newest first, and the cursor for the page
// after it. The cursor is empty at the end. A message's place is when it was
// raised, which never changes, so paging neither repeats nor skips.
func (s *Store) Page(tenantName string, q Query) ([]Message, string, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultPage
	}
	limit = min(limit, MaxPage)
	var beforeAt int64
	var beforeID string
	if q.Before != "" {
		var err error
		if beforeAt, beforeID, err = decodeCursor(q.Before); err != nil {
			return nil, "", err
		}
	}
	f := q.filter()
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.of(tenantName)
	out := []Message{}
	var last *Message
	for i := len(t.messages) - 1; i >= 0; i-- {
		m := t.messages[i]
		if q.Before != "" {
			at := m.At.UnixNano()
			// Ids are issued in order, so the later of two messages raised
			// at the same instant has the longer or greater id.
			if at > beforeAt || at == beforeAt && !older(m.ID, beforeID) {
				continue
			}
		}
		if !t.match(m, f) {
			continue
		}
		if len(out) == limit {
			return out, encodeCursor(last), nil
		}
		out = append(out, t.view(m, q.Reader, q.Now))
		last = m
	}
	return out, "", nil
}

// older reports whether id a was issued before id b.
func older(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

// Counts is what a reader's inbox holds open: Open and Unread count what
// may need them, and Updates the broadcasts open for them that they have
// not read. A broadcast counts only in Updates, so announcements never grow
// the badge.
type Counts struct {
	Open    int `json:"open"`
	Unread  int `json:"unread"`
	Updates int `json:"updates"`
}

// Count returns what is open for a reader across their whole inbox: the
// narrowing fields of q are ignored, and who is reading is kept. It is added
// up from counters kept as changes apply (tally), so it reads no message that
// is settled, and asks q.Sees once for each set of open messages alike in
// kind, addressing and what they are about.
func (s *Store) Count(tenantName string, q Query) Counts {
	q.State, q.Kind, q.Kinds, q.Projects, q.Shared, q.Alert, q.Agent, q.Unread = Open, "", nil, nil, false, "", "", false
	f := q.filter()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.of(tenantName).count(f)
}

// Bounds of an announcement.
const (
	// MaxSubject is an announcement's subject, in characters.
	MaxSubject = 150
	// MaxText is each part of an announcement, in characters.
	MaxText = 4000
)

// PostCaps bound how many announcements are posted in one UTC day: in one
// project, and in the tenant. A zero field keeps the default.
type PostCaps struct {
	PerProject int
	PerTenant  int
}

// DefaultPostCaps are 20 a project and 200 a tenant, a day.
func DefaultPostCaps() PostCaps { return PostCaps{PerProject: 20, PerTenant: 200} }

// Post raises an announcement in a project's inbox: m.Source.Project names
// the project, the root's "" included. It is a broadcast that reaches the
// members of the project and of every project below it, so its kind, its
// audience and its key ("announce:<project>:<n>") are set here, and every
// part is text, only there to be read. The person who posted it, its author
// or whoever published an agent's draft, has read it. The day's posts are
// counted and the announcement raised under one lock, so racing posts stop
// at the caps (ErrLimit).
func (s *Store) Post(tenantName string, m Message, at time.Time, caps PostCaps) (Message, error) {
	out, _, err := s.post(tenantName, "", m, at, caps)
	return out, err
}

// PostOnce is Post under a key the caller names, such as "release:0.15.0",
// for news posted once for as long as the inbox is kept. When an
// announcement was ever raised under the key, open or withdrawn since, it is
// returned with false and nothing is posted, whatever the day's caps. One
// that retention has dropped since is not returned, and is not posted again
// either.
func (s *Store) PostOnce(tenantName, key string, m Message, at time.Time, caps PostCaps) (Message, bool, error) {
	if key == "" {
		return Message{}, false, fmt.Errorf("%w: what is posted once is named by a key", ErrInvalid)
	}
	if strings.HasPrefix(key, "announce:") {
		return Message{}, false, fmt.Errorf("%w: the keys of what is posted once do not start with announce:, which Post keys its own", ErrInvalid)
	}
	return s.post(tenantName, key, m, at, caps)
}

// post is Post and PostOnce: key is the caller's, or "" for the next
// "announce:<project>:<n>".
func (s *Store) post(tenantName, key string, m Message, at time.Time, caps PostCaps) (Message, bool, error) {
	d := DefaultPostCaps()
	caps.PerProject, caps.PerTenant = cmp.Or(caps.PerProject, d.PerProject), cmp.Or(caps.PerTenant, d.PerTenant)
	m = m.clone()
	m.Severity = cmp.Or(m.Severity, Info)
	switch n := utf8.RuneCountInString(m.Subject); {
	case n == 0 || n > MaxSubject || strings.ContainsFunc(m.Subject, unicode.IsControl) || !utf8.ValidString(m.Subject):
		return Message{}, false, fmt.Errorf("%w: an announcement's subject is one line of 1 to %d characters", ErrInvalid, MaxSubject)
	case m.Severity != Info && m.Severity != Warning:
		return Message{}, false, fmt.Errorf("%w: an announcement is info or a warning", ErrInvalid)
	case m.From.Kind == "" || m.From.Name == "":
		return Message{}, false, fmt.Errorf("%w: an announcement says who it is from", ErrInvalid)
	case len(m.Parts) == 0:
		return Message{}, false, fmt.Errorf("%w: an announcement says something", ErrInvalid)
	}
	for i := range m.Parts {
		p := &m.Parts[i]
		if n := utf8.RuneCountInString(p.Text); p.Type != PartText || n == 0 || n > MaxText || !utf8.ValidString(p.Text) {
			return Message{}, false, fmt.Errorf("%w: an announcement is text of 1 to %d characters", ErrInvalid, MaxText)
		}
		p.State = PartInfo
	}
	project := m.Source.Project
	m.Kind, m.Broadcast, m.To = KindAnnouncement, true, ""
	m.Audience, m.Excluded = []string{ProjectAudience(project)}, nil
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.of(tenantName)
	if prior := t.byKey[key]; key != "" && prior != nil {
		return prior.clone(), false, nil
	}
	if key != "" && slices.Contains(t.cut.Gone, key) {
		return Message{}, false, nil
	}
	m.Once = key != ""
	inProject, inTenant := t.posted.on(project, at)
	switch {
	case inProject >= caps.PerProject:
		return Message{}, false, fmt.Errorf("%w: a project posts at most %d announcements a day", ErrLimit, caps.PerProject)
	case inTenant >= caps.PerTenant:
		return Message{}, false, fmt.Errorf("%w: a workspace posts at most %d announcements a day", ErrLimit, caps.PerTenant)
	}
	m.Key = cmp.Or(key, "announce:"+project+":"+strconv.Itoa(t.next+1))
	n := len(t.log)
	raised, _ := t.apply(Change{Tenant: tenantName, What: Raised, At: at, By: m.From.Name, Raised: &m})
	if raised == nil {
		return Message{}, false, fmt.Errorf("%w: %s was raised already", ErrInvalid, m.Key)
	}
	// The poster's read is written with the raise, so nobody, their own
	// console on the notice included, reads it unread for them first.
	if who := m.From.poster(); who != "" {
		t.apply(Change{Tenant: tenantName, Message: raised.ID, What: MarkRead, By: who, At: at})
	}
	if err := s.commitLocked(tenantName, n); err != nil {
		return Message{}, false, err
	}
	t.tell(raised)
	return t.view(raised, m.From.Name, time.Time{}), true, nil
}

// poster is the person who posted a message, by subject: a person who
// wrote it, or who published an agent's draft. "" for nobody.
func (f From) poster() string {
	if f.ApprovedBy != "" {
		return f.ApprovedBy
	}
	if f.Kind == "person" {
		return f.Name
	}
	return ""
}

// Tenants returns every tenant the inbox holds anything for.
func (s *Store) Tenants() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.tenants))
	for name, t := range s.tenants {
		if len(t.messages) > 0 {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// OpenOf returns the open and snoozed messages of a kind, for keeping the
// inbox in step with what the messages are about.
func (s *Store) OpenOf(tenantName string, kind Kind) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Message
	for _, m := range s.of(tenantName).messages {
		if m.Kind == kind && (m.State == Open || m.State == Snoozed) {
			out = append(out, m.clone())
		}
	}
	return out
}

// Watch reports changes to a tenant's inbox until cancel is called.
func (s *Store) Watch(tenantName string) (<-chan Notice, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.of(tenantName)
	ch := make(chan Notice, 32)
	id := t.nextW
	t.nextW++
	t.watchers[id] = ch
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(t.watchers, id)
	}
}
