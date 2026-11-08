package platform_test

import (
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestAnalystAlertReportKeepsPrivateSessionTitlesOutOfSharedAnswers(t *testing.T) {
	p := start(t, call.Limits{})
	project(t, p, "Field")
	run, _ := turn(t, p, "api-alert-reader", "Field")
	privateID := "s_" + strings.Repeat("a", 32)
	sharedID := "s_" + strings.Repeat("b", 32)
	childID := "s_" + strings.Repeat("c", 32)
	missingID := "s_" + strings.Repeat("d", 32)
	for _, th := range []threads.Thread{
		{ID: privateID, Visibility: threads.Private, Session: &threads.SessionIdentity{Agent: "lead", Parent: "console://ana"}},
		{ID: sharedID, Visibility: threads.Shared, Session: &threads.SessionIdentity{Agent: "lead", Parent: "console://shared"}},
		{ID: childID, Visibility: threads.Shared, Session: &threads.SessionIdentity{Agent: "lead", Parent: privateID}},
	} {
		th.Project, th.Channel, th.CreatedBy, th.Default = "Field", "general", "ana", "lead"
		th.Agents = []string{"lead"}
		th.Title = "A session"
		if _, err := p.Threads.Create(tenant, th, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	for id, title := range map[string]string{
		privateID: "PRIVATE-DECISION",
		childID:   "PRIVATE-ANCESTOR-DECISION",
		missingID: "MISSING-SESSION-DECISION",
		sharedID:  "Shared release decision",
	} {
		// Person-created alerts have no run. The session is their authority.
		if _, err := p.Alerts.CreateSession(tenant, alerts.Alert{Session: id, Project: "Field", Title: title, Priority: alerts.Normal, At: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := use(t, p, as(run, platform.BuiltinAnalyst), platform.ToolListAlerts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "PRIVATE") || strings.Contains(out, "MISSING") || !strings.Contains(out, "Shared release decision") {
		t.Fatalf("session alert report crossed its answer audience: %s", out)
	}
}
