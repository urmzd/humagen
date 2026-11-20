package platform_test

// Ported from the second review of the skill file read change. The skill
// packages are generated fixtures.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
)

// retryLog counts, by run, how often the skill_test completion step retried,
// and every record logged at error.
type retryLog struct {
	mu      sync.Mutex
	retries map[string]int
	errors  []string
}

func (h *retryLog) Enabled(context.Context, slog.Level) bool { return true }
func (h *retryLog) Handle(_ context.Context, r slog.Record) error {
	var run, action string
	r.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "run":
			run = a.Value.String()
		case "action":
			action = a.Value.String()
		}
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	if strings.Contains(r.Message, "completion action will retry") && action == "skill_test" {
		h.retries[run]++
		if r.Level >= slog.LevelError {
			h.errors = append(h.errors, r.Message)
		}
	}
	return nil
}
func (h *retryLog) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *retryLog) WithGroup(string) slog.Handler      { return h }
func (h *retryLog) count(run string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.retries[run]
}
func (h *retryLog) errorsLogged() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.errors)
}

// verdictShelf is a skill shelf whose platform logs into a retryLog, with
// careful, an agent that holds file_list for olu's approval.
func verdictShelf(t *testing.T) (*skillShelf, *retryLog) {
	t.Helper()
	model := &skillReader{}
	store := state.NewMemory()
	skew := &atomic.Int64{}
	logs := &retryLog{retries: map[string]int{}}
	p := startWith(t, platform.Config{
		Log:      slog.New(logs),
		Now:      func() time.Time { return time.Now().Add(time.Duration(skew.Load())) },
		Backends: platform.Backends{State: store},
		Ext:      platform.Extensions{Providers: map[string]native.ProviderFunc{"reader": func(native.Spec) (saigetypes.Provider, error) { return model, nil }}},
	})
	for _, spec := range []wire.AgentSpec{
		{Name: "reader", Provider: "reader", Model: "m", SystemPrompt: "Answer."},
		{Name: "careful", Provider: "reader", Model: "m", SystemPrompt: "Answer.", Tools: []string{platform.ToolFileList}, Approvals: []string{platform.ToolFileList}, Approvers: []string{"olu"}},
	} {
		if _, err := p.Agents.Create(t.Context(), tenant, spec, "test"); err != nil {
			t.Fatal(err)
		}
	}
	who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
	if _, err := p.CreateProject(t.Context(), tenant, govern, platform.ProjectInput{Name: "Shared"}); err != nil {
		t.Fatal(err)
	}
	return &skillShelf{p: p, store: store, skew: skew, who: who, model: model, scope: library.Scope{Kind: "workspace"}, shared: "Shared"}, logs
}

// recordedResult waits for the recorded result of a test run.
func recordedResult(t *testing.T, s *skillShelf, r library.Revision, run string) library.TestResult {
	t.Helper()
	var result library.TestResult
	within(t, "the result of "+run+" is recorded", func() bool {
		var ok bool
		result, ok = mustRevision(t, s.p, s.who, r).ResultOf(run)
		return ok
	})
	return result
}

// A skill test stopped before it completed is recorded as failed when it
// ends, saying it did not complete and how it ended, with no wait and no
// retry: its events are all kept, and nothing more is coming.
func TestASkillTestThatDidNotCompleteFailsAtOnce(t *testing.T) {
	s, logs := verdictShelf(t)
	r, err := s.p.ImportSkill(s.who, s.scope, generated("halted", importedGuide), 0)
	if err != nil {
		t.Fatal(err)
	}
	test, err := s.p.TestSkill(t.Context(), s.who, r.ID, r.Version, "careful", s.shared, "Try $halted CALL:file_list")
	if err != nil {
		t.Fatal(err)
	}
	if got := settle(t, s.p, test.ID); got.Status != runs.Parked {
		t.Fatalf("not parked: %s", got.Status)
	}
	if _, err := s.p.StopRun(t.Context(), s.who, test.ID); err != nil {
		t.Fatal(err)
	}
	await(t, s.p, test.ID, runs.Stopped)
	result := recordedResult(t, s, r, test.ID)
	if result.Passed || !strings.Contains(result.Reason, "the test did not complete: it stopped") {
		t.Fatalf("recorded %+v", result)
	}
	if n := logs.count(test.ID); n != 0 {
		t.Fatalf("the step retried %d times", n)
	}
	if v := s.verdicts(t, r)[test.ID]; v.Verdict != platform.TestFailed || v.Reason != result.Reason {
		t.Fatalf("history %+v", v)
	}
	if _, err := s.p.PublishSkill(s.who, r.ID, r.Version); !errors.Is(err, library.ErrConflict) || !strings.Contains(err.Error(), "it stopped") {
		t.Fatalf("publish: %v", err)
	}
}

// A test that parks for an approval, is approved and completes spans two
// attempts, and its verdict is recorded.
func TestASkillTestApprovedMidwayIsRecorded(t *testing.T) {
	s, _ := verdictShelf(t)
	r, err := s.p.ImportSkill(s.who, s.scope, generated("asked", importedGuide), 0)
	if err != nil {
		t.Fatal(err)
	}
	test := s.testOf(t, r, "careful", "Try $asked CALL:file_list", platform.ActApprove)
	if test.Status != runs.Completed {
		t.Fatalf("status %s", test.Status)
	}
	if result := recordedResult(t, s, r, test.ID); !result.Passed {
		t.Fatalf("recorded %+v", result)
	}
}

// Eleven tests of one revision started at once: ten are listed and each
// gets its verdict; the eleventh is refused and stopped, and is not listed,
// so no result takes a test's place. No step logs an error.
func TestElevenSkillTestsAtOnce(t *testing.T) {
	s, logs := verdictShelf(t)
	r, err := s.p.ImportSkill(s.who, s.scope, generated("crowded", importedGuide), 0)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var started []string
	refused := 0
	for i := range 11 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run, err := s.p.TestSkill(t.Context(), s.who, r.ID, r.Version, "reader", s.shared, fmt.Sprintf("Try $crowded %d", i))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				started = append(started, run.ID)
			case errors.Is(err, library.ErrInvalid):
				refused++
			default:
				t.Errorf("test %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	if len(started) != 10 || refused != 1 {
		t.Fatalf("%d started, %d refused", len(started), refused)
	}
	for _, id := range started {
		settle(t, s.p, id)
		if result := recordedResult(t, s, r, id); !result.Passed {
			t.Fatalf("%s: %+v", id, result)
		}
	}
	saved := mustRevision(t, s.p, s.who, r)
	if len(saved.Tests) != 10 || len(saved.Results) != 10 {
		t.Fatalf("tests %d, results %d", len(saved.Tests), len(saved.Results))
	}
	if n := logs.errorsLogged(); n != 0 {
		t.Fatalf("%d errors logged", n)
	}
	// A result for a run the revision does not list is not kept.
	if err := s.p.SkillLibrary.RecordTest(tenant, s.scope, r.Name, r.Version, library.TestResult{Run: "not-a-test", Passed: true}); !errors.Is(err, library.ErrNotTested) {
		t.Fatal(err)
	}
	if _, ok := mustRevision(t, s.p, s.who, r).ResultOf("not-a-test"); ok {
		t.Fatal("kept the result of a run the revision does not list")
	}
}

// skill_file_read is not a grant: an agent's tools and a skill's tool list,
// or its SKILL.md's allowed-tools, may not name it, and each refusal says why.
func TestSkillFileReadCannotBeGranted(t *testing.T) {
	s, _ := verdictShelf(t)
	if platform.ToolSkillFileRead != wire.SkillFileRead || library.SkillFileRead != wire.SkillFileRead {
		t.Fatal("the tool has more than one name")
	}
	_, err := s.p.CreateAgentBy(t.Context(), s.who, wire.AgentSpec{Name: "granted", Provider: "reader", Model: "m", SystemPrompt: "Answer.", Tools: []string{platform.ToolSkillFileRead}})
	if err == nil || !strings.Contains(err.Error(), "which no agent is granted") {
		t.Fatalf("an agent granted skill_file_read: %v", err)
	}
	d := generated("listed", importedGuide)
	d.Tools = []string{platform.ToolSkillFileRead}
	if _, err := s.p.DraftSkill(s.who, s.scope, d, 0, nil); !errors.Is(err, library.ErrInvalid) || !strings.Contains(err.Error(), "cannot be named in a skill's tools") {
		t.Fatalf("a skill that lists skill_file_read: %v", err)
	}
	d = generated("declared", importedGuide)
	d.Files[0].Data = []byte("---\nname: declared\ndescription: Generated fixture\nallowed-tools: file_read skill_file_read\n---\nRead references/guide.md.\n")
	d.Tools = nil
	if _, err := s.p.ImportSkill(s.who, s.scope, d, 0); !errors.Is(err, library.ErrInvalid) || !strings.Contains(err.Error(), "cannot be named in a skill's tools") {
		t.Fatalf("a SKILL.md that declares skill_file_read: %v", err)
	}
}
