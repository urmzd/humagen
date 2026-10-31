// Package inbox is the one place things that need a person land, and the place
// they are resolved from.
//
// A message is like an email with typed parts. A part is something to read or
// something to decide: acknowledge an alert, retry a failed run, choose which
// proposed memories to keep, raise a limit. Alert rules decide what to watch
// for. The inbox is where what they found gets dealt with.
//
// Everything that happens to a message is a Change, and apply is the only
// thing that alters what the inbox holds. Each change is kept, so what the
// inbox says can always be accounted for, and Rebuild replays changes into
// the same inbox.
//
// The inbox does not decide who may do what, and it does not carry out a
// decision. The platform checks the caller's rights and acts on the source
// (the alert, the run, the notes), then records the outcome here.
//
// It reads and writes the database per request (pkg/state, per request): it
// holds nothing in memory but this process's watchers, so any number of
// processes on one database agree, and it does not need pkg/state's
// single-writer lock. A change reads the records it depends on and writes
// them at the versions it read, in one write: of two processes answering or
// settling one message at once, one wins and the other reads again, and the
// counters kept beside the messages stay exact.
//
// # Retention and counts
//
// What was settled for everyone longer ago than a retention period is
// dropped (Retain): the message, every change about it, and every record
// kept for it. Retention pages an index of settled messages, so it never
// reads a tenant whole. A count (Count) is added up from counters kept in
// the same write as each change, so it never reads what is settled.
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

	"github.com/urmzd/humagen/pkg/state"
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
	KindSource        Kind = "source"
	KindAlert         Kind = "alert"
	KindFailure       Kind = "failure"
	KindOperation     Kind = "operation"
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
	PartSource    PartType = "source"
	PartText      PartType = "text"
	PartOperation PartType = "operation"
	PartAlert     PartType = "alert"
	PartFailure   PartType = "failure"
	PartMemory    PartType = "memory"
	PartBudget    PartType = "budget"
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
	Knowledge string `json:"knowledge,omitempty"`
	Alert     string `json:"alert,omitempty"`
	Rule      string `json:"rule,omitempty"`
	Run       string `json:"run,omitempty"`
	Thread    string `json:"thread,omitempty"`
	Agent     string `json:"agent,omitempty"`
	Project   string `json:"project,omitempty"`
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
	// Kind is approval, choice or reply, or budget for a run held at a
	// limit for a person to grant it more or stop it (Budget says which).
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
	// Budget is a budget question's limit and what approving it grants.
	Budget *QuestionBudget `json:"budget,omitempty"`
}

// QuestionBudget is the limit a run is held at, and what approving grants.
type QuestionBudget struct {
	ScopeKind string `json:"scope_kind"`
	ScopeName string `json:"scope_name,omitempty"`
	// Limit is "spend" or "tokens".
	Limit   string `json:"limit"`
	Period  string `json:"period"`
	Version int    `json:"version"`
	// Used and Max are where the scope stood when the run was held, in
	// micro-USD or tokens; Granted is what the run had been granted beyond
	// Max already this period, and Grant what approving grants it now.
	Used    int64 `json:"used"`
	Max     int64 `json:"max"`
	Granted int64 `json:"granted,omitempty"`
	Grant   int64 `json:"grant"`
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

// SourcePart is a source failure requiring an authorized refresh.
type SourcePart struct {
	Name  string `json:"name"`
	Cause string `json:"cause"`
}

// Part is one piece of a message.
// OperationPart identifies durable work needing a person without asserting a run failure.
type OperationPart struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type Part struct {
	Source *SourcePart `json:"source,omitempty"`
	ID     string      `json:"id"`
	Type   PartType    `json:"type"`
	State  PartState   `json:"state"`
	// Actions is what the reader may do with the part. The store leaves it
	// empty: it depends on who is reading.
	Actions []string `json:"actions"`
	Outcome *Outcome `json:"outcome,omitempty"`

	Operation *OperationPart `json:"operation,omitempty"`
	Text      string         `json:"text,omitempty"`
	Alert     *AlertPart     `json:"alert,omitempty"`
	Failure   *FailurePart   `json:"failure,omitempty"`
	Memory    *MemoryPart    `json:"memory,omitempty"`
	Budget    *BudgetPart    `json:"budget,omitempty"`
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
	reminded bool
	// changes is the number of the last change recorded about it.
	changes int64
	ID      string `json:"id"`
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
		if p.Source != nil {
			source := *p.Source
			p.Source = &source
		}
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
		if p.Operation != nil {
			operation := *p.Operation
			p.Operation = &operation
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
	// ProjectOf resolves current conversation placement for audience checks.
	ProjectOf func(Source) string
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
	if r.ProjectOf != nil {
		project := r.ProjectOf(m.Source)
		if project != m.Source.Project {
			m.Audience = slices.Clone(m.Audience)
			for i, a := range m.Audience {
				if a == ProjectAudience(m.Source.Project) {
					m.Audience[i] = ProjectAudience(project)
				}
				if a == OwnersAudience(m.Source.Project) {
					m.Audience[i] = OwnersAudience(project)
				}
			}
		}
	}
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
	Reminded    What = "reminded"
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

// Store holds every tenant's inbox in a state.Store, read and written per
// request.
type Store struct {
	st  state.Store
	dir *state.Tenants
	// watchers are this process's watchers, by tenant.
	mu       sync.Mutex
	watchers map[string]map[int]chan Notice
	nextW    int
}

// storedChange is a change as it is written, with what the wire forms of a
// change and a message leave out.
type storedChange struct {
	Change
	Raised   *storedMessage `json:"raised,omitempty"`
	Delivery *Delivery      `json:"delivery,omitempty"`
	// Cut is set on the one record an older release kept under cutKey,
	// which is no change: what retention kept of what it dropped.
	Cut *cut `json:"cut,omitempty"`
}

type storedMessage struct {
	Message
	Key       string   `json:"key"`
	Audience  []string `json:"audience,omitempty"`
	Excluded  []string `json:"excluded,omitempty"`
	Broadcast bool     `json:"broadcast,omitempty"`
	Once      *bool    `json:"once,omitempty"`
}

func storedOf(c Change) storedChange {
	out := storedChange{Change: c, Delivery: c.Delivery}
	if c.Raised != nil {
		r := c.Raised.clone()
		r.Unread, r.Hidden = false, false
		out.Raised = &storedMessage{Message: r, Key: r.Key, Audience: r.Audience, Excluded: r.Excluded, Broadcast: r.Broadcast, Once: &r.Once}
	}
	return out
}

func (sc storedChange) change(tenant string) Change {
	c := sc.Change
	c.Tenant, c.Delivery = tenant, sc.Delivery
	if sc.Raised != nil {
		m := sc.Raised.Message
		m.Key, m.Audience, m.Excluded, m.Broadcast = sc.Raised.Key, sc.Raised.Audience, sc.Raised.Excluded, sc.Raised.Broadcast
		// Older writers did not distinguish PostOnce from Post. Post used
		// announce: keys, and callers could name those too before PostOnce
		// refused them. Preserve old broadcast keys conservatively, so none
		// can be posted twice.
		m.Once = m.Broadcast && m.Key != ""
		if sc.Raised.Once != nil {
			m.Once = *sc.Raised.Once
		}
		c.Raised = &m
	}
	return c
}

// NewStore returns an empty inbox in this process's memory.
func NewStore() *Store {
	s, err := OpenStore(nil)
	if err != nil {
		panic(err)
	}
	return s
}

// OpenStore returns an inbox kept in st, which may be shared with other
// processes. A nil st keeps it in this process's memory.
//
// Changes kept by a release before this layout, one record each in the
// order they were made, are moved into it the first time any process opens
// the store; a process that opens it at the same time finds them moved.
func OpenStore(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	s := &Store{st: st, watchers: map[string]map[int]chan Notice{}}
	if err := s.convert(); err != nil {
		return nil, fmt.Errorf("inbox: convert: %w", err)
	}
	dir, err := state.OpenTenants(st, namespace)
	if err != nil {
		return nil, fmt.Errorf("inbox: tenants: %w", err)
	}
	s.dir = dir
	return s, nil
}

// change runs fn on a fresh read of a tenant and writes what it changed,
// again from a fresh read whenever another writer moved what it read, and
// tells this process's watchers once it is written.
func (s *Store) change(tenantName string, fn func(x *tx) (Message, bool, error)) (Message, bool, error) {
	var out Message
	var changed bool
	var notices []Notice
	err := state.Retry(func() error {
		x := newTx(s.st, tenantName)
		m, ch, err := fn(x)
		if err != nil {
			return err
		}
		if err := x.commit(); err != nil {
			return err
		}
		out, changed, notices = m, ch, x.notices
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalid) || errors.Is(err, ErrLimit) {
			return Message{}, false, err
		}
		return Message{}, false, fmt.Errorf("inbox: write: %w", err)
	}
	for _, n := range notices {
		s.tell(tenantName, n)
	}
	return out, changed, nil
}

func (s *Store) tell(tenantName string, n Notice) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.watchers[tenantName] {
		// A watcher that is not keeping up misses a notice, never blocks the
		// inbox. It refetches on the next one.
		select {
		case ch <- n:
		default:
		}
	}
}

// applyAs applies a change and returns its message as whoever made it sees
// it.
func applyAs(x *tx, c Change) (Message, bool, error) {
	if c.What != Raised {
		m, err := x.message(c.Message)
		if err != nil {
			return Message{}, false, err
		}
		if m == nil {
			return Message{}, false, fmt.Errorf("%w: message %q", ErrNotFound, c.Message)
		}
	}
	m, changed, err := x.apply(c)
	if err != nil || m == nil {
		return Message{}, false, err
	}
	k, err := x.markOf(m.ID, c.By)
	if err != nil {
		return Message{}, false, err
	}
	return view(m, k, time.Time{}), changed, nil
}

func (s *Store) do(c Change) (Message, bool, error) {
	return s.change(c.Tenant, func(x *tx) (Message, bool, error) { return applyAs(x, c) })
}

// byKey reads the latest message raised under a key, nil for none.
func (x *tx) byKey(key string) (*Message, error) {
	var ref keyRef
	if ok, err := x.get(keyKey(key), &ref); err != nil || !ok || ref.ID == "" {
		return nil, err
	}
	return x.message(ref.ID)
}

// Raise puts a message in the inbox. key names what it is about: while a
// message with that key is open, a second is not raised and the first is
// returned. The check and the raise are one write, so a raise that loses a
// race returns the message that won it, never an empty one.
func (s *Store) Raise(tenantName, key string, m Message, at time.Time) (Message, bool, error) {
	return s.raise(tenantName, key, m, at, false)
}

// RaiseOnce is Raise for a message about something that happens once, such
// as a question a run asked: while any message with that key is kept, open
// or not, it is returned and nothing is raised.
func (s *Store) RaiseOnce(tenantName, key string, m Message, at time.Time) (Message, bool, error) {
	return s.raise(tenantName, key, m, at, true)
}

func (s *Store) raise(tenantName, key string, m Message, at time.Time, once bool) (Message, bool, error) {
	if err := s.dir.Mark(tenantName); err != nil {
		return Message{}, false, fmt.Errorf("inbox: write: %w", err)
	}
	m.Key = key
	c := Change{Tenant: tenantName, What: Raised, At: at, By: m.From.Name, Raised: &m}
	return s.change(tenantName, func(x *tx) (Message, bool, error) {
		existing, err := x.byKey(key)
		if err != nil {
			return Message{}, false, err
		}
		if existing != nil && (once || !settled(existing)) {
			return existing.clone(), false, nil
		}
		return applyAs(x, c)
	})
}

// ByID returns one message as it is, for the platform's own use: who may
// read it is the caller's to decide. A message that cannot be read is not
// found.
func (s *Store) ByID(tenantName, id string) (Message, bool) {
	m, err := newTx(s.st, tenantName).message(id)
	if err != nil || m == nil {
		return Message{}, false
	}
	return m.clone(), true
}

// ByKey returns the latest message raised under a key. A message that
// cannot be read is not found.
func (s *Store) ByKey(tenantName, key string) (Message, bool) {
	m, err := newTx(s.st, tenantName).byKey(key)
	if err != nil || m == nil {
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
	x := newTx(s.st, tenantName)
	m, err := x.message(id)
	if err != nil {
		return Message{}, fmt.Errorf("inbox: read: %w", err)
	}
	if m == nil || !m.For(r) {
		return Message{}, fmt.Errorf("%w: message %q", ErrNotFound, id)
	}
	k, err := x.markOf(id, r.Subject)
	if err != nil {
		return Message{}, fmt.Errorf("inbox: read: %w", err)
	}
	return view(m, k, time.Time{}), nil
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
// Of two people answering one part at once, through one process or two,
// one decides it and the other is returned that decision.
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

func decodeChanges(tenantName string, recs []state.Record) ([]Change, error) {
	out := make([]Change, 0, len(recs))
	for _, r := range recs {
		var sc storedChange
		if err := json.Unmarshal(r.Value, &sc); err != nil {
			return nil, fmt.Errorf("inbox: %q: %w", r.Key, err)
		}
		out = append(out, sc.change(tenantName))
	}
	return out, nil
}

// History returns everything that happened to one message, oldest first:
// the record, every reader's own marks included, at most maxChanges. A
// record that cannot be read reads as none.
func (s *Store) History(tenantName, id string) []Change {
	if _, ok := num(id); !ok {
		return nil
	}
	recs, err := listAll(s.st, tenantName, changePrefix(id), maxChanges)
	if err != nil {
		return nil
	}
	out, err := decodeChanges(tenantName, recs)
	if err != nil || len(out) == 0 {
		return nil
	}
	return out
}

// MaxHistory is the most changes HistoryFor returns.
const MaxHistory = 100

// HistoryFor returns what happened to one message as one reader is shown it,
// oldest first: at most MaxHistory changes, the newest kept, found among the
// newest maxScan. A reader's own marks are theirs alone, so another
// reader's reads and unreads, and on a broadcast another reader's hiding or
// snoozing of their own copy, are left out; the record keeps them. A
// client's receipt (Delivered) is that client's own mark too: it is shown
// only where ownDelivery says the reader may see the client's receipts, and
// nil shows none. What changed the message for everyone is shown to
// everyone. A record that cannot be read reads as none.
func (s *Store) HistoryFor(tenantName, id, subject string, ownDelivery func(client string) bool) []Change {
	out := []Change{}
	m, err := newTx(s.st, tenantName).message(id)
	if err != nil || m == nil {
		return out
	}
	_, err = scan(s.st, tenantName, changePrefix(id), "", true, maxScan, func(r state.Record) (bool, error) {
		cs, err := decodeChanges(tenantName, []state.Record{r})
		if err != nil {
			return false, err
		}
		c := cs[0]
		if c.By != subject && ownMark(c.What, m.Broadcast) {
			return true, nil
		}
		if c.What == Delivered && (ownDelivery == nil || !ownDelivery(c.By)) {
			return true, nil
		}
		out = append(out, c)
		return len(out) < MaxHistory, nil
	})
	if err != nil {
		return []Change{}
	}
	slices.Reverse(out)
	return out
}

// scan calls fn for each record of a tenant under a prefix past after, in
// key order or with desc the reverse, until fn returns false or most were
// read. It reports whether it stopped at most.
func scan(st state.Store, tenantName, prefix, after string, desc bool, most int, fn func(state.Record) (bool, error)) (bool, error) {
	read := 0
	for read < most {
		c, cancel := ctx()
		page, next, err := st.List(c, namespace, tenantName, state.Query{Prefix: prefix, After: after, Limit: min(most-read, 200), Desc: desc})
		cancel()
		if err != nil {
			return false, err
		}
		for _, r := range page {
			read++
			more, err := fn(r)
			if err != nil || !more {
				return false, err
			}
		}
		if next == "" {
			return false, nil
		}
		after = next
	}
	return true, nil
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

// Changes returns a tenant's whole record, message by message in the order
// they were raised, each one's changes in the order they were made, at most
// maxChanges. A record that cannot be read reads as none.
func (s *Store) Changes(tenantName string) []Change {
	recs, err := listAll(s.st, tenantName, "c:", maxChanges)
	if err != nil {
		return nil
	}
	out, _ := decodeChanges(tenantName, recs)
	return out
}

// Rebuild returns the inbox a record describes, in this process's memory.
func Rebuild(changes []Change) *Store {
	s := NewStore()
	for _, c := range changes {
		_, _, _ = s.change(c.Tenant, func(x *tx) (Message, bool, error) {
			_, _, err := x.apply(c)
			return Message{}, false, err
		})
	}
	return s
}

// Snapshot returns every message in a tenant, in the order they were
// raised, with who has read each, at most maxMessages, for comparing two
// inboxes. A record that cannot be read reads as none.
func (s *Store) Snapshot(tenantName string) ([]Message, map[string][]string) {
	read := map[string][]string{}
	recs, err := listAll(s.st, tenantName, "a:", maxMessages)
	if err != nil {
		return nil, read
	}
	msgs := make([]Message, 0, len(recs))
	for _, r := range recs {
		var k keptMessage
		if err := json.Unmarshal(r.Value, &k); err != nil {
			return nil, read
		}
		msgs = append(msgs, k.message(tenantName).clone())
	}
	slices.SortFunc(msgs, func(a, b Message) int { return cmp.Compare(seqOf(a.ID), seqOf(b.ID)) })
	marks, err := listAll(s.st, tenantName, "u:", maxMessages*10)
	if err != nil {
		return msgs, read
	}
	for _, r := range marks {
		var k mark
		if json.Unmarshal(r.Value, &k) == nil && k.Read {
			id := "m-" + strings.TrimLeft(r.Key[len("u:"):strings.LastIndexByte(r.Key, ':')], "0")
			read[id] = append(read[id], k.Subject)
		}
	}
	for id := range read {
		slices.Sort(read[id])
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
	// Needs keeps only what may need the reader, what Counts.Open counts:
	// a broadcast, which counts only in Updates, is left out, and an
	// agent's draft, which waits on its owners, stays.
	Needs bool
	// News keeps only posted announcements, what Counts.Updates counts: an
	// agent's draft, which shares their kind, is left out.
	News bool
	// Visible reports whether the reader may see a kind. Nil means all.
	Visible func(Kind) bool
	// Sees reports whether the reader may read what a message is about:
	// the project and thread it lives in. Nil means all. It reads only the
	// message's kind, addressing and Source's run, project, thread and
	// agent: Count asks it once for all the open messages alike in those.
	ProjectOf func(Source) string
	Sees      func(Message) bool
	Now       time.Time
	Limit     int
	Before    string
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
	f := filter{Query: q, reader: Reader{Subject: q.Reader, Roles: q.Roles, Projects: q.ReaderProjects, Owns: q.ReaderOwns, ProjectOf: q.ProjectOf}}
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

// match reports whether a message is in a page, as one reader whose marks
// on it are k sees it.
func match(m *Message, k mark, q filter) bool {
	if !matchShared(m, q) {
		return false
	}
	want := q.State
	if want == "" {
		want = Open
	}
	if st, _, _ := stateFor(m, k, q.Now); want != All && st != want {
		return false
	}
	return !q.Unread || !k.Read
}

// matchShared is match for what does not depend on the reader's marks.
func matchShared(m *Message, q filter) bool {
	project := m.Source.Project
	if q.ProjectOf != nil {
		project = q.ProjectOf(m.Source)
	}
	if q.projects != nil && !q.projects[project] {
		return false
	}
	if !q.admits(m) {
		return false
	}
	if q.Kind != "" && m.Kind != q.Kind {
		return false
	}
	if q.Needs && m.Broadcast {
		return false
	}
	if q.News && !m.Broadcast {
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
	// Only a message open or snoozed for everyone can be open or snoozed
	// for a reader.
	switch q.State {
	case "", Open, Snoozed:
		return live(m)
	case Resolved, Dismissed:
		return m.State == q.State || (m.Broadcast && live(m) && q.State == Dismissed)
	}
	return true
}

func encodeCursor(m *Message) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(stamp(m.At), 10) + ":" + m.ID))
}

func decodeCursor(c string) (int64, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return 0, "", ErrCursor
	}
	at, id, ok := strings.Cut(string(raw), ":")
	if !ok {
		return 0, "", ErrCursor
	}
	n, err := strconv.ParseInt(at, 10, 64)
	if _, valid := num(id); err != nil || !valid || n < 0 {
		return 0, "", ErrCursor
	}
	return n, id, nil
}

// Page returns one page of an inbox, newest first, and the cursor for the page
// after it. The cursor is empty at the end. A message's place is when it was
// raised, which never changes, so paging neither repeats nor skips. A page
// reads at most maxScan messages: one that finds fewer than it holds in
// those returns them with a cursor where it stopped.
func (s *Store) Page(tenantName string, q Query) ([]Message, string, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultPage
	}
	limit = min(limit, MaxPage)
	prefix := "a:"
	switch q.State {
	case "", Open, Snoozed:
		prefix = "l:"
	}
	after := ""
	if q.Before != "" {
		at, id, err := decodeCursor(q.Before)
		if err != nil {
			return nil, "", err
		}
		after = prefix + state.Seq(at) + ":" + seqOf(id)
	}
	f := q.filter()
	x := newTx(s.st, tenantName)
	out := []Message{}
	var last *Message
	next := ""
	capped, err := scan(s.st, tenantName, prefix, after, true, maxScan, func(r state.Record) (bool, error) {
		var km keptMessage
		if err := json.Unmarshal(r.Value, &km); err != nil {
			return false, fmt.Errorf("inbox: %q: %w", r.Key, err)
		}
		m := km.message(tenantName)
		last = m
		if !matchShared(m, f) {
			return true, nil
		}
		k, err := x.markOf(m.ID, q.Reader)
		if err != nil {
			return false, err
		}
		if !match(m, k, f) {
			return true, nil
		}
		if len(out) == limit {
			next = encodeCursor(&Message{At: out[len(out)-1].At, ID: out[len(out)-1].ID})
			return false, nil
		}
		out = append(out, view(m, k, q.Now))
		return true, nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("inbox: read: %w", err)
	}
	if capped && next == "" && last != nil {
		next = encodeCursor(last)
	}
	return out, next, nil
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
// up from counters kept in the same write as each change, so it reads no
// message that is settled, and asks q.Sees once for each set of open
// messages, and of messages snoozed for everyone, alike in kind, addressing
// and what they are about.
func (s *Store) Count(tenantName string, q Query) (Counts, error) {
	q.State, q.Kind, q.Kinds, q.Projects, q.Shared, q.Alert, q.Agent, q.Unread, q.Needs, q.News = Open, "", nil, nil, false, "", "", false, false, false
	c, err := newTx(s.st, tenantName).count(q.filter())
	if err != nil {
		return Counts{}, fmt.Errorf("inbox: count: %w", err)
	}
	return c, nil
}

// CountProject counts one project's open inbox messages with the same reader
// counters and confinement as Count. It never includes descendant projects.
func (s *Store) CountProject(tenantName, project string, q Query) (Counts, error) {
	sees := q.Sees
	q.Sees = func(m Message) bool {
		current := m.Source.Project
		if q.ProjectOf != nil {
			current = q.ProjectOf(m.Source)
		}
		return current == project && (sees == nil || sees(m))
	}
	return s.Count(tenantName, q)
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
	m.Once = key != ""
	if err := s.dir.Mark(tenantName); err != nil {
		return Message{}, false, fmt.Errorf("inbox: write: %w", err)
	}
	return s.change(tenantName, func(x *tx) (Message, bool, error) {
		m := m.clone()
		if key != "" {
			var ref keyRef
			if ok, err := x.get(keyKey(key), &ref); err != nil {
				return Message{}, false, err
			} else if ok && ref.Gone {
				return Message{}, false, nil
			}
			prior, err := x.byKey(key)
			if err != nil {
				return Message{}, false, err
			}
			if prior != nil {
				return prior.clone(), false, nil
			}
		}
		var p postedRec
		if _, err := x.get(dayKey(dayOf(at)), &p); err != nil {
			return Message{}, false, err
		}
		switch {
		case p.By[project] >= caps.PerProject:
			return Message{}, false, fmt.Errorf("%w: a project posts at most %d announcements a day", ErrLimit, caps.PerProject)
		case p.Total >= caps.PerTenant:
			return Message{}, false, fmt.Errorf("%w: a workspace posts at most %d announcements a day", ErrLimit, caps.PerTenant)
		}
		var h head
		if _, err := x.get(headKey, &h); err != nil {
			return Message{}, false, err
		}
		m.Key = cmp.Or(key, "announce:"+project+":"+strconv.FormatInt(h.Next+1, 10))
		raised, _, err := x.apply(Change{Tenant: tenantName, What: Raised, At: at, By: m.From.Name, Raised: &m})
		if err != nil {
			return Message{}, false, err
		}
		if raised == nil {
			// Its key was raised by a post that read the same next id:
			// read again.
			return Message{}, false, state.ErrConflict
		}
		// The poster's read is written with the raise, so nobody, their own
		// console on the notice included, reads it unread for them first.
		if who := m.From.poster(); who != "" {
			if _, _, err := x.apply(Change{Tenant: tenantName, Message: raised.ID, What: MarkRead, By: who, At: at}); err != nil {
				return Message{}, false, err
			}
		}
		k, err := x.markOf(raised.ID, m.From.Name)
		if err != nil {
			return Message{}, false, err
		}
		return view(raised, k, time.Time{}), true, nil
	})
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

// Tenants returns every tenant the inbox has held anything for, from its
// directory of tenants.
func (s *Store) Tenants() ([]string, error) {
	var out []string
	if err := s.dir.Each(func(t string) error {
		out = append(out, t)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("inbox: tenants: %w", err)
	}
	return out, nil
}

// OpenOf returns the open and snoozed messages of a kind, for keeping the
// inbox in step with what the messages are about: the first maxOpen open
// messages, oldest first, are read. A record that cannot be read reads as
// none.
func (s *Store) OpenOf(tenantName string, kind Kind) []Message {
	var out []Message
	_, err := scan(s.st, tenantName, "l:", "", false, maxOpen, func(r state.Record) (bool, error) {
		var km keptMessage
		if err := json.Unmarshal(r.Value, &km); err != nil {
			return false, err
		}
		if km.Kind == kind && live(&km.Message) {
			out = append(out, km.message(tenantName).clone())
		}
		return true, nil
	})
	if err != nil {
		return nil
	}
	return out
}

// Raised returns the messages of a kind raised in [since, until), newest
// first, at most most, reading at most maxScan.
func (s *Store) Raised(tenantName string, kind Kind, since, until time.Time, most int) ([]Message, error) {
	var out []Message
	after := "a:" + state.Seq(stamp(until))
	floor := "a:" + state.Seq(stamp(since))
	_, err := scan(s.st, tenantName, "a:", after, true, maxScan, func(r state.Record) (bool, error) {
		if r.Key < floor {
			return false, nil
		}
		var km keptMessage
		if err := json.Unmarshal(r.Value, &km); err != nil {
			return false, err
		}
		if km.Kind == kind {
			out = append(out, km.message(tenantName).clone())
		}
		return len(out) < most, nil
	})
	if err != nil {
		return nil, fmt.Errorf("inbox: read: %w", err)
	}
	return out, nil
}

// Watch reports changes this process makes to a tenant's inbox until
// cancel is called.
func (s *Store) Watch(tenantName string) (<-chan Notice, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan Notice, 32)
	id := s.nextW
	s.nextW++
	if s.watchers[tenantName] == nil {
		s.watchers[tenantName] = map[int]chan Notice{}
	}
	s.watchers[tenantName][id] = ch
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.watchers[tenantName], id)
	}
}
