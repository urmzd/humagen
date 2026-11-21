package platform

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
)

// An agent template is a goal a new agent starts from: what it is for, said
// as the outcome a person gets, and the instructions, tools, look and
// starters that get there. A template is a starting point, never a link: the
// agent made from one is an ordinary agent, and changing the catalog changes
// no agent already made.

// AgentTemplateStarter is one job the agent offers on its home.
type AgentTemplateStarter struct {
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
}

// AgentTemplate is one goal a person can start an agent from.
type AgentTemplate struct {
	// ID is the template's address in the console (/agents/new/{id}).
	ID string `json:"id"`
	// Title is what the agent is, such as Document Writer.
	Title string `json:"title"`
	// Outcome is what a person gets from it, in a few words.
	Outcome string `json:"outcome"`
	// Description becomes the agent's description: what it is for.
	Description string `json:"description"`
	// Greeting is what it says about itself, in its own voice.
	Greeting string `json:"greeting"`
	// Shape is the bot it is drawn as, one of the look's shapes.
	Shape string `json:"shape"`
	// Prompt is its system prompt.
	Prompt string `json:"system_prompt"`
	// Tools are the tools it is granted, each one the builder may grant.
	Tools []string `json:"tools"`
	// Starters are the jobs its home offers, at most three.
	Starters []AgentTemplateStarter `json:"starters"`
}

// AgentTemplateCatalog is the templates, and the models a new agent made
// from one starts on: the entry agent's, as the builder's agents do.
type AgentTemplateCatalog struct {
	Templates []AgentTemplate `json:"templates"`
	// Model is the provider, model and fallbacks a new agent starts on. Empty
	// when the workspace has no entry agent to take them from.
	Model AgentTemplateModel `json:"model"`
}

// AgentTemplateModel is the model chain a new agent starts on.
type AgentTemplateModel struct {
	Provider   string          `json:"provider"`
	Model      string          `json:"model"`
	Fallbacks  []wire.ModelRef `json:"fallbacks,omitempty"`
	FallbackOn []string        `json:"fallback_on,omitempty"`
}

// BlankTemplate is the template with nothing chosen yet.
const BlankTemplate = "blank"

// agentTemplates is the catalog, in the order the console lists it. Blank
// comes first.
var agentTemplates = []AgentTemplate{{
	ID:          BlankTemplate,
	Title:       "Start from scratch",
	Outcome:     "A blank agent",
	Description: "A new agent. Say what it is for in its instructions.",
	Greeting:    "I am new here. Tell me what you need, and I will ask when something is unclear.",
	Shape:       "circle",
	Prompt:      "Do what you are asked, in as few steps as it takes. Ask a person when information or permission is missing, and say what you did when you finish.",
	Tools:       []string{ToolOutcome},
}, {
	ID:          "writer",
	Title:       "Document Writer",
	Outcome:     "Documents you can publish",
	Description: "Drafts documents, decks and sheets from your notes and publishes the finished files.",
	Greeting:    "I turn notes and sessions into documents, decks and sheets, and publish them when you approve.",
	Shape:       "square",
	Prompt:      "Create clear, editable deliverables. Use file_write with format native for DOCX, PPTX, XLSX and PDF, keep source evidence and notes, then publish completed files. Wait for each file_write to succeed before a dependent file_read or publish; never batch dependent calls. Record the result and the next action with record_outcome. Ask before any external action.",
	Tools:       []string{ToolFileList, ToolFileRead, ToolFileWrite, ToolPublish, ToolOutcome},
	Starters: []AgentTemplateStarter{
		{Title: "Draft a one-page brief", Prompt: "Draft a one-page brief from the notes I attach, then publish it as a document."},
		{Title: "Turn notes into slides", Prompt: "Turn the notes I attach into a short slide deck with one idea per slide."},
		{Title: "Clean up a spreadsheet", Prompt: "Clean up the spreadsheet I attach: consistent headers, no blank rows, and a summary tab."},
	},
}, {
	ID:          "researcher",
	Title:       "Researcher",
	Outcome:     "Answers with sources",
	Description: "Researches a question on the web and answers with the sources it used.",
	Greeting:    "I look things up and answer with the sources, so you can check every claim.",
	Shape:       "drop",
	Prompt:      "Answer research questions with evidence. Search the web, read the most relevant pages, and cite each claim with its source. Separate facts from assumptions. Say when sources disagree or when you could not find an answer.",
	Tools:       []string{ToolWebSearch, ToolWebFetch, ToolOutcome},
	Starters: []AgentTemplateStarter{
		{Title: "Compare two vendors", Prompt: "Compare two vendors I name on price, limits and support, and recommend one with sources."},
		{Title: "Summarize what changed", Prompt: "Summarize what changed this month in a topic I name, with a source for each point."},
		{Title: "Check a claim", Prompt: "Check whether a claim I paste is true, and show the sources either way."},
	},
}, {
	ID:          "reporter",
	Title:       "Weekly Reporter",
	Outcome:     "A status report every week",
	Description: "Writes a status report on a schedule and posts it where your team reads it.",
	Greeting:    "I write the weekly report so nobody has to, and post it on the day you choose.",
	Shape:       "pill",
	Prompt:      "Write short, factual status reports: what shipped, what is blocked, and what is next, each with its owner. Use the files and sessions you are given. When asked for a regular report, set up a schedule for it and confirm the day and time first.",
	Tools:       []string{ToolFileList, ToolFileRead, ToolFileWrite, ToolOutcome, ToolCreateSchedule, ToolUpdateSchedule, ToolPauseSchedule, ToolResumeSchedule},
	Starters: []AgentTemplateStarter{
		{Title: "Write this week's report", Prompt: "Write this week's status report: shipped, blocked and next, each with an owner."},
		{Title: "Report every Friday", Prompt: "Write a status report every Friday at 4pm and post it here."},
		{Title: "List open blockers", Prompt: "List every open blocker with its owner and how long it has been open."},
	},
}, {
	ID:          "coordinator",
	Title:       "Project Coordinator",
	Outcome:     "Owners, deadlines and handoffs",
	Description: "Turns a goal into owned steps, follows up on them and asks a person when a decision is needed.",
	Greeting:    "I turn a goal into steps with owners and dates, and follow up until each one is done.",
	Shape:       "clover",
	Prompt:      "Clarify the goal, the owner and what done means. Break the work into steps, each with an owner and a date. Make handoffs explicit. Ask a person when a decision or permission is needed, and keep a short record of what was decided.",
	Tools:       []string{ToolOutcome, toolAskPerson, ToolNotify},
	Starters: []AgentTemplateStarter{
		{Title: "Plan a launch", Prompt: "Plan the launch I describe: steps, owners, dates and the risks to watch."},
		{Title: "Run a follow-up", Prompt: "Follow up on the action items from the notes I attach, and tell me who has not answered."},
		{Title: "Write a handoff", Prompt: "Write a handoff for the work I describe: context, open questions and the next step."},
	},
}, {
	ID:          "watcher",
	Title:       "Watcher",
	Outcome:     "Tells you when something changes",
	Description: "Watches a page or a feed and tells you when what you care about changes.",
	Greeting:    "I keep an eye on the things you name and tell you only when something worth knowing changes.",
	Shape:       "ghost",
	Prompt:      "Watch what you are asked to watch. Check it on the schedule agreed, compare it with the last check, and notify the person only when something they care about changed. Say what changed and link to where you saw it.",
	Tools:       []string{ToolWebFetch, ToolWatchStart, ToolBackgroundList, ToolBackgroundStop, ToolNotify, ToolOutcome},
	Starters: []AgentTemplateStarter{
		{Title: "Watch a pricing page", Prompt: "Watch the pricing page I link and tell me when a price or a limit changes."},
		{Title: "Watch a changelog", Prompt: "Watch the changelog I link and tell me about any breaking change."},
		{Title: "Show what you watch", Prompt: "List everything you are watching for me and when you last checked each."},
	},
}, {
	ID:          "onboarding",
	Title:       "Onboarding Guide",
	Outcome:     "New teammates find answers",
	Description: "Answers a new teammate's questions from your project's files and says who to ask next.",
	Greeting:    "I help new teammates find their way: where things are, how things are done, and who to ask.",
	Shape:       "cat",
	Prompt:      "Help new teammates. Answer from the project's files, quote where an answer comes from, and say who to ask when the files do not settle it. Be patient and brief, and never guess at policy.",
	Tools:       []string{ToolFileList, ToolFileRead, ToolOutcome},
	Starters: []AgentTemplateStarter{
		{Title: "Make a first-week plan", Prompt: "Make a first-week plan for a new teammate from this project's files."},
		{Title: "Explain how we ship", Prompt: "Explain how this team ships a change, from idea to release, from the project's files."},
		{Title: "Who owns what", Prompt: "List who owns what in this project, from its files."},
	},
}}

// AgentTemplates lists the templates and the models a new agent starts on.
// Each is a copy, so a caller cannot change the catalog.
func (p *Platform) AgentTemplates(ctx context.Context, tenant string) AgentTemplateCatalog {
	out := AgentTemplateCatalog{Templates: make([]AgentTemplate, 0, len(agentTemplates))}
	for _, t := range agentTemplates {
		t.Tools = slices.Clone(t.Tools)
		t.Starters = slices.Clone(t.Starters)
		if t.Starters == nil {
			t.Starters = []AgentTemplateStarter{}
		}
		out.Templates = append(out.Templates, t)
	}
	if spec, ok := p.builderModels(ctx, tenant); ok {
		out.Model = AgentTemplateModel{Provider: spec.Provider, Model: spec.Model, Fallbacks: slices.Clone(spec.Fallbacks), FallbackOn: slices.Clone(spec.FallbackOn)}
	}
	return out
}

// ErrAgentProtected reports a change to an agent the platform keeps: the
// entry agent, the analyst and the approver are never retired.
var ErrAgentProtected = errors.New("platform: the platform keeps this agent")

// RetireAgent retires an agent, which a person does from its settings and
// the builder with retire_agent: it leaves the workspace, its sessions and
// revisions stay readable, and a new agent may take its name. Pausing it
// instead keeps it and stops it from answering. A revision written since
// expectedRev (when it is not 0) is a conflict, so a change made meanwhile
// is never retired unseen.
func (p *Platform) RetireAgent(ctx context.Context, who auth.Principal, name string, expectedRev int) (resultErr error) {
	name = wire.NormalizeName(name)
	if !who.Can(auth.AgentsWrite) {
		return auth.ErrForbidden
	}
	have, err := p.Catalog().Get(ctx, who.Tenant, name, 0)
	if err != nil {
		return err
	}
	if IsBuiltin(name) || p.helperProtected(name) {
		return fmt.Errorf("%w: %s", ErrAgentProtected, name)
	}
	if err := p.OwnsHome(who, name); err != nil {
		return err
	}
	if expectedRev != 0 && expectedRev != have.Rev {
		return fmt.Errorf("%w: agent %q is at revision %d, not %d", wire.ErrConflict, name, have.Rev, expectedRev)
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "agent.retire", Resource: "agent", ID: name, Scope: "agent", Agent: name, Before: map[string]string{"rev": strconv.Itoa(have.Rev)}, After: map[string]string{"retired": "true"}})
	if err != nil {
		return err
	}
	defer func() { p.finishOperationsAudit(attempt, resultErr) }()
	return p.retireAgent(ctx, who.Tenant, name, have.Rev)
}
