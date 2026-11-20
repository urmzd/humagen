package platform

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	presentation "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/session"
	"github.com/urmzd/saige/flow/thread"
)

func reviewEvidenceSession(t *testing.T, p *Platform, parent string, at time.Time) session.Record {
	t.Helper()
	rec, err := p.Sessions.Open(t.Context(), "t", session.Open{Parent: parent, Agent: "reviewer", Channel: "general"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Threads.Create("t", threads.Thread{ID: rec.ID, Channel: "general", Visibility: threads.Shared, CreatedBy: "owner", Default: "reviewer", Agents: []string{"reviewer"}, Session: &threads.SessionIdentity{Agent: "reviewer", Parent: rec.Key.Parent, Label: rec.Key.Label}}, at)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func reviewEvidenceRun(t *testing.T, p *Platform, rec session.Record, id string, parent *runs.Run, at time.Time) runs.Run {
	t.Helper()
	tag := wire.NewEvent(wire.Raise{ID: id, Tenant: "t", Agent: "reviewer", Origin: runs.OriginAPI, Text: "Review evidence", At: at})
	if parent != nil {
		var err error
		tag, err = call.New(parent.Tag(), call.Request{Key: id, Agent: "reviewer", Text: "Check input", Mode: call.Delegate}, call.State{}, call.Limits{MaxDepth: 2, MaxFanOut: 2}, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	wire.SetMeta(&tag, wire.MetaSession, rec.ID)
	wire.SetGitHubHeadSHA(&tag, strings.Repeat("a", 40))
	runID := runtime.RunID("t", tag.ID)
	if err := p.Runs.Record(runID, tag, 1); err != nil {
		t.Fatal(err)
	}
	if err := p.Sessions.BindRun(t.Context(), "t", rec.ID, runID); err != nil {
		t.Fatal(err)
	}
	run, err := p.Runs.Get("t", runID)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func reviewEvidenceRead(t *testing.T, p *Platform, reader runs.Run, target, sourceRun, readError string, at time.Time) {
	t.Helper()
	args, err := json.Marshal(map[string]string{"session": target})
	if err != nil {
		t.Fatal(err)
	}
	result, err := json.Marshal([]thread.Entry{{Seq: 1, Role: thread.RoleAssistant, RunID: sourceRun, Text: "Checked src/input.go", At: at.Add(-time.Second)}})
	if err != nil {
		t.Fatal(err)
	}
	reviewAppend(t, p, reader, 1, at, event.ToolCall{ID: "read-evidence", Name: "session_read", Arguments: args})
	reviewAppend(t, p, reader, 2, at.Add(time.Second), event.ToolDone{ID: "read-evidence", Name: "session_read", Arguments: args, Result: string(result), Error: readError})
}

func TestReviewProvenanceReusesHistoricalChildOnlyAfterRecordedRead(t *testing.T) {
	for _, tc := range []struct {
		name        string
		read        bool
		readError   string
		wrongRun    bool
		wrongTarget bool
		readAfter   time.Duration
		toolAfter   time.Duration
		want        bool
	}{
		{name: "successful read", read: true, want: true},
		{name: "no recorded read"},
		{name: "failed read", read: true, readError: "access denied"},
		{name: "other run returned", read: true, wrongRun: true},
		{name: "other session read", read: true, wrongTarget: true},
		{name: "read after answer", read: true, readAfter: 35 * time.Second},
		{name: "evidence after answer", read: true, toolAfter: 35 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, who, at := reviewFixture(t)
			parent := reviewEvidenceSession(t, p, "console://owner", at)
			child := reviewEvidenceSession(t, p, parent.ID, at)
			previous := reviewEvidenceRun(t, p, parent, "previous-review", nil, at)
			source := reviewEvidenceRun(t, p, child, "previous-check", &previous, at.Add(time.Second))
			toolAfter := tc.toolAfter
			if toolAfter == 0 {
				toolAfter = 2 * time.Second
			}
			reviewAppend(t, p, source, 1, at.Add(toolAfter), event.ToolDone{ID: "historical-test", Name: "test", Result: "src/input.go failed validation"})
			reviewAppend(t, p, source, 2, at.Add(toolAfter+time.Second), event.Completed{Text: "Checked src/input.go"})
			current := reviewEvidenceRun(t, p, parent, "continued-review", nil, at.Add(10*time.Second))
			if tc.read {
				target, sourceRun := child.ID, source.ID
				if tc.wrongTarget {
					target = parent.ID
				}
				if tc.wrongRun {
					sourceRun = previous.ID
				}
				readAfter := tc.readAfter
				if readAfter == 0 {
					readAfter = 20 * time.Second
				}
				reviewEvidenceRead(t, p, current, target, sourceRun, tc.readError, at.Add(readAfter))
			}
			text := reviewReport(t,
				presentation.ReviewEvidence{Session: child.ID, Step: "historical-test", Source: "src/input.go", Observation: "Recorded validation failure"},
				presentation.ReviewEvidence{Session: child.ID, Source: "src/input.go", Observation: "Earlier child answer"},
			)
			reviewAppend(t, p, current, 3, at.Add(30*time.Second), event.Completed{Text: text})
			got := evidenceOf(t, p.reviewText(t.Context(), current, text, &who, time.Time{}))
			for i, evidence := range got {
				if tc.want {
					if evidence.Session != child.ID || i == 0 && evidence.Step != "historical-test" {
						t.Fatalf("historical evidence %d lost its recorded reference: %+v", i, evidence)
					}
				} else if evidence.Session != "" || evidence.Step != "" {
					t.Fatalf("unread or future evidence %d retained its reference: %+v", i, evidence)
				}
			}
		})
	}
}

func TestReviewProvenanceRechecksSharedSessionGrant(t *testing.T) {
	p, _, who, at := reviewFixture(t)
	parent := reviewEvidenceSession(t, p, "console://owner", at)
	shared := reviewEvidenceSession(t, p, "console://other-review", at)
	source := reviewEvidenceRun(t, p, shared, "shared-check", nil, at)
	reviewAppend(t, p, source, 1, at.Add(time.Second), event.ToolDone{ID: "shared-test", Name: "test", Result: "src/input.go failed validation"})
	reviewAppend(t, p, source, 2, at.Add(2*time.Second), event.Completed{Text: "Checked src/input.go"})
	current := reviewEvidenceRun(t, p, parent, "review-shared-evidence", nil, at.Add(10*time.Second))
	access, ok := p.Sessions.(session.AccessStore)
	if !ok {
		t.Fatal("session store does not persist read grants")
	}
	if err := access.GrantRead(t.Context(), "t", shared.ID, parent.ID); err != nil {
		t.Fatal(err)
	}
	reviewEvidenceRead(t, p, current, shared.ID, source.ID, "", at.Add(20*time.Second))
	text := reviewReport(t, presentation.ReviewEvidence{Session: shared.ID, Step: "shared-test", Source: "src/input.go", Observation: "Shared validation failure"})
	reviewAppend(t, p, current, 3, at.Add(30*time.Second), event.Completed{Text: text})
	if got := evidenceOf(t, p.reviewText(t.Context(), current, text, &who, time.Time{})); got[0].Session != shared.ID || got[0].Step != "shared-test" {
		t.Fatalf("read evidence from an explicitly shared session was rejected: %+v", got)
	}
	if err := access.RevokeRead(t.Context(), "t", shared.ID, parent.ID); err != nil {
		t.Fatal(err)
	}
	if got := p.reviewText(t.Context(), current, text, &who, time.Time{}); got != reviewEvidenceUnavailable {
		t.Fatalf("historical read retained evidence after grant revocation: %+v", got)
	}
}
