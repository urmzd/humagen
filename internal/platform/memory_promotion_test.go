package platform_test

import (
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestMemoryPromotionNeedsExplicitReviewAndDropsPrivateProvenance(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		p, err := platform.New(platform.Config{Workspace: t.TempDir(), Backends: platform.Backends{State: st}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.Close() })
		who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
		if _, err := p.CreateProject(t.Context(), tenant, govern, platform.ProjectInput{Name: "Destination"}); err != nil {
			t.Fatal(err)
		}
		private, err := p.Memory.AddDetailed(tenant, memory.PersonalOwner(who.Subject), "Review quarterly goals", memory.Consolidated, memory.Approved, "private-run-id", time.Now(), memory.Details{Kind: memory.Decision})
		if err != nil {
			t.Fatal(err)
		}
		in := platform.MemoryPromotion{Scope: "personal", ID: private.ID, Destination: "Destination"}
		for _, scope := range []string{"agent", "project", "workspace"} {
			bad := in
			bad.Scope = scope
			bad.Owner = memory.PersonalOwner(who.Subject)
			if _, err := p.PromoteMemory(who, bad); err == nil {
				t.Fatal("scope confusion disclosed private note")
			}
		}
		other := who
		other.Subject = "other"
		if _, err := p.PromoteMemory(other, in); !errors.Is(err, memory.ErrNotFound) {
			t.Fatalf("other owner: %v", err)
		}
		copied, err := p.PromoteMemory(who, in)
		if err != nil {
			t.Fatal(err)
		}
		if copied.Status != memory.Pending || copied.Source != memory.Person || copied.By != who.Subject || copied.Kind != memory.Decision || copied.ID == private.ID {
			t.Fatalf("unsafe copy: %+v", copied)
		}
		if len(p.Memory.List(tenant, memory.PersonalOwner(who.Subject))) != 1 {
			t.Fatal("promotion moved original note")
		}
		if _, err := p.PromoteMemory(who, in); err == nil {
			t.Fatal("duplicate promotion accepted")
		}
		if got := p.Memory.PromptAt(tenant, memory.ProjectOwner("Destination"), "Context", time.Now()); got != "" {
			t.Fatal("unreviewed child learning reached destination")
		}

		replay, err := memory.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if got := replay.List(tenant, memory.ProjectOwner("Destination")); len(got) != 1 || got[0].Status != memory.Pending || got[0].ID != copied.ID {
			t.Fatal("pending review lost on replay")
		}
		for _, status := range []memory.Status{memory.Pending, memory.Approved} {
			at := time.Now().Add(-2 * time.Hour)
			details := memory.Details{}
			if status == memory.Approved {
				details.ExpiresAt = at.Add(time.Hour)
			}
			n, err := p.Memory.AddDetailed(tenant, memory.PersonalOwner(who.Subject), "Candidate "+string(status), memory.Person, status, who.Subject, at, details)
			if err != nil {
				t.Fatal(err)
			}
			bad := in
			bad.ID = n.ID
			if _, err := p.PromoteMemory(who, bad); !errors.Is(err, memory.ErrInvalid) {
				t.Fatalf("pending/expired note promoted: %v", err)
			}
		}
		if _, err := p.Memory.Approve(tenant, memory.ProjectOwner("Destination"), []string{copied.ID}); err != nil {
			t.Fatal(err)
		}
		if got := p.Memory.PromptAt(tenant, memory.ProjectOwner("Destination"), "Context", time.Now()); got == "" {
			t.Fatal("explicit destination review did not activate copied decision")
		}
	})
}
