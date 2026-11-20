package journal_test

import (
	"github.com/urmzd/humagen/pkg/journal"
	"github.com/urmzd/orchagent/flow/event"
	"testing"
	"time"
)

func TestFeedbackObservationsHaveDistinctIdentityFromExecution(t *testing.T) {
	j := journal.New(20)
	e := event.Envelope{ID: "one", Tenant: "t", Run: "r", Type: event.TypeFeedback, Source: "humagen://feedback", Channel: "humagen://t", Time: time.Now(), Data: event.Feedback{RunID: "r", Rating: "up"}}
	for _, id := range []string{"one", "two", "one"} {
		e.ID = id
		if err := j.Append(e, "slack"); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := j.Events("t", []string{"r"})
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
}
