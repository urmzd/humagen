package platform

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/runtime"
)

// The entry agent is the one agent a workspace is talked to through. It owns
// each root session that names nobody (a console chat, a Slack thread, a pull
// request) and answers every message there in one run: it answers simple asks
// itself and brings specialists in with the agent tool, each call nested in
// its own run, so a person gets one answer that says who did what. Routing
// has no page and no run of its own: it is this agent, on its own model.

// BuiltinEntry is the entry agent's name unless the spec names another
// (branding.entry_agent).
const BuiltinEntry = "humagen"

const entryPrompt = `You are the workspace's entry agent: people talk to the workspace through you.

Answer simple asks yourself, briefly. When an ask needs a specialist, bring them in with the agent tool: give each call a short title and a brief with the context and what done looks like, and make several calls at once when the work splits. Before a call, say in one short sentence who you are bringing in and why. Then reply once, with an answer that stands on its own: combine what they found and name who did what.

Each message may come with agents that may fit it, best first, with their instructions. Prefer those; you may call any other agent listed. Never invent an agent, a tool or a result, and never claim work that did not finish. If nobody fits, say so and answer as well as you can.

A message that mentions several agents comes with the answer each of them already gave. Show each of those answers in full under a heading with its agent's name, exactly as it was given, then add a short synthesis only if it helps: where they agree or differ and what to do next.

When you need what people were discussing where you were asked (the Slack thread, the pull request), call get_context with what you need in words.`

// entrySpec is the entry agent's definition, on the same models as base, the
// first of the platform's other agents. Its spawn list is filled per
// workspace (entrySpawn), so it starts empty here.
//
// It is also the workspace's helper: it builds and changes agents, sets up
// connections and explains how the workspace is doing, with the tools the
// builder had and the analyst's reads. Saving an agent waits for a manager or
// an admin, and the person who asked may approve it when they hold one of
// those roles.
func (p *Platform) entrySpec(base wire.AgentSpec) wire.AgentSpec {
	return wire.AgentSpec{
		Name:         p.cfg.EntryAgent,
		Description:  "Answers for the workspace, calls the right specialists, and builds and changes agents with you",
		Provider:     base.Provider,
		Model:        base.Model,
		Fallbacks:    slices.Clone(base.Fallbacks),
		Effort:       "medium",
		SystemPrompt: entryPrompt + "\n\n" + helperPrompt + "\n\nTo explain how agents are doing or why something failed, read runs_summary and list_alerts first.",
		Tools:        append(append(BuilderTools(), AnalystTools()...), ToolGetContext),
		Approvals:    []string{ToolSaveAgent},
		Approvers:    []string{"role:admin", "role:manager"},
		SelfApproval: true,
	}
}

// entryBuiltin is the entry agent as the platform ships it: its spec, on
// base's models, in a package of its own whose version is its content's hash.
func (p *Platform) entryBuiltin(base wire.AgentSpec) wire.AgentSpec {
	spec := p.entrySpec(base)
	// Routing rests on this agent, so it falls back only to providers its
	// definition can name here: one this deployment does not know would stop
	// it pinning at all, and every message would fall back to search.
	if known, ok := p.Agents.(interface{ Knows(string) bool }); ok {
		spec.Fallbacks = slices.DeleteFunc(spec.Fallbacks, func(m wire.ModelRef) bool { return !known.Knows(m.Provider) })
	}
	raw, _ := json.Marshal([]wire.AgentSpec{spec})
	spec.Package = &wire.PackageBase{Name: "humagen/entry", Version: fmt.Sprintf("sha256:%x", sha256.Sum256(raw))}
	return spec
}

// EntryAgent is the workspace's entry agent: its name, and the name people
// see for it (the workspace's branding, else Humagen). The GitHub check run
// and the root session of every place are named for it.
func (p *Platform) EntryAgent(tenant string) (name, display string) {
	return p.cfg.EntryAgent, p.wording(tenant).Name
}

// isEntry reports whether a revision is the platform's own entry agent, not
// a custom agent that took its name.
func (p *Platform) isEntry(r wire.Revision) bool {
	return r.Spec.Name == p.cfg.EntryAgent && r.Spec.Package != nil && r.CreatedBy == "platform"
}

// entryTurn reports whether req is a turn of the platform's own entry agent.
func (p *Platform) entryTurn(ctx context.Context, req runtime.Request) bool {
	if req.Definition.Spec.Name != p.cfg.EntryAgent {
		return false
	}
	r, err := p.Catalog().Get(ctx, req.Tenant, p.cfg.EntryAgent, 0)
	return err == nil && p.isEntry(r)
}

// entryReady reports why the entry agent cannot run in tenant now, or nil:
// its models name no provider the workspace has enabled, or a model policy
// it no longer has. It is what a turn of it would find before its first
// model call (resolveModel), asked without starting one.
func (p *Platform) entryReady(ctx context.Context, tenant string) error {
	r, err := p.Catalog().Get(ctx, tenant, p.cfg.EntryAgent, 0)
	if err != nil {
		return err
	}
	def, err := wire.EvaluationDefinition(r.Spec)
	if err != nil {
		return err
	}
	_, err = p.resolveModel(runtime.Request{Tenant: tenant, Definition: def})
	return err
}

// entryFor is the entry agent's name when it answers in project for whoever
// sees passes, or "" when it does not exist there (no built-in agents on
// this server), is not available in the project, or is hidden from them.
func (p *Platform) entryFor(ctx context.Context, tenant, project string, sees func(string) bool) string {
	if err := p.EnsureBuiltins(ctx, tenant); err != nil {
		p.log.Warn("platform: the entry agent could not be brought up to date", "tenant", tenant, "error", err)
	}
	r, err := p.Catalog().Get(ctx, tenant, p.cfg.EntryAgent, 0)
	if err != nil || !p.isEntry(r) || !p.Projects.Tree(tenant).IsAvailable(projects.NameOf(project), r.Spec.Name) || (sees != nil && !sees(r.Spec.Name)) {
		return ""
	}
	return r.Spec.Name
}

// entrySpawns remembers, per workspace, the agents the entry agent may call
// and what they were worked out from, so they are checked again only when an
// agent is added, revised or retired.
type entrySpawns struct {
	mu   sync.Mutex
	seen map[string]entrySpawn
}

type entrySpawn struct {
	from  [32]byte
	names []string
}

// entrySpawn is the agents the entry agent may call in tenant: every other
// agent whose definition pins. A run pins its whole call tree, so one agent
// that no longer pins (a provider taken away) would otherwise stop the entry
// agent answering at all. Which of them it may call in a given place is
// held to that place when it calls (Platform.call).
func (p *Platform) entrySpawn(ctx context.Context, tenant string) ([]string, error) {
	type head struct {
		name string
		rev  int
	}
	var heads []head
	page := wire.Page{Size: 100}
	for {
		rows, next, err := p.Catalog().List(ctx, tenant, page)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if r.Spec.Name == p.cfg.EntryAgent || legacy.IsBuiltinAgent(r.Spec.Name) {
				continue
			}
			heads = append(heads, head{r.Spec.Name, r.Rev})
		}
		if next == "" {
			break
		}
		page.Token = next
	}
	h := sha256.New()
	for _, x := range heads {
		fmt.Fprintf(h, "%s@%d\n", x.name, x.rev)
	}
	var from [32]byte
	copy(from[:], h.Sum(nil))
	p.entry.mu.Lock()
	if got, ok := p.entry.seen[tenant]; ok && got.from == from {
		p.entry.mu.Unlock()
		return slices.Clone(got.names), nil
	}
	p.entry.mu.Unlock()
	resolver := p.Agents.Resolver()
	names := make([]string, 0, len(heads))
	for _, x := range heads {
		if _, err := resolver.Resolve(ctx, tenant, x.name); err != nil {
			continue
		}
		names = append(names, x.name)
	}
	slices.Sort(names)
	p.entry.mu.Lock()
	if p.entry.seen == nil {
		p.entry.seen = map[string]entrySpawn{}
	}
	p.entry.seen[tenant] = entrySpawn{from: from, names: names}
	p.entry.mu.Unlock()
	return slices.Clone(names), nil
}

// withEntrySpawn fills the entry agent's spawn list for tenant, leaving every
// other spec as it is.
func (p *Platform) withEntrySpawn(ctx context.Context, tenant string, want wire.AgentSpec) (wire.AgentSpec, error) {
	if want.Name != p.cfg.EntryAgent {
		return want, nil
	}
	names, err := p.entrySpawn(ctx, tenant)
	if err != nil {
		return want, err
	}
	want.Access.Spawn = names
	return want, nil
}

// entryMayCall holds the entry agent to the place it answers in: its spawn
// list spans the workspace, and an agent not available in the run's project
// is refused there as it would be to a person. A run answering a message a
// client sent (Slack, GitHub) calls only the agents that client may route
// to, as the client could not have asked any other.
func (p *Platform) entryMayCall(tenant string, parent wire.Revision, run runs.Run, agent string) error {
	if !p.isEntry(parent) {
		return nil
	}
	if !p.Projects.Tree(tenant).IsAvailable(projects.NameOf(run.Project), agent) {
		return fmt.Errorf("%w: @%s is not available where this conversation is", ErrForbidden, agent)
	}
	if run.Client != "" {
		if c, err := p.Clients.Get(tenant, run.Client); err != nil || !c.Routes(agent) {
			return fmt.Errorf("%w: the app this conversation came through may not reach @%s", ErrForbidden, agent)
		}
	}
	return nil
}

// Bounds on what the entry agent is told about a message's agents.
const (
	// maxHintPrompt bounds how much of each matching agent's instructions the
	// entry agent is sent.
	maxHintPrompt = 1500
	// maxHintNames bounds how many other agents are named to it.
	maxHintNames = 60
	// maxHintDescription bounds what it is told each other agent is for.
	maxHintDescription = 200
)

// entryHints is what the entry agent is told about who may fit a message:
// the best few agents search finds (their names, why they matched, and their
// instructions, loaded only for these), then the names of the others it may
// call. Agents the poster may not read are left out. It is "" when there is
// nobody else.
func (p *Platform) entryHints(ctx context.Context, tenant, project, message string, sees func(string) bool) string {
	keep := func(a string) bool { return a != p.cfg.EntryAgent && (sees == nil || sees(a)) }
	cs, err := p.candidates(ctx, tenant, p.Projects.Tree(tenant), projects.NameOf(project), keep)
	if err != nil || len(cs) == 0 {
		return ""
	}
	return p.hintsFrom(join.Search(cs, message, join.Shortlist), cs)
}

func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n] + "…"
}

// entryHintsFor is entryHints when agent is the platform's entry agent, and
// "" for every other agent.
func (p *Platform) entryHintsFor(ctx context.Context, tenant, project, agent, message string, sees func(string) bool) string {
	if agent != p.cfg.EntryAgent {
		return ""
	}
	if r, err := p.Catalog().Get(ctx, tenant, agent, 0); err != nil || !p.isEntry(r) {
		return ""
	}
	return p.entryHints(ctx, tenant, project, message, sees)
}

// Pick is why a message's answer came from where it did, when the platform
// chose: the agents the message mentioned, or the search match that answered
// because the entry agent could not run. It is shown in the answer's activity,
// never as a line of the conversation.
type Pick struct {
	Decision string   `json:"decision"`
	Agents   []string `json:"agents"`
	Reason   string   `json:"reason"`
	Matched  []string `json:"matched"`
}

// What a pick came to.
const (
	PickAnswerMyself = "answer_myself"
	PickDelegate     = "delegate"
)

// maxDelegates bounds how many mentioned agents one message has the platform
// call before its answer.
const maxDelegates = 3

// entryRoot reports whether th is a root session the platform's entry agent
// owns and agent is that agent: a message to it is answered by the entry
// agent's one run.
func (p *Platform) entryRoot(ctx context.Context, tenant string, th threads.Thread, agent string) bool {
	if agent != p.cfg.EntryAgent || th.Session == nil || th.Session.Agent != agent || strings.HasPrefix(th.Session.Parent, "s_") {
		return false
	}
	r, err := p.Catalog().Get(ctx, tenant, agent, 0)
	return err == nil && p.isEntry(r)
}

// answerAsEntry answers a message in a root session the entry agent owns.
// The entry agent answers it in one run, the response's root: it is told who
// may fit the message (entryHints), answers itself or brings those agents in
// with the agent tool, and nothing picks for it first. When it cannot run
// (no provider of its models is enabled, or it is refused where the message
// is), a strong search match answers as the root in its place, then the
// workspace's named default; a weak match is never handed the message, and
// with neither the message is refused with why the entry agent could not
// run. A run that starts and then fails is a failed answer, not handed on.
func (p *Platform) answerAsEntry(ctx context.Context, tenant string, th threads.Thread, m threads.Message, via string, actor event.Actor, tagID string, keeps bool, sees func(string) bool) ([]runs.Run, error) {
	entry := p.cfg.EntryAgent
	// A run that cannot reach a model fails at its first turn, after the
	// message was told it has an answer coming, so that is asked first.
	err := p.entryReady(ctx, tenant)
	if err == nil {
		var started []runs.Run
		started, err = p.answerDirect(ctx, tenant, th, m, via, entry, nil, "", actor, tagID, keeps, sees)
		if err == nil {
			return started, nil
		}
	}
	if ctx.Err() != nil || len(mentionedOf(ctx)) > 0 {
		// Mentioned agents answer themselves when the entry agent cannot
		// (carryOut).
		return nil, err
	}
	tree := p.Projects.Tree(tenant)
	keep := func(a string) bool {
		return a != entry && (sees == nil || sees(a)) && p.mayRoute(tenant, th, via, a) == nil
	}
	cs, cerr := p.candidates(ctx, tenant, tree, th.Project, keep)
	if cerr != nil {
		return nil, err
	}
	pick := p.fallbackPick(tree, th.Project, join.Search(cs, m.Text, join.Shortlist), sees)
	if pick.Decision != PickDelegate {
		return nil, err
	}
	p.log.Info("platform: the entry agent cannot run; a search match answers", "thread", th.ID, "agent", pick.Agents[0], "error", err)
	agent := pick.Agents[0]
	started, ferr := p.answerDirect(ctx, tenant, th, m, via, agent, nil, "", actor, answerTagID(th.ID, m.ID, agent), keeps, sees)
	if ferr != nil || len(started) == 0 {
		return nil, err
	}
	p.keepPick(tenant, th.ID, m.ID, pickRecord{Pick: pick, Run: started[0].ID, Fallback: true})
	return started, nil
}

// fallbackPick is who answers when the entry agent cannot run: the best
// search match when it is strong, else the workspace's named default, else
// nobody. A weak match (one passing word of a prompt) or merely the first
// agent available is never handed a message.
func (p *Platform) fallbackPick(tree *projects.Tree, project string, found []join.Match, sees func(string) bool) Pick {
	switch {
	case len(found) > 0 && found[0].Strong:
		return Pick{Decision: PickDelegate, Agents: []string{found[0].Name}, Reason: "Picked by search: matched " + strings.Join(found[0].Terms, ", ") + ".", Matched: slices.Clone(found[0].Terms)}
	case func() bool {
		d := tree.Root().Agents.Default
		return d != "" && d != Open && d != p.cfg.EntryAgent && (sees == nil || sees(d)) && tree.IsAvailable(projects.NameOf(project), d)
	}():
		return Pick{Decision: PickDelegate, Agents: []string{tree.Root().Agents.Default}, Reason: "No agent matched it, so the workspace default answered."}
	}
	return Pick{Decision: PickAnswerMyself}
}

// hintsFrom is the agents block the entry agent is given with a message: the
// matches with their instructions, then the other agents by name.
func (p *Platform) hintsFrom(found []join.Match, cs []join.Candidate) string {
	strong := slices.DeleteFunc(slices.Clone(found), func(m join.Match) bool { return !m.Strong })
	var b strings.Builder
	b.WriteString("<agents>\n")
	if len(strong) > 0 {
		b.WriteString("Agents that may fit this message, best first:\n")
		for _, m := range strong {
			fmt.Fprintf(&b, "\n@%s (matched %s)\n<instructions>\n%s\n</instructions>\n", m.Name, strings.Join(m.Terms, ", "), clipText(m.Prompt, maxHintPrompt))
		}
	} else {
		b.WriteString("No agent's name or purpose matches this message's words; choose by what each agent is for.\n")
	}
	// Every other agent with what it is for, so the entry agent can choose
	// by meaning where words did not match.
	var others []string
	for _, c := range cs {
		if !slices.ContainsFunc(strong, func(m join.Match) bool { return m.Name == c.Name }) {
			line := "@" + c.Name
			if d := join.Describe(c); d != "" {
				line += ": " + clipText(d, maxHintDescription)
			}
			others = append(others, line)
		}
	}
	if len(others) > maxHintNames {
		others = append(others[:maxHintNames], fmt.Sprintf("and %d more", len(others)-maxHintNames))
	}
	if len(others) > 0 {
		fmt.Fprintf(&b, "\nOther agents you may call:\n%s\n", strings.Join(others, "\n"))
	}
	b.WriteString("</agents>")
	return b.String()
}

// pickRecord is why a message's answer came from where it did, kept so the
// answer's activity says it, after a restart as before. Run is the answer's
// root run. Children are the child sessions a message was handed to before
// answers were one run, kept so those conversations still trace.
type pickRecord struct {
	Pick     Pick          `json:"pick"`
	Run      string        `json:"run,omitempty"`
	Fallback bool          `json:"fallback,omitempty"`
	Children []pickedChild `json:"children,omitempty"`
	At       time.Time     `json:"at"`
}

// pickedChild is one agent an earlier pick handed a message to: its child
// session and the run that answered there.
type pickedChild struct {
	Agent   string `json:"agent"`
	Session string `json:"session"`
	Run     string `json:"run,omitempty"`
}

func pickKey(root, message string) string { return "p:" + root + ":" + message }

// pickStore keeps pick records on the shared store, or in memory without one.
func pickStore(st state.Store) *state.Table[pickRecord] {
	if st == nil {
		st = state.NewMemory()
	}
	return state.NewTable[pickRecord](st, "entry-picks")
}

// keepPick records why a message's answer came from where it did. It is for
// showing, so a failure is logged and never fails the answer.
func (p *Platform) keepPick(tenant, root, message string, rec pickRecord) {
	rec.At = p.now()
	if err := p.picks.Put(tenant, pickKey(root, message), rec); err != nil {
		p.log.Warn("platform: why an answer was picked could not be kept", "thread", root, "error", err)
	}
}
