// Package runs is the platform's index of runs and the tree they form.
//
// fiber's ledger knows whether a run survived. This index knows how runs
// relate: which run called which, how many calls each has made, and how much
// budget each has handed out. It is a projection of two inputs, the tag a run
// was accepted with and the lifecycle events that run later publishes.
//
// It reads and writes the database per request (pkg/state, per request): it
// holds no runs in memory, so any number of processes on one database agree,
// a run stopped through one is stopping in every other, and it does not need
// pkg/state's single-writer lock. Every change is written before it is
// reported, at the version it read, and read again when another process
// wrote first. An event two processes both fold changes a run once: a
// lifecycle event by its sequence, a usage report by its accounting id. See
// namespace for the layout.
package runs

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/runtime"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/usage"
)

// ErrNotFound reports a run that does not exist in the caller's tenant. A run
// in another tenant is reported the same way, so the answer never confirms that
// it exists.
var ErrNotFound = errors.New("runs: not found")

// OriginManual marks a schedule fired by a person rather than by the clock.
const OriginManual = "manual"

// OriginAssignment is retained source-change work raised by the platform.
const OriginAssignment = "assignment"

// OriginSystem marks a run the platform raised for its own housekeeping, such
// as naming a conversation. Nothing outside the platform can raise one.
const OriginSystem = "system"

// OriginAPI marks a run a caller asked for through the platform's API. It is
// the only origin whose conversation keys are believed.
const OriginAPI = "api"

// OriginEval marks a run raised by an eval, so it can be told apart from work
// a person or a schedule asked for. Nothing outside the platform can raise
// one.
const OriginEval = "eval"

// Keys the platform's API writes into a tag's Meta to place a run in a
// conversation. A tag from any other origin carrying them is ignored.
const (
	MetaThread  = "warp.thread"
	MetaPrev    = "warp.prev"
	MetaAsk     = "warp.ask"
	MetaPicked  = "warp.picked"
	MetaProject = "warp.project"
	// MetaRouted is how a thread's selected agents started the run, when
	// they did: "join" (or "best_fit", written before) or "all". It is private
	// (wire.PrivateMetaPrefix): the run's record says it, and fiber
	// keeps it out of the prompt and the journal.
	MetaRouted = wire.PrivateMetaPrefix + "warp.routed"
)

// Keys the platform's API writes into a tag's Meta to place a run in a
// shared thread: the project's channel the conversation is in, and the
// person's message the run answers. Like the conversation keys, they are
// believed only from the API.
const (
	MetaChannel = "warp.channel"
	MetaMessage = "warp.message"
)

// DefaultChannel is the channel a conversation is in when nothing filed it in
// another: its project's general.
const DefaultChannel = "general"

// MetaWithin is the key the platform writes into a tag's Meta at admission:
// the ancestors of the run's project, nearest first and comma separated (a
// project's name holds no comma). The platform overwrites whatever a caller
// put there, so it is believed from every origin.
const MetaWithin = "warp.within"

// WithinOf returns the ancestors a tag's run was admitted within.
func WithinOf(tag event.Envelope) []string {
	v := wire.Meta(tag)[MetaWithin]
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

// MetaSources is the key the platform writes into a tag's Meta at admission:
// the knowledge sources the run's project chain gives it, nearest first and
// comma separated (a source's name holds no comma). Like MetaWithin it is
// overwritten whatever a caller put there, so it is believed from every
// origin, and every attempt of the run is grounded in the same sources
// whatever the projects name later. It is a private key (it starts with
// wire.PrivateMetaPrefix), so fiber keeps it out of everything a model is
// sent: the model is grounded in the sources without being told their names.
const MetaSources = wire.PrivateMetaPrefix + "warp.sources"

// MetaSourcesV0 is the key a run accepted before MetaSources was private was
// pinned with. It is still read, so such a run keeps its sources, and the
// platform takes it out of what fiber renders.
const MetaSourcesV0 = "warp.sources"

// SourcesOf returns the knowledge sources a run's tag was pinned with, from
// its Meta: MetaSources, or MetaSourcesV0 for a run pinned before it.
func SourcesOf(meta map[string]string) []string {
	v, ok := meta[MetaSources]
	if !ok {
		v = meta[MetaSourcesV0]
	}
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

// MetaSkills is the key the platform writes into a tag's Meta when a
// person's message names skills: the skills the run is told to use, with
// their instructions, as the platform's own JSON, pinned when the message is
// accepted so every attempt of the run is told the same whatever the projects
// declare later. Admission drops it from every tag but a root run started
// through the platform's API (OriginAPI), which only the platform raises, so
// no connector, schedule or caller can put instructions in a run. It is
// private (wire.PrivateMetaPrefix), so fiber keeps it out of the prompt
// and out of what it journals, a run stopped before its attempt starts
// included.
const MetaSkills = wire.PrivateMetaPrefix + "warp.skills"

// MetaClient is the key the platform's ingress writes into a tag's Meta to
// say which external client raised it. Ingress overwrites whatever a
// connector put there, and only a tag whose origin is a client surface is
// believed.
const MetaClient = "warp.client"

// AskedBy is the person who asked a run, as its conversation lists its
// people: the person it acts for (Actor.User), and nobody for a bot,
// whatever it names. A run recorded before runs kept whom they act for,
// asked through the API by a person, names them by its actor's id, as every
// such run did then. Any other run that acts for nobody, a connector's for a
// user no client maps or a retry asked for nobody, names nobody: "". A
// connector's actor id is the connector's own user (a Slack user id), never
// a person here.
func AskedBy(r Run) string {
	switch {
	case r.Actor.Bot:
		return ""
	case r.Actor.User != "":
		return r.Actor.User
	case r.Origin == OriginAPI && !strings.HasPrefix(r.TagID, "api-retry-"):
		return r.Actor.ID
	}
	return ""
}

// ClientOf returns the external client that raised a tag, or "".
func ClientOf(tag event.Envelope) string {
	if wire.Origin(tag) == "slack" || wire.Origin(tag) == "github" {
		return wire.Meta(tag)[MetaClient]
	}
	return ""
}

// ProjectOf returns the project a tag files its run under, or "". Only origins
// whose project the server itself wrote are believed: the API, the cron trigger
// (a schedule's project is set when the schedule is created), a person's
// run-now, an eval (filed where the agent it tries lives), and Slack and
// GitHub, whose ingress writes it from the client's binding over whatever a
// connector said.
func ProjectOf(tag event.Envelope) string {
	switch wire.Origin(tag) {
	case OriginAPI, "cron", OriginManual, OriginEval, OriginSlack, OriginGitHub:
		return wire.Meta(tag)[MetaProject]
	}
	return ""
}

// The origins of runs a client's in-process connector raised.
const (
	OriginSlack  = "slack"
	OriginGitHub = "github"
)

// Status is where a run is in its life.
type Status string

const (
	Accepted  Status = "accepted"
	Running   Status = "running"
	Parked    Status = "parked"
	Completed Status = "completed"
	Failed    Status = "failed"
	// Stopped is a run that was ended from outside: by a person, or because
	// the run it was attached to ended. It did not fail. Its cause says which,
	// and its answer is what it had said when it stopped, which may be nothing.
	Stopped Status = "stopped"
)

// Terminal reports whether the run has ended.
func (s Status) Terminal() bool { return s == Completed || s == Failed || s == Stopped }

// Lifecycle says whose lifetime a called run lives in.
type Lifecycle string

const (
	// Attached runs live inside their caller's lifetime. The caller is not
	// done until they are, and if the caller fails or is stopped, so are they.
	Attached Lifecycle = "attached"
	// Delegated runs have a lifetime of their own. They carry on whatever
	// becomes of the run that started them.
	Delegated Lifecycle = "delegated"
)

// lifecycleOf maps how a run was called to how long it lives. A subagent is
// attached: its caller waits on it. A delegate is independent by definition,
// and so is the receiver of a handoff, whose caller ends by handing over.
func lifecycleOf(parent string, mode call.Mode) Lifecycle {
	switch {
	case parent == "":
		return ""
	case mode == call.Subagent:
		return Attached
	}
	return Delegated
}

// Run is one run and its place in the tree.
type Run struct {
	Citations []Citation `json:"citations"`
	ID        string     `json:"id"`
	// TagID is the id of the tag the run was accepted with: the trigger's
	// event id, or the one the API made from its caller and idempotency key.
	// The run's id is derived from it and the tenant, and cannot be turned
	// back into it, so a caller that named its own run reads it here.
	TagID  string `json:"tag_id"`
	Tenant string `json:"tenant"`
	Agent  string `json:"agent"`
	Rev    int    `json:"rev"`
	Origin string `json:"origin"`
	Text   string `json:"text"`
	// Input keeps the exact helper messages and original references.
	Input *call.Prepared `json:"input,omitempty"`
	Actor event.Actor    `json:"actor,omitzero"`

	// Root, Parent, Depth and Mode are the edge the run was called with.
	Root   string    `json:"root"`
	Parent string    `json:"parent,omitempty"`
	Depth  int       `json:"depth"`
	Mode   call.Mode `json:"mode,omitempty"`
	// Lifecycle is set on a run another run called.
	Lifecycle Lifecycle `json:"lifecycle,omitempty"`
	// Schedule names the schedule that raised the run, if one did.
	Schedule string `json:"schedule,omitempty"`

	// Thread is the conversation the run belongs to. A run that starts a
	// conversation is its own thread.
	Thread string `json:"thread"`
	// Prev is the turn this one follows. Two runs with the same Prev are
	// branches: different answers to the same point in the conversation.
	Prev string `json:"prev,omitempty"`
	// Picked says why the agent was chosen, when the caller left it to the
	// platform.
	Picked string `json:"picked,omitempty"`
	// Routed says the run was started by its thread's selected agents, and
	// how: "join" (or "best_fit", written before) or "all". A run a tag or a default started has none.
	Routed string `json:"routed,omitempty"`
	// Project is the project the conversation is filed under, if any.
	Project string `json:"project,omitempty"`
	// Within is the project's ancestors, nearest first and the root left
	// out, pinned when the run was admitted: what it counts toward besides
	// its project. A run called by an agent has its root's.
	Within []string `json:"within,omitempty"`
	// Client is the external client (a Slack or GitHub installation) that
	// raised the run. A run called by an agent has its root's.
	Client string `json:"client,omitempty"`
	// Channel is the project's channel the conversation is in. "" is
	// DefaultChannel. A run called by an agent has its root's.
	Channel string `json:"channel,omitempty"`
	// Message is the person's message in a shared thread that the run
	// answers. Runs answering one message are side by side, not branches.
	Message string `json:"message,omitempty"`

	// Lease is the run's budget in micro-USD. Zero means unmetered.
	Lease int64 `json:"lease"`
	// Granted is the budget the run has handed to calls that have not
	// settled yet, in micro-USD. A call settles when it ends: what it
	// leased is returned and what it spent is added to Spent.
	Granted int64 `json:"granted"`
	// Spent is what the run has spent, in micro-USD priced in US dollars:
	// its own model calls as they are reported, and the whole spend of each
	// call it made once that call has ended. It is what the lease is held
	// against. A call with no price, or priced in another currency, adds
	// nothing here; Usage says how many there were.
	Spent int64 `json:"spent"`
	// Usage is what the run used, as the runtime counts it: its own model
	// calls, the attached runs it rolled up, and how full its context was at
	// its latest call. While the run works it is the calls reported so far.
	// Once it has ended it is the runtime's record. Absent until the first
	// model call.
	Usage *event.RunUsage `json:"usage,omitempty"`
	// Calls is how many calls the run has made.
	Calls int `json:"calls"`

	Status Status `json:"status"`
	// Assistance is needed once a human question was observed, including in
	// called work. none is recorded only after a completely observed run ends.
	// Older or recovered incomplete histories remain absent, never guessed.
	Assistance string `json:"assistance,omitempty"`
	// Answer is the final text of a completed run. On a stopped run it is the
	// partial answer: what the agent had said when it stopped, which people
	// may already have read. It is never the whole answer, and Status says so.
	Answer  string   `json:"answer,omitempty"`
	Outcome *Outcome `json:"outcome,omitempty"`
	Error   string   `json:"error,omitempty"`
	// Reason says what a parked run is waiting for, or that a run which has
	// answered is waiting for the agents attached to it.
	Reason string `json:"reason,omitempty"`
	// Question is what a parked run is waiting on a person for. It is gone
	// once the run resumes or ends.
	Question *event.Question `json:"question,omitempty"`
	// Cause says why a stopped run was stopped.
	Cause string `json:"cause,omitempty"`
	// Stopping is true from the moment someone asks a run to stop until the
	// runtime says how it ended. A run that is stopping can start no further
	// calls. It usually ends stopped, and ends completed or failed when its
	// turn finished first: the request lost the race, and the run says so.
	Stopping bool `json:"stopping,omitempty"`
	// StopRequestedAt is when the run was first asked to stop.
	StopRequestedAt time.Time `json:"stop_requested_at,omitzero"`
	// Finishing is true for a run that has its answer and is held open by
	// attached runs that are still working.
	Finishing bool `json:"finishing,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// tag is what the run was accepted with. A call from this run is built
	// from it.
	tag event.Envelope
	// requesters are every name the person who asked for the run went by
	// when it was accepted, pinned so a mapping removed later still counts.
	requesters []string
	// tally sums the usage reported while the run works. final is set once
	// the runtime's own figure has replaced it.
	tally *event.Tally
	final bool
	// closed is set once the run's own spending is settled, after which
	// Spend changes nothing. settledUp is set once its spend has been
	// settled into its caller, so a call that ends later climbs past it.
	closed, settledUp bool
	privateContext    bool
	// seq is the run's number in its tenant. observedBy, lastSeq and
	// openAttached are storedRun's.
	seq          int64
	observedBy   string
	lastSeq      uint64
	openAttached int
}

// Requesters returns the names pinned for the person who asked for the run.
func (r Run) Requesters() []string { return slices.Clone(r.requesters) }

// PinRequesters records every name the person who asked for a run goes by,
// once, when it is accepted.
func (x *Index) PinRequesters(tenant, runID string, names []string) {
	x.update("requesters", tenant, runID, func(t *txn, r *Run) (Run, bool) {
		if r.requesters != nil {
			return Run{}, false
		}
		r.requesters = slices.Clone(names)
		t.touch(r)
		return Run{}, true
	})
}

// Tag returns the tag the run was accepted with.
func (r Run) Tag() event.Envelope { return r.tag }

// State is what the run has used so far, for call.New: what it spent and what
// it has leased to calls still working.
func (r Run) State() call.State { return call.State{Calls: r.Calls, Spent: r.Spent + r.Granted} }

// UsageRecorded reports whether Usage is the runtime's own record of the run,
// rather than what was tallied from its events while it worked.
func (r Run) UsageRecorded() bool { return r.final && r.Usage != nil }

// Left is how much of its lease the run may still hand on or spend. Zero for
// an unmetered run.
func (r Run) Left() int64 {
	if r.Lease <= 0 {
		return 0
	}
	return max(r.Lease-r.Spent-r.Granted, 0)
}

// update changes one run in a txn. fn returns what the caller is told, and
// touches the run when it changed it, so it is written. A run the tenant
// does not have is reported as Run{}, false, and so is a change that could
// not be written, which is logged.
func (x *Index) update(what, tenant, runID string, fn func(t *txn, r *Run) (Run, bool)) (Run, bool) {
	var out Run
	var ok bool
	err := x.do(tenant, func(t *txn) error {
		out, ok = Run{}, false
		r, err := t.run(runID)
		if err != nil || r == nil {
			return err
		}
		out, ok = fn(t, r)
		return nil
	})
	if err != nil {
		x.fail(what, tenant, runID, err)
		return Run{}, false
	}
	return out, ok
}

// SetTitle names a conversation. A conversation with no name is shown by the
// first thing that was asked in it.
func (x *Index) SetTitle(tenant, thread, title string) {
	if err := x.setTitle(tenant, thread, title, false); err != nil {
		x.fail("title", tenant, thread, err)
	}
}

// setTitle names a conversation; with unset only one not yet named.
func (x *Index) setTitle(tenant, thread, title string, unset bool) error {
	return x.do(tenant, func(t *txn) error {
		s, err := t.thread(thread)
		if err != nil {
			return err
		}
		if s.Name == title || unset && s.Name != "" {
			return errSkip
		}
		s.Name = title
		t.threadChanged(thread)
		return t.x.tenants.Mark(tenant)
	})
}

// Record adds an accepted run. Recording a run that is already known changes
// nothing, because accepting the same tag twice joins the existing run.
func (x *Index) Record(runID string, tag event.Envelope, rev int) error {
	edge, err := call.EdgeOf(tag)
	if err != nil {
		return err
	}
	input, err := call.InputOf(tag)
	if err != nil {
		return err
	}
	// The edge names tags. Runs are indexed by run id, which fiber derives
	// from the tenant and the tag id together, so the links are translated
	// once, here. A call never leaves its caller's tenant, so the tag's own
	// tenant is the one its root and its parent were accepted under.
	root, parent := runtime.RunID(tag.Tenant, edge.Root), ""
	if edge.Parent != "" {
		parent = runtime.RunID(tag.Tenant, edge.Parent)
	}
	// Only the cron trigger and the platform's own "run now" set this key. A
	// tag from anywhere else does not get to claim a schedule.
	schedule := ""
	if wire.Origin(tag) == "cron" || wire.Origin(tag) == OriginManual {
		schedule = wire.SurfaceOf(tag).CronSchedule
	}
	// A run is its own conversation unless the API placed it in one. A run
	// called by an agent belongs to its root's conversation.
	thread, prev, picked, routed, text, client := runID, "", "", "", wire.TextOf(tag), ClientOf(tag)
	channel, message := "", ""
	// A schedule's project is written by the server when the schedule is
	// created, so the cron trigger and a person's run-now are believed too.
	project, within := ProjectOf(tag), WithinOf(tag)
	if wire.Origin(tag) == OriginAPI {
		if v := wire.Meta(tag)[MetaThread]; v != "" {
			thread = v
		}
		prev, picked, routed = wire.Meta(tag)[MetaPrev], wire.Meta(tag)[MetaPicked], wire.Meta(tag)[MetaRouted]
		channel, message = wire.Meta(tag)[MetaChannel], wire.Meta(tag)[MetaMessage]
	}
	// Slack and GitHub ingress writes the channel from the client's binding.
	if wire.Origin(tag) == OriginSlack || wire.Origin(tag) == OriginGitHub {
		channel = wire.Meta(tag)[MetaChannel]
	}
	// The agent is sent the conversation so far, or a schedule's text with
	// its project's context. What was actually asked is kept apart, and is
	// what the record shows. The platform writes it for these origins.
	if v := wire.Meta(tag)[MetaAsk]; v != "" && (wire.Origin(tag) == OriginAPI || wire.Origin(tag) == "cron" || wire.Origin(tag) == OriginManual || wire.Origin(tag) == OriginAssignment) {
		text = v
	}
	return x.do(tag.Tenant, func(t *txn) error {
		if n, err := x.seq(tag.Tenant, runID); err != nil || n != 0 {
			return cmp.Or(err, errSkip)
		}
		thread, project, client, within, channel, message := thread, project, client, within, channel, message
		if parent != "" {
			top, err := t.run(root)
			if err != nil {
				return err
			}
			if top != nil {
				thread, project, client, within, channel, message = top.Thread, top.Project, top.Client, top.Within, top.Channel, ""
			}
		}
		r := &Run{
			Citations: []Citation{},
			ID:        runID, TagID: tag.ID, Tenant: tag.Tenant, Agent: wire.AgentOf(tag), Rev: rev, Origin: wire.Origin(tag),
			Text: text, Input: input, Actor: wire.ActorOf(tag), Thread: thread, Prev: prev, Picked: picked, Routed: routed, Project: project, Within: slices.Clone(within), Client: client,
			Channel: channel, Message: message,
			Root: root, Parent: parent, Depth: edge.Depth, Mode: edge.Mode, Lifecycle: lifecycleOf(parent, edge.Mode),
			Lease: edge.Lease, Schedule: schedule,
			Status: Accepted, CreatedAt: tag.Time, UpdatedAt: tag.Time,
			tag: tag, observedBy: x.instance,
		}
		if r.Lifecycle == Attached {
			p, err := t.run(parent)
			if err != nil {
				return err
			}
			if p != nil {
				p.openAttached++
				t.touch(p)
			}
		}
		return t.place(r)
	})
}

// Called records that a run made a call and granted it a lease.
func (x *Index) Called(tenant, runID string, lease int64) {
	x.update("call", tenant, runID, func(t *txn, r *Run) (Run, bool) {
		t.touch(r)
		r.Calls++
		r.Granted += lease
		return Run{}, true
	})
}

// fresh reports whether an event is one the run has not folded yet: an event
// with no sequence always is, and one at or below the last sequence folded
// was folded already, by this process or another.
func fresh(r *Run, e event.Envelope) bool {
	return e.Seq == 0 || e.Seq > r.lastSeq
}

// Observe folds a lifecycle event into the run it belongs to. It returns the
// run as it now stands and whether this event is the one that ended it, so a
// caller can react to an ending exactly once, whichever process folds it
// first. Other kinds, and events for runs this index never recorded, change
// nothing.
func (x *Index) Observe(e event.Envelope) (Run, bool) {
	if e.Type == event.TypeAction {
		return x.observeDetail(e), false
	}
	if !wire.IsLifecycle(e.Type) {
		return Run{}, false
	}
	var out Run
	var ended bool
	err := x.do(e.Tenant, func(t *txn) error {
		out, ended = Run{}, false
		r, err := t.run(e.Run)
		if err != nil || r == nil || r.Status.Terminal() || !fresh(r, e) {
			return err
		}
		t.touch(r)
		if e.Seq > 0 {
			r.lastSeq = e.Seq
		}
		r.UpdatedAt = e.Time
		switch b := e.Data.(type) {
		case event.Started, event.Resumed:
			r.Status, r.Reason, r.Question = Running, "", nil
		case event.Parked:
			r.Status, r.Reason, r.Question = Parked, b.Reason, b.Question
			if b.Question != nil {
				if err := t.neededPerson(r); err != nil {
					return err
				}
			}
		case event.Completed:
			r.Answer = b.Text
			if b.Usage != nil {
				u := *b.Usage
				r.Usage, r.final = &u, true
			}
			if hold(r) {
				out = citationSnapshot(r)
				return nil
			}
			r.Status = Completed
		case event.Failed:
			// A failed attempt may still be retried. Only a terminal failure
			// ends the run.
			r.Error = b.Error
			if b.Terminal {
				r.Status = Failed
			}
		case event.Stopped:
			// The runtime says the run wrapped up on request. What it had
			// said is the answer people saw, and is kept. A stopped run is
			// not held open for what is attached to it: those are stopped
			// with it.
			r.Answer, r.Status, r.Finishing, r.Reason = b.Text, Stopped, false, ""
			if b.Usage != nil {
				u := *b.Usage
				r.Usage, r.final = &u, true
			}
			r.Cause = causeOf(b.Reason, b.By)
		}
		if r.Status.Terminal() {
			x.finishAssistance(r)
			r.Stopping, r.Question = false, nil
		}
		out, ended = citationSnapshot(r), r.Status.Terminal()
		return nil
	})
	if err != nil {
		x.fail("event", e.Tenant, e.Run, err)
		return Run{}, false
	}
	return out, ended
}

// ObserveUsage folds a usage.call or usage.child event into the usage of the
// run that published it, while the runtime's own total is not in yet. Each
// report is counted once by its AccountingID, so a redelivered event changes
// nothing, whichever process folds it. It returns the run as it now stands,
// and false for any other kind, a run this index does not hold, or another
// tenant's.
func (x *Index) ObserveUsage(e event.Envelope) (Run, bool) {
	if e.Type != event.TypeUsageCall && e.Type != event.TypeUsageChild {
		return Run{}, false
	}
	var out Run
	var ok bool
	err := x.do(e.Tenant, func(t *txn) error {
		out, ok = Run{}, false
		r, err := t.run(e.Run)
		if err != nil || r == nil {
			return err
		}
		if r.final {
			// The runtime's record stands. The call is still the ledger's
			// to judge, so the run is returned.
			out, ok = citationSnapshot(r), true
			return nil
		}
		if r.tally == nil {
			r.tally = &event.Tally{}
		}
		var counted bool
		// A report with no accounting id of its own is told apart by where
		// it was published, which folding it again repeats.
		where := ""
		if e.Seq > 0 {
			where = fmt.Sprintf("%s#%d", e.Run, e.Seq)
		}
		switch b := e.Data.(type) {
		case event.UsageCall:
			b.AccountingID = cmp.Or(b.AccountingID, where)
			counted = r.tally.Call(b)
		case event.UsageChild:
			b.AccountingID = cmp.Or(b.AccountingID, where)
			counted = r.tally.Child(b)
		default:
			return nil
		}
		if counted {
			u := r.tally.Usage()
			r.Usage = &u
			t.touch(r)
		}
		out, ok = citationSnapshot(r), true
		return nil
	})
	if err != nil {
		x.fail("usage", e.Tenant, e.Run, err)
		return Run{}, false
	}
	return out, ok
}

// SetUsage closes a run's spending once it has ended: the runtime's own
// figure, when there is one, replaces what was tallied from its events, and
// the micro-USD its settlement added beyond the calls already spent joins
// Spent, in one step. Nothing changes a closed run's usage or Spent after
// that but the calls it made, as they settle with it. A run is closed once.
func (x *Index) SetUsage(tenant, runID string, u *event.RunUsage, micros int64) {
	x.update("usage", tenant, runID, func(t *txn, r *Run) (Run, bool) {
		if r.closed {
			return Run{}, false
		}
		t.touch(r)
		if u != nil && !r.final {
			v := *u
			r.Usage, r.final = &v, true
		}
		r.tally, r.closed = nil, true
		r.Spent += micros
		return Run{}, true
	})
}

// Spend adds what a run's own model call cost, in micro-USD, to what it has
// spent, and returns the run as it now stands. A run whose spending is closed
// has settled, and perhaps settled into its caller, so a call reported after
// that changes nothing here and Spend reports false: the usage ledger still
// counts whatever the call adds beyond the run's settled total.
func (x *Index) Spend(tenant, runID string, micros int64) (Run, bool) {
	var out Run
	_, ok := x.update("spend", tenant, runID, func(t *txn, r *Run) (Run, bool) {
		out = citationSnapshot(r)
		if r.closed {
			return Run{}, false
		}
		t.touch(r)
		r.Spent += micros
		out = citationSnapshot(r)
		return out, true
	})
	return out, ok
}

// SettleCall settles a call that has ended with the run that made it: the
// budget it was leased is no longer held for it, and what it spent, with
// everything it called in turn, is the caller's spending. It is called once
// per call, when the call ends.
//
// A caller may have ended first, a delegated call's caller most of all, and
// settled its own spend into its caller already. The spend then climbs on,
// through every ancestor that has settled, to the nearest one that has not,
// or to the root, so the tree's figure is whole wherever it is read.
func (x *Index) SettleCall(child Run) {
	err := x.do(child.Tenant, func(t *txn) error {
		c, err := t.run(child.ID)
		if err != nil || c == nil || c.settledUp {
			return err
		}
		r, err := t.run(child.Parent)
		if err != nil || r == nil {
			return err
		}
		c.settledUp = true
		r.Granted = max(r.Granted-c.Lease, 0)
		t.touch(c)
		for seen := map[string]bool{}; !seen[r.ID]; {
			seen[r.ID] = true
			r.Spent += c.Spent
			t.touch(r)
			if !r.settledUp {
				return nil
			}
			next, err := t.run(r.Parent)
			if err != nil || next == nil {
				return err
			}
			r = next
		}
		return nil
	})
	if err != nil {
		x.fail("settle call", child.Tenant, child.ID, err)
	}
}

// causeOf says why a run stopped, from the request the runtime honoured.
func causeOf(reason, by string) string {
	switch {
	case reason != "":
		return reason
	case by != "":
		return "Stopped by " + by
	}
	return "Stopped"
}

// Unfinished returns the runs that have not ended, each with the tag it was
// accepted with: tenant by tenant, oldest first in each, at most MaxTree in
// one tenant.
func (x *Index) Unfinished() []Run {
	var out []Run
	err := x.tenants.Each(func(tenant string) error {
		rs, err := x.under(tenant, "a:", MaxTree)
		for _, r := range rs {
			out = append(out, citationSnapshot(r))
		}
		return err
	})
	if err != nil {
		x.log.Error("runs: could not read the runs that have not ended", "error", err)
	}
	return out
}

// End closes a run from a source other than its own events. It returns the run
// and whether this call is what ended it, so an ending is still acted on once.
//
// The bus reports what each attempt did. Whether the run is over is the durable
// ledger's call, and the two can disagree: a run that exhausts its retries is
// failed in the ledger without a final event ever being published.
func (x *Index) End(tenant, runID string, status Status, answer, failure string, at time.Time) (Run, bool) {
	return x.update("end", tenant, runID, func(t *txn, r *Run) (Run, bool) {
		if r.Status.Terminal() || !status.Terminal() {
			return Run{}, false
		}
		t.touch(r)
		if answer != "" {
			r.Answer = answer
		}
		if failure != "" {
			r.Error = failure
		}
		if status == Completed && hold(r) {
			// Held open: written, but not ended.
			return Run{}, false
		}
		r.Status, r.UpdatedAt, r.Finishing, r.Reason, r.Stopping, r.Question = status, at, false, "", false, nil
		x.finishAssistance(r)
		return citationSnapshot(r), true
	})
}

// hold keeps a run that has answered open while runs attached to it are still
// working, and reports whether it did.
func hold(r *Run) bool {
	n := r.openAttached
	if n <= 0 {
		return false
	}
	r.Status, r.Finishing = Running, true
	r.Reason = fmt.Sprintf("Waiting for %d attached agent(s) to finish", n)
	return true
}

// Settle completes a run that was held open by attached runs, once none of
// them is still working. It returns the run and whether this call ended it.
func (x *Index) Settle(tenant, runID string, at time.Time) (Run, bool) {
	return x.update("settle", tenant, runID, func(t *txn, r *Run) (Run, bool) {
		if !r.Finishing || r.Status.Terminal() || r.openAttached > 0 {
			return Run{}, false
		}
		t.touch(r)
		r.Status, r.UpdatedAt, r.Finishing, r.Reason, r.Stopping = Completed, at, false, "", false
		x.finishAssistance(r)
		return citationSnapshot(r), true
	})
}

// AskStop records that a run was asked to stop. From now on the run
// is stopping: it can start no further calls, and the runtime decides how it
// ends. It returns the run as it stands and false for a run that has ended or
// that this index does not hold. Asking again keeps the first request, as the
// runtime does.
func (x *Index) AskStop(tenant, runID string, at time.Time) (Run, bool) {
	var out Run
	var ok bool
	x.update("ask stop", tenant, runID, func(t *txn, r *Run) (Run, bool) {
		out, ok = Run{}, false
		if r.Status.Terminal() {
			return Run{}, false
		}
		ok = true
		if r.Stopping {
			out = citationSnapshot(r)
			return Run{}, false
		}
		r.Stopping, r.StopRequestedAt, r.UpdatedAt = true, at, at
		t.touch(r)
		out = citationSnapshot(r)
		return out, true
	})
	return out, ok
}

// Stop ends a run as stopped from a source other than its own events: one the
// ledger says ended stopped before its final event was observed. answer, when given, is
// what the run had said, and cause is why it stopped. It returns the run and
// whether this call is what ended it.
func (x *Index) Stop(tenant, runID, answer, cause string, at time.Time) (Run, bool) {
	return x.update("stop", tenant, runID, func(t *txn, r *Run) (Run, bool) {
		if r.Status.Terminal() {
			return Run{}, false
		}
		t.touch(r)
		if answer != "" {
			r.Answer = answer
		}
		r.Status, r.Cause, r.UpdatedAt, r.Finishing, r.Reason, r.Stopping, r.Question = Stopped, causeOf(cause, ""), at, false, "", false, nil
		x.finishAssistance(r)
		return citationSnapshot(r), true
	})
}

// AttachedTo returns the runs attached to a run that have not ended.
func (x *Index) AttachedTo(tenant, runID string) []Run {
	r, _, err := x.get(tenant, runID)
	if err != nil {
		return nil
	}
	tree, err := x.under(tenant, treePrefix(r.Root), MaxTree)
	if err != nil {
		x.log.Error("runs: could not read a call tree", "tenant", tenant, "run", runID, "error", err)
		return nil
	}
	var out []Run
	for _, c := range tree {
		if c.Parent == runID && c.Lifecycle == Attached && !c.Status.Terminal() {
			out = append(out, citationSnapshot(c))
		}
	}
	return out
}

// Window is what the runs in a scope did since a time.
type Window struct {
	// Total is how many runs started, and Failed how many of those failed.
	Total, Failed int
	// Spent is what those runs' own model calls cost, in micro-USD. Each run
	// counts only its own calls, so a tree of runs is not counted twice.
	// Calls with no price, or priced in another currency, add nothing.
	Spent int64
}

// Recent sums a tenant's runs that started since a time. An empty agent or
// project matches every agent or project; both narrow when set.
func (x *Index) Recent(tenant, agent, project string, since time.Time) Window {
	var w Window
	err := x.scan(tenant, func(r *Run) bool {
		if r.CreatedAt.Before(since.Add(-scanSlack)) {
			return false
		}
		if r.CreatedAt.Before(since) || (agent != "" && r.Agent != agent) || (project != "" && r.Project != project) {
			return true
		}
		w.Total++
		if r.Status == Failed {
			w.Failed++
		}
		if r.Usage != nil {
			w.Spent += usage.Of(r.Usage.Own).Micros
		}
		return true
	})
	if err != nil {
		x.log.Error("runs: could not read recent runs", "tenant", tenant, "error", err)
	}
	return w
}

// Get returns one run in a tenant.
func (x *Index) Get(tenant, runID string) (Run, error) {
	r, _, err := x.get(tenant, runID)
	if err != nil {
		return Run{}, err
	}
	return citationSnapshot(r), nil
}

// List returns a tenant's runs, newest first, at most limit of them, and at
// most state.MaxList.
func (x *Index) List(tenant string, limit int) []Run {
	if limit <= 0 || limit > state.MaxList {
		limit = state.MaxList
	}
	var out []Run
	err := x.scan(tenant, func(r *Run) bool {
		out = append(out, citationSnapshot(r))
		return len(out) < limit
	})
	if err != nil {
		x.log.Error("runs: could not list runs", "tenant", tenant, "error", err)
	}
	return out
}

// Tree returns every run under the same root as runID, oldest first, so a
// parent always precedes its children: at most MaxTree.
func (x *Index) Tree(tenant, runID string) ([]Run, error) {
	r, _, err := x.get(tenant, runID)
	if err != nil {
		return nil, err
	}
	tree, err := x.under(tenant, treePrefix(r.Root), MaxTree)
	if err != nil {
		return nil, err
	}
	out := make([]Run, 0, len(tree))
	for _, c := range tree {
		out = append(out, citationSnapshot(c))
	}
	return out, nil
}

// ThreadSummary is one conversation at a glance.
type ThreadSummary struct {
	ID string `json:"id"`
	// Title is the conversation's name, or the first thing that was asked
	// when it has not been named.
	Title string `json:"title"`
	// Named reports whether the title is a name rather than the first message.
	Named   bool   `json:"named"`
	Origin  string `json:"origin"`
	Project string `json:"project,omitempty"`
	// Channel is the project's channel it is in: DefaultChannel when its runs
	// name none.
	Channel string `json:"channel"`
	// Agents is every agent that has answered in it, in order of first reply.
	Agents []string `json:"agents"`
	// People is every person who asked in it, by subject, in order of first
	// turn. A credential that is not a person is not one of them.
	People []string `json:"people"`
	Turns  int      `json:"turns"`
	// Branches is how many turns have more than one answer.
	Branches int `json:"branches"`
	// Active is how many of its runs have not ended.
	Active    int       `json:"active"`
	Failed    int       `json:"failed"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Conversation returns the turns of a thread, oldest first. Runs an agent
// called along the way are not turns and are left out: they hang off a turn's
// call tree.
func (x *Index) Conversation(tenant, thread string) ([]Run, error) {
	turns, err := x.under(tenant, turnPrefix(thread), MaxTree)
	if err != nil {
		return nil, err
	}
	if len(turns) == 0 {
		return nil, ErrNotFound
	}
	out := make([]Run, 0, len(turns))
	for _, r := range turns {
		out = append(out, citationSnapshot(r))
	}
	return out, nil
}

// Path returns the turns from the start of a conversation down to a run,
// oldest first: the one branch that run sits on, at most MaxTree turns.
func (x *Index) Path(tenant, runID string) ([]Run, error) {
	var out []Run
	seen := map[string]bool{}
	for id := runID; id != "" && !seen[id] && len(out) < MaxTree; {
		r, _, err := x.get(tenant, id)
		if errors.Is(err, ErrNotFound) {
			break
		}
		if err != nil {
			return nil, err
		}
		seen[id] = true
		out = append(out, citationSnapshot(r))
		id = r.Prev
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	slices.Reverse(out)
	return out, nil
}

// Standing narrows conversations by how they stand.
type Standing string

const (
	// StandingRunning is conversations with a run in flight.
	StandingRunning Standing = "running"
	// StandingFailed is conversations in which a reply failed.
	StandingFailed Standing = "failed"
)

// Paging bounds for ThreadPage.
const (
	DefaultThreadPage = 200
	MaxThreadPage     = 200
)

// ErrCursor reports a paging cursor this index did not issue.
var ErrCursor = errors.New("runs: invalid cursor")

// ThreadQuery selects and pages a tenant's conversations. Every field is
// optional. Filters apply before the limit, so a page of 30 is 30 matches.
type ThreadQuery struct {
	// Origin keeps conversations started from one origin, such as "api".
	Origin string
	// Agent keeps conversations that agent has answered in.
	Agent string
	// Standing keeps conversations that are running or that had a failure.
	Standing Standing
	// Channel keeps conversations in one channel, DefaultChannel included
	// for those whose runs name none.
	Channel string
	// Projects, when not nil, keeps conversations filed in one of these
	// projects: one project, or a project and every project below it. "" is
	// the root: conversations filed in no project. An empty, non-nil list
	// keeps nothing. It is at most a subtree of the tree, which the tree's
	// caps bound.
	Projects []string
	// Limit is the page size. Zero means DefaultThreadPage, and it is capped
	// at MaxThreadPage.
	Limit int
	// Before is the cursor a previous page returned. Empty starts at the top.
	Before string
}

// A cursor is the position of the last conversation on a page: when it was
// last active, and its id to break ties. The next page is everything strictly
// after that position.
//
// Activity only ever moves a conversation toward the top. So one already seen
// never comes back on a later page, and one that becomes active while a reader
// is paging moves above the cursor: it is absent from the pages still to come
// and present the next time the first page is read.
func encodeCursor(s ThreadSummary) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(s.UpdatedAt.UnixNano(), 10) + ":" + s.ID))
}

func decodeCursor(c string) (at int64, id string, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return 0, "", ErrCursor
	}
	stamp, id, ok := strings.Cut(string(raw), ":")
	if !ok || id == "" {
		return 0, "", ErrCursor
	}
	at, err = strconv.ParseInt(stamp, 10, 64)
	if err != nil {
		return 0, "", ErrCursor
	}
	return at, id, nil
}

// Threads returns a tenant's conversations, most recently active first. A limit
// of zero returns them all.
func (x *Index) Threads(tenant string, limit int) []ThreadSummary {
	if limit <= 0 {
		limit = MaxThreadScan
	}
	var out []ThreadSummary
	err := x.eachThread(tenant, "", func(s storedThread) bool {
		out = append(out, s.summary())
		return len(out) < limit
	})
	if err != nil {
		x.log.Error("runs: could not list conversations", "tenant", tenant, "error", err)
	}
	return out
}

// eachThread calls fn for a tenant's conversations, most recently active
// first, from after a key of the activity order, until fn returns false or
// MaxThreadScan have been read.
func (x *Index) eachThread(tenant, after string, fn func(s storedThread) bool) error {
	seen := 0
	for seen < MaxThreadScan {
		recs, next, err := x.page(tenant, state.Query{Prefix: "u:", After: after, Limit: min(state.MaxList, MaxThreadScan-seen)})
		if err != nil {
			return err
		}
		for _, rec := range recs {
			seen++
			var s storedThread
			if err := json.Unmarshal(rec.Value, &s); err != nil {
				return fmt.Errorf("runs: %q: %w", rec.Key, err)
			}
			if !fn(s) {
				return nil
			}
		}
		if next == "" {
			return nil
		}
		after = next
	}
	return nil
}

// ThreadPage returns one page of a tenant's conversations, most recently active
// first, and the cursor for the page after it. The cursor is empty when there
// is nothing older. A page looks at most MaxThreadScan conversations; when it
// stops there short of its limit, its cursor goes on from the last it looked
// at.
func (x *Index) ThreadPage(tenant string, q ThreadQuery) ([]ThreadSummary, string, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultThreadPage
	}
	limit = min(limit, MaxThreadPage)
	after := ""
	if q.Before != "" {
		at, id, err := decodeCursor(q.Before)
		if err != nil {
			return nil, "", err
		}
		after = activityKey(time.Unix(0, at), id)
	}
	var in map[string]bool
	if q.Projects != nil {
		in = make(map[string]bool, len(q.Projects))
		for _, p := range q.Projects {
			in[p] = true
		}
	}
	page := []ThreadSummary{}
	next, scanned := "", 0
	var last ThreadSummary
	err := x.eachThread(tenant, after, func(st storedThread) bool {
		s := st.summary()
		scanned++
		last = s
		switch {
		case in != nil && !in[s.Project]:
			return true
		case q.Origin != "" && s.Origin != q.Origin:
			return true
		case q.Agent != "" && !slices.Contains(s.Agents, q.Agent):
			return true
		case q.Standing == StandingRunning && s.Active == 0:
			return true
		case q.Standing == StandingFailed && s.Failed == 0:
			return true
		case q.Channel != "" && s.Channel != q.Channel:
			return true
		}
		if len(page) == limit {
			// One more match exists, so there is a next page.
			next = encodeCursor(page[len(page)-1])
			return false
		}
		page = append(page, s)
		return true
	})
	if err != nil {
		return nil, "", err
	}
	if next == "" && scanned == MaxThreadScan {
		next = encodeCursor(last)
	}
	return page, next, nil
}

// AnyActive returns whether an unfinished run satisfies keep. The callback is
// pure and must not call the index. This includes delegated and system work,
// which Conversation deliberately leaves out of its human turn list. It looks
// at most at MaxTree runs.
func (x *Index) AnyActive(tenant string, keep func(Run) bool) bool {
	rs, err := x.under(tenant, "a:", MaxTree)
	if err != nil {
		x.log.Error("runs: could not read the runs in flight", "tenant", tenant, "error", err)
		return false
	}
	for _, r := range rs {
		if keep(*r) {
			return true
		}
	}
	return false
}

// neededPerson propagates only within the authoritative execution tree. The
// flag survives question resolution.
func (t *txn) neededPerson(r *Run) error {
	seen := map[string]bool{}
	for r != nil && !seen[r.ID] {
		seen[r.ID] = true
		r.Assistance = "needed"
		t.touch(r)
		if r.Parent == "" {
			return nil
		}
		parent, err := t.run(r.Parent)
		if err != nil {
			return err
		}
		r = parent
	}
	return nil
}

// finishAssistance says a run needed no person, when this index recorded it
// and so folded every question it asked.
func (x *Index) finishAssistance(r *Run) {
	if r.Assistance == "" && r.observedBy == x.instance {
		r.Assistance = "none"
	}
}
