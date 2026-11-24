package wire

import (
	"encoding/json"
	"testing"

	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/thread"
)

// A run's request is read from its ledger input: its thread, the request as
// it was asked, and whether a journaled text is that request with the
// passages a turn retrieved before it.
func TestRunRequestOfReadsTheRequestAsItWasAsked(t *testing.T) {
	for name, context := range map[string]map[string]string{"with trigger context": {"pr": "7", "_hidden": "x"}, "without": nil} {
		t.Run(name, func(t *testing.T) {
			tag := NewEvent(Raise{ID: "e1", Tenant: "acme", Origin: "api", Agent: "docs", Text: "what changed?", Context: context})
			tag.Source = "slack://T1/C1?thread=1.2"
			raw, err := json.Marshal(runtime.Input{Trigger: tag, Subscriber: "docs", Run: runtime.RunID("acme", "e1")})
			if err != nil {
				t.Fatal(err)
			}
			r, err := RunRequestOf(raw)
			if err != nil {
				t.Fatal(err)
			}
			want, err := event.Render(tag)
			if err != nil {
				t.Fatal(err)
			}
			if r.Asked != want || r.Thread != thread.KeyOf(tag, "docs") {
				t.Fatalf("request = %q in %+v, want %q", r.Asked, r.Thread, want)
			}
			text, ctx, _ := event.Prompt(tag.Data)
			for journaled, with := range map[string]bool{
				event.RequestText("Passages.\n\n"+text, ctx): true,
				event.RequestText(" \n\n"+text, ctx):         false,
				want:                                         false,
				"Passages.\n\n" + text + "!":                 false,
				"another question":                           false,
			} {
				if got := r.WithPassages(journaled); got != with {
					t.Errorf("WithPassages(%q) = %v, want %v", journaled, got, with)
				}
			}
		})
	}
	if _, err := RunRequestOf([]byte("{")); err == nil {
		t.Fatal("an unreadable input was read")
	}
}
