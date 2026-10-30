package platform

import (
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/usage"
)

// A run a quota refused is refused in the words of the policy that set it,
// and held for approval when that policy asks for it.
func TestAQuotaRefusalIsThePolicys(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	project := policy.Scope{Kind: policy.Project, Name: "Launch"}
	pols := []policy.Policy{
		{Scope: policy.Scope{Kind: policy.Tenant}, Period: policy.Day, MaxRuns: 10, Action: policy.Block},
		{Scope: project, Period: policy.Month, MaxConcurrent: 2, Action: policy.Approve},
	}
	quotas := quotasOf(pols, now)
	if len(quotas) != 2 || quotas[0].Kind != usage.QuotaRuns || !quotas[0].Since.Equal(policy.PeriodStart(policy.Day, now)) || quotas[1].Kind != usage.QuotaConcurrent || quotas[1].Max != 2 {
		t.Fatalf("quotas = %+v", quotas)
	}
	applied := []policy.Applied{{Policy: pols[0]}, {Policy: pols[1]}}
	d := fullDecision(applied, quotas[0], 0)
	if d.Verdict != policy.Deny || d.Limit != policy.LimitRuns || d.Policy == nil || d.Policy.Scope != pols[0].Scope || !strings.Contains(d.Reason, "10 runs") {
		t.Fatalf("runs: %+v", d)
	}
	d = fullDecision(applied, quotas[1], 0)
	if d.Verdict != policy.NeedsApproval || d.Limit != policy.LimitConcurrent || d.Policy.Scope != project {
		t.Fatalf("concurrent: %+v", d)
	}
	// A policy changed since the quota was taken still refuses.
	if d := fullDecision(nil, quotas[0], 0); d.Verdict != policy.Deny || d.Limit != policy.LimitRuns {
		t.Fatalf("changed: %+v", d)
	}
}
