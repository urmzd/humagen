package platform_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestOperationPublicationGuardIsInstalledAndOffersReviewedSummary(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		p, err := newOn(t, open(t))
		if err != nil {
			t.Fatal(err)
		}
		who := auth.Principal{Tenant: tenant, Subject: "ada", Kind: auth.User, Roles: []auth.Role{auth.Operator}}
		if _, err := p.Projects.Create(tenant, projects.Project{Name: "Field"}, projects.Writer{By: "ada"}, time.Now()); err != nil {
			t.Fatal(err)
		}
		th, _, err := p.Threads.Start(tenant, threads.Thread{Project: "Field", Channel: "general", Visibility: threads.Private}, threads.Message{By: "ada", Name: "Ada", Text: "PRIVATE-CONTRACT-CANARY"}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.Operations.SetAssignment(tenant, operations.Assignment{ID: "private-work", Agent: "helper", Project: "Field", Thread: th.ID, Owner: "ada", Goal: "PRIVATE-CONTRACT-CANARY", DoneWhen: "Review complete", State: operations.Requested}, 0, "ada", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		preview, err := p.PreviewPlacement(t.Context(), who, th.ID, "Field", "general")
		if err != nil || !preview.SummaryRequired {
			t.Fatalf("operation guard was not installed: %+v, %v", preview, err)
		}
		if _, err := p.Share(t.Context(), who, th.ID, platform.ThreadShare{PlacementToken: preview.Token}); err == nil {
			t.Fatal("raw contract publication bypassed review")
		}
		published, err := p.PublishSummary(t.Context(), who, th.ID, platform.SummaryPublication{Project: "Field", Channel: "general", Summary: "Reviewed public decision", PlacementToken: preview.Token})
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(published)
		if strings.Contains(string(data), "PRIVATE-CONTRACT") || strings.Contains(string(data), th.ID) {
			t.Fatal("summary exposed original provenance")
		}
		original, _ := p.Threads.Get(tenant, th.ID)
		if original.Visibility != threads.Private {
			t.Fatal("summary widened original assignment history")
		}
	})
}
