// Package threads keeps shared threads: the conversations people and agents
// have together in a project's channel.
//
// A thread is a humagen conversation with a record: the channel it is in, who
// takes part, its visibility and an optional default agent. Its turns are of
// two kinds. A message is a person's turn, kept here, and starts no run of
// its own. An agent's turn exists only as a run, kept in the run index and
// pointing at the message it answers. So no record here is ever an agent's
// words: a message's author is always a person's subject, and nothing in
// this package writes a turn for an agent. A thread a reply to an
// announcement started names it (Thread.Announcement) and starts with it,
// read from the inbox, from whoever posted it: it is no message here.
//
// The package also holds what the platform decides with, as pure functions:
// the tag grammar (Parse), what a message starts (Decide), the defaults set
// nearer a message than its project (Nearer) and the room's transcript
// (Ask).
//
// # Storage
//
// The Store reads and writes the database per request (pkg/state, per
// request): it holds no thread or message in memory, so any number of
// processes on one database agree, and it does not need pkg/state's
// single-writer lock. A conversation is its shard key, then time: its
// messages are appended under it, numbered from the head its record keeps,
// and never updated in place but to settle their tags.
//
// # Default deny
//
// A thread is shared, read by everyone who can see its project, or private,
// read by its participants alone: the platform's read rule confines every
// read path to them.
package threads

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/urmzd/humagen/fiber/pkg/attachment"

	"github.com/urmzd/humagen/pkg/projects"
)

// Errors a caller can match.
var (
	// ErrNotFound reports a thread or a message the tenant does not have.
	ErrNotFound = errors.New("threads: not found")
	// ErrExists reports a thread id already taken.
	ErrExists = errors.New("threads: already exists")
	// ErrInvalid reports a record the store refuses.
	ErrInvalid = errors.New("threads: invalid")
	// ErrVersion reports a write that names a version that is not the
	// current one.
	ErrVersion = errors.New("threads: changed since it was read")
	// ErrFull reports a thread that holds as many messages as it may.
	ErrFull = errors.New("threads: this thread is full; start a new thread")
	// ErrPrivate reported a private thread while reads were not confined.
	// Nothing raises it any more.
	//
	// Deprecated: private threads are accepted.
	// path is confined.
	ErrPrivate = errors.New("threads: private threads need read confinement, not built")
	// ErrCursor reports a paging cursor this store did not issue.
	ErrCursor = errors.New("threads: invalid cursor")
	// ErrLimit reports a workspace, or a project, that holds as many
	// threads as it may.
	ErrLimit = errors.New("threads: limit reached")
)

// Bounds.
const (
	// MaxThreads is how many threads a workspace holds records of, and
	// MaxProjectThreads how many one project does, its root included. A
	// conversation from before threads counts once its record is written.
	MaxThreads        = 10_000
	MaxProjectThreads = 2_000
	// MaxMessages is how many messages one thread holds.
	MaxMessages = 5000
	// MaxParticipants is how many people and agents one writer adds a
	// thread up to, counting the ones they are shown.
	MaxParticipants = 50
	// MaxHeld is the most participants a thread's record holds: with the
	// ones a writer was not shown and so did not count (Participants), and
	// the people who post and the agents who answer, who join up to it.
	MaxHeld = 2 * MaxParticipants
	// MaxText bounds a message, in characters.
	MaxText = 8000
	// MaxMentions is how many tags one message holds that each take a
	// lookup of their own: people, experts, roles and skills. Agents are
	// read in one roster read (Roster) and are not counted; their bound is
	// MaxText.
	MaxMentions = 10
	// MaxSkills is how many skills one message names. They are tags, so
	// they count toward MaxMentions too.
	MaxSkills = 4
	// MaxTitle bounds a thread's title, in characters.
	MaxTitle = 200
	// MaxName bounds a person's display name and a participant's name.
	MaxName = 200
	// MaxSurfaceID bounds a surface's id for a message, such as a Slack ts.
	MaxSurfaceID = 64
	// MaxPage bounds one page of messages or threads.
	MaxPage = 200
	// DefaultPage is a page of threads when none is asked for.
	DefaultPage = 50
	// MaxRouted is how many agents a thread selects to answer its untagged
	// messages.
	MaxRouted = 5
)

// How a thread's selected agents answer an untagged message.
const (
	// ModeJoin: each of them decides for itself whether to take the
	// message (pkg/join), and each that does answers.
	ModeJoin = "join"
	// modeBestFit is what ModeJoin was written as when one agent was picked.
	modeBestFit = "best_fit"
	// ModeAll: each of them, side by side, at most MaxAll.
	ModeAll = "all"
)

// Routing is a thread's selected agents and how they answer, normalized:
// each agent once, in the order given, and a mode only with two or more,
// ModeJoin when none is named. It refuses more than MaxRouted agents, a
// mode that is neither, ModeAll over more than MaxAll agents, and a name no
// agent can have (ErrInvalid).
func Routing(agents []string, mode string) ([]string, string, error) {
	var out []string
	for _, a := range agents {
		if !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	if mode == modeBestFit {
		mode = ModeJoin
	}
	switch {
	case len(out) > MaxRouted:
		return nil, "", fmt.Errorf("%w: a thread selects at most %d agents", ErrInvalid, MaxRouted)
	case mode != "" && mode != ModeJoin && mode != ModeAll:
		return nil, "", fmt.Errorf("%w: a mode is join or all", ErrInvalid)
	}
	for _, a := range out {
		if a == OpenName || a == AutoName || !projects.ValidDefault(a) {
			return nil, "", fmt.Errorf("%w: no agent can be called %q", ErrInvalid, a)
		}
	}
	if len(out) < 2 {
		return out, "", nil
	}
	if mode == "" {
		mode = ModeJoin
	}
	if mode == ModeAll && len(out) > MaxAll {
		return nil, "", fmt.Errorf("%w: at most %d agents answer one message side by side; select fewer, or join", ErrInvalid, MaxAll)
	}
	return out, mode, nil
}

// Visibility says who reads a thread.
type Visibility string

const (
	// Shared is read by everyone who can see the thread's project.
	Shared Visibility = "shared"
	// Private is read by its participants only.
	Private Visibility = "private"
)

// Kinds of participant and of mention.
const (
	Person = "person"
	Agent  = "agent"
	// SME is a tag of a project's experts, for a topic or for any.
	SME = "sme"
	// Skill is a skill a message names, $<name> or @skill:<name>: the runs
	// it starts are told to use it. It notifies nobody.
	Skill = "skill"
	// Role is a tag of the people holding a policy role, through a mention
	// kind that resolves roles, such as @oncall:sre.
	Role = "role"
)

// ViaConsole is a message posted in the console.
const ViaConsole = "console"

// Participant is a person or an agent taking part in a thread.
type Participant struct {
	// Kind is person or agent.
	Kind string `json:"kind"`
	// Name is a person's subject or an agent's name.
	Name string `json:"name"`
	// Owner is the person who started the thread.
	Owner   bool      `json:"owner,omitempty"`
	AddedBy string    `json:"added_by,omitempty"`
	At      time.Time `json:"at"`
}

// Thread is one shared thread's record.
type Thread struct {
	// ID is the conversation's id: its first run's, for a conversation from
	// before threads, or "th-" and 16 hex digits.
	ID string `json:"id"`
	// Project is the project it is filed in. "" is the root.
	Project string `json:"project"`
	// Channel is the project's channel it is in.
	Channel    string     `json:"channel"`
	Title      string     `json:"title,omitempty"`
	Visibility Visibility `json:"visibility"`
	// Participants are who takes part, in the order they joined.
	Participants []Participant `json:"participants"`
	// Default answers an untagged message here: an agent, or OpenName. ""
	// inherits from the channel.
	Default string `json:"default,omitempty"`
	// Agents are the agents selected to answer the thread's untagged
	// messages, at most MaxRouted, and Mode how: ModeJoin, the one that
	// fits each message best, or ModeAll, each of them side by side. A
	// message's tags win over them. Mode is "" with fewer than two.
	Agents []string `json:"agents,omitempty"`
	Mode   string   `json:"mode,omitempty"`
	// Surface is the one thread elsewhere this thread is, such as a Slack
	// thread: a client, its channel there and the thread's own id. It is
	// fixed when the thread is started, and one surface is one thread.
	Surface *Surface `json:"surface,omitempty"`
	// Announcement is the inbox id of the announcement the thread was
	// started from: its surface is the announcement's post there, and a
	// reply to it started the thread. It is fixed when the thread is
	// started, only a thread with a surface has one, and the thread's
	// turns start with it.
	Announcement string `json:"announcement,omitempty"`
	ReadOnly     bool   `json:"read_only,omitempty"`
	// Fork is immutable passive history from another conversation.
	Fork *Fork `json:"fork,omitempty"`
	// Archive is set while the conversation is archived, and stays as a
	// tombstone once an admin approves its deletion (SetArchive, Purge).
	Archive *Archive `json:"archive,omitempty"`
	// Version counts the edits made to the record: its title, visibility,
	// default and participants. Posting, and joining by posting, leave it.
	Version int `json:"version"`
	// Moves is a neutral indicator: earlier audience identities are never public.
	Moves   int `json:"moves,omitempty"`
	movedBy string
	// CreatedBy is the person who started the thread, its owner. A record
	// of a conversation from before threads that no person asked in, which
	// a change by someone who may not own it wrote, has none until a member
	// changes it (Store.Claim).
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is when anything last happened in the thread: an edit or a
	// message.
	UpdatedAt time.Time `json:"updated_at"`
}

// Surface is a thread on a client's surface: for Slack, the client's id, the
// Slack channel and the ts of the thread's first message.
type Surface struct {
	Client  string `json:"client"`
	Channel string `json:"channel"`
	TS      string `json:"ts"`
}

// key is where a surface is found.
func (s Surface) key() string { return s.Client + "\x00" + s.Channel + "\x00" + s.TS }

// validSurface is a surface's channel or thread id: one token of what Slack
// and GitHub write, at most 100 characters.
var validSurface = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/#-]{0,99}$`)

// Validate checks a surface's ids.
func (s Surface) Validate() error { return s.validate() }

func (s Surface) validate() error {
	if !validVia.MatchString(s.Client) || !validSurface.MatchString(s.Channel) || !validSurface.MatchString(s.TS) {
		return fmt.Errorf("%w: a surface is a client's id, a channel and a thread's id there", ErrInvalid)
	}
	return nil
}

// AnswerRef names the question a message answers.
type AnswerRef struct {
	Run      string `json:"run"`
	Question string `json:"question"`
}

// Mention is one tag in a message.
type Mention struct {
	// Kind is agent, person, sme, skill or role.
	Kind string `json:"kind"`
	// Name is the agent ("*" for every agent asked), the person's subject, the topic
	// ("" for every expert), the skill or the role. A person nobody goes by
	// keeps the handle as written.
	Name string `json:"name"`
	// Run is the run an agent's tag started.
	Run string `json:"run,omitempty"`
	// Refused says why a tag started or notified nothing.
	Refused string `json:"refused,omitempty"`
}

// Message is a person's turn in a thread. It starts no run of its own.
type Message struct {
	Attachments []attachment.Attachment `json:"attachments,omitempty"`
	// ID is "m-" and the message's place in the thread, from 1.
	ID     string `json:"id"`
	Thread string `json:"thread"`
	// Prev is the turn it follows: a message of the thread, or the run a
	// conversation from before threads ended on. "" starts the thread.
	Prev string `json:"prev,omitempty"`
	// By is the subject of the person who posted it. Never an agent.
	By string `json:"by"`
	// Name is what they were called when they posted.
	Name     string    `json:"name"`
	Text     string    `json:"text"`
	Mentions []Mention `json:"mentions"`
	// Answers is set when the message is recorded as a question's answer.
	Answers *AnswerRef `json:"answers,omitempty"`
	// Via is where it was posted: console, or a client's id.
	Via string `json:"via"`
	// SurfaceID is the surface's own id for it, such as a Slack ts. A second
	// post with the same one is the first.
	SurfaceID string    `json:"surface_id,omitempty"`
	At        time.Time `json:"at"`
}

func (m Message) clone() Message {
	m.Attachments = slices.Clone(m.Attachments)
	m.Mentions = cloneMentions(m.Mentions)
	if m.Answers != nil {
		a := *m.Answers
		m.Answers = &a
	}
	return m
}

func cloneMentions(ms []Mention) []Mention {
	out := make([]Mention, len(ms))
	copy(out, ms)
	return out
}

func (t Thread) clone() Thread {
	ps := make([]Participant, len(t.Participants))
	copy(ps, t.Participants)
	t.Participants = ps
	t.Agents = slices.Clone(t.Agents)
	t.Fork = t.Fork.clone()
	t.Archive = t.Archive.clone()
	if t.Surface != nil {
		sf := *t.Surface
		t.Surface = &sf
	}
	return t
}

// has reports whether someone takes part already.
func (t Thread) has(kind, name string) bool {
	for _, p := range t.Participants {
		if p.Kind == kind && p.Name == name {
			return true
		}
	}
	return false
}

var (
	validID = regexp.MustCompile(`^(th-[0-9a-f]{16}|r[0-9a-f]{32})$`)
	// validRun is a run's id, as a message's Prev or an answer names it.
	validRun = regexp.MustCompile(`^r[0-9a-f]{32}$`)
	// validVia is a client's id.
	validVia = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	// validInboxID is an inbox message's id, as a thread names the
	// announcement it was started from.
	validInboxID = regexp.MustCompile(`^m-[0-9]{1,18}$`)
)

// ValidID reports whether an id can name a thread.
func ValidID(id string) bool { return validID.MatchString(id) }

// MessageID is the id of a thread's nth message.
func MessageID(n int) string { return fmt.Sprintf("m-%d", n) }

func oneLine(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl)
}

// plain is text of several lines with no other control characters.
func plain(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' })
}

func validRouting(agents []string, mode string) bool {
	got, m, err := Routing(agents, mode)
	return err == nil && len(got) == len(agents) && m == mode
}

func validDefault(agent string) bool { return agent == "" || projects.ValidDefault(agent) }

func (p Participant) validate() error {
	switch {
	case p.Kind != Person && p.Kind != Agent:
		return fmt.Errorf("%w: a participant is a person or an agent", ErrInvalid)
	case strings.TrimSpace(p.Name) == "" || utf8.RuneCountInString(p.Name) > MaxName || !oneLine(p.Name):
		return fmt.Errorf("%w: a participant is named on one line of at most %d characters", ErrInvalid, MaxName)
	case p.Kind == Agent && (p.Name == OpenName || !projects.ValidDefault(p.Name)):
		return fmt.Errorf("%w: no agent can be called %q", ErrInvalid, p.Name)
	case p.Kind == Agent && p.Owner:
		return fmt.Errorf("%w: a thread is started by a person", ErrInvalid)
	}
	return nil
}

// validate checks a thread's record on its own. A private thread is a valid
// record, so one written later still loads; writing one is refused.
func (t Thread) validate() error {
	if t.ReadOnly && (t.Fork == nil || t.Surface != nil || len(t.Agents) > 0 || t.Default != "") {
		return fmt.Errorf("%w: an archive holds passive fork context only", ErrInvalid)
	}
	switch {
	case !ValidID(t.ID):
		return fmt.Errorf("%w: a thread's id is th- and 16 hex digits, or a run's id", ErrInvalid)
	case t.Project != projects.Root && !projects.ValidName(t.Project):
		return fmt.Errorf("%w: no project can be called %q", ErrInvalid, t.Project)
	case !projects.ValidChannel(t.Channel):
		return fmt.Errorf("%w: no channel can be called %q", ErrInvalid, t.Channel)
	case t.Visibility != Shared && t.Visibility != Private:
		return fmt.Errorf("%w: a thread is shared or private", ErrInvalid)
	case utf8.RuneCountInString(t.Title) > MaxTitle || !oneLine(t.Title):
		return fmt.Errorf("%w: a thread's title is one line of at most %d characters", ErrInvalid, MaxTitle)
	case !validDefault(t.Default):
		return fmt.Errorf("%w: a default is an agent's name or auto", ErrInvalid)
	case !validRouting(t.Agents, t.Mode):
		return fmt.Errorf("%w: a thread selects at most %d agents, join or all", ErrInvalid, MaxRouted)
	case len(t.Participants) > MaxHeld:
		return fmt.Errorf("%w: a thread holds at most %d participants", ErrInvalid, MaxHeld)
	case t.CreatedBy != "" && strings.TrimSpace(t.CreatedBy) == "":
		return fmt.Errorf("%w: a thread is started by someone", ErrInvalid)
	}
	if err := t.Fork.validate(t.ID); err != nil {
		return err
	}
	if t.Surface != nil {
		if err := t.Surface.validate(); err != nil {
			return err
		}
	}
	if t.Announcement != "" && (t.Surface == nil || !validInboxID.MatchString(t.Announcement)) {
		return fmt.Errorf("%w: a thread started from an announcement is a surface's, and names it by its id", ErrInvalid)
	}
	seen := map[[2]string]bool{}
	for _, p := range t.Participants {
		if err := p.validate(); err != nil {
			return err
		}
		if seen[[2]string{p.Kind, p.Name}] {
			return fmt.Errorf("%w: %s takes part twice", ErrInvalid, p.Name)
		}
		seen[[2]string{p.Kind, p.Name}] = true
	}
	return nil
}

func (m Mention) validate() error {
	switch {
	case m.Kind != Agent && m.Kind != Person && m.Kind != SME && m.Kind != Skill && m.Kind != Role:
		return fmt.Errorf("%w: a mention is of an agent, a person, experts, a skill or a role", ErrInvalid)
	case m.Kind != SME && strings.TrimSpace(m.Name) == "":
		return fmt.Errorf("%w: a mention names someone", ErrInvalid)
	case utf8.RuneCountInString(m.Name) > MaxName || !oneLine(m.Name) || !oneLine(m.Refused) || len(m.Refused) > 500:
		return fmt.Errorf("%w: a mention is one line", ErrInvalid)
	case m.Run != "" && (m.Kind != Agent || !validRun.MatchString(m.Run)):
		return fmt.Errorf("%w: only an agent's tag starts a run", ErrInvalid)
	}
	return nil
}

// CheckText refuses a message's text that cannot be posted: empty, longer
// than MaxText, or not plain text. A post checks it before anything reads the
// text, so nothing is looked up for a message that is refused.
func CheckText(text string) error {
	switch {
	case strings.TrimSpace(text) == "":
		return fmt.Errorf("%w: a message says something", ErrInvalid)
	case len(text) > 4*MaxText || utf8.RuneCountInString(text) > MaxText || !plain(text):
		return fmt.Errorf("%w: a message is at most %d characters", ErrInvalid, MaxText)
	}
	return nil
}

// validate checks a message on its own, as it is posted: not its place in
// the thread, which the store fills in.
func (m Message) validate() error {
	chars := utf8.RuneCountInString
	switch {
	case strings.TrimSpace(m.By) == "" || chars(m.By) > MaxName || !oneLine(m.By):
		return fmt.Errorf("%w: a message is posted by a person", ErrInvalid)
	case chars(m.Name) > MaxName || !oneLine(m.Name):
		return fmt.Errorf("%w: a name is one line of at most %d characters", ErrInvalid, MaxName)
	case CheckText(m.Text) != nil:
		return CheckText(m.Text)
	case PerNameTags(m.Mentions) > MaxMentions:
		return fmt.Errorf("%w: a message tags at most %d people, experts, roles and skills", ErrInvalid, MaxMentions)
	case len(m.Mentions) > MaxText/2:
		// Each tag is written in the text, at least two characters apart.
		return fmt.Errorf("%w: a message holds more tags than its text can", ErrInvalid)
	case m.Via != ViaConsole && !validVia.MatchString(m.Via):
		return fmt.Errorf("%w: a message is posted in the console or through a client", ErrInvalid)
	case chars(m.SurfaceID) > MaxSurfaceID || !oneLine(m.SurfaceID):
		return fmt.Errorf("%w: a surface id is one line of at most %d characters", ErrInvalid, MaxSurfaceID)
	case m.Answers != nil && (!validRun.MatchString(m.Answers.Run) || strings.TrimSpace(m.Answers.Question) == "" || !oneLine(m.Answers.Question) || len(m.Answers.Question) > MaxName):
		return fmt.Errorf("%w: an answer names a run and its question", ErrInvalid)
	case m.Prev != "" && !validRun.MatchString(m.Prev) && !strings.HasPrefix(m.Prev, "m-"):
		return fmt.Errorf("%w: a message follows a message of its thread or a run", ErrInvalid)
	}
	for _, mention := range m.Mentions {
		if err := mention.validate(); err != nil {
			return err
		}
	}
	return nil
}
