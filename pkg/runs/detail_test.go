package runs

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestOutcomeAndCitationsSurviveRestartWithoutJournal(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		store := open(t)
		index, err := Open(store)
		if err != nil {
			t.Fatal(err)
		}
		record(t, index, "acme", "r1", "lead", OriginAPI, 0, nil)
		outcome := []byte(`{"kind":"recommendation","summary":"Renew the agreement.","decisions":["Keep the current term"],"next_action":"Confirm the owner"}`)
		action, _ := payload.Encode(payload.Action{Name: "record_outcome", Data: json.RawMessage(outcome)})
		index.Observe(envelope.Event{RunID: "r1", Tenant: "acme", Kind: envelope.KindActionTaken, Payload: action, At: t0.Add(time.Second)})
		citation, _ := payload.Encode(payload.Citation{Label: "[1]", Text: "The source passage", Source: payload.Provenance{DocumentTitle: "Agreement", SourceURI: "https://example.test/agreement"}})
		index.Observe(envelope.Event{RunID: "r1", Tenant: "acme", Kind: envelope.KindCitation, Payload: citation, At: t0.Add(time.Second)})
		end(t, index, "acme", "r1", 2*time.Second, envelope.KindCompleted, payload.Lifecycle{Text: "Renew it. [[1]]"})
		if err := index.Flush(); err != nil {
			t.Fatal(err)
		}
		again, err := Open(store)
		if err != nil {
			t.Fatal(err)
		}
		got, err := again.Get("acme", "r1")
		if err != nil {
			t.Fatal(err)
		}
		if got.Outcome == nil || got.Outcome.Summary != "Renew the agreement." || len(got.Citations) != 1 || got.Citations[0].Title != "Agreement" {
			t.Fatalf("lost outcome/evidence: %+v", got)
		}
	})
}

func TestOutcomeAndEvidenceProjectionIsBoundedAndTenantConfined(t *testing.T) {
	index := NewIndex()
	record(t, index, "acme", "r1", "lead", OriginAPI, 0, nil)
	for i := range 100 {
		citation, _ := payload.Encode(payload.Citation{Label: fmt.Sprintf("[%d]", i), Text: "source"})
		index.Observe(envelope.Event{RunID: "r1", Tenant: "acme", Kind: envelope.KindCitation, Payload: citation, At: t0})
	}
	bad, _ := payload.Encode(payload.Action{Name: "record_outcome", Data: json.RawMessage(`{"kind":"executed","summary":"I approved myself"}`)})
	index.Observe(envelope.Event{RunID: "r1", Tenant: "acme", Kind: envelope.KindActionTaken, Payload: bad, At: t0})
	good, _ := payload.Encode(payload.Action{Name: "record_outcome", Data: json.RawMessage(`{"kind":"recommendation","summary":"hidden"}`)})
	index.Observe(envelope.Event{RunID: "r1", Tenant: "other", Kind: envelope.KindActionTaken, Payload: good, At: t0})
	got, _ := index.Get("acme", "r1")
	if got.Outcome != nil || len(got.Citations) != MaxCitations {
		t.Fatalf("projection bounds: %+v", got)
	}
}
