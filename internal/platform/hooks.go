package platform

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/toolprovider"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/usage"
)

// Housekeeping the platform does around a conversation, by asking an agent.
//
// A workspace can name an agent to title conversations and an agent to keep
// each agent's memory. After a turn ends, the platform sends that agent a
// request of its own, as a run with origin "system", and files the answer when
// it comes back. They are ordinary runs: they show on the Runs page, they are
// traced, and they can fail without affecting the conversation they serve.
// Each is read where the turn it serves is, and an agent that lives in a
// project is asked only about the turns there and below.
//
// Both are off until a workspace chooses an agent for them, and neither runs on
// the offline provider, whose answer is a notice and not a title or a fact.
const (
	metaHook = "warp.hook"
	metaFor  = "warp.for"
	// metaKeepsNotes names the project whose notes the person a turn was
	// for may change, on the turn and on the memory run after it.
	metaKeepsNotes = "warp.keeps_notes"

	hookTitle  = "title"
	hookMemory = "memory"
)

// maxTitle bounds a conversation's name.
const maxTitle = 80

const titlePrompt = "Write a title for a conversation that starts with the message below. " +
	"Reply with the title only: at most six words, no quotes, no full stop.\n\nMessage: %s"

const memoryPrompt = "Below is one exchange between a person and the agent %q. " +
	"List what that agent should remember for later conversations: lasting facts about the person, " +
	"their work or their preferences. One per line, at most three lines, each a full sentence. " +
	"Leave out anything that only mattered in this exchange. Reply NONE if there is nothing.\n\n" +
	"Person: %s\n%s: %s"

// ended is called exactly once for every run that ends.
func (p *Platform) ended(ctx context.Context, run runs.Run) {
	if err := p.Operations.Settle(run.Tenant, run.ID, run.Status == runs.Completed, p.now()); err != nil {
		p.log.Error("platform: operation outcome could not be kept", "error", err)
	}
	p.settleUsage(ctx, run)
	p.raise(run)
	// A question nobody answered is not waiting any more.
	p.settleQuestionsOf(run)
	p.forgetApprovedCalls(run.Tenant, run.ID)
	p.cascade(ctx, run)
	p.deliverAnswer(ctx, run)
	switch {
	case run.Origin == runs.OriginSystem:
		p.fileHook(run)
	case run.Origin == runs.OriginAPI && run.Parent == "" && run.Status == runs.Completed:
		p.afterTurn(ctx, run)
	}
}

// over marks a run ended with change and frees its place under any
// concurrency limit in the one critical section admission takes. A new run
// is then never refused for a place a run that has already ended still held.
func (p *Platform) over(change func() (runs.Run, bool)) (runs.Run, bool) {
	p.admission.Lock()
	defer p.admission.Unlock()
	run, ended := change()
	if ended {
		p.free(run)
	}
	return run, ended
}

// free gives an ended run's place back. The caller holds p.admission.
func (p *Platform) free(run runs.Run) {
	p.releaseRoom(run)
	if run.Origin != runs.OriginSystem {
		p.appendUsage(usage.Entry{Kind: usage.Ended, Tenant: run.Tenant, RunID: run.ID, At: p.now()})
	}
}

// afterTurn starts the housekeeping a finished turn calls for.
func (p *Platform) afterTurn(ctx context.Context, run runs.Run) {
	cfg := p.Settings(run.Tenant)
	if cfg.TitleAgent != "" && run.Prev == "" {
		p.startHook(ctx, run, cfg.TitleAgent, hookTitle, run.Thread, fmt.Sprintf(titlePrompt, clip(run.Text, 2000)))
	}
	// A person's opt-in to remember how they work creates private proposals,
	// never shared project memory. Review is still required.
	if cfg.MemoryAgent != "" && run.Actor.User != "" && !run.Actor.Bot && p.privateThread(run.Tenant, run.Thread) && p.PreferencesOf(run.Tenant, run.Actor.User).Personal.Remember {
		p.startHook(ctx, run, cfg.MemoryAgent, hookMemory, memory.PersonalOwner(run.Actor.User), fmt.Sprintf(memoryPrompt, run.Agent, clip(run.Text, 4000), run.Agent, clip(run.Answer, 4000)))
	}
	// What is said in a private thread is its participants' alone: nothing
	// in it becomes a note anyone else would be sent.
	if cfg.MemoryAgent != "" && !p.privateThread(run.Tenant, run.Thread) {
		// Every note is sent to other people's runs, so what a person's
		// own credential reached is not taught at all.
		if p.mayCarryPersonData(ctx, run) {
			p.log.Info("platform: the memory agent is not asked about a turn that may carry what a person's own credential reached",
				"tenant", run.Tenant, "run", run.ID)
			return
		}
		// In a project, what is learned is the project's: every agent and
		// every conversation there shares it. Outside one, it is the agent's.
		owner := run.Agent
		meta := map[string]string{}
		if run.Project != "" && p.HasProject(run.Tenant, run.Project) {
			owner = memory.ProjectOwner(run.Project)
			if keeps := wire.Meta(run.Tag())[metaKeepsNotes]; keeps != "" {
				meta[metaKeepsNotes] = keeps
			}
		}
		p.startHook(ctx, run, cfg.MemoryAgent, hookMemory, owner,
			fmt.Sprintf(memoryPrompt, run.Agent, clip(run.Text, 4000), run.Agent, clip(run.Answer, 4000)), meta)
	}
}

// mayCarryPersonData reports whether a turn's answer may carry what a
// person's own credential reached. No memory is read by one person alone: an
// agent's notes are sent to every run of that agent, and a project's to every
// run in it and below it, whoever the run acts for. So a turn may carry it
// when it, or any turn before it on its branch, acted for a person and was
// answered by an agent revision granted a person-scoped tool provider. The
// whole branch counts because the next agent is sent the conversation so far,
// and one with no tools of its own can repeat what an earlier turn found.
//
// A turn that answers a person's message in a shared thread, or follows one
// that did, is sent the room up to that message, and the room holds what
// every earlier message's agents answered, whoever those runs acted for. So
// there every turn of the conversation made before it counts as one before
// it, not only its branch.
//
// What cannot be read now counts as such a turn: a branch whose start the
// index no longer holds, a revision or a granted provider that cannot be
// found, and a provider changed since the turn, whose scope then is not kept.
func (p *Platform) mayCarryPersonData(ctx context.Context, run runs.Run) bool {
	path, err := p.Runs.Path(run.Tenant, run.ID)
	if err != nil || len(path) == 0 {
		return true
	}
	if head := path[0]; head.Prev != "" {
		if !strings.HasPrefix(head.Prev, "m-") {
			return true
		}
		room, err := p.Runs.Conversation(run.Tenant, head.Thread)
		if err != nil {
			return true
		}
		for _, r := range room {
			if r.CreatedAt.Before(run.CreatedAt) {
				path = append(path, r)
			}
		}
		// Oldest first, as a branch is, so each revision counts from its
		// earliest turn.
		slices.SortStableFunc(path, func(a, b runs.Run) int { return a.CreatedAt.Compare(b.CreatedAt) })
	}
	checked := map[string]bool{}
	for _, turn := range path {
		// As personCredential reads the actor: a bot or no person is nobody.
		if turn.Actor.Bot || turn.Actor.User == "" {
			continue
		}
		// The earliest turn of a revision is the one a change counts from.
		key := fmt.Sprintf("%s@%d", turn.Agent, turn.Rev)
		if checked[key] {
			continue
		}
		checked[key] = true
		rev, err := p.Catalog().Get(ctx, turn.Tenant, turn.Agent, turn.Rev)
		if err != nil || p.grantsPersonScoped(ctx, turn.Tenant, rev.Spec.Tools, turn.CreatedAt) {
			return true
		}
	}
	return false
}

// grantsPersonScoped reports whether tools grant a provider that takes each
// person's own credential. A granted provider that cannot be found counts as
// one, and so does one changed after since, when since is set, whose scope
// then is not kept.
func (p *Platform) grantsPersonScoped(ctx context.Context, tenant string, tools []string, since time.Time) bool {
	for _, grant := range tools {
		if grant == ToolSourceSearch {
			return true
		}
		// As fiber expands a grant: anything else is not a provider's.
		parsed, ok, err := wire.ParseMCPGrant(grant)
		if !ok || err != nil || !toolprovider.ValidName(parsed.Provider) {
			continue
		}
		prov, err := p.ToolProviders.GetProvider(ctx, tenant, parsed.Provider)
		if err != nil || prov.Scope() == toolprovider.ScopePerson || (!since.IsZero() && prov.UpdatedAt.After(since)) {
			return true
		}
	}
	return false
}

func (p *Platform) startHook(ctx context.Context, turn runs.Run, agent, hook, target, prompt string, extra ...map[string]string) {
	rev, err := p.Catalog().Get(ctx, turn.Tenant, agent, 0)
	if err != nil {
		p.log.Warn("platform: housekeeping agent is missing", "hook", hook, "agent", agent)
		return
	}
	if rev.Spec.Provider == "offline" && !p.cfg.OfflineHooks {
		return
	}
	// The run is read where the turn is, so an agent that lives in a project
	// is asked only about the turns there and below. Everyone who reads such
	// a turn reads the project the agent lives in, so its run is never shown
	// to someone who may not read the agent. It runs as the agent's own name,
	// whatever spelling the settings hold.
	agent = rev.Spec.Name
	tree := p.Projects.Tree(turn.Tenant)
	if home, homed := tree.Home(agent); homed && home != projects.Root && !onChain(tree, turn.Project, home) {
		return
	}
	contextMeta := map[string]string{}

	for _, key := range []string{MetaPersonalMemoryContext, MetaPrivateLibraryContext} {
		if wire.Meta(turn.Tag())[key] == "true" {
			contextMeta[key] = "true"
		}
	}

	if _, personal := memory.OwnerPerson(target); personal {
		contextMeta["_humagen.context.personal_memory"] = "true"
	}
	_, err = p.Invoke(ctx, wire.NewEvent(wire.Raise{
		ID:     fmt.Sprintf("sys-%s-%s", hook, turn.ID),
		Tenant: turn.Tenant,
		Agent:  agent,
		Origin: runs.OriginSystem,
		Actor:  event.Actor{ID: "system:" + hook, Display: "platform " + hook, Bot: true},
		// The housekeeping run is sent the turn's words, so it is read
		// where the turn is: in its project and its conversation.
		Context: hookMeta(hook, target, append([]map[string]string{{metaPlaceProject: turn.Project, metaPlaceThread: turn.Thread}, contextMeta}, extra...)...),
		Text:    prompt,
	}))
	if err != nil {
		p.log.Warn("platform: housekeeping run could not start", "hook", hook, "error", err)
	}
}

// hookMeta is what a housekeeping run carries: which hook it is, what it is
// for, and whatever else the turn passes on.
func hookMeta(hook, target string, extra ...map[string]string) map[string]string {
	meta := map[string]string{metaHook: hook, metaFor: target}
	for _, m := range extra {
		for k, v := range m {
			meta[k] = v
		}
	}
	return meta
}

// fileHook puts a housekeeping run's answer where it belongs. The answer is a
// model's words about text a person supplied, so it is treated as untrusted:
// trimmed to one line, bounded, and stored only as a title or as notes people
// can read and remove.
func (p *Platform) fileHook(run runs.Run) {
	if run.Status != runs.Completed {
		return
	}
	meta := wire.Meta(run.Tag())
	target := meta[metaFor]
	switch meta[metaHook] {
	case hookTitle:
		if title := oneLine(run.Answer, maxTitle); title != "" {
			p.Runs.SetTitle(run.Tenant, target, title)
		}
	case hookMemory:
		kept := 0
		var proposed []memory.Note
		_, personal := memory.OwnerPerson(target)
		_, projectMemory := memory.OwnerProject(target)
		unasked := !personal && !projectMemory && p.Settings(run.Tenant).MemoryAutoApprove && p.mayKeepUnasked(run.Tenant, target, meta[metaKeepsNotes])
		for _, line := range strings.Split(run.Answer, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.EqualFold(strings.Trim(line, ".-* "), "none") || kept == 3 {
				continue
			}
			if len(line) > memory.MaxChars {
				continue
			}
			// A proposal waits for a person unless the workspace chose
			// otherwise; for a project's notes, unless the turn was one
			// of its owners' as well; and always for the workspace's
			// and a company project's notes, which need projects:govern.
			// Text that carries personal data is not kept at all.
			status := memory.Pending
			if unasked {
				status = memory.Approved
			}
			if note, err := p.Memory.Add(run.Tenant, target, line, memory.Consolidated, status, run.ID, run.UpdatedAt); err == nil {
				kept++
				if status == memory.Pending {
					proposed = append(proposed, note)
				}
			} else {
				p.log.Info("platform: a proposed memory was not kept", "agent", target, "reason", err)
			}
		}
		if !personal {
			p.inboxMemory(run, target, proposed)
		}
	}
}

// oneLine reduces text to its first line, without wrapping quotes, bounded.
func oneLine(text string, limit int) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	line = strings.Trim(strings.Join(strings.Fields(line), " "), "\"'`.")
	return clip(line, limit)
}

// clip shortens text to a limit in bytes, on a rune boundary.
func clip(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	for limit > 0 && s[limit]&0xC0 == 0x80 {
		limit--
	}
	return strings.TrimSpace(s[:limit])
}
