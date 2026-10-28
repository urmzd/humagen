package legacy_test

import (
	"testing"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/internal/legacy"
)

// The journal stored the old envelope.Event with json.Marshal, so its
// []byte payload is base64.
func TestAnOldJournalRowDecodes(t *testing.T) {
	raw := []byte(`{"topic":"agent:a:r1","run_id":"r1","tenant":"t1","agent":"a","rev":2,"origin":"slack","kind":"delta.text","payload":"eyJ0ZXh0IjoiaGkifQ==","at":"2026-01-02T03:04:05Z","seq":3}`)
	e, origin, err := legacy.DecodeV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	if origin != "slack" || e.Type != event.TypeText || e.Run != "r1" || e.Seq != 3 {
		t.Fatalf("got %+v from %q", e, origin)
	}
	if txt, ok := e.Data.(event.Text); !ok || txt.Text != "hi" {
		t.Fatalf("data = %#v", e.Data)
	}
}
