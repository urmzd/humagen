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
	"github.com/urmzd/saige/flow/event"
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

func TestEvalHistoryRetainsOriginalAccessAfterCasesRemovedOrReplaced(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "removed", true: "replaced"}[replacement], func(t *testing.T) {
			p, err := New(Config{Workspace: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = p.Close() }()
			privateEvalTraceRun(t, p, "original", "ana")
			suite := evalHistorySuite()
			if err := p.Evals.Put("acme", suite, false); err != nil {
				t.Fatal(err)
			}
			finishEvalHistory(t, p, suite, "PRIVATE TOOL RESULT")
			owner := auth.Principal{Tenant: "acme", Subject: "ana", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
			stranger := auth.Principal{Tenant: "acme", Subject: "bob", Kind: auth.User, Roles: []auth.Role{auth.Operator}}
			suite.Cases[0].Trace = nil
			if replacement {
				tag := wire.NewEvent(wire.Raise{ID: "public", Origin: runs.OriginAPI, Tenant: "acme", Agent: BuiltinBuilder, At: time.Now()})
				if err := p.Runs.Record("public", tag, 1); err != nil {
					t.Fatal(err)
				}
				suite.Cases[0].Trace = &evals.Trace{Run: "public"}
			}
			if err := p.Evals.Put("acme", suite, true); err != nil {
				t.Fatal(err)
			}
			finishEvalHistory(t, p, suite, "public answer")
			if !p.Seer(stranger).Suite(suite) {
				t.Fatal("replacement suite is not public")
			}
			owned, err := p.EvalRunsView(owner, suite.Name)
			if err != nil || len(owned) != 2 {
				t.Fatalf("owner history = %+v %v", owned, err)
			}
			visible, err := p.EvalRunsView(stranger, suite.Name)
			if err != nil || len(visible) != 1 || visible[0].Results[0].Output != "public answer" {
				t.Fatalf("old private result declassified: %+v %v", visible, err)
			}
			other := owner
			other.Tenant = "other"
			if _, err := p.EvalRunsView(other, suite.Name); !errors.Is(err, evals.ErrNotFound) {
				t.Fatalf("history crossed tenants: %v", err)
			}
		})
	}
}

func TestEvalHistoryRechecksOriginalRunAndSourceAccess(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	privateEvalTraceRun(t, p, "original", "ana")
	thread, err := p.Threads.Get("acme", "th-1234567890abcdef")
	if err != nil {
		t.Fatal(err)
	}
	thread, err = p.Threads.Participants("acme", thread.ID, thread.Version, []threads.Participant{{Kind: threads.Person, Name: "bob"}}, nil, nil, "ana", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Knowledge.Create("acme", "manual", "", "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	r := evals.Retrieval{Input: "query", Sources: []string{"manual"}, Prompt: "retained empty result"}.Seal()
	suite := evalHistorySuite()
	suite.Cases[0].Trace.Retrieval = &r
	if err := p.Evals.Put("acme", suite, false); err != nil {
		t.Fatal(err)
	}
	finishEvalHistory(t, p, suite, "PRIVATE PASSAGE")
	suite.Cases[0].Trace = nil
	if err := p.Evals.Put("acme", suite, true); err != nil {
		t.Fatal(err)
	}
	bob := auth.Principal{Tenant: "acme", Subject: "bob", Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	if history, err := p.EvalRunsView(bob, suite.Name); err != nil || len(history) != 1 {
		t.Fatalf("authorized history = %+v %v", history, err)
	}
	if _, err := p.Threads.Participants("acme", thread.ID, thread.Version, nil, []string{"bob"}, nil, "ana", time.Now()); err != nil {
		t.Fatal(err)
	}
	if history, err := p.EvalRunsView(bob, suite.Name); err != nil || len(history) != 0 {
		t.Fatalf("revoked session still readable: %+v %v", history, err)
	}
	owner := auth.Principal{Tenant: "acme", Subject: "ana", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	if history, err := p.EvalRunsView(owner, suite.Name); err != nil || len(history) != 1 {
		t.Fatalf("owner lost source access: %+v %v", history, err)
	}
	if err := p.Knowledge.Delete("acme", "manual"); err != nil {
		t.Fatal(err)
	}
	if history, err := p.EvalRunsView(owner, suite.Name); err != nil || len(history) != 0 {
		t.Fatalf("missing source still readable: %+v %v", history, err)
	}
	if p.evalRunVisible(owner, evals.RunRecord{Run: evals.Run{Target: evals.Agent, Subject: BuiltinBuilder}}) {
		t.Fatal("unknown legacy provenance granted history access")
	}
}

func evalHistorySuite() evals.Suite {
	return evals.Suite{Name: "history", Target: evals.Agent, Subject: BuiltinBuilder, Cases: []evals.Case{{ID: "case", Input: "query", Checks: []evals.Check{{Kind: evals.Contains, Value: "answer"}}, Trace: &evals.Trace{Run: "original", Tools: []evals.ToolResult{{Name: "lookup", Arguments: json.RawMessage(`{}`), Result: "PRIVATE TOOL RESULT"}}}}}}
}

func finishEvalHistory(t *testing.T, p *Platform, suite evals.Suite, output string) {
	t.Helper()
	run, err := p.Evals.Begin("acme", suite, 1, "ana", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	run.Finish([]evals.Result{{CaseID: "case", Output: output, Passed: true}}, nil, time.Now())
	if err := p.Evals.Save("acme", run); err != nil {
		t.Fatal(err)
	}
}
