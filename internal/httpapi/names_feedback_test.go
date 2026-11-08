package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/orchagent/flow/event"
)

func TestShownFeedbackPreservesSystemEvidenceAndSanitizesPeople(t *testing.T) {
	f := newFixture(t)
	for _, actor := range []event.Actor{
		{ID: "system:github:commit:" + strings.Repeat("a", 40), Bot: true, Display: "Verified commit evidence"},
		{ID: "person@example.com", User: "person@example.com", Display: "person@example.com"},
	} {
		body, err := json.Marshal(event.Feedback{RunID: "r", Actor: actor, Rating: "up"})
		if err != nil {
			t.Fatal(err)
		}
		shown := shownEvent(f.p.Names("acme"), platform.APIEvent{Type: event.TypeFeedback, Data: body})
		var got event.Feedback
		if err := json.Unmarshal(shown.Data, &got); err != nil {
			t.Fatal(err)
		}
		if actor.Bot && got.Actor != actor {
			t.Fatalf("system observation lost attribution: %+v", got.Actor)
		}
		if !actor.Bot && strings.Contains(string(shown.Data), "person@example.com") {
			t.Fatalf("person address leaked: %s", shown.Data)
		}
	}
}
