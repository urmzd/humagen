// Package join decides which agents take a message that names none.
//
// There is no central pick. Each agent available where the message was
// posted is asked, all at once, one low-effort question: should I be
// involved? An agent that says yes jumps in and answers; one that says no
// exits, and nothing of it is kept but the reason. Several agents can join
// the same message. When none does, nothing starts, and the message waits
// for a person to answer it or to tag an agent.
//
// The question is behind Asker, so a deployment chooses what answers it: a
// small model (Model), or anything else. An Asker that fails, or answers in
// a shape that cannot be read, counts as a no: an agent never joins by
// accident.
package join

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"
)

// Candidate is an agent that could join.
type Candidate struct {
	Name        string
	Description string
	// Prompt is the agent's system prompt, so the question knows what it is
	// for when its description says little.
	Prompt string
}

// Decision is one agent's answer to "should I be involved?".
type Decision struct {
	Agent string `json:"agent"`
	Join  bool   `json:"join"`
	// Reason is a sentence a person can read.
	Reason string `json:"reason"`
}

// Asker asks one agent whether it should take a message.
type Asker interface {
	Ask(ctx context.Context, c Candidate, message string) (Decision, error)
}

// AskerFunc is a function as an Asker.
type AskerFunc func(ctx context.Context, c Candidate, message string) (Decision, error)

// Ask calls f.
func (f AskerFunc) Ask(ctx context.Context, c Candidate, message string) (Decision, error) {
	return f(ctx, c, message)
}

// Limits on one routing.
const (
	// MaxCandidates bounds how many agents are asked about one message.
	MaxCandidates = 64
	// parallel bounds how many are asked at once.
	parallel = 16
	// askTimeout bounds one agent's answer.
	askTimeout = 20 * time.Second
	// maxPrompt bounds how much of an agent's prompt the question carries.
	maxPrompt = 4000
	// maxMessage bounds how much of the message the question carries.
	maxMessage = 8000
)

// Route asks every candidate at once and returns each one's decision, in
// the candidates' order. A nil Asker asks nobody, and every decision is a
// no: with nothing to ask, no agent takes a message on its own.
func Route(ctx context.Context, a Asker, cs []Candidate, message string) []Decision {
	if len(cs) > MaxCandidates {
		cs = cs[:MaxCandidates]
	}
	out := make([]Decision, len(cs))
	if a == nil {
		for i, c := range cs {
			out[i] = Decision{Agent: c.Name, Reason: "Nothing asks agents whether to join here."}
		}
		return out
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, parallel)
	for i, c := range cs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			actx, cancel := context.WithTimeout(ctx, askTimeout)
			defer cancel()
			d, err := a.Ask(actx, c, message)
			d.Agent = c.Name
			if err != nil {
				d = Decision{Agent: c.Name, Reason: "It could not be asked, so it stayed out: " + err.Error()}
			}
			out[i] = d
		}()
	}
	wg.Wait()
	return out
}

// Joined is the agents of decisions that said yes, in order.
func Joined(ds []Decision) []Decision {
	var out []Decision
	for _, d := range ds {
		if d.Join {
			out = append(out, d)
		}
	}
	return out
}

// Model is an Asker that puts the question to a model, and expects a short
// JSON answer back. It sends no tools and no options, so any provider can
// answer it, and it reads the first JSON object in the reply, so a model
// that wraps it in prose or a fence is still understood.
type Model struct {
	Provider saigetypes.Provider
}

const question = `You decide whether one agent on a team should take part in answering a message nobody addressed to anyone in particular.

Say yes only when the message is clearly within what this agent is for, and it has something to add. Several agents may say yes to the same message; an agent that would only repeat what another is better placed to say should say no. When in doubt, say no: a person can always tag the agent.

Answer with one line of JSON and nothing else:
{"join": true or false, "reason": "one short sentence"}`

// Ask asks the model whether c should take message.
func (m Model) Ask(ctx context.Context, c Candidate, message string) (Decision, error) {
	if m.Provider == nil {
		return Decision{}, fmt.Errorf("join: no model")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Agent: %s\n", c.Name)
	if c.Description != "" {
		fmt.Fprintf(&b, "What it is for: %s\n", c.Description)
	}
	if c.Prompt != "" {
		fmt.Fprintf(&b, "Its instructions:\n<instructions>\n%s\n</instructions>\n", clip(c.Prompt, maxPrompt))
	}
	fmt.Fprintf(&b, "\nThe message:\n<message>\n%s\n</message>", clip(message, maxMessage))
	ch, err := m.Provider.Stream(ctx, saigetypes.Request{Messages: []saigetypes.Message{
		saigetypes.SystemMsg(saigetypes.Text(question)),
		saigetypes.UserMsg(saigetypes.Text(b.String())),
	}})
	if err != nil {
		return Decision{}, err
	}
	var text strings.Builder
	var serr error
	for d := range ch {
		switch v := d.(type) {
		case saigetypes.PartDelta:
			text.WriteString(v.Text)
		case saigetypes.ErrorDelta:
			if serr == nil {
				serr = v.Error
			}
		}
	}
	if serr != nil {
		return Decision{}, serr
	}
	return parse(text.String())
}

// parse reads the first JSON object in a reply.
func parse(reply string) (Decision, error) {
	start := strings.Index(reply, "{")
	end := strings.LastIndex(reply, "}")
	if start < 0 || end < start {
		return Decision{}, fmt.Errorf("join: the answer held no JSON")
	}
	var v struct {
		Join   *bool  `json:"join"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(reply[start:end+1]), &v); err != nil {
		return Decision{}, fmt.Errorf("join: the answer could not be read: %w", err)
	}
	if v.Join == nil {
		return Decision{}, fmt.Errorf("join: the answer did not say whether to join")
	}
	return Decision{Join: *v.Join, Reason: clip(strings.TrimSpace(v.Reason), 300)}, nil
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
