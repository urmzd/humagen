// Package runs is the platform's index of runs and the tree they form.
//
// fiber's ledger knows whether a run survived. This index knows how runs
// relate: which run called which, how many calls each has made, and how much
// budget each has handed out. It is a projection of two inputs, the tag a run
// was accepted with and the lifecycle events that run later publishes.
//
// Index is in memory. Built with Open, it keeps its runs in a state.Store and
// reads them back when it is built again; see persisted for when each change
// is written.
package runs

import (
	"cmp"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"

	"github.com/urmzd/warp/pkg/call"
	"github.com/urmzd/warp/pkg/usage"
)

// ErrNotFound reports a run that does not exist in the caller's tenant. A run
// in another tenant is reported the same way, so the answer never confirms that
// it exists.
var ErrNotFound = errors.New("runs: not found")

// OriginManual marks a schedule fired by a person rather than by the clock.
const OriginManual = "manual"

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
func WithinOf(tag envelope.Tag) []string {
	v := tag.Meta[MetaWithin]
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
// envelope.PrivateMetaPrefix), so fiber keeps it out of everything a model is
// sent: the model is grounded in the sources without being told their names.
const MetaSources = envelope.PrivateMetaPrefix + "warp.sources"

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
func ClientOf(tag envelope.Tag) string {
	if tag.Origin == "slack" || tag.Origin == "github" {
		return tag.Meta[MetaClient]
	}
	return ""
}

// ProjectOf returns the project a tag files its run under, or "". Only origins
// whose project the server itself wrote are believed: the API, the cron trigger
// (a schedule's project is set when the schedule is created), a person's
// run-now, an eval (filed where the agent it tries lives), and Slack and
// GitHub, whose ingress writes it from the client's binding over whatever a
// connector said.
func ProjectOf(tag envelope.Tag) string {
	switch tag.Origin {
	case OriginAPI, "cron", OriginManual, OriginEval, OriginSlack, OriginGitHub:
		return tag.Meta[MetaProject]
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
	ID string `json:"id"`
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
	Actor envelope.Actor `json:"actor,omitzero"`

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
	Usage *payload.RunUsage `json:"usage,omitempty"`
	// Calls is how many calls the run has made.
	Calls int `json:"calls"`

	Status Status `json:"status"`
	// Answer is the final text of a completed run. On a stopped run it is the
	// partial answer: what the agent had said when it stopped, which people
	// may already have read. It is never the whole answer, and Status says so.
	Answer string `json:"answer,omitempty"`
	Error  string `json:"error,omitempty"`
	// Reason says what a parked run is waiting for, or that a run which has
	// answered is waiting for the agents attached to it.
	Reason string `json:"reason,omitempty"`
	// Question is what a parked run is waiting on a person for. It is gone
	// once the run resumes or ends.
	Question *payload.Question `json:"question,omitempty"`
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
	tag envelope.Tag
	// requesters are every name the person who asked for the run went by
	// when it was accepted, pinned so a mapping removed later still counts.
	requesters []string
	// tally sums the usage reported while the run works. final is set once
	// the runtime's own figure has replaced it.
	tally *payload.Tally
	final bool
	// closed is set once the run's own spending is settled, after which
	// Spend changes nothing. settledUp is set once its spend has been
	// settled into its caller, so a call that ends later climbs past it.
	closed, settledUp bool
}

// Requesters returns the names pinned for the person who asked for the run.
func (r Run) Requesters() []string { return slices.Clone(r.requesters) }

// PinRequesters records every name the person who asked for a run goes by,
// once, when it is accepted.
func (x *Index) PinRequesters(runID string, names []string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if r, ok := x.runs[runID]; ok && r.requesters == nil {
		r.requesters = slices.Clone(names)
		x.touch(runID)
	}
}

// Tag returns the tag the run was accepted with.
func (r Run) Tag() envelope.Tag { return r.tag }

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

// Index holds every run this process has accepted.
type Index struct {
	mu   sync.RWMutex
	runs map[string]*Run
	// order is run ids, oldest first.
	order []string
	// titles names conversations, by tenant and thread.
	titles map[string]string
	// counts are what each project holds, kept as runs change.
	counts counters
	// kept is where runs are written. Nil keeps them only in memory.
	kept *persisted
}

// NewIndex returns an empty index.
func NewIndex() *Index {
	return &Index{runs: map[string]*Run{}, titles: map[string]string{}, counts: newCounters()}
}

// SetTitle names a conversation. A conversation with no name is shown by the
// first thing that was asked in it.
func (x *Index) SetTitle(tenant, thread, title string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.titles[tenant+"\x00"+thread] = title
	x.titled(tenant + "\x00" + thread)
}

// Record adds an accepted run. Recording a run that is already known changes
// nothing, because accepting the same tag twice joins the existing run.
func (x *Index) Record(runID string, tag envelope.Tag, rev int) error {
	edge, err := call.EdgeOf(tag)
	if err != nil {
		return err
	}
	input, err := call.InputOf(tag)
	if err != nil {
		return err
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if _, ok := x.runs[runID]; ok {
		return nil
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
	if tag.Origin == "cron" || tag.Origin == OriginManual {
		schedule = tag.Meta["cron_schedule"]
	}
	// A run is its own conversation unless the API placed it in one. A run
	// called by an agent belongs to its root's conversation.
	thread, prev, picked, text, project, client := runID, "", "", tag.Text, "", ClientOf(tag)
	channel, message := "", ""
	// A schedule's project is written by the server when the schedule is
	// created, so the cron trigger and a person's run-now are believed too.
	project, within := ProjectOf(tag), WithinOf(tag)
	if tag.Origin == OriginAPI {
		if v := tag.Meta[MetaThread]; v != "" {
			thread = v
		}
		prev, picked = tag.Meta[MetaPrev], tag.Meta[MetaPicked]
		channel, message = tag.Meta[MetaChannel], tag.Meta[MetaMessage]
	}
	// Slack and GitHub ingress writes the channel from the client's binding.
	if tag.Origin == OriginSlack || tag.Origin == OriginGitHub {
		channel = tag.Meta[MetaChannel]
	}
	// The agent is sent the conversation so far, or a schedule's text with
	// its project's context. What was actually asked is kept apart, and is
	// what the record shows. The platform writes it for these origins.
	if v := tag.Meta[MetaAsk]; v != "" && (tag.Origin == OriginAPI || tag.Origin == "cron" || tag.Origin == OriginManual) {
		text = v
	}
	if top, ok := x.runs[root]; ok && parent != "" && top.Tenant == tag.Tenant {
		thread, project, client, within, channel, message = top.Thread, top.Project, top.Client, top.Within, top.Channel, ""
	}
	r := &Run{
		ID: runID, TagID: tag.ID, Tenant: tag.Tenant, Agent: tag.Agent, Rev: rev, Origin: tag.Origin,
		Text: text, Input: input, Actor: tag.Actor, Thread: thread, Prev: prev, Picked: picked, Project: project, Within: slices.Clone(within), Client: client,
		Channel: channel, Message: message,
		Root: root, Parent: parent, Depth: edge.Depth, Mode: edge.Mode, Lifecycle: lifecycleOf(parent, edge.Mode),
		Lease: edge.Lease, Schedule: schedule,
		Status: Accepted, CreatedAt: tag.At, UpdatedAt: tag.At,
		tag: tag,
	}
	if err := x.recorded(r); err != nil {
		return err
	}
	x.runs[runID] = r
	x.order = append(x.order, runID)
	x.count(r)
	return nil
}

// Called records that a run made a call and granted it a lease.
func (x *Index) Called(runID string, lease int64) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if r, ok := x.runs[runID]; ok {
		r.Calls++
		r.Granted += lease
		x.touch(runID)
	}
}

// Observe folds a lifecycle event into the run it belongs to. It returns the
// run as it now stands and whether this event is the one that ended it, so a
// caller can react to an ending exactly once. Other kinds, and events for runs
// this index never recorded, change nothing.
func (x *Index) Observe(e envelope.Event) (Run, bool) {
	if e.Kind.Family() != "lifecycle" {
		return Run{}, false
	}
	body, err := payload.Decode[payload.Lifecycle](e)
	if err != nil {
		return Run{}, false
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	r, ok := x.runs[e.RunID]
	if !ok || r.Tenant != e.Tenant || r.Status.Terminal() {
		return Run{}, false
	}
	defer x.recount(r)
	x.touch(r.ID)
	r.UpdatedAt = e.At
	switch e.Kind {
	case envelope.KindStarted, envelope.KindResumed:
		r.Status, r.Reason, r.Question = Running, "", nil
	case envelope.KindParked:
		r.Status, r.Reason, r.Question = Parked, body.Reason, body.Question
	case envelope.KindCompleted:
		r.Answer = body.Text
		if body.Usage != nil {
			u := *body.Usage
			r.Usage, r.final = &u, true
		}
		if x.hold(r) {
			return *r, false
		}
		r.Status = Completed
	case envelope.KindFailed:
		// A failed attempt may still be retried. Only a terminal failure
		// ends the run.
		r.Error = body.Error
		if body.Terminal {
			r.Status = Failed
		}
	case envelope.KindStopped:
		// The runtime says the run wrapped up on request. What it had said
		// is the answer people saw, and is kept. A stopped run is not held
		// open for what is attached to it: those are stopped with it.
		stopped, _ := payload.Decode[payload.Stopped](e)
		r.Answer, r.Status, r.Finishing, r.Reason = stopped.Text, Stopped, false, ""
		if stopped.Usage != nil {
			u := *stopped.Usage
			r.Usage, r.final = &u, true
		}
		r.Cause = causeOf(stopped.Reason, stopped.By)
	}
	if r.Status.Terminal() {
		r.Stopping, r.Question = false, nil
	}
	return *r, r.Status.Terminal()
}

// ObserveUsage folds a usage.call or usage.child event into the usage of the
// run that published it, while the runtime's own total is not in yet. Each
// report is counted once by its AccountingID, so a redelivered event changes
// nothing. It returns the run as it now stands, and false for any other kind,
// a run this index does not hold, or another tenant's.
func (x *Index) ObserveUsage(e envelope.Event) (Run, bool) {
	if e.Kind != envelope.KindUsageCall && e.Kind != envelope.KindUsageChild {
		return Run{}, false
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	r, ok := x.runs[e.RunID]
	if !ok || r.Tenant != e.Tenant {
		return Run{}, false
	}
	if r.final {
		// The runtime's record stands. The call is still the ledger's to
		// judge, so the run is returned.
		return *r, true
	}
	x.touch(r.ID)
	if r.tally == nil {
		r.tally = &payload.Tally{}
	}
	switch e.Kind {
	case envelope.KindUsageCall:
		c, err := payload.Decode[payload.UsageCall](e)
		if err != nil {
			return Run{}, false
		}
		r.tally.Call(c)
	case envelope.KindUsageChild:
		c, err := payload.Decode[payload.UsageChild](e)
		if err != nil {
			return Run{}, false
		}
		r.tally.Child(c)
	}
	u := r.tally.Usage()
	r.Usage = &u
	return *r, true
}

// SetUsage closes a run's spending once it has ended: the runtime's own
// figure, when there is one, replaces what was tallied from its events, and
// the micro-USD its settlement added beyond the calls already spent joins
// Spent, in one step. Nothing changes a closed run's usage or Spent after
// that but the calls it made, as they settle with it. A run is closed once.
func (x *Index) SetUsage(runID string, u *payload.RunUsage, micros int64) {
	x.mu.Lock()
	defer x.mu.Unlock()
	r, ok := x.runs[runID]
	if !ok || r.closed {
		return
	}
	if u != nil && !r.final {
		v := *u
		r.Usage, r.final = &v, true
	}
	r.tally, r.closed = nil, true
	r.Spent += micros
	x.touch(runID)
}

// Spend adds what a run's own model call cost, in micro-USD, to what it has
// spent, and returns the run as it now stands. A run whose spending is closed
// has settled, and perhaps settled into its caller, so a call reported after
// that changes nothing here and Spend reports false: the usage ledger still
// counts whatever the call adds beyond the run's settled total.
func (x *Index) Spend(runID string, micros int64) (Run, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	r, ok := x.runs[runID]
	if !ok {
		return Run{}, false
	}
	if r.closed {
		return *r, false
	}
	r.Spent += micros
	x.touch(runID)
	return *r, true
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
	x.mu.Lock()
	defer x.mu.Unlock()
	c, ok := x.runs[child.ID]
	if !ok || c.settledUp {
		return
	}
	r, ok := x.runs[child.Parent]
	if !ok || r.Tenant != child.Tenant {
		return
	}
	c.settledUp = true
	r.Granted = max(r.Granted-c.Lease, 0)
	x.touch(c.ID)
	for {
		r.Spent += c.Spent
		x.touch(r.ID)
		if !r.settledUp {
			return
		}
		next, ok := x.runs[r.Parent]
		if !ok || next.Tenant != r.Tenant {
			return
		}
		r = next
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

// Active returns the ids of runs that have not ended.
func (x *Index) Active() []string {
	x.mu.RLock()
	defer x.mu.RUnlock()
	var out []string
	for _, id := range x.order {
		if !x.runs[id].Status.Terminal() {
			out = append(out, id)
		}
	}
	return out
}

// Unfinished returns the runs that have not ended, oldest first, each with the
// tag it was accepted with.
func (x *Index) Unfinished() []Run {
	x.mu.RLock()
	defer x.mu.RUnlock()
	var out []Run
	for _, id := range x.order {
		if r := x.runs[id]; !r.Status.Terminal() {
			out = append(out, *r)
		}
	}
	return out
}

// End closes a run from a source other than its own events. It returns the run
// and whether this call is what ended it, so an ending is still acted on once.
//
// The bus reports what each attempt did. Whether the run is over is the durable
// ledger's call, and the two can disagree: a run that exhausts its retries is
// failed in the ledger without a final event ever being published.
func (x *Index) End(runID string, status Status, answer, failure string, at time.Time) (Run, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	r, ok := x.runs[runID]
	if !ok || r.Status.Terminal() || !status.Terminal() {
		return Run{}, false
	}
	defer x.recount(r)
	x.touch(runID)
	if answer != "" {
		r.Answer = answer
	}
	if failure != "" {
		r.Error = failure
	}
	if status == Completed && x.hold(r) {
		return Run{}, false
	}
	r.Status, r.UpdatedAt, r.Finishing, r.Reason, r.Stopping, r.Question = status, at, false, "", false, nil
	return *r, true
}

// openAttached counts the runs attached to a run that have not ended. The
// caller holds the lock.
func (x *Index) openAttached(runID string) int {
	n := 0
	for _, c := range x.runs {
		if c.Parent == runID && c.Lifecycle == Attached && !c.Status.Terminal() {
			n++
		}
	}
	return n
}

// hold keeps a run that has answered open while runs attached to it are still
// working, and reports whether it did. The caller holds the lock.
func (x *Index) hold(r *Run) bool {
	n := x.openAttached(r.ID)
	if n == 0 {
		return false
	}
	r.Status, r.Finishing = Running, true
	r.Reason = fmt.Sprintf("Waiting for %d attached agent(s) to finish", n)
	return true
}

// Settle completes a run that was held open by attached runs, once none of
// them is still working. It returns the run and whether this call ended it.
func (x *Index) Settle(runID string, at time.Time) (Run, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	r, ok := x.runs[runID]
	if !ok || !r.Finishing || r.Status.Terminal() || x.openAttached(runID) > 0 {
		return Run{}, false
	}
	defer x.recount(r)
	x.touch(runID)
	r.Status, r.UpdatedAt, r.Finishing, r.Reason, r.Stopping = Completed, at, false, "", false
	return *r, true
}

// AskStop records that a run was asked to stop. From now on the run
// is stopping: it can start no further calls, and the runtime decides how it
// ends. It returns the run as it stands and false for a run that has ended or
// that this index does not hold. Asking again keeps the first request, as the
// runtime does.
func (x *Index) AskStop(runID string, at time.Time) (Run, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	r, ok := x.runs[runID]
	if !ok || r.Status.Terminal() {
		return Run{}, false
	}
	if !r.Stopping {
		r.Stopping, r.StopRequestedAt, r.UpdatedAt = true, at, at
		x.touch(runID)
		x.recount(r)
	}
	return *r, true
}

// Stop ends a run as stopped from a source other than its own events: one the
// ledger says ended stopped before its final event was observed. answer, when given, is
// what the run had said, and cause is why it stopped. It returns the run and
// whether this call is what ended it.
func (x *Index) Stop(runID, answer, cause string, at time.Time) (Run, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	r, ok := x.runs[runID]
	if !ok || r.Status.Terminal() {
		return Run{}, false
	}
	defer x.recount(r)
	x.touch(runID)
	if answer != "" {
		r.Answer = answer
	}
	r.Status, r.Cause, r.UpdatedAt, r.Finishing, r.Reason, r.Stopping, r.Question = Stopped, causeOf(cause, ""), at, false, "", false, nil
	return *r, true
}

// AttachedTo returns the runs attached to a run that have not ended.
func (x *Index) AttachedTo(runID string) []Run {
	x.mu.RLock()
	defer x.mu.RUnlock()
	var out []Run
	for _, id := range x.order {
		if c := x.runs[id]; c.Parent == runID && c.Lifecycle == Attached && !c.Status.Terminal() {
			out = append(out, *c)
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
	x.mu.RLock()
	defer x.mu.RUnlock()
	var w Window
	for _, id := range x.order {
		r := x.runs[id]
		if r.Tenant != tenant || r.CreatedAt.Before(since) ||
			(agent != "" && r.Agent != agent) || (project != "" && r.Project != project) {
			continue
		}
		w.Total++
		if r.Status == Failed {
			w.Failed++
		}
		if r.Usage != nil {
			w.Spent += usage.Of(r.Usage.Own).Micros
		}
	}
	return w
}

// Get returns one run in a tenant.
func (x *Index) Get(tenant, runID string) (Run, error) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	r, ok := x.runs[runID]
	if !ok || r.Tenant != tenant {
		return Run{}, ErrNotFound
	}
	return *r, nil
}

// List returns a tenant's runs, newest first, at most limit of them.
func (x *Index) List(tenant string, limit int) []Run {
	x.mu.RLock()
	defer x.mu.RUnlock()
	var out []Run
	for _, id := range slices.Backward(x.order) {
		if r := x.runs[id]; r.Tenant == tenant {
			out = append(out, *r)
			if limit > 0 && len(out) == limit {
				break
			}
		}
	}
	return out
}

// Tree returns every run under the same root as runID, oldest first, so a
// parent always precedes its children.
func (x *Index) Tree(tenant, runID string) ([]Run, error) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	r, ok := x.runs[runID]
	if !ok || r.Tenant != tenant {
		return nil, ErrNotFound
	}
	var out []Run
	for _, id := range x.order {
		if c := x.runs[id]; c.Tenant == tenant && c.Root == r.Root {
			out = append(out, *c)
		}
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
	x.mu.RLock()
	defer x.mu.RUnlock()
	var out []Run
	for _, id := range x.order {
		if r := x.runs[id]; r.Tenant == tenant && r.Thread == thread && r.Parent == "" {
			out = append(out, *r)
		}
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

// Path returns the turns from the start of a conversation down to a run,
// oldest first: the one branch that run sits on.
func (x *Index) Path(tenant, runID string) ([]Run, error) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	var out []Run
	seen := map[string]bool{}
	for id := runID; id != "" && !seen[id]; {
		r, ok := x.runs[id]
		if !ok || r.Tenant != tenant {
			break
		}
		seen[id] = true
		out = append(out, *r)
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
	all := x.threads(tenant)
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all
}

// ThreadPage returns one page of a tenant's conversations, most recently active
// first, and the cursor for the page after it. The cursor is empty when there
// is nothing older.
func (x *Index) ThreadPage(tenant string, q ThreadQuery) ([]ThreadSummary, string, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultThreadPage
	}
	limit = min(limit, MaxThreadPage)
	var afterAt int64
	var afterID string
	if q.Before != "" {
		var err error
		if afterAt, afterID, err = decodeCursor(q.Before); err != nil {
			return nil, "", err
		}
	}

	var in map[string]bool
	if q.Projects != nil {
		in = make(map[string]bool, len(q.Projects))
		for _, p := range q.Projects {
			in[p] = true
		}
	}
	page := []ThreadSummary{}
	for _, s := range x.threads(tenant) {
		switch {
		case in != nil && !in[s.Project]:
			continue
		case q.Origin != "" && s.Origin != q.Origin:
			continue
		case q.Agent != "" && !slices.Contains(s.Agents, q.Agent):
			continue
		case q.Standing == StandingRunning && s.Active == 0:
			continue
		case q.Standing == StandingFailed && s.Failed == 0:
			continue
		case q.Channel != "" && s.Channel != q.Channel:
			continue
		}
		if q.Before != "" {
			at := s.UpdatedAt.UnixNano()
			// Skip everything at or above the cursor's position.
			if at > afterAt || (at == afterAt && s.ID <= afterID) {
				continue
			}
		}
		if len(page) == limit {
			// One more match exists, so there is a next page.
			return page, encodeCursor(page[len(page)-1]), nil
		}
		page = append(page, s)
	}
	return page, "", nil
}

// threads builds every conversation in a tenant, most recently active first,
// with the id breaking ties so the order is the same on every read.
func (x *Index) threads(tenant string) []ThreadSummary {
	x.mu.RLock()
	defer x.mu.RUnlock()
	byID := map[string]*ThreadSummary{}
	answers := map[string]int{}
	var order []string
	for _, id := range x.order {
		r := x.runs[id]
		if r.Tenant != tenant {
			continue
		}
		s, ok := byID[r.Thread]
		if !ok {
			s = &ThreadSummary{ID: r.Thread, Title: r.Text, Origin: r.Origin, Project: r.Project, Channel: cmp.Or(r.Channel, DefaultChannel), People: []string{}, CreatedAt: r.CreatedAt}
			byID[r.Thread] = s
			order = append(order, r.Thread)
		}
		if !r.Status.Terminal() {
			s.Active++
		}
		if r.Status == Failed {
			s.Failed++
		}
		if r.UpdatedAt.After(s.UpdatedAt) {
			s.UpdatedAt = r.UpdatedAt
		}
		if r.Parent != "" {
			continue
		}
		s.Turns++
		if !slices.Contains(s.Agents, r.Agent) {
			s.Agents = append(s.Agents, r.Agent)
		}
		if who := AskedBy(*r); who != "" && !slices.Contains(s.People, who) {
			s.People = append(s.People, who)
		}
		// A second answer at the same point makes that point a branch. Runs
		// answering one person's message answer it side by side instead.
		key := r.Thread + "\x00" + r.Prev
		if answers[key]++; answers[key] == 2 && r.Message == "" {
			s.Branches++
		}
	}
	out := make([]ThreadSummary, 0, len(order))
	for _, id := range order {
		s := *byID[id]
		if title := x.titles[tenant+"\x00"+id]; title != "" {
			s.Title, s.Named = title, true
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b ThreadSummary) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), strings.Compare(a.ID, b.ID))
	})
	return out
}
