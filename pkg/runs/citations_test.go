package runs

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestCitationRetentionUsesOnlyTrustedMetadataAndWritesThrough(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := statetest.NewFaulty(open(t))
		x, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		record(t, x, "acme", "r1", "lead", "api", 0, nil)
		raw := json.RawMessage(`{"label":"[1]","text":"actual text","source":{"document_title":"Runbook","source_uri":"https://example.test"},"mode":"live","refresh_failed":true}`)
		event := envelope.Event{Tenant: "acme", RunID: "r1", Kind: envelope.KindCitation, Payload: raw}
		if err := x.RecordCitation(event, nil); err != nil {
			t.Fatal(err)
		}
		got, _ := x.Get("acme", "r1")
		if len(got.Citations) != 1 || got.Citations[0].Mode != "" || got.Citations[0].RefreshFailed != nil {
			t.Fatal("raw payload forged retrieval facts", got.Citations)
		}
		at := time.Now()
		meta := &CitationMetadata{Mode: "indexed", CopyAt: &at, RefreshFailed: true}
		st.Fail(statetest.ErrInjected)
		if err := x.RecordCitation(event, meta); !errors.Is(err, statetest.ErrInjected) {
			t.Fatal(err)
		}
		got, _ = x.Get("acme", "r1")
		if got.Citations[0].Mode != "" {
			t.Fatal("failed write published facts")
		}
		st.Fail(nil)
		if err := x.RecordCitation(event, meta); err != nil {
			t.Fatal(err)
		}
		if err := x.RecordCitation(event, nil); err != nil {
			t.Fatal(err)
		}
		replay, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		got, _ = replay.Get("acme", "r1")
		if got.Citations[0].Mode != "indexed" || !got.Citations[0].CopyAt.Equal(at) || !*got.Citations[0].RefreshFailed {
			t.Fatal("trusted facts lost without flush", got.Citations)
		}
		*got.Citations[0].RefreshFailed = false
		untouched, _ := replay.Get("acme", "r1")
		if !*untouched.Citations[0].RefreshFailed {
			t.Fatal("read mutated citation")
		}
	})
}
