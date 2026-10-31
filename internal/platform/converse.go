package platform

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/runtime"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
)

// maxTranscript bounds how much of a conversation is replayed to the next
// agent, in bytes. Older turns are dropped first. This is truncation, not
// summarization: nothing is rewritten, and the dropped turns stay on record.
const maxTranscript = 12_000

// ErrNoBaseline reports a setting that names no agent in the tenant: the
// agent that names conversations or the one that keeps memory.
var ErrNoBaseline = errors.New("platform: a setting names an agent that is not in this tenant")

// ErrSettings reports settings that cannot be stored.
var ErrSettings = errors.New("platform: invalid settings")

// History is how much of a conversation the next agent is sent.
type History string

const (
	// HistoryRecent sends the most recent turns that fit the limit. Older
	// turns are dropped, not summarized, and stay on record.
	HistoryRecent History = "recent"
	// HistoryNone sends only the new message.
	HistoryNone History = "none"
)

// Bounds on how much conversation is replayed, in characters.
const (
	MinHistory     = 1_000
	MaxHistory     = 200_000
	DefaultHistory = maxTranscript
)

// Settings is what a tenant has chosen for itself.
type Settings struct {
	// History and HistoryLimit decide how much of a conversation is replayed
	// to the agent answering the next turn.
	History      History `json:"history"`
	HistoryLimit int     `json:"history_limit"`
	// TitleAgent names conversations after their first turn. Empty leaves a
	// conversation shown by its first message.
	TitleAgent string `json:"title_agent"`
	// MemoryAgent reads each finished turn and writes what the answering
	// agent should remember. Empty leaves memory to people.
	MemoryAgent string `json:"memory_agent"`
	// MemoryAutoApprove keeps what the memory agent writes without asking.
	// Off, which is the default, every note it writes waits for a person.
	MemoryAutoApprove bool `json:"memory_auto_approve"`
	// WorkspaceName is what the workspace calls itself.
	WorkspaceName string `json:"workspace_name"`
	// ModelPolicies are the workspace's named lists of models, with when a
	// call moves from one to the next. An agent runs on one by naming the
	// provider "policy" and the policy's name as its model.
	ModelPolicies []ModelPolicy `json:"model_policies"`
	// Router is whether, and on which model, each agent is asked about a
	// message that names none: "should I be involved?".
	Router RouterSettings `json:"router"`
	// SetupAt and SetupBy record the first time an admin saved the
	// workspace's settings. The server sets them once. A value sent by a
	// client is ignored.
	SetupAt time.Time `json:"setup_at,omitzero"`
	SetupBy string    `json:"setup_by,omitempty"`
}

func (s Settings) withDefaults() Settings {
	if s.History == "" {
		s.History = HistoryRecent
	}
	if s.HistoryLimit == 0 {
		s.HistoryLimit = DefaultHistory
	}
	if s.ModelPolicies == nil {
		s.ModelPolicies = []ModelPolicy{}
	}
	return s
}

// settings are every tenant's Settings. They are read and written per
// request (pkg/state, per request): nothing is held in memory, so every
// process on one database reads the same settings, and they do not need
// pkg/state's single-writer lock.
//
// Each tenant's settings are one record, <tenant>/settings, in the namespace
// "settings". The tenant "" holds settingsMigrated once the projects and the
// baseline older settings held have been moved out of them.
type settings struct {
	rec recordSet
}

// storedSettings is a tenant's settings as they are written. Settings once
// held the workspace's projects, because one struct was both the wire and the
// record. LegacyProjects reads them back for the migration into the project
// store, and is never written again. It is raw so a record an earlier version
// wrote is told apart even when its array is null.
type storedSettings struct {
	Settings
	LegacyProjects json.RawMessage `json:"projects,omitempty"`
	// LegacyBaseline is the agent that once answered when nobody chose one.
	// The root's default replaces it: the migration reads it once, and it is
	// never written again.
	LegacyBaseline *string `json:"baseline,omitempty"`
}

// legacyProject is a project as settings kept it.
type legacyProject struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// settingsKey is the one key a tenant's settings are kept under.
const settingsKey = "settings"

// settingsMigrated marks, in the tenant "", that no tenant's settings hold
// projects or a baseline any more (migrateProjects, migrateBaseline).
const settingsMigrated = "migrated:v1"

// open keeps settings in st, which may be shared with other processes. A nil
// st keeps them in this process's memory.
func (s *settings) open(st state.Store) error {
	if st == nil {
		st = state.NewMemory()
	}
	s.rec = recordSet{st: st, namespace: "settings"}
	return nil
}

// read returns a tenant's record and its version, 0 when it has none.
func (s *settings) read(tenant string) (storedSettings, int64, error) {
	var v storedSettings
	ver, err := s.rec.lookup(tenant, settingsKey, &v)
	switch {
	case errors.Is(err, state.ErrNotFound):
		return storedSettings{}, 0, nil
	case err != nil:
		return storedSettings{}, 0, fmt.Errorf("platform: read settings: %w", err)
	}
	return v, ver, nil
}

// get returns a tenant's settings as stored, without defaults.
func (s *settings) get(tenant string) (Settings, error) {
	v, _, err := s.read(tenant)
	return v.Settings, err
}

// update changes a tenant's record by compare-and-set: fn sees it as it is
// and changes it, and is called again when another writer moved it first.
// What fn leaves alone, such as a baseline the migration has not yet moved,
// is written back as it was read. fn returning state.ErrUnchanged writes
// nothing.
func (s *settings) update(tenant string, fn func(v *storedSettings) error) error {
	return state.Retry(func() error {
		cur, ver, err := s.read(tenant)
		if err != nil {
			return err
		}
		if err := fn(&cur); errors.Is(err, state.ErrUnchanged) {
			return nil
		} else if err != nil {
			return err
		}
		op, err := recordOp(tenant, settingsKey, cur, versionOf(ver))
		if err != nil {
			return err
		}
		if err := s.rec.write(op); err != nil {
			if errors.Is(err, state.ErrConflict) {
				return err
			}
			return fmt.Errorf("platform: write settings: %w", err)
		}
		return nil
	})
}

// leftovers returns the projects and the baselines tenants' settings still
// hold, read once across tenants, until settingsMigrated is written. A
// tenant whose record an earlier version wrote is in legacy even when its
// array was null.
func (s *settings) leftovers() (legacy map[string][]legacyProject, baselines map[string]string, done bool, err error) {
	var marked bool
	if _, err := s.rec.lookup("", settingsMigrated, &marked); err == nil {
		return nil, nil, true, nil
	} else if !errors.Is(err, state.ErrNotFound) {
		return nil, nil, false, err
	}
	c, cancel := storeCtx()
	recs, err := s.rec.st.Load(c, s.rec.namespace)
	cancel()
	if err != nil {
		return nil, nil, false, fmt.Errorf("platform: load settings: %w", err)
	}
	legacy, baselines = map[string][]legacyProject{}, map[string]string{}
	for _, r := range recs {
		if r.Tenant == "" || r.Key != settingsKey {
			continue
		}
		var v storedSettings
		if err := json.Unmarshal(r.Value, &v); err != nil {
			return nil, nil, false, fmt.Errorf("platform: the settings of %q: %w", r.Tenant, err)
		}
		if v.LegacyProjects != nil {
			var held []legacyProject
			if err := json.Unmarshal(v.LegacyProjects, &held); err != nil {
				return nil, nil, false, fmt.Errorf("platform: the projects of %q: %w", r.Tenant, err)
			}
			legacy[r.Tenant] = held
		}
		if v.LegacyBaseline != nil {
			baselines[r.Tenant] = *v.LegacyBaseline
		}
	}
	return legacy, baselines, false, nil
}

// migrated writes settingsMigrated.
func (s *settings) migrated() error {
	op, err := recordOp("", settingsMigrated, true, state.Missing)
	if err != nil {
		return err
	}
	if err := s.rec.write(op); err != nil && !errors.Is(err, state.ErrConflict) {
		return fmt.Errorf("platform: mark settings migrated: %w", err)
	}
	return nil
}

// Settings returns a tenant's settings. Settings that cannot be read are
// logged and read as the defaults, which grant nothing beyond them.
func (p *Platform) Settings(tenant string) Settings {
	v, err := p.settings.get(tenant)
	if err != nil {
		p.log.Error("platform: settings could not be read", "tenant", tenant, "error", err)
	}
	return v.withDefaults()
}

// SetSettings replaces a tenant's settings.
func (p *Platform) SetSettings(ctx context.Context, tenant string, next Settings) error {
	return p.setSettings(ctx, tenant, next, nil)
}

// setSettings is SetSettings for a writer shown only the agents shown
// reports, nil for every agent: a refusal names no other.
func (p *Platform) setSettings(ctx context.Context, tenant string, next Settings, shown func(string) bool) error {
	next = next.withDefaults()
	// Who set the workspace up, and when, is the server's to say.
	raw, err := p.settings.get(tenant)
	if err != nil {
		return err
	}
	stored := raw.withDefaults()
	next.SetupAt, next.SetupBy = stored.SetupAt, stored.SetupBy
	next.WorkspaceName = strings.TrimSpace(next.WorkspaceName)
	if len(next.WorkspaceName) > 80 || strings.ContainsAny(next.WorkspaceName, "\r\n\t<>") {
		return fmt.Errorf("%w: a workspace name is plain text on one line, at most 80 characters", ErrSettings)
	}
	// Each agent is kept by its own name, whatever spelling named it, as a
	// home and a list are.
	for _, name := range []*string{&next.TitleAgent, &next.MemoryAgent} {
		if *name == "" {
			continue
		}
		rev, err := p.Catalog().Get(ctx, tenant, *name, 0)
		if err != nil {
			return fmt.Errorf("%w: %q", ErrNoBaseline, *name)
		}
		*name = rev.Spec.Name
	}
	switch {
	case next.History != HistoryRecent && next.History != HistoryNone:
		return fmt.Errorf("%w: history is recent or none", ErrSettings)
	case next.HistoryLimit < MinHistory || next.HistoryLimit > MaxHistory:
		return fmt.Errorf("%w: the history limit is from %d to %d characters", ErrSettings, MinHistory, MaxHistory)
	}
	if err := checkModelPolicies(next.ModelPolicies, p.cfg.Ext.ProviderNames()...); err != nil {
		return err
	}
	if err := p.checkRouter(&next.Router); err != nil {
		return err
	}
	// A policy an agent still runs on cannot be removed, or that agent would
	// have no model at its next call.
	for _, old := range stored.ModelPolicies {
		if _, kept := policyIn(next.ModelPolicies, old.Name); kept {
			continue
		}
		if users := p.policyUsers(ctx, tenant, old.Name); len(users) > 0 {
			return policyInUse(old.Name, users, shown)
		}
	}
	return p.settings.update(tenant, func(v *storedSettings) error {
		next.SetupAt, next.SetupBy = v.SetupAt, v.SetupBy
		v.Settings = next
		return nil
	})
}

// policyInUse refuses removing a policy that users still run on, naming the
// ones shown reports (every one when it is nil). The others are said to live
// where the writer may not read, by no name and no count.
func policyInUse(policy string, users []string, shown func(string) bool) error {
	named := users
	if shown != nil {
		named = slices.DeleteFunc(slices.Clone(users), func(a string) bool { return !shown(a) })
	}
	switch {
	case len(named) == len(users):
		return fmt.Errorf("%w: %q is used by %s; move them to another policy first", ErrModelPolicyInUse, policy, strings.Join(users, ", "))
	case len(named) > 0:
		return fmt.Errorf("%w: %q is used by %s, and by agents in projects you may not read; move them to another policy first", ErrModelPolicyInUse, policy, strings.Join(named, ", "))
	}
	return fmt.Errorf("%w: %q is used by agents in projects you may not read; their owners can move them to another policy", ErrModelPolicyInUse, policy)
}

// MarkSetup records who first set the workspace up. It does nothing once that
// is on record, so it is safe to call after every admin write.
func (p *Platform) MarkSetup(tenant, by string) (Settings, error) {
	var cfg Settings
	err := p.settings.update(tenant, func(v *storedSettings) error {
		cfg = v.Settings
		if !v.SetupAt.IsZero() {
			return state.ErrUnchanged
		}
		v.SetupAt, v.SetupBy = p.now(), by
		cfg = v.Settings
		return nil
	})
	if err != nil {
		return Settings{}, err
	}
	return cfg.withDefaults(), nil
}

// Turn is one message a person sends.
type Turn struct {
	// TestSkill is a server-selected immutable draft. It grants no tools.
	TestSkill *library.Revision
	// ForceAsk tightens every action to require a recorded approval. It grants no tools.
	ForceAsk bool
	// AssignmentID pins a durable assignment reservation to this ordinary turn.
	AssignmentID string
	ID           string
	// Agent is who should answer: an agent, Open to ask every agent, or "" for the
	// default the conversation's place inherits.
	Agent string
	Text  string
	// After is the turn this one follows. Empty starts a conversation. Naming
	// a turn that already has a reply starts a branch from that point.
	After string
	// Project files a new conversation. A follow-up keeps its conversation's
	// project, whatever is passed here.
	Project string
	Actor   event.Actor
	// Client is the client a surface's credential sends the turn through,
	// when it acts for a person (act_as): its default is nearer than the
	// channel's, and only the agents it may route to are asked.
	Client string
	// KeepsNotes says the person the turn is for may change the notes of the
	// project it is filed in (MayChangeNotes), so what the memory agent
	// learns from it may be kept without asking, when the workspace chose
	// that. Anyone else's turn teaches the project only proposals for its
	// owners. Only a caller that asked MayChangeNotes for that person sets it.
	KeepsNotes bool
	// Sees, when set, is the agents the person the turn is for may read. An
	// agent the turn names that they may not read is refused as one that
	// does not exist, before anything is asked of it.
	Sees func(agent string) bool
}

// NoProjectCalled is what a turn filed in a project that is not there is
// refused with, and so is one filed in a project its asker may not see.
func NoProjectCalled(name string) error {
	return fmt.Errorf("%w: no project called %q", ErrSettings, projects.NameOf(name))
}

// Converse sends a message into a conversation and starts the run that answers.
//
// Any agent available where the conversation is filed can answer any turn.
// The turn names it, or names Open, or names none and the default
// the conversation's place inherits answers: the thread's, the client's, the
// channel's, then the project chain's, as for a message posted in the thread
// (a new conversation is in general of its project). With no default set it
// is ErrNoDefault, and nothing starts. The agent that answers is sent the
// branch so far, so a different agent picking up mid-conversation knows what
// was said, and when the branch follows a message people posted, the room up
// to it, each turn with who asked or said it, fenced off from the words it
// answers, which follow with who asks them (threads.Ask); ahead of it is
// what the conversation's project chain, its channel and the agent itself
// bring (see contextIn).
func (p *Platform) Converse(ctx context.Context, tenant string, t Turn) (runs.Run, error) {
	// Refused before anything is posted, so a refused turn leaves no
	// message without a run behind it.
	if p.Draining() {
		return runs.Run{}, ErrDraining
	}
	cfg := p.Settings(tenant)
	meta := map[string]string{runs.MetaAsk: t.Text}
	if t.TestSkill != nil {
		t.ForceAsk = true
		pinSkills(meta, []pinnedSkill{pinRevision(*t.TestSkill)})
	}
	body := t.Text
	th := threads.Thread{Channel: projects.General}
	if t.After != "" {
		path, err := p.Runs.Path(tenant, t.After)
		if err != nil {
			return runs.Run{}, err
		}
		meta[runs.MetaThread] = path[0].Thread
		meta[runs.MetaPrev] = t.After
		// A project deleted since is refused at admission (see placeOf),
		// after a retry of a turn already let in has joined it.
		meta[runs.MetaProject] = path[0].Project
		th = p.threadOf(tenant, path[0])
		if th.Channel != projects.General {
			meta[runs.MetaChannel] = th.Channel
		}
		if cfg.History == HistoryRecent {
			body = threads.Ask(p.branchBefore(tenant, th, path), cfg.HistoryLimit, p.shower(tenant), threads.AskerOf(t.Actor), t.Text)
		}
	} else if project := projects.NameOf(t.Project); project != "" {
		if !p.HasProject(tenant, project) {
			return runs.Run{}, NoProjectCalled(project)
		}
		meta[runs.MetaProject] = project
	}
	th.Project = meta[runs.MetaProject]

	if t.KeepsNotes && meta[runs.MetaProject] != "" {
		meta[metaKeepsNotes] = meta[runs.MetaProject]
	}

	var routes func(string) bool
	clientDefault := ""
	if t.Client != "" {
		c, err := p.Clients.Get(tenant, t.Client)
		if err != nil {
			return runs.Run{}, fmt.Errorf("%w: this credential does not answer for people of that client", ErrForbidden)
		}
		routes, clientDefault = c.Routes, c.DefaultAgent
	}
	asked := legacy.Default(strings.TrimSpace(t.Agent))
	if t.Sees != nil && asked != "" && asked != Open {
		if _, err := p.Catalog().Get(ctx, tenant, asked, 0); err != nil {
			return runs.Run{}, err
		}
		if !t.Sees(asked) {
			return runs.Run{}, fmt.Errorf("%w: %q", wire.ErrNotFound, asked)
		}
	}
	place := p.placeIn(tenant, th, clientDefault)
	if t.Sees != nil {
		// A default naming an agent the person may not read is passed over
		// for them, as if unset (placeSeen), and no agent they may not
		// read is asked.
		place = placeSeen(place, t.Sees)
		client := routes
		routes = func(a string) bool { return (client == nil || client(a)) && t.Sees(a) }
	}
	who, err := p.resolveAgents(ctx, tenant, place, asked, t.Text, routes)
	if err != nil {
		return runs.Run{}, err
	}
	if t.AssignmentID != "" {
		meta[metaAssignment] = t.AssignmentID
	}
	if t.ForceAsk {
		meta[operations.MetaForceAsk] = "true"
	}
	// Each agent that joined answers side by side, in its own run of the
	// same conversation, streamed on its own. The first is returned; the
	// rest are in the conversation.
	var first runs.Run
	for i, a := range who {
		id, m := t.ID, meta
		if a.Picked != "" {
			id, m = joinedTagID(t.ID, a.Agent), maps.Clone(meta)
			m[runs.MetaPicked] = a.Picked
			// Agents after the first answer in the first one's conversation.
			if i > 0 && m[runs.MetaThread] == "" && first.Thread != "" {
				m[runs.MetaThread] = first.Thread
			}
		}
		run, err := p.Runs.Get(tenant, runtime.RunID(tenant, id))
		if err != nil {
			run, err = p.Invoke(ctx, wire.NewEvent(wire.Raise{
				ID: id, Tenant: tenant, Agent: a.Agent, Origin: runs.OriginAPI, Actor: t.Actor, Context: m,
				Text: p.turnContext(tenant, th.Project, th.Channel, a.Agent, t.Actor.User, body, m),
			}))
		}
		if err != nil {
			if i == 0 {
				return runs.Run{}, err
			}
			p.log.Warn("platform: an agent that joined could not start", "tenant", tenant, "agent", a.Agent, "tag", id, "error", err)
			continue
		}
		if i == 0 {
			first = run
		}
	}
	return first, nil
}

// joinedTagID is the tag of the run an agent starts on joining a message
// whose tag is id, so a retry of the message starts each one once.
func joinedTagID(id, agent string) string { return id + "-" + agent }

// threadOf is the thread a conversation's first run is in: its record, or
// what a conversation from before threads reads as.
func (p *Platform) threadOf(tenant string, first runs.Run) threads.Thread {
	if th, err := p.ThreadRecord(tenant, first.Thread); err == nil {
		return th
	}
	return threads.Thread{ID: first.Thread, Project: first.Project, Channel: cmp.Or(first.Channel, projects.General), Visibility: threads.Shared}
}
