package platform

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/session"
)

// The entry agent is the one agent a workspace is talked to through. It owns
// each root session that names nobody (a console chat now; a Slack thread or
// a pull request next), answers simple asks itself and calls specialists as
// child sessions, so a person gets one answer that says who did what.
// Routing has no page of its own: it is this agent, on its own model.

// BuiltinEntry is the entry agent's name unless the spec names another
// (branding.entry_agent).
const BuiltinEntry = "humagen"

const entryPrompt = `You are the workspace's entry agent: people talk to the workspace through you.

Answer simple asks yourself, briefly. When an ask needs a specialist, call them with session_call: give each one the context and what done looks like, and call several at once when the work splits. Wait for their answers (session_listen), then reply once, combining what they found and naming who did what. Continue a specialist's session for a follow-up instead of starting a new one.

Each message may come with agents that may fit it, best first, with their instructions. Prefer those; you may call any other agent listed. Never invent an agent, a tool or a result, and never claim work that did not finish. If nobody fits, say so and answer as well as you can.

When you are asked to decide who answers a message, do only that: reply with the JSON asked for and nothing else, and call no tools.`

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
		Tools:        append(BuilderTools(), AnalystTools()...),
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
// is refused there as it would be to a person.
func (p *Platform) entryMayCall(tenant string, parent wire.Revision, project, agent string) error {
	if !p.isEntry(parent) || p.Projects.Tree(tenant).IsAvailable(projects.NameOf(project), agent) {
		return nil
	}
	return fmt.Errorf("%w: @%s is not available where this conversation is", ErrForbidden, agent)
}

// Bounds on what the entry agent is told about a message's agents.
const (
	// maxHintPrompt bounds how much of each matching agent's instructions the
	// entry agent is sent.
	maxHintPrompt = 1500
	// maxHintNames bounds how many other agents are named to it.
	maxHintNames = 60
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

// Pick is the entry agent's decision about a message that names nobody:
// answer it itself, or hand it to the agents it names, and why.
type Pick struct {
	Decision string   `json:"decision"`
	Agents   []string `json:"agents"`
	Reason   string   `json:"reason"`
	Matched  []string `json:"matched"`
}

// The entry agent's decisions.
const (
	PickAnswerMyself = "answer_myself"
	PickDelegate     = "delegate"
)

// Bounds on a pick.
const (
	// pickTimeout bounds how long a message waits for the entry agent to
	// pick. Past it the pick is stopped and the fallback answers.
	pickTimeout = 45 * time.Second
	// maxDelegates bounds how many agents one pick hands a message to.
	maxDelegates = 3
)

// pickSchema is the shape of a pick, checked before it is acted on.
var pickSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["decision","agents","reason","matched"],"properties":{"decision":{"enum":["answer_myself","delegate"]},"agents":{"type":"array","items":{"type":"string"},"maxItems":3},"reason":{"type":"string","maxLength":300},"matched":{"type":"array","items":{"type":"string"},"maxItems":10}}}`)

// pickTagID is the tag of the run that picks who answers the run tagID
// would have been, so a retried message picks once.
func pickTagID(tagID string) string { return "pick-" + tagID }

// entryRoot reports whether th is a root session the platform's entry agent
// owns and agent is that agent: a message to it is picked for.
func (p *Platform) entryRoot(ctx context.Context, tenant string, th threads.Thread, agent string) bool {
	if agent != p.cfg.EntryAgent || th.Session == nil || th.Session.Agent != agent || strings.HasPrefix(th.Session.Parent, "s_") {
		return false
	}
	r, err := p.Catalog().Get(ctx, tenant, agent, 0)
	return err == nil && p.isEntry(r)
}

// answerAsEntry answers a message in a root session the entry agent owns.
// The entry agent first picks, in a run of its own that is recorded and
// metered like any other: it answers itself, or each agent it names answers
// in its own child session under the root (continuing one already there).
// Every answer shows the pick's reason above it. When the pick cannot be
// made (no provider of its chain is enabled, it failed or timed out, or its
// answer is not a pick), the best search match answers as if picked, then
// the workspace's named default, then the first agent available, and with
// nobody else the entry agent answers itself.
func (p *Platform) answerAsEntry(ctx context.Context, tenant string, th threads.Thread, m threads.Message, via string, actor event.Actor, tagID string, keeps bool, sees func(string) bool) ([]runs.Run, error) {
	entry := p.cfg.EntryAgent
	tree := p.Projects.Tree(tenant)
	keep := func(a string) bool {
		return a != entry && (sees == nil || sees(a)) && p.mayRoute(tenant, th, via, a) == nil
	}
	cs, err := p.candidates(ctx, tenant, tree, th.Project, keep)
	if err != nil {
		return nil, err
	}
	found := join.Search(cs, m.Text, join.Shortlist)
	rec := pickRecord{Run: runtime.RunID(tenant, pickTagID(tagID))}
	pick, err := p.pick(ctx, tenant, th, m, actor, tagID, found, cs)
	if err != nil {
		p.log.Info("platform: the entry agent could not pick; falling back", "thread", th.ID, "error", err)
		pick, rec.Fallback = p.fallbackPick(tree, th.Project, found, cs, sees), true
	}
	rec.Pick = pick
	if pick.Decision != PickDelegate {
		p.keepPick(tenant, th.ID, m.ID, rec)
		return p.answerDirect(withPicked(ctx, pickLine(pick)), tenant, th, m, via, entry, nil, "", actor, tagID, keeps, sees)
	}
	var out []runs.Run
	var firstErr error
	for _, agent := range pick.Agents {
		child, cm, err := p.delegateChild(ctx, tenant, th, m, agent)
		if err != nil {
			firstErr = cmp.Or(firstErr, err)
			continue
		}
		started, err := p.answerDirect(withPicked(ctx, pickLine(pick)), tenant, child, cm, via, agent, nil, "", actor, answerTagID(child.ID, cm.ID, agent), keeps, sees)
		if err != nil {
			firstErr = cmp.Or(firstErr, err)
		}
		c := pickedChild{Agent: agent, Session: child.ID}
		if len(started) > 0 {
			c.Run = started[0].ID
		}
		rec.Children = append(rec.Children, c)
		out = append(out, started...)
	}
	p.keepPick(tenant, th.ID, m.ID, rec)
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// pickLine is the line shown above an answer the pick chose: its reason, and
// the words that matched when it names them.
func pickLine(pick Pick) string {
	line := strings.TrimSpace(pick.Reason)
	if len(pick.Matched) > 0 && !strings.Contains(line, "matched") {
		line = strings.TrimSuffix(line, ".") + " (matched " + strings.Join(pick.Matched, ", ") + ")."
	}
	return line
}

// fallbackPick is who answers when the entry agent cannot pick: the best
// search match, the workspace's named default, the first agent available, or
// with none of them the entry agent itself.
func (p *Platform) fallbackPick(tree *projects.Tree, project string, found []join.Match, cs []join.Candidate, sees func(string) bool) Pick {
	switch {
	case len(found) > 0:
		return Pick{Decision: PickDelegate, Agents: []string{found[0].Name}, Reason: "Picked by search: matched " + strings.Join(found[0].Terms, ", ") + "."}
	case func() bool {
		d := tree.Root().Agents.Default
		return d != "" && d != Open && d != p.cfg.EntryAgent && (sees == nil || sees(d)) && tree.IsAvailable(projects.NameOf(project), d)
	}():
		return Pick{Decision: PickDelegate, Agents: []string{tree.Root().Agents.Default}, Reason: "No agent matched it, so the workspace default answered."}
	case len(cs) > 0:
		return Pick{Decision: PickDelegate, Agents: []string{cs[0].Name}, Reason: "No agent matched it, so the first agent available here answered."}
	}
	return Pick{Decision: PickAnswerMyself}
}

// pick runs the entry agent once, with the message, the agents search found
// (with their instructions), the others it may name and the agents already
// answering in this session, and reads its decision. The run is filed on its
// own, not in the session, and is metered to the session's project. An
// agent it names that is not a candidate is dropped; a delegation left with
// nobody answers itself.
func (p *Platform) pick(ctx context.Context, tenant string, th threads.Thread, m threads.Message, actor event.Actor, tagID string, found []join.Match, cs []join.Candidate) (Pick, error) {
	format := call.JSONSchemaFormat{Schema: pickSchema}
	var b strings.Builder
	b.WriteString("Decide who answers this message before anyone does. Answer it yourself when it is simple or no agent fits; otherwise delegate it to the one to three agents best placed to answer, preferring an agent already answering in this conversation for a follow-up. Give one short reason a person will read above the answer, and the words of the message that matched.\n\n")
	fmt.Fprintf(&b, "<message>\n%s\n</message>\n\n", clipText(m.Text, maxHintPrompt*4))
	b.WriteString(p.hintsFrom(found, cs))
	if children := p.childAgents(ctx, tenant, th.ID); len(children) > 0 {
		fmt.Fprintf(&b, "\nAgents already answering in this conversation: @%s\n", strings.Join(children, ", @"))
	}
	b.WriteString("\n" + format.Instruction())
	id := pickTagID(tagID)
	runID := runtime.RunID(tenant, id)
	if _, err := p.Runs.Get(tenant, runID); err != nil {
		meta := map[string]string{runs.MetaAsk: m.Text, runs.MetaProject: th.Project, runs.MetaChannel: th.Channel}
		if _, err := p.Invoke(ctx, wire.NewEvent(wire.Raise{ID: id, Tenant: tenant, Agent: p.cfg.EntryAgent, Origin: runs.OriginAPI, Actor: actor, Context: meta, Text: b.String()})); err != nil {
			return Pick{}, err
		}
	}
	r, err := p.awaitPick(ctx, tenant, runID)
	if err != nil {
		return Pick{}, err
	}
	text := strings.TrimSpace(r.Answer)
	text = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(text, "```json"), "```"), "```")
	text = strings.TrimSpace(text)
	if err := format.ValidateOutput(text); err != nil {
		return Pick{}, err
	}
	var pk Pick
	if err := json.Unmarshal([]byte(text), &pk); err != nil {
		return Pick{}, err
	}
	allowed := map[string]bool{}
	for _, c := range cs {
		allowed[c.Name] = true
	}
	var agents []string
	for _, a := range pk.Agents {
		a = strings.TrimPrefix(strings.TrimSpace(a), "@")
		if allowed[a] && !slices.Contains(agents, a) && len(agents) < maxDelegates {
			agents = append(agents, a)
		}
	}
	pk.Agents = agents
	if pk.Decision == PickDelegate && len(agents) == 0 {
		pk.Decision = PickAnswerMyself
	}
	return pk, nil
}

// awaitPick waits for a pick to end, within pickTimeout. A pick that has not
// ended by then is stopped, so it spends no more.
func (p *Platform) awaitPick(ctx context.Context, tenant, runID string) (runs.Run, error) {
	ctx, cancel := context.WithTimeout(ctx, pickTimeout)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		r, err := p.Runs.Get(tenant, runID)
		if err == nil && r.Status.Terminal() {
			if r.Status != runs.Completed {
				return r, fmt.Errorf("the pick ended %s: %s", r.Status, cmp.Or(r.Error, r.Reason))
			}
			return r, nil
		}
		if r.Status == runs.Parked {
			_ = p.requestStop(context.WithoutCancel(ctx), tenant, runID, "a pick may not wait on a person", "platform")
			return r, fmt.Errorf("the pick asked a question")
		}
		select {
		case <-ctx.Done():
			_ = p.requestStop(context.WithoutCancel(ctx), tenant, runID, "the pick took too long", "platform")
			return runs.Run{}, fmt.Errorf("the pick took longer than %s", pickTimeout)
		case <-tick.C:
		}
	}
}

// hintsFrom is the agents block a pick and an answer are given: the matches
// with their instructions, then the other agents by name.
func (p *Platform) hintsFrom(found []join.Match, cs []join.Candidate) string {
	var b strings.Builder
	b.WriteString("<agents>\n")
	if len(found) > 0 {
		b.WriteString("Agents that may fit this message, best first:\n")
		for _, m := range found {
			fmt.Fprintf(&b, "\n@%s (matched %s)\n<instructions>\n%s\n</instructions>\n", m.Name, strings.Join(m.Terms, ", "), clipText(m.Prompt, maxHintPrompt))
		}
	} else {
		b.WriteString("No agent's instructions match this message.\n")
	}
	var others []string
	for _, c := range cs {
		if !slices.ContainsFunc(found, func(m join.Match) bool { return m.Name == c.Name }) {
			others = append(others, "@"+c.Name)
		}
	}
	if len(others) > maxHintNames {
		others = append(others[:maxHintNames], fmt.Sprintf("and %d more", len(others)-maxHintNames))
	}
	if len(others) > 0 {
		fmt.Fprintf(&b, "\nOther agents you may call: %s\n", strings.Join(others, ", "))
	}
	b.WriteString("</agents>")
	return b.String()
}

// childAgents are the agents with a session under root, in name order.
func (p *Platform) childAgents(ctx context.Context, tenant, root string) []string {
	kids, err := p.Sessions.List(ctx, tenant, session.Filter{Parent: root, Limit: session.MaxPage})
	if err != nil {
		return nil
	}
	var out []string
	for _, k := range kids {
		if !slices.Contains(out, k.Key.Agent) {
			out = append(out, k.Key.Agent)
		}
	}
	slices.Sort(out)
	return out
}

// delegateChild is agent's child session under root, opened or continued,
// with the person's message copied into it once.
func (p *Platform) delegateChild(ctx context.Context, tenant string, root threads.Thread, m threads.Message, agent string) (threads.Thread, threads.Message, error) {
	rec, err := p.Sessions.Open(ctx, tenant, session.Open{Parent: root.ID, Agent: agent, Channel: wire.Channel(tenant)})
	if err != nil {
		return threads.Thread{}, threads.Message{}, err
	}
	child := threads.Thread{ID: rec.ID, Project: root.Project, Channel: root.Channel, Visibility: root.Visibility, CreatedBy: root.CreatedBy, Participants: slices.Clone(root.Participants), Default: agent, Agents: []string{agent}, Title: firstWords(m.Text, threads.MaxTitle), Session: &threads.SessionIdentity{Agent: agent, Parent: root.ID, Label: rec.Key.Label, SourceAlert: root.Session.SourceAlert}}
	if _, err := p.Threads.Create(tenant, child, p.now()); err != nil && !errors.Is(err, threads.ErrExists) {
		return threads.Thread{}, threads.Message{}, err
	}
	cm, _, err := p.Threads.Post(tenant, rec.ID, threads.Message{By: m.By, Name: m.Name, Text: m.Text, Attachments: m.Attachments, Via: m.Via, SurfaceID: "delegated:" + root.ID + ":" + m.ID}, p.now())
	if err != nil {
		return threads.Thread{}, threads.Message{}, err
	}
	child, err = p.Threads.Get(tenant, rec.ID)
	return child, cm, err
}

// pickRecord is what a pick came to, kept so the root shows it with each
// answer it led to, after a restart as before.
type pickRecord struct {
	Pick     Pick          `json:"pick"`
	Run      string        `json:"run,omitempty"`
	Fallback bool          `json:"fallback,omitempty"`
	Children []pickedChild `json:"children,omitempty"`
	At       time.Time     `json:"at"`
}

// pickedChild is one agent a pick handed a message to: its child session and
// the run that answers there.
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

// keepPick records what a pick came to. It is for showing, so a failure is
// logged and never fails the answer.
func (p *Platform) keepPick(tenant, root, message string, rec pickRecord) {
	rec.At = p.now()
	if err := p.picks.Put(tenant, pickKey(root, message), rec); err != nil {
		p.log.Warn("platform: a pick could not be kept", "thread", root, "error", err)
	}
}

// pickMessage is how a root shows who reads it what its entry agent picked
// for a person's message: the reason, then each agent it handed the message
// to, with that agent's answer, so nobody has to open a child to read it. A
// child session the reader may not read is left out, answer and all. It is
// false for a message nothing was picked for.
func (p *Platform) pickMessage(who auth.Principal, root threads.Thread, m threads.Message) (SessionMessage, bool) {
	tenant := who.Tenant
	it, err := p.picks.Lookup(tenant, pickKey(root.ID, m.ID))
	if err != nil {
		return SessionMessage{}, false
	}
	rec := it.Value
	display := p.wording(tenant).Name
	out := SessionMessage{ID: "pick-" + m.ID, Session: root.ID, Author: SessionAuthor{Kind: "agent", Name: p.cfg.EntryAgent, Display: display}, Status: "done", At: rec.At, Parts: []SessionPart{{Type: "pick", Text: pickLine(rec.Pick)}}}
	var status []runs.Run
	for _, c := range rec.Children {
		if !p.SeesSession(who, c.Session) {
			continue
		}
		part := SessionPart{Type: "call", Session: c.Session, Agent: c.Agent, Status: "working"}
		if r, err := p.Runs.Get(tenant, c.Run); err == nil {
			part.Status, part.Text = sessionStatus([]runs.Run{r}), p.ShownRunAs(who, r).Answer
			status = append(status, r)
		}
		out.Parts = append(out.Parts, part)
	}
	if len(status) > 0 {
		out.Status = sessionStatus(status)
	}
	return out, true
}
