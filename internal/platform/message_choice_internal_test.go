package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/urmzd/orchagent/flow/definition"

	"github.com/urmzd/humagen/pkg/personctx"
	"github.com/urmzd/humagen/pkg/runctx"
	runsettings "github.com/urmzd/humagen/pkg/settings"
	"github.com/urmzd/humagen/pkg/threads"
)

// A message's pick is pinned to the runs it starts, applied where the agent
// offers it, passed over where it does not, and still bound by the run's
// ceilings, which drop an effort picked for a model they take away.
func TestAMessageChoiceAppliesWhereTheAgentOffersIt(t *testing.T) {
	pinned := func(c messageChoice) map[string]string {
		meta := map[string]string{metaChoiceModel: "stale/x"}
		pinMessageChoice(withMessageChoice(context.Background(), c), meta)
		return meta
	}
	if m := pinned(messageChoice{}); len(m) != 0 {
		t.Fatalf("no pick pinned %v; a pick the meta held must be dropped", m)
	}

	offers := &definition.Choices{Models: []string{"openai/gpt"}, Efforts: []string{"high"}}
	turn := func(ws runsettings.Layer, choices *definition.Choices, c messageChoice) (string, string, string, error) {
		req := requestWith(t, runctx.New("ada", runsettings.Resolve(ws), personctx.Context{}))
		for k, v := range pinned(c) {
			req.Meta[k] = v
		}
		req.Policy.Choices = choices
		req, err := preferRunContext(req)
		if err != nil {
			return "", "", "", err
		}
		req, err = boundRunContext(chooseForMessage(req))
		s := req.Definition.Spec
		return s.Provider, s.Model, s.Effort, err
	}

	pick := messageChoice{Model: "openai/gpt", Effort: "high"}
	if p, m, e, err := turn(runsettings.Layer{}, offers, pick); err != nil || p != "openai" || m != "gpt" || e != "high" {
		t.Fatalf("an offered pick ran on %s/%s at %q (%v)", p, m, e, err)
	}
	if p, m, e, _ := turn(runsettings.Layer{}, nil, pick); p != "anthropic" || m != "a" || e != "" {
		t.Fatalf("an agent that offers nothing ran on %s/%s at %q", p, m, e)
	}
	if p, m, e, _ := turn(runsettings.Layer{}, offers, messageChoice{Model: "openai/other", Effort: "low"}); p != "anthropic" || m != "a" || e != "" {
		t.Fatalf("a pick the agent does not offer ran on %s/%s at %q", p, m, e)
	}
	ceiling := runsettings.Layer{Ceilings: runsettings.Ceilings{Models: []string{"anthropic/*"}}}
	// A picked model outside the ceiling gives way to the agent's first
	// allowed fallback, without the effort picked for it.
	if p, m, e, err := turn(ceiling, offers, pick); err != nil || p != "anthropic" || m != "b" || e != "" {
		t.Fatalf("a pick outside the ceiling ran on %s/%s at %q (%v)", p, m, e, err)
	}
	if p, m, e, err := turn(ceiling, offers, messageChoice{Effort: "high"}); err != nil || p != "anthropic" || m != "a" || e != "high" {
		t.Fatalf("an effort alone ran on %s/%s at %q (%v)", p, m, e, err)
	}
	narrow := runsettings.Layer{Ceilings: runsettings.Ceilings{Models: []string{"anthropic/b"}}}
	if p, m, e, err := turn(narrow, offers, messageChoice{Effort: "high"}); err != nil || m != "b" || e != "" {
		t.Fatalf("a ceiling that swapped the model kept effort: %s/%s at %q (%v)", p, m, e, err)
	}
}

func TestCheckMessageChoiceRefusesWhatCannotBeApplied(t *testing.T) {
	if c, err := checkMessageChoice(" openai/gpt ", "High"); err != nil || c.Model != "openai/gpt" || c.Effort != "high" {
		t.Fatalf("checkMessageChoice = %+v, %v", c, err)
	}
	for _, bad := range [][2]string{{"gpt", ""}, {"/gpt", ""}, {"", "very-high"}, {"", "x2"}} {
		if _, err := checkMessageChoice(bad[0], bad[1]); !errors.Is(err, threads.ErrInvalid) {
			t.Errorf("checkMessageChoice(%q, %q) = %v, want ErrInvalid", bad[0], bad[1], err)
		}
	}
}
