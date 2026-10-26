package platform_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/registry"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/saige/agent/provider/openai"
	saigetypes "github.com/urmzd/saige/agent/types"
)

type setupFault struct {
	state.Store
	mu         sync.Mutex
	operations int
	fail       bool
}

func (s *setupFault) Write(ctx context.Context, namespace string, ops ...state.Op) error {
	s.mu.Lock()
	if namespace == "operations" {
		s.operations++
		if s.fail && s.operations == 2 {
			s.fail = false
			s.mu.Unlock()
			return statetest.ErrInjected
		}
	}
	s.mu.Unlock()
	return s.Store.Write(ctx, namespace, ops...)
}
func setupPlan(name string) platform.AgentSetupPlan {
	return platform.AgentSetupPlan{Spec: agentspec.AgentSpec{Name: name, Provider: "openai", Model: "gpt-4o-mini", SystemPrompt: "Help the company."}, Project: "~", Rules: map[operations.Category]operations.Level{operations.Other: operations.Ask}}
}
func TestAgentSetupResumesAfterRegistryCreationAndNeverHijacksExistingNames(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := &setupFault{Store: open(t), fail: true}
		p, err := platform.New(platform.Config{Workspace: t.TempDir(), Backends: platform.Backends{State: st}})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = p.Close() }()
		saved, err := p.CreateAgentSetup(t.Context(), admin, setupPlan("draft"))
		if err != nil {
			t.Fatal(err)
		}
		if saved.Phase != "home" || saved.Problem == "" {
			t.Fatalf("saved partial=%+v", saved)
		}
		first, err := p.Agents.Get(t.Context(), tenant, "draft", 0)
		if err != nil {
			t.Fatal(err)
		}
		if !p.AgentControl(tenant, "draft").Paused {
			t.Fatal("partially configured draft is active")
		}
		if _, err := p.Invoke(t.Context(), tag("ordinary-draft", "draft")); !errors.Is(err, operations.ErrPaused) {
			t.Fatalf("ordinary draft invocation=%v", err)
		}
		resumed, err := p.ResumeAgentSetup(t.Context(), admin, "draft")
		if err != nil || resumed.Phase != "configured" {
			t.Fatalf("resume=%+v,%v", resumed, err)
		}
		after, _ := p.Agents.Get(t.Context(), tenant, "draft", 0)
		if after.Rev != first.Rev || after.Hash != first.Hash {
			t.Fatal("resume duplicated or revised the registry entry")
		}
		other := admin
		other.Subject = "ben"
		if _, err := p.AgentSetup(other, "draft"); !errors.Is(err, operations.ErrNotFound) {
			t.Fatalf("foreign setup=%v", err)
		}
		if _, err := p.CreateAgentBy(t.Context(), other, setupPlan("DRAFT").Spec); !errors.Is(err, operations.ErrConflict) {
			t.Fatalf("normalized reservation=%v", err)
		}
		oldSpec := setupPlan("existing").Spec
		original, err := p.CreateAgentBy(t.Context(), other, oldSpec)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.CreateAgentSetup(t.Context(), admin, setupPlan("existing")); !errors.Is(err, registry.ErrConflict) {
			t.Fatalf("existing name=%v", err)
		}
		kept, _ := p.Agents.Get(t.Context(), tenant, "existing", 0)
		if kept.Hash != original.Hash || p.AgentControl(tenant, "existing").Paused {
			t.Fatal("losing setup changed the existing agent")
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		restarted, err := platform.New(platform.Config{Workspace: t.TempDir(), Backends: platform.Backends{State: st, Agents: p.Agents}})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = restarted.Close() }()
		loaded, err := restarted.AgentSetup(admin, "draft")
		if err != nil || loaded.Phase != "configured" || loaded.RevisionHash != first.Hash {
			t.Fatalf("restart=%+v,%v", loaded, err)
		}
		if _, err := restarted.Invoke(t.Context(), tag("restart-draft", "draft")); !errors.Is(err, operations.ErrPaused) {
			t.Fatalf("restart admission=%v", err)
		}
	})
}
func TestAgentSetupNormalCreateRaceKeepsTheWinningAgentUnchanged(t *testing.T) {
	for i := 0; i < 8; i++ {
		p, err := platform.New(platform.Config{Workspace: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		name := fmt.Sprintf("race-%d", i)
		plan := setupPlan(name)
		other := admin
		other.Subject = "ben"
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var normalErr, setupErr error
		go func() { defer wg.Done(); <-start; _, normalErr = p.CreateAgentBy(t.Context(), other, plan.Spec) }()
		go func() { defer wg.Done(); <-start; _, setupErr = p.CreateAgentSetup(t.Context(), admin, plan) }()
		close(start)
		wg.Wait()
		rev, err := p.Agents.Get(t.Context(), tenant, name, 0)
		if err != nil {
			t.Fatal(err)
		}
		if normalErr == nil {
			if setupErr == nil || rev.CreatedBy != other.Subject || p.AgentControl(tenant, name).Paused {
				t.Fatalf("normal winner was changed: %v,%v,%+v", normalErr, setupErr, rev)
			}
		} else if setupErr != nil {
			t.Fatalf("neither create won: %v,%v", normalErr, setupErr)
		}
		_ = p.Close()
	}
}
func TestAgentSetupDryRunUsesRealAdapterAndApprovalBeforeActivation(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		hasTool := false
		for _, message := range request.Messages {
			hasTool = hasTool || message.Role == "tool"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		requests.Add(1)
		if !hasTool {
			fmt.Fprint(w, "data: {\"id\":\"tool\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"write\",\"type\":\"function\",\"function\":{\"name\":\"file_write\",\"arguments\":\"{\\\"path\\\":\\\"draft.md\\\",\\\"content\\\":\\\"Reviewed local test\\\"}\"}}]},\"finish_reason\":null}]}\n\ndata: {\"id\":\"tool\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
		} else {
			fmt.Fprint(w, "data: {\"id\":\"answer\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Created the reviewed test draft.\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"answer\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	p, err := platform.New(platform.Config{Workspace: t.TempDir(), Ext: platform.Extensions{Providers: map[string]agentrt.ProviderFunc{"local-real": func(spec agentrt.Spec) (saigetypes.Provider, error) {
		return openai.New(openai.Config{APIKey: "isolated-fixture", Model: saigetypes.ModelID(spec.Model)}, openai.WithBaseURL(server.URL+"/v1"))
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	plan := setupPlan("real-draft")
	plan.Spec.Provider = "local-real"
	plan.Spec.Tools = []string{platform.ToolFileWrite}
	saved, err := p.CreateAgentSetup(t.Context(), admin, plan)
	if err != nil || saved.Phase != "configured" {
		t.Fatalf("setup=%+v,%v", saved, err)
	}
	if _, err := p.ActivateAgentSetup(t.Context(), admin, saved.Name); err == nil {
		t.Fatal("draft activated before test")
	}
	tested, err := p.TestAgentSetup(t.Context(), admin, saved.Name, "Write a local draft.")
	if err != nil || tested.TestRun == "" {
		t.Fatalf("test=%+v,%v", tested, err)
	}
	parked := await(t, p, tested.TestRun, runs.Parked)
	if parked.Question == nil {
		t.Fatal("real tool call has no native approval")
	}
	if _, err := os.Stat(filepath.Join(p.SandboxDir(tenant, parked.Thread), "draft.md")); err == nil {
		t.Fatal("unapproved test tool executed")
	}
	reviewer := auth.Principal{Tenant: tenant, Subject: "ben", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	if _, err := p.AnswerQuestion(t.Context(), reviewer, parked.ID, parked.Question.ID, platform.QuestionAnswer{Action: platform.ActApprove}); err != nil {
		t.Fatal(err)
	}
	completed := await(t, p, tested.TestRun, runs.Completed)
	if completed.Answer == "" || requests.Load() < 2 {
		t.Fatal("native adapter did not complete the real approved tool round trip")
	}
	active, err := p.ActivateAgentSetup(t.Context(), admin, saved.Name)
	if err != nil || active.Phase != "ready" || p.AgentControl(tenant, saved.Name).Paused {
		t.Fatalf("activate=%+v,%v", active, err)
	}
	again, err := p.ActivateAgentSetup(t.Context(), admin, saved.Name)
	if err != nil || again.Revision != active.Revision {
		t.Fatalf("activation retry=%+v,%v", again, err)
	}
}

func TestAgentSetupReadOmitsSourcesWhoseAudienceChanged(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		p, err := platform.New(platform.Config{Workspace: t.TempDir(), Backends: platform.Backends{State: open(t)}})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = p.Close() }()
		if _, err := p.Knowledge.Create(tenant, "retained-source", "", admin.Subject, time.Now()); err != nil {
			t.Fatal(err)
		}
		plan := setupPlan("source-draft")
		plan.Spec.Sources = []agentspec.Source{{Name: "retained-source", URI: "https://private.example.test/source"}}
		saved, err := p.CreateAgentSetup(t.Context(), admin, plan)
		if err != nil || saved.Phase != "configured" {
			t.Fatalf("saved=%+v,%v", saved, err)
		}
		ben := admin
		ben.Subject = "ben"
		personal, err := p.PersonalProject(ben)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Knowledge.Configure(tenant, "retained-source", knowledge.SourceConfig{Mode: knowledge.Indexed, Provider: "manual", Owner: "ben", Project: personal.Name}, time.Now()); err != nil {
			t.Fatal(err)
		}
		reader := admin
		reader.Roles = []auth.Role{auth.Operator}
		shown, err := p.AgentSetup(reader, saved.Name)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(shown)
		if len(shown.Plan.Spec.Sources) != 0 || strings.Contains(string(raw), "retained-source") || strings.Contains(string(raw), "private.example.test") {
			t.Fatalf("hidden source leaked=%s", raw)
		}
	})
}
