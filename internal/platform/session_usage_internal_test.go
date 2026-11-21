package platform

import (
	"fmt"
	"testing"

	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/saige/flow/event"
)

func usd(m int64, in, out int) event.Usage {
	return event.Usage{ModelCalls: 1, PromptTokens: in, CompletionTokens: out, Cost: &event.Cost{Micros: m, Currency: "USD"}}
}

// A run called by another counted run is already in its caller's total.
func TestFoldUsageDoesNotCountACalledRunTwice(t *testing.T) {
	lead := usd(300, 100, 10)
	kid := usd(200, 50, 5)
	both := lead.Add(kid)
	rr := []runs.Run{
		{ID: "a", Agent: "lead", Status: runs.Completed, Usage: &event.RunUsage{Own: lead, Children: kid, Total: both, Context: &event.ContextUsage{Model: "m", Window: 10, Tokens: 4}}},
		{ID: "b", Agent: "kid", Parent: "a", Status: runs.Completed, Usage: &event.RunUsage{Own: kid, Total: kid}},
		{ID: "c", Agent: "zed", Status: runs.Running, Usage: &event.RunUsage{Own: usd(200, 20, 0), Total: usd(200, 20, 0)}},
		{ID: "d", Agent: "abe", Status: runs.Running, Usage: &event.RunUsage{Own: usd(200, 20, 0), Total: usd(200, 20, 0)}},
		{ID: "e", Agent: "nousage", Status: runs.Accepted},
	}
	got := foldUsage(rr)
	if got.Runs != 5 || got.Working != 3 || got.Truncated {
		t.Fatalf("counts = %+v", got)
	}
	if got.Usage.Cost.Micros != 900 || got.Usage.ModelCalls != 4 || got.Usage.PromptTokens != 190 {
		t.Fatalf("total = %+v", got.Usage)
	}
	var names []string
	for _, a := range got.Agents {
		names = append(names, a.Agent)
	}
	// 300 first; 200 and 200 tie, then 200 (kid) has 55 tokens against 20 each; abe before zed by name.
	if fmt.Sprint(names) != "[lead kid abe zed]" {
		t.Fatalf("order = %v", names)
	}
	if got.Context == nil || got.Context.Run != "a" || got.Context.Agent != "lead" || got.Context.Tokens != 4 {
		t.Fatalf("context = %+v", got.Context)
	}
}

func TestFoldUsageCapsAgentsAndFlagsTruncation(t *testing.T) {
	var rr []runs.Run
	for i := range runs.MaxTree {
		rr = append(rr, runs.Run{ID: fmt.Sprint(i), Agent: fmt.Sprintf("a%02d", i%30), Status: runs.Completed, Usage: &event.RunUsage{Own: usd(1, 1, 1), Total: usd(1, 1, 1)}})
	}
	got := foldUsage(rr)
	if len(got.Agents) != MaxSessionUsageAgents || !got.Truncated {
		t.Fatalf("agents=%d truncated=%v", len(got.Agents), got.Truncated)
	}
	if z := foldUsage(nil); z.Runs != 0 || z.Agents == nil || z.Context != nil {
		t.Fatalf("empty = %+v", z)
	}
}
