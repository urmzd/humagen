package platform_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/orchagent/flow/event"
	turnruntime "github.com/urmzd/orchagent/flow/turn"
)

func TestSetupDryRunStaysOutOfHistoryAndFailureAlerts(t *testing.T) {
	engine := turnruntime.Func(func(context.Context, turnruntime.Pinned, turnruntime.Input, func(context.Context, event.Body) error) (turnruntime.Result, error) {
		return turnruntime.Result{}, errors.New("fixture: unavailable provider")
	})
	p := startWith(t, platform.Config{Runtime: engine})
	r, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "dry-test", Agent: "lead", Text: "Try this setup", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	completed := await(t, p, r.ID, runs.Failed)
	if !platform.IsEvaluation(completed) {
		t.Fatal("setup test lost evaluation marker")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		pending, err := p.Runs.PendingCompletions()
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(pending, func(v runs.Run) bool { return v.ID == r.ID }) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("completion did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	page, _, err := p.ThreadPage(tenant, runs.ThreadQuery{})
	if err != nil || len(page) != 0 {
		t.Fatalf("setup test appeared as work: %+v %v", page, err)
	}
	if messages := p.Inbox.OpenOf(tenant, inbox.KindFailure); len(messages) != 0 {
		t.Fatalf("setup test raised failure: %+v", messages)
	}
	if alerts, err := p.Alerts.Alerts(tenant); err != nil || len(alerts) != 0 {
		t.Fatalf("setup test raised alert: %+v %v", alerts, err)
	}
}

func TestConfiguredAgentHasReadySetupViewWithoutWizardOwnership(t *testing.T) {
	p := startWith(t, platform.Config{})
	who := auth.Principal{Tenant: tenant, Subject: "owner", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	view, err := p.AgentSetupView(who, "lead")
	if err != nil || view.Phase != "ready" || view.Revision != 1 || view.Plan.Spec.Name != "lead" || view.Owner != "" {
		t.Fatalf("configured agent setup: %+v %v", view, err)
	}
	if _, err = p.AgentSetup(who, "lead"); !errors.Is(err, operations.ErrNotFound) {
		t.Fatalf("read projection granted wizard mutation access: %v", err)
	}
	who.Tenant = "other"
	if _, err = p.AgentSetupView(who, "lead"); !errors.Is(err, operations.ErrNotFound) {
		t.Fatalf("other tenant saw setup: %v", err)
	}
}
