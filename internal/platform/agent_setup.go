package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/connectors/cron"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/registry"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
)

const maxAgentSetups = 200

type SetupAssignment struct {
	Goal     string `json:"goal"`
	DoneWhen string `json:"done_when"`
	Cron     string `json:"cron,omitempty"`
	Trigger  string `json:"trigger,omitempty"`
	Source   string `json:"source,omitempty"`
}
type AgentSetupPlan struct {
	Spec       agentspec.AgentSpec                      `json:"spec"`
	Project    string                                   `json:"project"`
	Manager    string                                   `json:"manager,omitempty"`
	StopRules  string                                   `json:"stop_rules,omitempty"`
	Rules      map[operations.Category]operations.Level `json:"rules,omitempty"`
	DailySpend int64                                    `json:"daily_spend,omitempty"`
	MaxLease   int64                                    `json:"max_lease,omitempty"`
	Assignment *SetupAssignment                         `json:"assignment,omitempty"`
}
type AgentSetup struct {
	Name         string         `json:"name"`
	Owner        string         `json:"owner"`
	Plan         AgentSetupPlan `json:"plan"`
	Phase        string         `json:"phase"`
	Version      int            `json:"version"`
	Revision     int            `json:"revision,omitempty"`
	RevisionHash string         `json:"revision_hash,omitempty"`
	Problem      string         `json:"problem,omitempty"`
	TestRun      string         `json:"test_run,omitempty"`
	AssignmentID string         `json:"assignment_id,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	TestTag      string         `json:"test_tag,omitempty"`
}

type agentSetups struct {
	mu      sync.RWMutex
	records map[string]map[string]AgentSetup
	kept    *state.Table[AgentSetup]
	locks   sync.Map
}

func openAgentSetups(st state.Store) (*agentSetups, error) {
	s := &agentSetups{records: map[string]map[string]AgentSetup{}, kept: state.NewTable[AgentSetup](st, "agent-setups")}
	err := s.kept.Load(func(tenant, name string, record AgentSetup) error {
		if record.Name != name || record.Owner == "" || record.Version < 1 {
			return operations.ErrInvalid
		}
		if s.records[tenant] == nil {
			s.records[tenant] = map[string]AgentSetup{}
		}
		if len(s.records[tenant]) >= maxAgentSetups {
			return operations.ErrInvalid
		}
		s.records[tenant][name] = record
		return nil
	})
	return s, err
}
func (s *agentSetups) get(tenant, name string) (AgentSetup, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.records[tenant][name]
	raw, _ := json.Marshal(record)
	var copy AgentSetup
	_ = json.Unmarshal(raw, &copy)
	return copy, ok
}
func (s *agentSetups) put(tenant string, record AgentSetup) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := s.records[tenant]
	if records == nil {
		records = map[string]AgentSetup{}
	}
	if _, exists := records[record.Name]; !exists && len(records) >= maxAgentSetups {
		return operations.ErrInvalid
	}
	if err := s.kept.Put(tenant, record.Name, record); err != nil {
		return err
	}
	records[record.Name] = record
	s.records[tenant] = records
	return nil
}
func (s *agentSetups) lock(tenant, name string) *sync.Mutex {
	v, _ := s.locks.LoadOrStore(tenant+"\x00"+name, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// CreateAgentBy serializes public creation with wizard reservations. A failed
// setup never attaches its admission gate to an unrelated same-name agent.
func (p *Platform) CreateAgentBy(ctx context.Context, who auth.Principal, spec agentspec.AgentSpec) (agentspec.Revision, error) {
	spec, err := p.checkSetupSpec(ctx, who, spec)
	if err != nil {
		return agentspec.Revision{}, err
	}
	lock := p.setups.lock(who.Tenant, spec.Name)
	lock.Lock()
	defer lock.Unlock()
	p.admission.Lock()
	defer p.admission.Unlock()
	if record, ok := p.setups.get(who.Tenant, spec.Name); ok && record.Phase != "ready" {
		return agentspec.Revision{}, operations.ErrConflict
	}
	return p.Agents.Create(ctx, who.Tenant, spec, who.Subject)
}
func (p *Platform) checkSetupSpec(ctx context.Context, who auth.Principal, spec agentspec.AgentSpec) (agentspec.AgentSpec, error) {
	spec.Name = agentspec.NormalizeName(spec.Name)
	if !who.Can(auth.AgentsWrite) {
		return agentspec.AgentSpec{}, auth.ErrForbidden
	}
	if IsBuiltin(spec.Name) {
		return agentspec.AgentSpec{}, auth.ErrForbidden
	}
	if err := p.CheckAgentName(who.Tenant, spec.Name); err != nil {
		return agentspec.AgentSpec{}, err
	}
	if err := p.CheckAgentModel(who.Tenant, spec); err != nil {
		return agentspec.AgentSpec{}, err
	}
	return p.SpecWrittenBy(ctx, who, nil, spec)
}
func (p *Platform) AgentSetup(who auth.Principal, name string) (AgentSetup, error) {
	record, ok := p.setups.get(who.Tenant, name)
	if !ok || who.Kind != auth.User || record.Owner != who.Subject || !p.Seer(who).Project(record.Plan.Project) {
		return AgentSetup{}, operations.ErrNotFound
	}

	seer := p.Seer(who)
	if _, err := p.Agents.Get(context.Background(), who.Tenant, name, 0); err == nil && !seer.Agent(name) {
		return AgentSetup{}, operations.ErrNotFound
	}
	shown := seer.Revision(agentspec.Revision{Spec: record.Plan.Spec, Rev: record.Revision, Hash: record.RevisionHash})
	record.Plan.Spec = shown.Spec
	record.RevisionHash = shown.Hash
	if record.Plan.Assignment != nil && record.Plan.Assignment.Source != "" && !p.SourceVisible(who, record.Plan.Assignment.Source) {
		record.Plan.Assignment = nil
		record.AssignmentID = ""
	}
	if !who.Can(auth.RunsRead) {
		record.TestRun = ""
	} else if record.TestRun != "" {
		if run, err := p.Runs.Get(who.Tenant, record.TestRun); err == nil && !seer.Run(run) {
			record.TestRun = ""
		}
	}
	record.TestTag = ""
	return record, nil
}
func (p *Platform) saveSetup(tenant string, record *AgentSetup, phase string) error {
	record.Phase = phase
	record.Problem = ""
	record.Version++
	record.UpdatedAt = p.now()
	return p.setups.put(tenant, *record)
}
func (p *Platform) failSetup(tenant string, record AgentSetup, cause error) (AgentSetup, error) {
	p.log.Warn("platform: agent setup phase incomplete", "agent", record.Name, "phase", record.Phase, "error", cause)
	record.Problem = "Setup stopped after the saved " + record.Phase + " phase. Resume Setup to retry the next step."
	record.Version++
	record.UpdatedAt = p.now()
	if err := p.setups.put(tenant, record); err != nil {
		return AgentSetup{}, err
	}
	record.TestTag = ""
	return record, nil
}
func (p *Platform) CreateAgentSetup(ctx context.Context, who auth.Principal, plan AgentSetupPlan) (AgentSetup, error) {
	if who.Kind != auth.User {
		return AgentSetup{}, auth.ErrForbidden
	}
	spec, err := p.checkSetupSpec(ctx, who, plan.Spec)
	if err != nil {
		return AgentSetup{}, err
	}
	canonical, err := agentspec.NewRevisionWith(spec, 1, who.Subject, p.now(), p.agentProviders())
	if err != nil {
		return AgentSetup{}, err
	}
	spec = canonical.Spec
	plan.Spec = spec
	plan.Project = projects.NameOf(plan.Project)
	if _, _, err := p.ProjectRole(who, plan.Project, projects.Owner); err != nil {
		return AgentSetup{}, err
	}
	if plan.Spec.Provider == "offline" || plan.DailySpend < 0 || plan.MaxLease < 0 || len(plan.StopRules) > 4000 || len(plan.Manager) > 200 {
		return AgentSetup{}, operations.ErrInvalid
	}
	if (plan.DailySpend > 0 || plan.MaxLease > 0) && !who.Can(auth.BillingWrite) {
		return AgentSetup{}, auth.ErrForbidden
	}

	// Validate choices before reserving a name. Later failures can then be
	// resumed after an unavailable store or source is repaired.
	scratch, _ := operations.Open(nil)
	if _, err := scratch.SetPolicy(who.Tenant, operations.Policy{Scope: operations.Scope{Kind: "agent", Name: spec.Name}, Rules: plan.Rules}, 0, who.Subject, p.now()); err != nil {
		return AgentSetup{}, err
	}
	if plan.Manager != "" {
		if _, err := p.People.Get(who.Tenant, plan.Manager); err != nil {
			return AgentSetup{}, operations.ErrInvalid
		}
	}
	if plan.DailySpend > 0 || plan.MaxLease > 0 {
		if err := (policy.Policy{Scope: policy.Scope{Kind: policy.Agent, Name: spec.Name}, Period: policy.Day, SpendLimit: plan.DailySpend, MaxLease: plan.MaxLease, Action: policy.Block}).Validate(); err != nil {
			return AgentSetup{}, err
		}
	}
	if a := plan.Assignment; a != nil {
		if _, err := scratch.SetAssignment(who.Tenant, operations.Assignment{ID: "validation", Agent: spec.Name, Owner: who.Subject, Project: plan.Project, Goal: a.Goal, DoneWhen: a.DoneWhen, Cron: a.Cron, Trigger: a.Trigger, Source: a.Source, State: operations.Requested}, 0, who.Subject, p.now()); err != nil {
			return AgentSetup{}, err
		}
		if a.Cron != "" {
			if err := (cron.Schedule{Name: "validation", Tenant: who.Tenant, Agent: spec.Name, Expression: a.Cron, Text: a.Goal}).Validate(); err != nil {
				return AgentSetup{}, err
			}
		}
		if a.Source != "" && !p.SourceVisible(who, a.Source) {
			return AgentSetup{}, operations.ErrNotFound
		}
	}
	lock := p.setups.lock(who.Tenant, spec.Name)
	lock.Lock()
	defer lock.Unlock()
	if old, ok := p.setups.get(who.Tenant, spec.Name); ok {
		if old.Owner != who.Subject || !reflect.DeepEqual(old.Plan, plan) {
			return AgentSetup{}, operations.ErrConflict
		}
		return p.resumeAgentSetup(ctx, who, old)
	}
	p.admission.Lock()
	if _, err := p.Agents.Get(ctx, who.Tenant, spec.Name, 0); err == nil {
		p.admission.Unlock()
		return AgentSetup{}, registry.ErrConflict
	} else if !errors.Is(err, registry.ErrNotFound) {
		p.admission.Unlock()
		return AgentSetup{}, err
	}
	record := AgentSetup{Name: spec.Name, Owner: who.Subject, Plan: plan, Phase: "reserved", Version: 1, CreatedAt: p.now(), UpdatedAt: p.now()}
	err = p.setups.put(who.Tenant, record)
	p.admission.Unlock()
	if err != nil {
		return AgentSetup{}, err
	}
	return p.resumeAgentSetup(ctx, who, record)
}
func (p *Platform) ResumeAgentSetup(ctx context.Context, who auth.Principal, name string) (AgentSetup, error) {
	_, err := p.AgentSetup(who, name)
	if err != nil {
		return AgentSetup{}, err
	}
	lock := p.setups.lock(who.Tenant, name)
	lock.Lock()
	defer lock.Unlock()
	record, _ := p.setups.get(who.Tenant, name)
	return p.resumeAgentSetup(ctx, who, record)
}
func (p *Platform) resumeAgentSetup(ctx context.Context, who auth.Principal, record AgentSetup) (AgentSetup, error) {
	if !who.Can(auth.AgentsWrite) {
		return AgentSetup{}, auth.ErrForbidden
	}
	if _, _, err := p.ProjectRole(who, record.Plan.Project, projects.Owner); err != nil {
		return AgentSetup{}, err
	}
	name := record.Name
	step := func(next string, fn func() error) error {
		if err := fn(); err != nil {
			return err
		}
		return p.saveSetup(who.Tenant, &record, next)
	}
	var err error
	if record.Phase == "reserved" {
		err = step("created", func() error {
			p.admission.Lock()
			defer p.admission.Unlock()
			current := p.Operations.Control(who.Tenant, name)
			if !current.Paused {
				if _, err := p.Operations.SetControl(who.Tenant, name, operations.Control{Paused: true}, current.Version, who.Subject, p.now()); err != nil {
					return err
				}
			}
			rev, e := p.Agents.Create(ctx, who.Tenant, record.Plan.Spec, who.Subject)
			if e != nil {
				existing, getErr := p.Agents.Get(ctx, who.Tenant, name, 0)
				if getErr != nil || existing.CreatedBy != who.Subject || !reflect.DeepEqual(existing.Spec, record.Plan.Spec) {
					return e
				}
				rev = existing
			}
			record.Revision = rev.Rev
			record.RevisionHash = rev.Hash
			return nil
		})
	}
	if err == nil && record.Phase == "created" {
		err = step("home", func() error { return p.setAgentHome(ctx, who.Tenant, name, record.Plan.Project, WriterOf(who)) })
	}
	if err == nil && record.Phase == "home" {
		err = step("controls", func() error {
			current := p.AgentControl(who.Tenant, name)
			_, err := p.SetAgentControl(who, name, operations.Control{Paused: true, Manager: record.Plan.Manager, StopRules: record.Plan.StopRules}, current.Version)
			return err
		})
	}
	if err == nil && record.Phase == "controls" {
		err = step("policy", func() error {
			scope := operations.Scope{Kind: "agent", Name: name}
			current := p.Operations.Policy(who.Tenant, scope)
			_, err := p.SetActionPolicy(who, scope, record.Plan.Rules, current.Version)
			return err
		})
	}
	if err == nil && record.Phase == "policy" {
		err = step("budget", func() error {
			if record.Plan.DailySpend == 0 && record.Plan.MaxLease == 0 {
				return nil
			}
			if !who.Can(auth.BillingWrite) {
				return auth.ErrForbidden
			}
			_, err := p.Policies.Put(who.Tenant, policy.Policy{Scope: policy.Scope{Kind: policy.Agent, Name: name}, Period: policy.Day, SpendLimit: record.Plan.DailySpend, MaxLease: record.Plan.MaxLease, Action: policy.Block}, who.Subject, p.now())
			return err
		})
	}
	if err == nil && record.Phase == "budget" {
		err = step("assignment", func() error {
			plan := record.Plan.Assignment
			if plan == nil {
				return nil
			}
			digest := sha256.Sum256([]byte(who.Tenant + "\x00" + name + "\x00" + record.Owner))
			record.AssignmentID = "as-" + hex.EncodeToString(digest[:12])
			if existing, ok := p.Operations.Assignment(who.Tenant, record.AssignmentID); ok {
				if existing.Agent != name || existing.Owner != who.Subject {
					return operations.ErrConflict
				}
				return nil
			}
			_, err := p.SetAssignment(ctx, who, operations.Assignment{ID: record.AssignmentID, Agent: name, Project: record.Plan.Project, Owner: who.Subject, Goal: plan.Goal, DoneWhen: plan.DoneWhen, Cron: plan.Cron, Trigger: plan.Trigger, Source: plan.Source, State: operations.Requested}, 0)
			return err
		})
	}
	if err == nil && record.Phase == "assignment" {
		err = p.saveSetup(who.Tenant, &record, "configured")
	}
	if err != nil {
		return p.failSetup(who.Tenant, record, err)
	}
	record.TestTag = ""
	return record, nil
}

type setupGate struct{ p *Platform }

func (g setupGate) Ready(tenant, name string) bool {
	record, ok := g.p.setups.get(tenant, name)
	if !ok || record.Phase == "ready" || record.Phase == "activating" {
		return true
	}
	rev, err := g.p.Agents.Get(context.Background(), tenant, name, 0)
	if err != nil {
		return false
	}
	// Reservations cannot pause an unrelated agent that won a trusted registry
	// race. Public creation also serializes against the reservation itself.
	if record.Revision == 0 {
		return rev.CreatedBy != record.Owner || !reflect.DeepEqual(rev.Spec, record.Plan.Spec)
	}
	first, err := g.p.Agents.Get(context.Background(), tenant, name, record.Revision)
	if err != nil {
		return false
	}
	return first.CreatedBy != record.Owner || first.Hash != record.RevisionHash
}
func (g setupGate) AllowTag(tag envelope.Tag) bool {
	record, ok := g.p.setups.get(tag.Tenant, tag.Agent)
	if !ok || record.TestTag == "" || record.TestTag != tag.ID || record.Owner != tag.Actor.User || tag.Actor.Bot || tag.Origin != runs.OriginAPI || record.Phase != "configured" {
		return false
	}
	rev, err := g.p.Agents.Get(context.Background(), tag.Tenant, tag.Agent, 0)
	return err == nil && rev.Rev == record.Revision && rev.Hash == record.RevisionHash
}
func (g setupGate) AllowRequest(req runtime.Request) bool {
	record, ok := g.p.setups.get(req.Tenant, req.Agent())
	if !ok || record.TestTag == "" || req.RunID != runtime.RunID(req.Tenant, record.TestTag) || record.Phase != "configured" {
		return false
	}
	if req.Revision.Rev != record.Revision || req.Revision.Hash != record.RevisionHash {
		return false
	}
	run, err := g.p.Runs.Get(req.Tenant, req.RunID)
	return err == nil && run.Actor.User == record.Owner && !run.Actor.Bot
}
func (p *Platform) TestAgentSetup(ctx context.Context, who auth.Principal, name, text string) (AgentSetup, error) {
	record, err := p.AgentSetup(who, name)
	if err != nil {
		return AgentSetup{}, err
	}
	if !who.Can(auth.RunsWrite) || !who.Can(auth.RunsRead) || !who.Can(auth.AgentsWrite) {
		return AgentSetup{}, auth.ErrForbidden
	}
	lock := p.setups.lock(who.Tenant, name)
	lock.Lock()
	defer lock.Unlock()
	record, _ = p.setups.get(who.Tenant, name)
	if record.Phase != "configured" || len(text) == 0 || len(text) > 8000 {
		return AgentSetup{}, operations.ErrInvalid
	}
	if record.TestRun != "" {
		if run, err := p.Runs.Get(who.Tenant, record.TestRun); err == nil && !run.Status.Terminal() {
			record.TestTag = ""
			return record, nil
		}
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return AgentSetup{}, err
	}
	record.TestTag = "setup-" + hex.EncodeToString(raw)
	record.TestRun = runtime.RunID(who.Tenant, record.TestTag)
	if err := p.saveSetup(who.Tenant, &record, "configured"); err != nil {
		return AgentSetup{}, err
	}
	_, err = p.Converse(ctx, who.Tenant, Turn{ID: record.TestTag, Agent: name, Project: record.Plan.Project, Text: text, Actor: envelope.Actor{ID: who.Subject, User: who.Subject, Display: p.NameOf(who.Tenant, who.Subject)}, ForceAsk: true, Sees: p.Seer(who).Agent})
	if err != nil {
		return p.failSetup(who.Tenant, record, err)
	}
	record.TestTag = ""
	return record, nil
}
func (p *Platform) ActivateAgentSetup(ctx context.Context, who auth.Principal, name string) (AgentSetup, error) {
	record, err := p.AgentSetup(who, name)
	if err != nil {
		return AgentSetup{}, err
	}
	if !who.Can(auth.AgentsWrite) || !who.Can(auth.RunsWrite) || !who.Can(auth.RunsRead) {
		return AgentSetup{}, auth.ErrForbidden
	}
	lock := p.setups.lock(who.Tenant, name)
	lock.Lock()
	defer lock.Unlock()
	record, _ = p.setups.get(who.Tenant, name)
	if record.Phase == "ready" {
		record.TestTag = ""
		return record, nil
	}
	if record.Phase != "configured" && record.Phase != "activating" {
		return AgentSetup{}, operations.ErrInvalid
	}
	run, err := p.Runs.Get(who.Tenant, record.TestRun)
	if err != nil || run.Status != runs.Completed {
		return AgentSetup{}, fmt.Errorf("%w: a completed real dry run is required", operations.ErrInvalid)
	}
	rev, err := p.Agents.Get(ctx, who.Tenant, name, 0)
	if err != nil || rev.Rev != record.Revision || rev.Hash != record.RevisionHash {
		return AgentSetup{}, operations.ErrConflict
	}
	if _, _, err := p.ProjectRole(who, record.Plan.Project, projects.Owner); err != nil {
		return AgentSetup{}, err
	}
	if err := p.saveSetup(who.Tenant, &record, "activating"); err != nil {
		return AgentSetup{}, err
	}
	current := p.AgentControl(who.Tenant, name)
	current.Paused = false
	if _, err := p.SetAgentControl(who, name, current, current.Version); err != nil {
		return p.failSetup(who.Tenant, record, err)
	}
	if record.AssignmentID != "" {
		a, err := p.Assignment(who, record.AssignmentID)
		if err != nil {
			return p.failSetup(who.Tenant, record, err)
		}
		if a.State == operations.Requested {
			a.State = operations.Accepted
			if _, err := p.SetAssignment(ctx, who, a, a.Version); err != nil {
				return p.failSetup(who.Tenant, record, err)
			}
		}
	}
	if err := p.saveSetup(who.Tenant, &record, "ready"); err != nil {
		return AgentSetup{}, err
	}
	record.TestTag = ""
	return record, nil
}

// lockSetupRevision makes public revision writes share the finalization boundary.
// Pending wizard plans change through setup review, never the active editor.
func (p *Platform) lockSetupRevision(tenant, name string) (func(), error) {
	if p.setups == nil {
		return func() {}, nil
	}
	lock := p.setups.lock(tenant, agentspec.NormalizeName(name))
	lock.Lock()
	if record, ok := p.setups.get(tenant, agentspec.NormalizeName(name)); ok && record.Phase != "ready" {
		lock.Unlock()
		return nil, operations.ErrConflict
	}
	return lock.Unlock, nil
}
