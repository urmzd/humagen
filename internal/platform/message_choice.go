package platform

import (
	"context"
	"fmt"
	"strings"

	"github.com/urmzd/saige/flow/runtime"

	"github.com/urmzd/humagen/pkg/threads"
)

// A person may pick, for one message, the model and reasoning effort the runs
// it starts use in place of their agents' own, from what each agent's spec
// offers (wire.AgentSpec.Choices).
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
// agent's model and effort, each only when the agent's spec offers it.
func chooseForMessage(req runtime.Request) runtime.Request {
	choices := req.Policy.Choices
	if choices == nil {
		return req
	}
	if m := req.Meta[metaChoiceModel]; m != "" && choices.AllowsModel(m) {
		provider, model, _ := strings.Cut(m, "/")
		req.Definition.Spec.Provider, req.Definition.Spec.Model = provider, model
	}
	if e := req.Meta[metaChoiceEffort]; e != "" && choices.AllowsEffort(e) {
		req.Definition.Spec.Effort = e
	}
	return req
}
