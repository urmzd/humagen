package platform

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/urmzd/orchagent/flow/runtime"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/threads"
)

// A person may pick, for one message, the model and reasoning effort the runs
// it starts use in place of their agents' own, from what the workspace's
// administrators offer (Settings.MessageChoices). Every agent's own chain is
// the default; a pick applies to every agent the message starts. For one
// release, a workspace that offers nothing falls back to what each agent's
// spec offered (wire.AgentSpec.Choices), which no longer has its own control.
//
//   - PostMessage and StartThread check the pick's shape and carry it to the
//     runs the message starts (withMessageChoice).
//   - answerMessage pins it to each run's tag under private keys, which flow
//     never shows a model, so every attempt reads the same pick.
//   - Each turn applies it (chooseForMessage) after the run's preferred
//     model and before its model policy and ceilings: a pick the agent does
//     not offer is passed over, and the run's ceilings still bound the
//     model it lands on.
const (
	metaChoiceModel  = "_humagen.choice.model"
	metaChoiceEffort = "_humagen.choice.effort"
)

// messageChoice is a person's pick for one message: a model as
// "provider/model", a reasoning effort, or both. The zero value picks
// nothing.
type messageChoice struct {
	Model  string
	Effort string
}

type messageChoiceKey struct{}

// checkMessageChoice refuses a pick that cannot name a model or an effort.
// Whether an agent offers it is decided per run, since one message may start
// several.
func checkMessageChoice(model, effort string) (messageChoice, error) {
	c := messageChoice{Model: strings.TrimSpace(model), Effort: strings.ToLower(strings.TrimSpace(effort))}
	if c.Model != "" {
		if provider, name, ok := strings.Cut(c.Model, "/"); !ok || provider == "" || name == "" || len(c.Model) > 200 {
			return messageChoice{}, fmt.Errorf("%w: model %q must be provider/model", threads.ErrInvalid, model)
		}
	}
	if c.Effort != "" {
		if len(c.Effort) > 32 || strings.TrimFunc(c.Effort, func(r rune) bool { return r >= 'a' && r <= 'z' }) != "" {
			return messageChoice{}, fmt.Errorf("%w: effort %q must be a word such as low or high", threads.ErrInvalid, effort)
		}
	}
	return c, nil
}

// withMessageChoice carries a message's pick to the runs it starts.
func withMessageChoice(ctx context.Context, c messageChoice) context.Context {
	if c == (messageChoice{}) {
		return ctx
	}
	return context.WithValue(ctx, messageChoiceKey{}, c)
}

// pinMessageChoice writes the pick ctx carries into a run's meta, and drops
// any pick the meta held: only the person's message picks.
func pinMessageChoice(ctx context.Context, meta map[string]string) {
	delete(meta, metaChoiceModel)
	delete(meta, metaChoiceEffort)
	c, _ := ctx.Value(messageChoiceKey{}).(messageChoice)
	if c.Model != "" {
		meta[metaChoiceModel] = c.Model
	}
	if c.Effort != "" {
		meta[metaChoiceEffort] = c.Effort
	}
}

// chooseForMessage puts the pick a run was pinned with in place of its
// agent's model and effort, each only when the workspace offers it (or, for
// a workspace that offers nothing yet, the agent's spec).
func (p *Platform) chooseForMessage(req runtime.Request) runtime.Request {
	allowsModel, allowsEffort := req.Policy.Choices.AllowsModel, req.Policy.Choices.AllowsEffort
	if w := p.Settings(req.Tenant).MessageChoices; w != nil {
		allowsModel = func(m string) bool { return slices.Contains(w.Models, m) }
		allowsEffort = func(e string) bool { return slices.Contains(w.Efforts, e) }
	}
	if m := req.Meta[metaChoiceModel]; m != "" && allowsModel(m) {
		provider, model, _ := strings.Cut(m, "/")
		req.Definition.Spec.Provider, req.Definition.Spec.Model = provider, model
	}
	if e := req.Meta[metaChoiceEffort]; e != "" && allowsEffort(e) {
		req.Definition.Spec.Effort = e
	}
	return req
}

// MessageEfforts are the reasoning efforts a workspace may offer for one
// message, lightest first.
func MessageEfforts() []string { return []string{"low", "medium", "high"} }

// MaxMessageModels bounds the models a workspace offers for one message.
const MaxMessageModels = 24

// checkMessageChoices holds the workspace's offer to what can run: each
// model "provider/model" on a provider an agent may name there, and each
// effort one of MessageEfforts. It sorts both lists and collapses an offer
// of nothing to nil.
func (p *Platform) checkMessageChoices(tenant string, c *wire.Choices) (*wire.Choices, error) {
	if c == nil {
		return nil, nil
	}
	out := &wire.Choices{}
	names := p.modelNames(tenant)
	for _, m := range c.Models {
		m = strings.TrimSpace(m)
		provider, model, ok := strings.Cut(m, "/")
		switch {
		case !ok || provider == "" || model == "" || len(m) > 200 || strings.ContainsAny(m, " \t\n"):
			return nil, fmt.Errorf("%w: message choice %q must be provider/model", ErrSettings, m)
		case !slices.Contains(names, provider):
			return nil, fmt.Errorf("%w: message choice %q names a provider this workspace does not have", ErrSettings, m)
		case slices.Contains(out.Models, m):
			continue
		}
		out.Models = append(out.Models, m)
	}
	if len(out.Models) > MaxMessageModels {
		return nil, fmt.Errorf("%w: a workspace offers at most %d models for one message", ErrSettings, MaxMessageModels)
	}
	for _, e := range c.Efforts {
		e = strings.ToLower(strings.TrimSpace(e))
		if !slices.Contains(MessageEfforts(), e) {
			return nil, fmt.Errorf("%w: a reasoning effort is one of %s", ErrSettings, strings.Join(MessageEfforts(), ", "))
		}
		if !slices.Contains(out.Efforts, e) {
			out.Efforts = append(out.Efforts, e)
		}
	}
	if len(out.Models) == 0 && len(out.Efforts) == 0 {
		return nil, nil
	}
	slices.Sort(out.Models)
	slices.SortFunc(out.Efforts, func(a, b string) int {
		return slices.Index(MessageEfforts(), a) - slices.Index(MessageEfforts(), b)
	})
	return out, nil
}
