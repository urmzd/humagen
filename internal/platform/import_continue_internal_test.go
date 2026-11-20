package platform

import (
	"errors"
	"testing"

	"github.com/urmzd/saige/flow/event"

	"github.com/urmzd/humagen/pkg/threads"
)

// A continuation's run is refused as it starts when the workspace has since
// stopped sending agents earlier conversation, whatever let the message in:
// it never runs without what the continuation carries. Another thread's
// run is not held to it.
func TestAContinuationsRunIsRefusedAsItStartsWithNoHistory(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	cfg := p.Settings("acme")
	cfg.History = HistoryNone
	if err := p.SetSettings(t.Context(), "acme", cfg); err != nil {
		t.Fatal(err)
	}
	th := threads.Thread{ID: "th-generated", Project: "Field", Channel: "general", Import: &threads.Import{Source: "sessions-in-field"}}
	m := threads.Message{ID: "m-1", By: "okta|opal", Text: "@coder go on"}
	started, err := p.answerDirect(t.Context(), "acme", th, m, "", "coder", nil, "", event.Actor{ID: "okta|opal", User: "okta|opal"}, "generated-tag", false, nil)
	if !errors.Is(err, ErrImportNoHistory) || len(started) != 0 {
		t.Fatalf("started %d runs, err %v", len(started), err)
	}
	if got := refusalOf(err); got != noHistoryWords {
		t.Fatalf("a person is told %q", got)
	}
	th.Import = nil
	if _, err := p.answerDirect(t.Context(), "acme", th, m, "", "coder", nil, "", event.Actor{ID: "okta|opal", User: "okta|opal"}, "generated-tag", false, nil); errors.Is(err, ErrImportNoHistory) {
		t.Fatal("a plain thread was refused for a continuation's reason")
	}
}
