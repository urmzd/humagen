package platform_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/runs"
	saigetypes "github.com/urmzd/saige/agent/types"
)

func TestRealPendingQuestionReminderUsesTheLedgerAndSurvivesRestart(t *testing.T) {
	eachBackend(t, func(t *testing.T, b platform.Backends, again func() platform.Backends) {
		base := time.Now().UTC()
		var clock atomic.Int64
		clock.Store(base.UnixNano())
		now := func() time.Time { return time.Unix(0, clock.Load()).UTC() }
		var messageID, runID string
		t.Run("first-process", func(t *testing.T) {
			p := stage(t, &cast{continues: true, says: map[string][][]saigetypes.Delta{"picker": {asking("Which direction?", "left", "right"), say("Reviewed.")}}, tools: map[string][]string{"picker": {"ask_person"}}}, platform.Config{Backends: b, Now: now})
			run := ask(t, p, "reminder-real", "picker")
			runID = run.ID
			message := questionFor(t, p, run.ID)
			messageID = message.ID
			if _, err := p.Inbox.Read(tenant, message.ID, "reader", true, now()); err != nil {
				t.Fatal(err)
			}
			off := 0
			if _, err := p.Operations.SetControl(tenant, "", operations.Control{ReminderAfterSeconds: &off}, 0, "owner", now()); err != nil {
				t.Fatal(err)
			}
			clock.Store(base.Add(4*time.Hour + time.Minute).UnixNano())
			time.Sleep(1200 * time.Millisecond)
			current, _ := p.Inbox.Get(tenant, message.ID, "reader")
			if current.Unread {
				t.Fatal("disabled reminder fired")
			}
			delay := 60
			if _, err := p.Operations.SetControl(tenant, "picker", operations.Control{ReminderAfterSeconds: &delay}, 0, "owner", now()); err != nil {
				t.Fatal(err)
			}
			within(t, "native reminder updated the pending question", func() bool {
				current, _ := p.Inbox.Get(tenant, message.ID, "reader")
				return current.Unread && current.UpdatedAt.Equal(now())
			})
			parked, err := p.Runs.Get(tenant, run.ID)
			if err != nil || parked.Status != runs.Parked {
				t.Fatal("reminder resumed or approved native work")
			}
		})
		restarted, stop := runningWith(t, platform.Config{Backends: again(), Now: now})
		defer stop()
		if _, err := restarted.Inbox.Read(tenant, messageID, "reader", true, now()); err != nil {
			t.Fatal(err)
		}
		clock.Store(base.Add(5 * time.Hour).UnixNano())
		time.Sleep(1200 * time.Millisecond)
		current, _ := restarted.Inbox.Get(tenant, messageID, "reader")
		if current.Unread {
			t.Fatal("restart repeated a delivered reminder")
		}
		pending := restarted.Inbox.OpenOf(tenant, inbox.KindClarification)
		if len(pending) != 1 || pending[0].Source.Run != runID {
			t.Fatal("reminder duplicated or replaced the native question")
		}
	})
}
