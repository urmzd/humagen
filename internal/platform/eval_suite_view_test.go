package platform

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/event"
)

func TestEvalSuiteViewKeepsRecordedSourceTextPrivateWithoutMutatingReplay(t *testing.T) {
	r := evals.Retrieval{Input: "query", Sources: []string{"private-source-binding"}, Prompt: "PRIVATE PASSAGE"}.Seal()
	suite := evals.Suite{Name: "suite", Cases: []evals.Case{{ID: "case", Input: "query", Trace: &evals.Trace{Run: "original", Retrieval: &r}}}}
	view := (&Platform{}).EvalSuiteView(suite)
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "PRIVATE PASSAGE") || strings.Contains(string(raw), "private-source-binding") {
		t.Fatalf("retained context exposed: %s", raw)
	}
	if suite.Cases[0].Trace.Retrieval == nil || suite.Cases[0].Trace.Retrieval.Prompt != "PRIVATE PASSAGE" {
		t.Fatal("projection erased stored replay")
	}
}

func TestEvalSuiteReplayRoundtripPreservesSnapshotAndRejectsForgery(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	privateEvalTraceRun(t, p, "original", "ana")
	if _, err = p.Knowledge.Create("acme", "manual", "", "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	r := evals.Retrieval{Input: "query", Sources: []string{"manual"}, Blocks: []evals.ContextBlock{}}.Seal()
	suite := evals.Suite{Name: "recorded", Target: evals.Agent, Subject: "writer", Cases: []evals.Case{{ID: "case", Input: "query", Checks: []evals.Check{{Kind: evals.Contains, Value: "evidence"}}, Trace: &evals.Trace{Run: "original", Tools: []evals.ToolResult{}, Retrieval: &r}}}}
	if err = p.Evals.Put("acme", suite, false); err != nil {
		t.Fatal(err)
	}
	who := auth.Principal{Tenant: "acme", Subject: "ana", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	blind := auth.Principal{Tenant: "acme", Subject: "eval-only", Kind: auth.Machine, Scopes: []auth.Permission{auth.EvalsRead, auth.EvalsWrite}}
	if p.EvalSuiteSourcesVisible(blind, suite) {
		t.Fatal("eval permissions exposed recorded source answers")
	}
	view := p.EvalSuiteView(suite)
	merged, err := p.MergeEvalSuiteReplay(who, view, true)
	if err != nil || merged.Cases[0].Trace.Retrieval == nil || merged.Cases[0].Trace.Retrieval.Digest != r.Digest {
		t.Fatalf("roundtrip lost snapshot: %+v %v", merged, err)
	}
	if _, err = p.MergeEvalSuiteReplay(who, suite, true); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("client supplied retained source bytes: %v", err)
	}
	view.Cases[0].Input = "different question"
	if _, err = p.MergeEvalSuiteReplay(who, view, true); !errors.Is(err, evals.ErrInvalid) {
		t.Fatalf("changed input retained context: %v", err)
	}
}

func privateEvalTraceRun(t *testing.T, p *Platform, id, owner string) {
	t.Helper()
	th, err := p.Threads.Create("acme", threads.Thread{ID: "th-1234567890abcdef", Channel: "general", Visibility: threads.Private, CreatedBy: owner}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tag := wire.NewEvent(wire.Raise{ID: id, Origin: runs.OriginAPI, Tenant: "acme", Agent: BuiltinBuilder, Actor: event.Actor{ID: owner}, At: time.Now(), Context: map[string]string{runs.MetaThread: th.ID}})
	if err := p.Runs.Record(id, tag, 1); err != nil {
		t.Fatal(err)
	}
}

func TestToolOnlyEvalTraceRequiresCurrentOriginalRunAccess(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	privateEvalTraceRun(t, p, "original", "ana")
	suite := evals.Suite{Name: "private-tools", Target: evals.Agent, Subject: BuiltinBuilder, Cases: []evals.Case{{ID: "case", Input: "query", Trace: &evals.Trace{Run: "original", Tools: []evals.ToolResult{{Name: "lookup", Arguments: json.RawMessage(`{}`), Result: "PRIVATE TOOL RESULT"}}}}}}
	owner := auth.Principal{Tenant: "acme", Subject: "ana", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	stranger := owner
	stranger.Subject = "bob"
	stranger.Roles = []auth.Role{auth.Operator}
	if !p.EvalSuiteSourcesVisible(owner, suite) {
		t.Fatal("owner cannot read their recorded case")
	}
	if p.EvalSuiteSourcesVisible(stranger, suite) || p.Seer(stranger).Suite(suite) {
		t.Fatal("tool-only trace crossed private run boundary")
	}
	if _, err := p.MergeEvalSuiteReplay(stranger, suite, false); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("unreadable source accepted: %v", err)
	}
	other := owner
	other.Tenant = "other"
	if p.EvalSuiteSourcesVisible(other, suite) {
		t.Fatal("trace crossed tenant boundary")
	}
	suite.Cases[0].Trace.Run = "missing"
	if p.EvalSuiteSourcesVisible(owner, suite) {
		t.Fatal("missing source granted recorded context")
	}
	suite.Cases[0].Trace = nil
	if !p.EvalSuiteSourcesVisible(stranger, suite) {
		t.Fatal("plain manually authored case acquired a run dependency")
	}
}
