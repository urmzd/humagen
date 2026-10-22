package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"

	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
	"github.com/urmzd/warp/pkg/state"
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

// settings are every tenant's Settings. With a state store each tenant's is
// written before it is kept, and read back when the platform is built.
type settings struct {
	mu   sync.RWMutex
	by   map[string]Settings
	kept *state.Table[storedSettings]
	// legacy is the projects a tenant's settings still held when they were
	// read, by tenant, until the migration moves them to the project store.
	// A tenant whose record an earlier version wrote is here even when the
	// array was null.
	legacy map[string][]legacyProject
	// baselines is the baseline agent a tenant's settings still named when
	// they were read, by tenant, until the migration makes it the root's
	// default. A tenant whose settings never named one is not in it.
	baselines map[string]string
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

// open reads every tenant's settings from st and keeps them there from now
// on. A nil st keeps them in memory.
func (s *settings) open(st state.Store) error {
	s.by, s.legacy, s.baselines, s.kept = map[string]Settings{}, map[string][]legacyProject{}, map[string]string{}, state.NewTable[storedSettings](st, "settings")
	err := s.kept.Load(func(tenant, _ string, v storedSettings) error {
		s.by[tenant] = v.Settings
		if v.LegacyProjects != nil {
			var held []legacyProject
			if err := json.Unmarshal(v.LegacyProjects, &held); err != nil {
				return fmt.Errorf("the projects of %q: %w", tenant, err)
			}
			s.legacy[tenant] = held
		}
		if v.LegacyBaseline != nil {
			s.baselines[tenant] = *v.LegacyBaseline
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("platform: load settings: %w", err)
	}
	return nil
}

// put keeps a tenant's settings. A baseline the migration has not yet made
// the workspace default is kept with them, so no write loses it. The caller
// holds the lock.
func (s *settings) put(tenant string, v Settings) error {
	rec := storedSettings{Settings: v}
	if b, ok := s.baselines[tenant]; ok {
		rec.LegacyBaseline = &b
	}
	if err := s.kept.Put(tenant, settingsKey, rec); err != nil {
		return fmt.Errorf("platform: write settings: %w", err)
	}
	if s.by == nil {
		s.by = map[string]Settings{}
	}
	s.by[tenant] = v
	return nil
}

func (s *settings) get(tenant string) Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.by[tenant].withDefaults()
}

// Settings returns a tenant's settings.
func (p *Platform) Settings(tenant string) Settings { return p.settings.get(tenant) }

// SetSettings replaces a tenant's settings.
func (p *Platform) SetSettings(ctx context.Context, tenant string, next Settings) error {
	next = next.withDefaults()
	// Who set the workspace up, and when, is the server's to say.
	stored := p.Settings(tenant)
	next.SetupAt, next.SetupBy = stored.SetupAt, stored.SetupBy
	next.WorkspaceName = strings.TrimSpace(next.WorkspaceName)
	if len(next.WorkspaceName) > 80 || strings.ContainsAny(next.WorkspaceName, "\r\n\t<>") {
		return fmt.Errorf("%w: a workspace name is plain text on one line, at most 80 characters", ErrSettings)
	}
	for _, name := range []string{next.TitleAgent, next.MemoryAgent} {
		if name == "" {
			continue
		}
		if _, err := p.Agents.Get(ctx, tenant, name, 0); err != nil {
			return fmt.Errorf("%w: %q", ErrNoBaseline, name)
		}
	}
	switch {
	case next.History != HistoryRecent && next.History != HistoryNone:
		return fmt.Errorf("%w: history is recent or none", ErrSettings)
	case next.HistoryLimit < MinHistory || next.HistoryLimit > MaxHistory:
		return fmt.Errorf("%w: the history limit is from %d to %d characters", ErrSettings, MinHistory, MaxHistory)
	}
	if err := checkModelPolicies(next.ModelPolicies); err != nil {
		return err
	}
	// A policy an agent still runs on cannot be removed, or that agent would
	// have no model at its next call.
	for _, old := range stored.ModelPolicies {
		if _, kept := policyIn(next.ModelPolicies, old.Name); kept {
			continue
		}
		if users := p.policyUsers(ctx, tenant, old.Name); len(users) > 0 {
			return fmt.Errorf("%w: %q is used by %s; move them to another policy first", ErrModelPolicyInUse, old.Name, strings.Join(users, ", "))
		}
	}
	p.settings.mu.Lock()
	defer p.settings.mu.Unlock()
	return p.settings.put(tenant, next)
}

// MarkSetup records who first set the workspace up. It does nothing once that
// is on record, so it is safe to call after every admin write.
func (p *Platform) MarkSetup(tenant, by string) (Settings, error) {
	p.settings.mu.Lock()
	defer p.settings.mu.Unlock()
	cfg := p.settings.by[tenant]
	if cfg.SetupAt.IsZero() {
		cfg.SetupAt, cfg.SetupBy = p.now(), by
		if err := p.settings.put(tenant, cfg); err != nil {
			return Settings{}, err
		}
	}
	return cfg.withDefaults(), nil
}

// Turn is one message a person sends.
type Turn struct {
	ID string
	// Agent is who should answer: an agent, Auto for @auto, or "" for the
	// default the conversation's place inherits.
	Agent string
	Text  string
	// After is the turn this one follows. Empty starts a conversation. Naming
	// a turn that already has a reply starts a branch from that point.
	After string
	// Project files a new conversation. A follow-up keeps its conversation's
	// project, whatever is passed here.
	Project string
	Actor   envelope.Actor
	// KeepsNotes says the person the turn is for may change the notes of the
	// project it is filed in (MayChangeNotes), so what the memory agent
	// learns from it may be kept without asking, when the workspace chose
	// that. Anyone else's turn teaches the project only proposals for its
	// owners. Only a caller that asked MayChangeNotes for that person sets it.
	KeepsNotes bool
}

// Converse sends a message into a conversation and starts the run that answers.
//
// Any agent available where the conversation is filed can answer any turn.
// The turn names it, or names Auto for @auto, or names none and the default
// the conversation's place inherits answers; with no default set it is
// ErrNoDefault, and nothing starts. The agent that answers is sent the branch
// so far, so a different agent picking up mid-conversation knows what was
// said, and ahead of it what the conversation's project chain and the agent
// itself bring (see contextOf).
func (p *Platform) Converse(ctx context.Context, tenant string, t Turn) (runs.Run, error) {
	cfg := p.Settings(tenant)
	meta := map[string]string{runs.MetaAsk: t.Text}
	body := t.Text
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
		if cfg.History == HistoryRecent {
			body = transcript(path, cfg.HistoryLimit) + "\n\nUser: " + t.Text
		}
	} else if project := projects.NameOf(t.Project); project != "" {
		if !p.HasProject(tenant, project) {
			return runs.Run{}, fmt.Errorf("%w: no project called %q", ErrSettings, project)
		}
		meta[runs.MetaProject] = project
	}

	if t.KeepsNotes && meta[runs.MetaProject] != "" {
		meta[metaKeepsNotes] = meta[runs.MetaProject]
	}

	who, err := p.resolveAgent(ctx, tenant, Place{Project: meta[runs.MetaProject]}, strings.TrimSpace(t.Agent), t.Text)
	if err != nil {
		return runs.Run{}, err
	}
	agent := who.Agent
	if who.Picked != "" {
		meta[runs.MetaPicked] = who.Picked
	}

	return p.Invoke(ctx, envelope.Tag{
		ID: t.ID, Tenant: tenant, Agent: agent, Origin: runs.OriginAPI,
		Text: p.contextOf(tenant, meta[runs.MetaProject], agent, body), Actor: t.Actor, Meta: meta,
	})
}

// stopMarker ends a stopped reply in the transcript. It is the runtime's own
// marker, so a conversation reads the same whichever of them replays it, and
// like the runtime's it says nothing about who stopped the reply or why.
const stopMarker = runtime.StopMarker

// transcript renders a branch for the next agent, newest turns kept.
func transcript(path []runs.Run, limit int) string {
	var turns []string
	size := 0
	for i := len(path) - 1; i >= 0; i-- {
		r := path[i]
		reply := r.Answer
		switch {
		case r.Status == runs.Failed:
			reply = "(this reply failed)"
		case r.Status == runs.Stopped && strings.TrimSpace(r.Answer) == "":
			reply = stopMarker
		case r.Status == runs.Stopped:
			// What the person read, and that it is not the whole answer.
			reply = strings.TrimRight(r.Answer, " \t\r\n") + "\n" + stopMarker
		case !r.Status.Terminal():
			reply = "(still working)"
		}
		turn := fmt.Sprintf("User: %s\n%s: %s", r.Text, r.Agent, reply)
		if size+len(turn) > limit && len(turns) > 0 {
			break
		}
		size += len(turn)
		turns = append(turns, turn)
	}
	var b strings.Builder
	b.WriteString("Conversation so far:\n")
	if len(turns) < len(path) {
		fmt.Fprintf(&b, "(%d earlier turns omitted)\n", len(path)-len(turns))
	}
	for i := len(turns) - 1; i >= 0; i-- {
		b.WriteString("\n")
		b.WriteString(turns[i])
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
