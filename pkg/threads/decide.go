package threads

import (
	"fmt"
	"slices"

	"github.com/urmzd/warp/pkg/projects"
)

// MaxRuns is how many agents answer one message, side by side.
const MaxRuns = 3

// Rule names which rule decided what a message starts.
type Rule string

// The rules, in the order they apply: the first that applies decides.
const (
	// RuleTagged: the message tags agents or @auto, and each answers.
	RuleTagged Rule = "tagged"
	// RulePeople: it tags only people or experts, so no agent answers.
	RulePeople Rule = "people"
	// RuleReply: it answers a question or replies to a mention.
	RuleReply Rule = "reply"
	// RulePostOnly: the person chose to post only.
	RulePostOnly Rule = "post_only"
	// RuleRouted: it tags no agent, and the thread's selected agents answer
	// it: the one that fits best, or each of them.
	RuleRouted Rule = "routed"
	// RuleUntagged: it was posted in the console and tags no agent, so
	// nothing runs. In the console an agent answers only when it is tagged.
	RuleUntagged Rule = "untagged"
	// RuleDefault: the default the thread inherits answers, if there is one.
	RuleDefault Rule = "default"
)

// Where a default is set.
const (
	FromThread  = "thread"
	FromClient  = "client"
	FromChannel = "channel"
)

// Default is the agent that answers an untagged message, or "auto", and
// where it is set: FromThread, FromClient, FromChannel, or "project:<name>"
// ("project:~" for the root). The zero Default is none.
type Default struct {
	Agent string `json:"agent"`
	From  string `json:"from"`
}

// Nearer returns the defaults set nearer a message than its project, nearest
// first: the thread's, the client's default agent (for a thread a client
// files outside a bound channel; "" otherwise) and the channel's. Each is
// returned, set or not, so the first that is set wins and the project's
// chain decides only when none is.
func Nearer(t Thread, clientDefault string, ch projects.Channel) []Default {
	return []Default{{Agent: t.Default, From: FromThread}, {Agent: clientDefault, From: FromClient}, {Agent: ch.Default, From: FromChannel}}
}

// Input is what decides what a message starts.
type Input struct {
	// Mentions are the message's tags, as Parse found them, with Refused set
	// on each agent the platform found is not available where the message
	// is posted.
	Mentions []Mention
	// Reply is set when the message answers a question or replies to a
	// mention.
	Reply bool
	// PostOnly is set when the person chose to post only.
	PostOnly bool
	// Routed are the thread's selected agents and Mode how they answer an
	// untagged message (Thread.Agents, Thread.Mode).
	Routed []string
	Mode   string
	// TagsOnly is set for a message a person posted in the console, as
	// themselves and not through a client's surface: only the agents it
	// tags answer it, and no default ever does.
	TagsOnly bool
	// Default is what the thread inherits: the first set of Nearer, then
	// the project's chain.
	Default Default
	// Unavailable says why the default cannot answer, when it has stopped
	// being available where the thread is. The next default along is never
	// tried in its place.
	Unavailable string
}

// Decision is what a message starts.
type Decision struct {
	Rule Rule `json:"rule"`
	// Agents answer, in order, at most MaxRuns. "auto" is @auto, which the
	// platform resolves.
	Agents []string `json:"agents"`
	// Among is set when the thread's selected agents answer with ModeBestFit:
	// Agents is then @auto, picking among these alone.
	Among []string `json:"among,omitempty"`
	// Default is set when the agent answering is the inherited default.
	Default bool `json:"default,omitempty"`
	// Reason says why no agent answers, when none does.
	Reason string `json:"reason,omitempty"`
	// Mentions are the input's, with the agents beyond MaxRuns refused.
	Mentions []Mention `json:"mentions"`
}

// Decide applies the rules in order, and the first that applies decides:
//
//  1. a message that tags agents or @auto is answered by each tagged agent
//     that can answer, at most MaxRuns, and by no default, even when none
//     can;
//  2. one that tags only people, experts or a role starts no run, unless it
//     names a skill in a thread with selected agents;
//  3. so does one that answers a question or replies to a mention;
//  4. and one posted only;
//  5. one posted in the console (TagsOnly) in a thread with selected agents
//     is answered by them: the one that fits it best (Among, through
//     @auto), or each of them (ModeAll);
//  6. one posted in the console (TagsOnly) starts no run, whatever the
//     default;
//  7. otherwise the inherited default answers. With none, or with one that
//     has stopped being available, no agent answers, and the reason says so.
//
// A refused tag still counts as a tag of its kind, so a tag that names
// nobody never sends the message to the default instead. A skill is no tag of
// anyone: a message that names one and tags no agent starts @auto, which the
// platform adds to its tags before deciding (WithSkillAgent). Decide is a
// pure function of its input.
func Decide(in Input) Decision {
	ms := cloneMentions(in.Mentions)
	agents, tagged, people, skill := []string{}, false, false, false
	for i, m := range ms {
		switch m.Kind {
		case Agent:
			tagged = true
			if m.Refused != "" {
				continue
			}
			if len(agents) == MaxRuns {
				ms[i].Refused = fmt.Sprintf("at most %d agents answer one message", MaxRuns)
				continue
			}
			agents = append(agents, m.Name)
		case Person, SME, Role:
			people = true
		case Skill:
			skill = skill || m.Refused == ""
		}
	}
	// A thread's selected agents answer only in the console: a client's
	// surface keeps its defaults.
	routed := in.TagsOnly && len(in.Routed) > 0
	d := Decision{Agents: []string{}, Mentions: ms}
	switch {
	case tagged:
		d.Rule, d.Agents = RuleTagged, agents
		if len(agents) == 0 {
			d.Reason = "No agent it tags can answer here."
		}
	case people && (!skill || !routed):
		d.Rule, d.Reason = RulePeople, "It tags only people, so no agent answers."
	case in.Reply:
		d.Rule, d.Reason = RuleReply, "A reply to a person starts no run."
	case in.PostOnly:
		d.Rule, d.Reason = RulePostOnly, "Posted only, so no agent answers."
	case routed && (len(in.Routed) == 1 || in.Mode == ModeAll):
		d.Rule, d.Agents = RuleRouted, slices.Clone(in.Routed[:min(len(in.Routed), MaxRuns)])
	case routed:
		d.Rule, d.Agents, d.Among = RuleRouted, []string{AutoName}, slices.Clone(in.Routed)
	case in.TagsOnly:
		d.Rule, d.Reason = RuleUntagged, "Nothing runs until you tag an agent."
	case in.Default.Agent == "":
		d.Rule, d.Reason = RuleDefault, "Nobody answers untagged messages here; tag an agent."
	case in.Unavailable != "":
		d.Rule, d.Reason = RuleDefault, fmt.Sprintf("@%s answers untagged messages here, but %s. Tag an agent.", in.Default.Agent, in.Unavailable)
	default:
		d.Rule, d.Agents, d.Default = RuleDefault, []string{in.Default.Agent}, true
	}
	return d
}
