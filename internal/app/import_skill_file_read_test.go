package app

// The skill folder in this file is a generated fixture: its name, text and
// fact were written for this test and come from no real skill folder.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/internal/httpapi"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

var (
	namedTool = regexp.MustCompile(`Supporting files are read on demand with (\S+) using these exact paths`)
	namedFile = regexp.MustCompile(`(?m)^(skill:[a-z-]+/\S+) \(\d+ bytes\)$`)
)

// fileModel is a scripted model that reads a skill's supporting file the way
// the turn's text says to, with the tool and path it names, and answers with
// what the read returned, or says it could not read it.
type fileModel struct {
	mu      sync.Mutex
	offered [][]string
}

func (m *fileModel) Stream(_ context.Context, req saigetypes.Request) (<-chan saigetypes.Delta, error) {
	var text string
	var result *saigetypes.ToolResultPart
	for _, msg := range req.Messages {
		var parts []any
		switch msg := msg.(type) {
		case saigetypes.UserMessage:
			for _, part := range msg.Parts {
				parts = append(parts, part)
			}
		case saigetypes.SystemMessage:
			for _, part := range msg.Parts {
				parts = append(parts, part)
			}
		}
		for _, part := range parts {
			switch part := part.(type) {
			case saigetypes.TextPart:
				if strings.Contains(part.Text, "Supporting files are") {
					text = part.Text
				}
			case saigetypes.ToolResultPart:
				result = &part
			}
		}
	}
	var offered []string
	for _, d := range req.Tools {
		offered = append(offered, d.Name)
	}
	m.mu.Lock()
	m.offered = append(m.offered, offered)
	m.mu.Unlock()
	var deltas []saigetypes.Delta
	tool, file := namedTool.FindStringSubmatch(text), namedFile.FindStringSubmatch(text)
	switch {
	case result != nil && !result.IsError:
		deltas = saigetypes.PartDeltas(0, saigetypes.Text("The file says: "+result.Text()))
	case result != nil:
		deltas = saigetypes.PartDeltas(0, saigetypes.Text("I cannot read the supporting file: "+result.Text()))
	case tool != nil && file != nil:
		c := saigetypes.ToolCallPart{ID: "read-1", Name: tool[1], Arguments: map[string]any{"path": file[1]}}
		deltas = []saigetypes.Delta{saigetypes.PartStart{Index: 0, Kind: saigetypes.KindToolCall, ID: c.ID, Name: c.Name}, saigetypes.PartEnd{Index: 0, Part: c}}
	default:
		deltas = saigetypes.PartDeltas(0, saigetypes.Text("No supporting file was named."))
	}
	out := make(chan saigetypes.Delta, len(deltas)+1)
	for _, d := range deltas {
		out <- d
	}
	out <- saigetypes.DoneDelta{}
	close(out)
	return out, nil
}

// api calls the server as the development principal and decodes the answer.
func api(t *testing.T, server *httptest.Server, method, path string, body, out any) int {
	t.Helper()
	var in bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&in).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, server.URL+path, &in)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw := new(bytes.Buffer)
	_, _ = raw.ReadFrom(res.Body)
	if out != nil && res.StatusCode < 300 {
		if err := json.Unmarshal(raw.Bytes(), out); err != nil {
			t.Fatalf("%s %s: %v\n%s", method, path, err, raw)
		}
	}
	if res.StatusCode >= 300 {
		t.Logf("%s %s = %d %s", method, path, res.StatusCode, raw)
	}
	return res.StatusCode
}

type apiRun struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Answer string `json:"answer"`
}

// finished waits for a run, read through the API, to complete.
func finished(t *testing.T, server *httptest.Server, id string) apiRun {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		var run apiRun
		api(t, server, "GET", "/api/runs/"+id, nil, &run)
		switch run.Status {
		case "completed":
			return run
		case "failed", "stopped", "parked":
			t.Fatalf("run %s %s", id, run.Status)
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s did not finish: %q", id, run.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A fact that is only in an imported skill's supporting file reaches the
// answer of an agent granted no tool, with no edit to the skill: import the
// folder, test the draft, publish it, and name it in a run. These are the
// CLI and API steps a person takes, against the real server and store.
func TestAnImportedSkillsSupportingFileReachesTheAnswer(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		model := &fileModel{}
		p, err := platform.New(platform.Config{Workspace: t.TempDir(), Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}, Backends: platform.Backends{State: open(t)},
			Ext: platform.Extensions{Providers: map[string]native.ProviderFunc{"scripted": func(native.Spec) (saigetypes.Provider, error) { return model, nil }}}})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- p.Run(ctx) }()
		dev := auth.Principal{Subject: "dev", Tenant: "acme", Kind: auth.User, Name: "Developer", Roles: []auth.Role{auth.Admin}}
		server := httptest.NewServer((&httpapi.Server{P: p, Authn: &httpapi.Authn{Dev: &dev}, Log: slog.New(slog.DiscardHandler)}).Handler())
		t.Cleanup(func() {
			server.Close()
			cancel()
			if err := <-done; err != nil {
				t.Errorf("platform shutdown: %v", err)
			}
		})
		// An agent granted no tool at all.
		if _, err := p.Agents.Create(ctx, "acme", wire.AgentSpec{Name: "reader", Provider: "scripted", Model: "m", SystemPrompt: "Answer."}, "test"); err != nil {
			t.Fatal(err)
		}

		const fact = "The generated fixture depot code is 4711-KESTREL."
		dir := depot(t, fact)
		if out, err := runImport(t, "skills", dir, "--scope", "workspace", "--server", server.URL); err != nil {
			t.Fatalf("import = %v\n%s", err, out)
		}
		var list struct {
			Skills []struct {
				ID      string    `json:"id"`
				Version int       `json:"version"`
				Tools   *[]string `json:"tools"`
			} `json:"skills"`
		}
		if code := api(t, server, "GET", "/api/library/skills?scope=workspace", nil, &list); code != 200 || len(list.Skills) != 1 {
			t.Fatalf("list = %d %+v", code, list)
		}
		skill := list.Skills[0]
		if skill.Tools != nil {
			t.Fatalf("the import limited the skill's tools to %v; a folder that declares none limits none", *skill.Tools)
		}

		var test apiRun
		if code := api(t, server, "POST", fmt.Sprintf("/api/library/skills/%s/versions/%d/test", skill.ID, skill.Version), map[string]string{"agent": "reader", "project": "~", "text": "What is the depot code? Use $depot"}, &test); code != 202 {
			t.Fatalf("test = %d", code)
		}
		if got := finished(t, server, test.ID); !strings.Contains(got.Answer, fact) {
			t.Fatalf("the test's answer lacks the fact: %q", got.Answer)
		}
		var history struct {
			Revisions []struct {
				TestResults []struct {
					Verdict string `json:"verdict"`
				} `json:"test_results"`
			} `json:"revisions"`
		}
		// The verdict is recorded by the run's end hook (recordSkillTest), just
		// after the run reads as completed, so it is waited for, not assumed.
		for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(20 * time.Millisecond) {
			history.Revisions = nil
			api(t, server, "GET", "/api/library/skills/"+skill.ID, nil, &history)
			if len(history.Revisions) == 1 && len(history.Revisions[0].TestResults) > 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("no verdict was recorded: history = %+v", history)
			}
		}
		if len(history.Revisions[0].TestResults) != 1 || history.Revisions[0].TestResults[0].Verdict != "passed" {
			t.Fatalf("history = %+v", history)
		}
		if code := api(t, server, "POST", fmt.Sprintf("/api/library/skills/%s/versions/%d/publish", skill.ID, skill.Version), nil, nil); code != 200 {
			t.Fatalf("publish = %d", code)
		}

		var run apiRun
		if code := api(t, server, "POST", "/api/runs", map[string]string{"agent": "reader", "text": "$depot What is the depot code?"}, &run); code != 202 {
			t.Fatalf("run = %d", code)
		}
		if got := finished(t, server, run.ID); !strings.Contains(got.Answer, fact) {
			t.Fatalf("the answer lacks the fact: %q", got.Answer)
		}
		model.mu.Lock()
		defer model.mu.Unlock()
		for _, offered := range model.offered {
			if !slices.Contains(offered, platform.ToolSkillFileRead) || slices.Contains(offered, platform.ToolFileRead) {
				t.Fatalf("a turn was offered %v: want the skill's file read, and no file tool the agent is not granted", offered)
			}
		}
	})
}

// An import's tool limit is what its SKILL.md declares: the draft route
// refuses a tool list with an import, and reads the empty one the CLI sent
// before this release as none. A SKILL.md that declares more tools than a
// skill may name, or a name past the bound, is named under failed with the
// bound, by a dry run as by the real one, and nothing is written.
func TestImportSkillsNamesAnAllowedToolsPastTheBound(t *testing.T) {
	p, err := platform.New(platform.Config{Workspace: t.TempDir(), Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	dev := auth.Principal{Subject: "dev", Tenant: "acme", Kind: auth.User, Name: "Developer", Roles: []auth.Role{auth.Admin}}
	server := httptest.NewServer((&httpapi.Server{P: p, Authn: &httpapi.Authn{Dev: &dev}, Log: slog.New(slog.DiscardHandler)}).Handler())
	t.Cleanup(func() {
		server.Close()
		cancel()
		if err := <-done; err != nil {
			t.Errorf("platform shutdown: %v", err)
		}
	})

	md := func(name, allowed string) string {
		return "---\nname: " + name + "\ndescription: Generated fixture skill " + name + "\nallowed-tools: " + allowed + "\n---\n\nRead references/facts.md.\n"
	}
	names := func(n int) string {
		var out []string
		for i := range n {
			out = append(out, fmt.Sprintf("mcp__fixture__tool_%d", i))
		}
		return strings.Join(out, " ")
	}
	dir := t.TempDir()
	putFile(t, dir, "most/SKILL.md", md("most", names(library.MaxSkillTools)))
	putFile(t, dir, "too-many/SKILL.md", md("too-many", names(library.MaxSkillTools+1)))
	putFile(t, dir, "too-long/SKILL.md", md("too-long", strings.Repeat("x", library.MaxSkillToolName+1)))
	for _, name := range []string{"most", "too-many", "too-long"} {
		putFile(t, dir, name+"/references/facts.md", "Generated fixture fact")
	}
	want := []string{
		"failed too-many: library: invalid: SKILL.md's allowed-tools names 129 tools; a skill names at most 128",
		"failed too-long: library: invalid: SKILL.md's allowed-tools names a tool of 129 characters; a tool name holds at most 128",
	}
	for _, args := range [][]string{{"--dry-run"}, nil} {
		out, err := runImport(t, append([]string{"skills", dir, "--scope", "workspace", "--server", server.URL}, args...)...)
		if err == nil || err.Error() != "2 skills were not drafted" {
			t.Fatalf("%v: err = %v\n%s", args, err, out)
		}
		for _, line := range want {
			if !strings.Contains(out, line) {
				t.Errorf("%v: out lacks %q:\n%s", args, line, out)
			}
		}
	}
	rows, _, err := p.SkillLibrary.List("acme", library.Scope{Kind: "workspace"})
	if err != nil || len(rows) != 1 || rows[0].Name != "most" || len(rows[0].Tools) != library.MaxSkillTools {
		t.Fatalf("library holds %+v, %v", rows, err)
	}

	// The draft route: what the CLI before this release sent, and a list.
	def := func(tools []string) map[string]any {
		d := map[string]any{"name": "routed", "description": "Generated fixture skill routed", "files": []map[string]any{
			{"path": "SKILL.md", "data": []byte(md("routed", "file_read"))},
		}}
		if tools != nil {
			d["tools"] = tools
		}
		return d
	}
	if code := api(t, server, "POST", "/api/library/skills", map[string]any{"scope": map[string]string{"kind": "workspace"}, "definition": def([]string{"file_list"}), "version": 0, "via": "import"}, nil); code != 400 {
		t.Fatalf("an import with a tool list = %d", code)
	}
	var saved struct {
		Tools *[]string `json:"tools"`
	}
	if code := api(t, server, "POST", "/api/library/skills", map[string]any{"scope": map[string]string{"kind": "workspace"}, "definition": def([]string{}), "version": 0, "via": "import"}, &saved); code != 201 || saved.Tools == nil || !slices.Equal(*saved.Tools, []string{"file_read"}) {
		t.Fatalf("an import as the earlier CLI sent it = %d, tools %v", code, saved.Tools)
	}
}
