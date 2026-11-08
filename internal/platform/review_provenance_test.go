package platform

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	presentation "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/session"
)

func reviewFixture(t *testing.T) (*Platform, runs.Run, auth.Principal, time.Time) {
	t.Helper()
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if _, err := p.Agents.Create(t.Context(), "t", wire.AgentSpec{Name: "reviewer", Provider: "offline", Model: "offline", SystemPrompt: "Review recorded evidence"}, "test"); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(-time.Minute)
	tag := wire.NewEvent(wire.Raise{ID: "review", Tenant: "t", Agent: "reviewer", Origin: runs.OriginAPI, Text: "Review", At: at})
	wire.SetGitHubHeadSHA(&tag, strings.Repeat("a", 40))
	id := runtime.RunID("t", tag.ID)
	if err := p.Runs.Record(id, tag, 1); err != nil {
		t.Fatal(err)
	}
	run, _ := p.Runs.Get("t", id)
	return p, run, auth.Principal{Tenant: "t", Subject: "owner", Kind: auth.User, Roles: []auth.Role{auth.Admin}}, at
}

func reviewReport(t *testing.T, evidence ...presentation.ReviewEvidence) string {
	t.Helper()
	r := presentation.ReviewReport{Format: presentation.ReviewFormatV2, Title: "Review", Verdict: "comment", Scope: &presentation.ReviewScope{Commit: strings.Repeat("a", 40)}, Summary: "Inspected the changed paths", Findings: []presentation.ReviewFinding{{ID: "f1", Severity: "warning", Title: "Check input", Suggestion: "Validate input", Prompt: "Fix validation", Evidence: evidence}}}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func reviewAppend(t *testing.T, p *Platform, run runs.Run, seq uint64, at time.Time, body event.Body) event.Envelope {
	t.Helper()
	e := event.Envelope{ID: run.ID + ":" + strconv.FormatUint(seq, 10), Tenant: run.Tenant, Run: run.ID, Agent: run.Agent, Seq: seq, Time: at, Type: body.Type(), Data: body}
	if err := p.Journal.Append(e, runs.OriginAPI); err != nil {
		t.Fatal(err)
	}
	return e
}

func evidenceOf(t *testing.T, text string) []presentation.ReviewEvidence {
	t.Helper()
	var report presentation.ReviewReport
	if err := json.Unmarshal([]byte(text), &report); err != nil {
		t.Fatalf("invalid projected report %q: %v", text, err)
	}
	return report.Findings[0].Evidence
}

func TestReviewProvenanceChecksRecordedStepSourceAndHistoricalCutoff(t *testing.T) {
	p, run, who, at := reviewFixture(t)
	reviewAppend(t, p, run, 1, at.Add(time.Second), event.ToolDone{ID: "real", Name: "test", Result: "src/input.go failed validation"})
	text := reviewReport(t,
		presentation.ReviewEvidence{Step: "real", Source: "src/input.go", Observation: "Recorded test failed"},
		presentation.ReviewEvidence{Step: "invented", Observation: "Claimed test failed"},
		presentation.ReviewEvidence{Step: "real", Source: "secret/unread.go", Observation: "Claimed another source"},
		presentation.ReviewEvidence{Step: "later", Observation: "Claimed future evidence"},
	)
	text = strings.Replace(text, `"step":"invented"`, `"verified":true,"step":"invented"`, 1)
	completed := reviewAppend(t, p, run, 2, at.Add(2*time.Second), event.Completed{Text: text})
	p.Runs.Observe(completed)
	// Equal wall-clock timestamps still cannot promote a later journal step.
	reviewAppend(t, p, run, 3, completed.Time, event.ToolDone{ID: "later", Name: "test", Result: "later"})
	run, _ = p.Runs.Get(run.Tenant, run.ID)
	shown := p.ShownRunAs(who, run)
	evidence := evidenceOf(t, shown.Answer)
	if evidence[0].Step != "real" {
		t.Fatal("recorded step lost", evidence)
	}
	for _, e := range evidence[1:] {
		if e.Step != "" || e.Session != "" {
			t.Fatal("unproven reference retained", e)
		}
	}
	if strings.Contains(shown.Answer, "verified") && strings.Contains(shown.Answer, `"verified"`) {
		t.Fatal("model verification flag survived")
	}
	api, ok := p.ShownAPI(who, completed)
	if !ok {
		t.Fatal("completed event unavailable")
	}
	var body event.Completed
	if err := json.Unmarshal(api.Data, &body); err != nil {
		t.Fatal(err)
	}
	if got := evidenceOf(t, body.Text); got[1].Step != "" || got[3].Step != "" {
		t.Fatal("event API promoted fabricated evidence", got)
	}
	foreign := who
	foreign.Tenant = "foreign"
	if got := p.reviewText(t.Context(), run, text, &foreign, time.Time{}); got != reviewEvidenceUnavailable {
		t.Fatal("foreign reader retained proof")
	}
	wrong := strings.Replace(text, strings.Repeat("a", 40), strings.Repeat("b", 40), 1)
	if got := p.reviewText(t.Context(), run, wrong, &who, time.Time{}); got != reviewNeedsCorrection {
		t.Fatal("unpinned scope displayed", got)
	}
}

func TestReviewProvenanceDoesNotCertifyPrivateSourceEvidence(t *testing.T) {
	p, run, who, at := reviewFixture(t)
	reviewAppend(t, p, run, 1, at.Add(time.Second), event.ToolDone{ID: "search", Name: "knowledge_search", Result: "private result"})
	_, err := p.SourceEvidence.RecordQuery("t", run.ID, "query", []knowledge.CitationEvidence{{Provider: "drive", Source: "private", Person: "owner", DocumentID: "doc", Text: "private result", Mode: knowledge.Live}})
	if err != nil {
		t.Fatal(err)
	}
	text := reviewReport(t, presentation.ReviewEvidence{Step: "search", Observation: "Claim"})
	reviewAppend(t, p, run, 2, at.Add(2*time.Second), event.Completed{Text: text})
	for _, reader := range []*auth.Principal{&who, nil} {
		if got := p.reviewText(t.Context(), run, text, reader, time.Time{}); got != reviewEvidenceUnavailable {
			t.Fatal("private source proof promoted", got)
		}
	}
}

func TestReviewProvenancePublicationWaitsForRecordedCompletion(t *testing.T) {
	p, run, _, at := reviewFixture(t)
	text := reviewReport(t, presentation.ReviewEvidence{Step: "real", Observation: "Recorded failure"})
	completed := event.Envelope{Tenant: run.Tenant, Run: run.ID, Seq: 2, Agent: run.Agent, Type: event.TypeCompleted, Time: at.Add(2 * time.Second), Data: event.Completed{Text: text}}
	if _, err := p.reviewPublication(t.Context(), completed); err == nil {
		t.Fatal("publication outran journal")
	}
	reviewAppend(t, p, run, 1, at.Add(time.Second), event.ToolDone{ID: "real", Name: "test", Result: "failure"})
	if _, err := p.reviewPublication(t.Context(), completed); err == nil {
		t.Fatal("tool result alone authorized publication")
	}
	reviewAppend(t, p, run, 2, completed.Time, completed.Data)
	shown, err := p.reviewPublication(t.Context(), completed)
	if err != nil {
		t.Fatal(err)
	}
	if got := evidenceOf(t, shown.Data.(event.Completed).Text); got[0].Step != "real" {
		t.Fatal("observer lag degraded valid proof", got)
	}
}

func TestReviewProvenanceAcceptsOnlyItsCompletedChildSession(t *testing.T) {
	p, _, who, at := reviewFixture(t)
	parentKey := session.Key{Tenant: "t", Parent: "console://owner", Agent: "reviewer"}
	childKey := session.Key{Tenant: "t", Parent: parentKey.ID(), Agent: "reviewer"}
	for _, key := range []session.Key{parentKey, childKey} {
		_, err := p.Threads.Create("t", threads.Thread{ID: key.ID(), Channel: "general", Visibility: threads.Shared, CreatedBy: "owner", Default: "reviewer", Agents: []string{"reviewer"}, Session: &threads.SessionIdentity{Agent: "reviewer", Parent: key.Parent}}, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	// A fresh root pins the canonical parent session without mutating history.
	tag := wire.NewEvent(wire.Raise{ID: "session-review", Tenant: "t", Agent: "reviewer", Origin: runs.OriginAPI, Text: "Review", At: at, Context: map[string]string{wire.MetaSession: parentKey.ID()}})
	wire.SetGitHubHeadSHA(&tag, strings.Repeat("a", 40))
	if err := p.Runs.Record(runtime.RunID("t", tag.ID), tag, 1); err != nil {
		t.Fatal(err)
	}
	root, _ := p.Runs.Get("t", runtime.RunID("t", tag.ID))
	childTag, err := call.New(tag, call.Request{Key: "helper", Agent: "reviewer", Text: "Check", Mode: call.Delegate}, call.State{}, call.Limits{MaxDepth: 2, MaxFanOut: 2}, at.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	wire.SetMeta(&childTag, wire.MetaSession, childKey.ID())
	if err := p.Runs.Record(runtime.RunID("t", childTag.ID), childTag, 1); err != nil {
		t.Fatal(err)
	}
	child, _ := p.Runs.Get("t", runtime.RunID("t", childTag.ID))
	reviewAppend(t, p, child, 1, at.Add(2*time.Second), event.ToolDone{ID: "child-test", Name: "test", Result: "failure"})
	reviewAppend(t, p, child, 2, at.Add(3*time.Second), event.Completed{Text: "Checked input"})
	text := reviewReport(t, presentation.ReviewEvidence{Session: child.Thread, Step: "child-test", Observation: "Recorded failure"}, presentation.ReviewEvidence{Session: child.Thread, Observation: "Helper answered"}, presentation.ReviewEvidence{Session: "s_00000000000000000000000000000000", Step: "child-test", Observation: "Other session"})
	reviewAppend(t, p, root, 1, at.Add(4*time.Second), event.Completed{Text: text})
	got := evidenceOf(t, p.reviewText(t.Context(), root, text, &who, time.Time{}))
	if got[0].Step != "child-test" || got[1].Session != child.Thread || got[2].Session != "" {
		t.Fatal("child lineage not enforced", got)
	}
}
