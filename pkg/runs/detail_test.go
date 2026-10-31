package runs

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/fiber/pkg/event"
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
		action, _ := json.Marshal(event.Action{Name: "record_outcome", Data: json.RawMessage(outcome)})
		index.Observe(event.Envelope{Run: "r1", Tenant: "acme", Type: event.TypeAction, Data: wire.BodyOf(event.TypeAction, action), Time: t0.Add(time.Second)})
		citation, _ := json.Marshal(event.Citation{Label: "[1]", Text: "The source passage", Source: event.Provenance{DocumentTitle: "Agreement", SourceURI: "https://example.test/agreement"}})
		if err := index.RecordCitation(event.Envelope{Run: "r1", Tenant: "acme", Type: event.TypeCitation, Data: wire.BodyOf(event.TypeCitation, citation), Time: t0.Add(time.Second)}, nil); err != nil {
			t.Fatal(err)
		}
		end(t, index, "acme", "r1", 2*time.Second, event.TypeCompleted, wire.Lifecycle{Text: "Renew it. [[1]]"})
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
		citation, _ := json.Marshal(event.Citation{Label: fmt.Sprintf("[%d]", i), Text: "source"})
		if err := index.RecordCitation(event.Envelope{Run: "r1", Tenant: "acme", Type: event.TypeCitation, Data: wire.BodyOf(event.TypeCitation, citation), Time: t0}, nil); err != nil {
			t.Fatal(err)
		}
	}
	bad, _ := json.Marshal(event.Action{Name: "record_outcome", Data: json.RawMessage(`{"kind":"executed","summary":"I approved myself"}`)})
	index.Observe(event.Envelope{Run: "r1", Tenant: "acme", Type: event.TypeAction, Data: wire.BodyOf(event.TypeAction, bad), Time: t0})
	good, _ := json.Marshal(event.Action{Name: "record_outcome", Data: json.RawMessage(`{"kind":"recommendation","summary":"hidden"}`)})
	index.Observe(event.Envelope{Run: "r1", Tenant: "other", Type: event.TypeAction, Data: wire.BodyOf(event.TypeAction, good), Time: t0})
	got, _ := index.Get("acme", "r1")
	if got.Outcome != nil || len(got.Citations) != MaxCitations {
		t.Fatalf("projection bounds: %+v", got)
	}
}
