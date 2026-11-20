package platform_test

// The skill packages in this file are generated fixtures: their names,
// instructions and file contents were written for these tests and come from
// no real skill folder.

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Generated fixture bytes, with a tab, a trailing space and no final newline,
// so a read that changed a byte would not hash the same.
const (
	importedGuide = "Generated fixture guide for the imported skill.\n\tStep one \nStep two"
	draftedGuide  = "Generated fixture guide for the console skill.\n\tCheck \nDone"
	otherSecret   = "Generated fixture file of a skill this turn does not use"
	workspaceNote = "Generated fixture note in the conversation's working directory"
)

var (
	toldTool  = regexp.MustCompile(`Supporting files are read on demand with (\S+) using these exact paths`)
	toldGuide = regexp.MustCompile(`(?m)^(skill:[a-z-]+/references/guide\.md) \(\d+ bytes\)$`)
)

// readCall is one tool call the scripted model made and what it got back.
type readCall struct {
	tool, path string
	result     string
	isError    bool
}

// skillReader is a scripted model that does with a skill's supporting files
// what a real one does: it finds, in the turn's text, the tool the text says
// reads them and the guide's path, and calls that tool with that path whether
// or not it was offered. It then calls the same tool with each path the
// message lists after "READ:", the tool the message names after "CALL:",
// and, for "BURST:<n>", the guide n times at once, and answers whether the
// last call it saw succeeded.
type skillReader struct {
	mu      sync.Mutex
	offered [][]string
	told    []string
	calls   []readCall
}

func (m *skillReader) Stream(_ context.Context, req saigetypes.Request) (<-chan saigetypes.Delta, error) {
	text, at := "", -1
	for i, msg := range req.Messages {
		u, ok := msg.(saigetypes.UserMessage)
		if !ok {
			continue
		}
		for _, part := range u.Parts {
			if tp, ok := part.(saigetypes.TextPart); ok && strings.Contains(tp.Text, "$") {
				text, at = tp.Text, i
			}
		}
	}
	var asked []saigetypes.ToolCallPart
	results := map[string]saigetypes.ToolResultPart{}
	for _, msg := range req.Messages[at+1:] {
		switch msg := msg.(type) {
		case saigetypes.AssistantMessage:
			for _, part := range msg.Parts {
				if c, ok := part.(saigetypes.ToolCallPart); ok {
					asked = append(asked, c)
				}
			}
		case saigetypes.UserMessage:
			for _, part := range msg.Parts {
				if r, ok := part.(saigetypes.ToolResultPart); ok {
					results[r.CallID] = r
				}
			}
		case saigetypes.SystemMessage:
			for _, part := range msg.Parts {
				if r, ok := part.(saigetypes.ToolResultPart); ok {
					results[r.CallID] = r
				}
			}
		}
	}
	var calls []readCall
	for _, c := range asked {
		r, ok := results[c.ID]
		if !ok {
			continue
		}
		var b strings.Builder
		for _, part := range r.Parts {
			if tp, ok := part.(saigetypes.TextPart); ok {
				b.WriteString(tp.Text)
			}
		}
		path, _ := c.Arguments["path"].(string)
		calls = append(calls, readCall{tool: c.Name, path: path, result: b.String(), isError: r.IsError})
	}
	var offered []string
	for _, d := range req.Tools {
		offered = append(offered, d.Name)
	}

	// Each round is one response: one call, or a burst of calls at once.
	var script [][]saigetypes.ToolCallPart
	call := func(name string, args map[string]any) saigetypes.ToolCallPart {
		return saigetypes.ToolCallPart{ID: "call-" + strconv.Itoa(len(script)) + "-" + name, Name: name, Arguments: args}
	}
	tool, guide := "", ""
	if found := toldTool.FindStringSubmatch(text); found != nil {
		tool = found[1]
		if g := toldGuide.FindStringSubmatch(text); g != nil {
			guide = g[1]
			script = append(script, []saigetypes.ToolCallPart{call(tool, map[string]any{"path": guide})})
		}
	}
	// The person's own words are the turn's last paragraph: the context
	// above them may repeat them.
	words := text[strings.LastIndex(text, "\n\n")+1:]
	for word := range strings.FieldsSeq(words) {
		if path, ok := strings.CutPrefix(word, "READ:"); ok && tool != "" {
			script = append(script, []saigetypes.ToolCallPart{call(tool, map[string]any{"path": path})})
		}
		if name, ok := strings.CutPrefix(word, "CALL:"); ok {
			script = append(script, []saigetypes.ToolCallPart{call(name, map[string]any{})})
		}
		if path, ok := strings.CutPrefix(word, "WITH:"); ok && guide != "" {
			// path and the guide at once, so the round is not all failures.
			first, second := call(tool, map[string]any{"path": path}), call(tool, map[string]any{"path": guide})
			second.ID += "-guide"
			script = append(script, []saigetypes.ToolCallPart{first, second})
		}
		if n, ok := strings.CutPrefix(word, "BURST:"); ok && guide != "" {
			count, _ := strconv.Atoi(n)
			var burst []saigetypes.ToolCallPart
			for i := range count {
				c := call(tool, map[string]any{"path": guide})
				c.ID += "-" + strconv.Itoa(i)
				burst = append(burst, c)
			}
			script = append(script, burst)
		}
	}
	// The next round is the first whose calls were not all made yet.
	made := map[string]bool{}
	for _, c := range asked {
		made[c.ID] = true
	}
	next := -1
	for i, round := range script {
		if !made[round[0].ID] {
			next = i
			break
		}
	}
	var deltas []saigetypes.Delta
	if next >= 0 {
		for i, c := range script[next] {
			deltas = append(deltas, saigetypes.PartStart{Index: i, Kind: saigetypes.KindToolCall, ID: c.ID, Name: c.Name}, saigetypes.PartEnd{Index: i, Part: c})
		}
	} else if len(calls) > 0 && !calls[len(calls)-1].isError {
		deltas = say("Read it.")
	} else {
		deltas = say("I cannot read the supporting file.")
	}

	m.mu.Lock()
	m.offered = append(m.offered, offered)
	if len(asked) == 0 {
		m.told = append(m.told, text)
	}
	m.calls = calls
	m.mu.Unlock()
	out := make(chan saigetypes.Delta, len(deltas)+1)
	for _, d := range deltas {
		out <- d
	}
	out <- saigetypes.DoneDelta{}
	close(out)
	return out, nil
}

// take returns the calls made and the texts told since the last take.
func (m *skillReader) take() (calls []readCall, told []string, offered [][]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	calls, told, offered = m.calls, m.told, m.offered
	m.calls, m.told, m.offered = nil, nil, nil
	return calls, told, offered
}

// skillShelf is a platform whose agents read with skillReader, and the
// generated skills they use: imported, read from a folder as `humagen
// import skills` drafts it; drafted, saved in the console's editor; and
// other, a published skill no turn here is told to use.
type skillShelf struct {
	p      *platform.Platform
	who    auth.Principal
	model  *skillReader
	scope  library.Scope
	shared string
}

func skillShelfOf(t *testing.T, open func(*testing.T) state.Store) *skillShelf {
	t.Helper()
	model := &skillReader{}
	p := startWith(t, platform.Config{
		Backends: platform.Backends{State: open(t)},
		Ext:      platform.Extensions{Providers: map[string]native.ProviderFunc{"reader": func(native.Spec) (saigetypes.Provider, error) { return model, nil }}},
	})
	// No agent here is granted a file tool. careful holds file_list
	// for approval, to show that other tools' approvals are unchanged.
	for _, spec := range []wire.AgentSpec{
		{Name: "reader", Provider: "reader", Model: "m", SystemPrompt: "Answer."},
		{Name: "bound", Provider: "reader", Model: "m", SystemPrompt: "Answer."},
		{Name: "careful", Provider: "reader", Model: "m", SystemPrompt: "Answer.", Tools: []string{platform.ToolFileList}, Approvals: []string{platform.ToolFileList}},
	} {
		if _, err := p.Agents.Create(t.Context(), tenant, spec, "test"); err != nil {
			t.Fatal(err)
		}
	}
	who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
	if _, err := p.CreateProject(t.Context(), tenant, govern, platform.ProjectInput{Name: "Shared"}); err != nil {
		t.Fatal(err)
	}
	return &skillShelf{p: p, who: who, model: model, scope: library.Scope{Kind: "workspace"}, shared: "Shared"}
}

// generated is a skill package of SKILL.md and the files given, with the
// empty tool list the console's editor sends.
func generated(name, guide string, more ...library.File) library.Definition {
	files := append([]library.File{
		{Path: "SKILL.md", Data: []byte("---\nname: " + name + "\ndescription: Generated fixture skill " + name + "\n---\nRead references/guide.md before answering.\n")},
		{Path: "references/guide.md", Data: []byte(guide)},
	}, more...)
	return library.Definition{Skill: projects.Skill{Name: name}, Tools: []string{}, Files: files}
}

// settle waits for a run to stop moving: done, failed, stopped or parked.
func settle(t *testing.T, p *platform.Platform, id string) runs.Run {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		r, err := p.Runs.Get(tenant, id)
		if err == nil && (r.Status == runs.Completed || r.Status == runs.Failed || r.Status == runs.Stopped || r.Status == runs.Parked) {
			return r
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s did not settle: %q, %v", id, r.Status, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// tested drafts d (imported or from the editor), tests it on reader, which
// must complete without asking anyone, and publishes it.
func (s *skillShelf) tested(t *testing.T, d library.Definition, imported bool) library.Revision {
	t.Helper()
	var r library.Revision
	var err error
	if imported {
		// What `humagen import skills` sends: no tool list.
		d.Tools = nil
		r, err = s.p.ImportSkill(s.who, s.scope, d, 0)
	} else {
		r, err = s.p.DraftSkill(s.who, s.scope, d, 0, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	if imported && r.Tools != nil {
		t.Fatalf("%s was imported with tools %v, want none", d.Name, r.Tools)
	}
	if !imported && (r.Tools == nil || len(r.Tools) != 0) {
		t.Fatalf("%s was drafted with tools %v, want the empty list", d.Name, r.Tools)
	}
	test, err := s.p.TestSkill(t.Context(), s.who, r.ID, r.Version, "reader", s.shared, "Try $"+d.Name)
	if err != nil {
		t.Fatal(err)
	}
	if got := settle(t, s.p, test.ID); got.Status != runs.Completed {
		t.Fatalf("the test of %s did not complete unasked: %s, question %+v", d.Name, got.Status, got.Question)
	}
	failed, err := s.p.FailedSkillTests(s.who, mustRevision(t, s.p, s.who, r))
	if err != nil || len(failed) != 0 {
		t.Fatalf("the test of %s failed a tool call: %v, %v", d.Name, failed, err)
	}
	if r, err = s.p.PublishSkill(s.who, r.ID, r.Version); err != nil {
		t.Fatal(err)
	}
	return r
}

func mustRevision(t *testing.T, p *platform.Platform, who auth.Principal, r library.Revision) library.Revision {
	t.Helper()
	got, err := p.SkillLibrary.Get(who.Tenant, r.Scope, r.Name, r.Version)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// readOnce checks that a turn's first call read guide whole, with the tool
// its text named, which the turn was offered.
func readOnce(t *testing.T, who string, calls []readCall, told []string, offered [][]string, guide string) {
	t.Helper()
	if len(told) == 0 || !strings.Contains(told[0], "with "+platform.ToolSkillFileRead+" using these exact paths") {
		t.Fatalf("%s: the turn's text does not name %s: %q", who, platform.ToolSkillFileRead, told)
	}
	if len(offered) == 0 || !slices.Contains(offered[0], platform.ToolSkillFileRead) {
		t.Fatalf("%s: the turn was not offered %s: %v", who, platform.ToolSkillFileRead, offered)
	}
	if len(calls) == 0 || calls[0].tool != platform.ToolSkillFileRead || calls[0].isError {
		t.Fatalf("%s: the first read failed: %+v", who, calls)
	}
	if sha256.Sum256([]byte(calls[0].result)) != sha256.Sum256([]byte(guide)) {
		t.Fatalf("%s: read %q, want the package's bytes %q", who, calls[0].result, guide)
	}
}

// An imported skill, with no tool list, and a console-drafted skill, with the
// empty one, each with a supporting file, are read by a model on an agent granted
// no file tool: in the skill's test, by mention, and by default binding,
// through the tool the turn's text names, with no person asked.
func TestASkillsOwnFilesAreReadOnAnAgentWithNoFileTools(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		s := skillShelfOf(t, open)
		imported := s.tested(t, generated("imported", importedGuide), true)
		if imported.Via != library.ViaImport {
			t.Fatalf("imported via %q", imported.Via)
		}
		calls, told, offered := s.model.take()
		readOnce(t, "the imported skill's test", calls, told, offered, importedGuide)
		drafted := s.tested(t, generated("drafted", draftedGuide), false)
		calls, told, offered = s.model.take()
		readOnce(t, "the console skill's test", calls, told, offered, draftedGuide)

		for _, c := range []struct{ name, guide string }{{"imported", importedGuide}, {"drafted", draftedGuide}} {
			posted, err := s.p.StartThread(t.Context(), s.who, platform.ThreadInput{Project: s.shared, Agents: []string{"reader"}, Text: "$" + c.name + " Summarize the guide"})
			if err != nil || len(posted.Runs) != 1 {
				t.Fatalf("mention %s: %v", c.name, err)
			}
			if got := settle(t, s.p, posted.Runs[0].ID); got.Status != runs.Completed || got.Answer != "Read it." {
				t.Fatalf("mention %s: %s %q", c.name, got.Status, got.Answer)
			}
			calls, told, offered := s.model.take()
			readOnce(t, "a mention of "+c.name, calls, told, offered, c.guide)
			assertRecorded(t, s.p, posted.Runs[0].ID, platform.ToolSkillFileRead, c.guide)
		}

		if err := s.p.SetAgentHome(t.Context(), tenant, "bound", projects.Root, govern); err != nil {
			t.Fatal(err)
		}
		rev, err := s.p.Agents.Get(t.Context(), tenant, "bound", 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.p.SetAgentSkills(s.who, "bound", rev.Rev, 0, []platform.AgentSkillRef{{ID: imported.ID, Version: imported.Version}, {ID: drafted.ID, Version: drafted.Version}}); err != nil {
			t.Fatal(err)
		}
		run, err := s.p.Converse(t.Context(), tenant, platform.Turn{ID: "default-bound-read", Agent: "bound", Text: "A plain turn", Actor: platform.ActorOf(s.who)})
		if err != nil {
			t.Fatal(err)
		}
		if got := settle(t, s.p, run.ID); got.Status != runs.Completed {
			t.Fatalf("default binding: %s", got.Status)
		}
		calls, told, offered = s.model.take()
		readOnce(t, "default binding", calls, told, offered, importedGuide)
		if !strings.Contains(told[0], "skill:drafted/references/guide.md") {
			t.Fatalf("the second default skill's file is not listed: %q", told[0])
		}
	})
}

// assertRecorded checks that the run's events hold the read as any tool call
// is held: the tool's name, and the bytes it returned.
func assertRecorded(t *testing.T, p *platform.Platform, run, tool, want string) {
	t.Helper()
	events, err := p.Journal.Events(tenant, []string{run})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if done, ok := e.Data.(event.ToolDone); ok && done.Name == tool && done.Error == "" && done.Result == want {
			return
		}
	}
	t.Fatalf("run %s holds no recorded %s call that returned the file", run, tool)
}

// skill_file_read reads only the files of the skills the turn uses: a path
// out of the package, another skill's file, a conversation file, a missing
// file and one over the bound are each refused, with the reason, and the
// refusal is recorded on the run as a failed tool call.
func TestSkillFileReadReachesOnlyThePinnedPackages(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		s := skillShelfOf(t, open)
		big := library.File{Path: "references/big.md", Data: []byte(strings.Repeat("g", 128<<10+1))}
		s.tested(t, generated("imported", importedGuide, big), true)
		s.tested(t, generated("other", otherSecret), false)
		s.model.take()

		posted, err := s.p.StartThread(t.Context(), s.who, platform.ThreadInput{Project: s.shared, Agents: []string{"reader"}, Text: "$imported First read"})
		if err != nil || len(posted.Runs) != 1 {
			t.Fatalf("start: %v", err)
		}
		settle(t, s.p, posted.Runs[0].ID)
		s.model.take()
		// A file in the conversation's working directory, which the agent
		// could read only with a file tool it is not granted.
		dir := s.p.SandboxDir(tenant, posted.Thread.ID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte(workspaceNote), 0o600); err != nil {
			t.Fatal(err)
		}
		refused := map[string]string{
			"skill:imported/../outside.md":      "outside the package of skill imported",
			"skill:imported/references/../../x": "outside the package of skill imported",
			"skill:other/references/guide.md":   "skill other is not one this turn uses",
			"notes.md":                          "reads only the supporting files of the skills this turn uses",
			"skill:imported/missing.md":         `skill imported has no file "missing.md"`,
			"skill:imported/references/big.md":  "over the 131072 bytes skill_file_read returns",
			filepath.Join(dir, "notes.md"):      "reads only the supporting files of the skills this turn uses",
		}
		// The runtime ends a turn after two failed tool rounds in a row,
		// and after ten rounds, so each refused read is made beside one that
		// succeeds.
		text := "$imported Read these"
		for path := range refused {
			text += " WITH:" + path
		}
		next, err := s.p.PostMessage(t.Context(), s.who, posted.Thread.ID, platform.MessageInput{Text: text})
		if err != nil || len(next.Runs) != 1 {
			t.Fatalf("post: %v", err)
		}
		if got := settle(t, s.p, next.Runs[0].ID); got.Status != runs.Completed {
			t.Fatalf("the refusals stopped the run: %s, %+v", got.Status, got.Question)
		}
		calls, _, _ := s.model.take()
		if len(calls) != 2*len(refused)+1 {
			t.Fatalf("calls: %+v", calls)
		}
		for _, c := range calls {
			if c.path == "skill:imported/references/guide.md" {
				if c.isError || c.result != importedGuide {
					t.Errorf("the guide between refusals: %+v", c)
				}
				continue
			}
			why, ok := refused[c.path]
			if !ok || !c.isError || !strings.Contains(c.result, why) {
				t.Errorf("%s = %q (error %v), want refused: %s", c.path, c.result, c.isError, why)
			}
			for _, secret := range []string{otherSecret, workspaceNote, "ggggg"} {
				if strings.Contains(c.result, secret) {
					t.Errorf("%s returned what it may not: %q", c.path, c.result)
				}
			}
		}
		events, err := s.p.Journal.Events(tenant, []string{next.Runs[0].ID})
		if err != nil {
			t.Fatal(err)
		}
		failed := 0
		for _, e := range events {
			if done, ok := e.Data.(event.ToolDone); ok && done.Name == platform.ToolSkillFileRead && done.Error != "" {
				failed++
			}
		}
		if failed != len(refused) {
			t.Fatalf("%d refusals recorded, want %d", failed, len(refused))
		}
	})
}

// A turn reads at most as many skill files as its text can list and one
// more, however its model asks for them: the reads past that are refused,
// with the reason.
func TestSkillFileReadIsBoundedInCount(t *testing.T) {
	s := skillShelfOf(t, func(*testing.T) state.Store { return state.NewMemory() })
	s.tested(t, generated("imported", importedGuide), true)
	s.model.take()
	run, err := s.p.Converse(t.Context(), tenant, platform.Turn{ID: "bounded-reads", Agent: "reader", Text: "$imported Read it again BURST:40 BURST:30", Project: s.shared, Actor: platform.ActorOf(s.who), NamesSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	settle(t, s.p, run.ID)
	calls, _, _ := s.model.take()
	if len(calls) != 71 {
		t.Fatalf("%d calls", len(calls))
	}
	read, refused := 0, 0
	for _, c := range calls {
		switch {
		case !c.isError && c.result == importedGuide:
			read++
		case c.isError && strings.Contains(c.result, "as many as one turn may"):
			refused++
		default:
			t.Fatalf("unexpected: %+v", c)
		}
	}
	if read != 64 || refused != 7 {
		t.Fatalf("%d read, %d refused; want 64 and 7", read, refused)
	}
}

// Reading a skill's files asks no one, in a skill's test, where every
// other action is asked about, or in a normal turn; another tool the agent
// holds for approval is still held.
func TestSkillFileReadAsksNoOneAndLeavesOtherApprovals(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		s := skillShelfOf(t, open)
		d := generated("listing", draftedGuide)
		d.Tools = []string{platform.ToolFileList}
		r, err := s.p.DraftSkill(s.who, s.scope, d, 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		test, err := s.p.TestSkill(t.Context(), s.who, r.ID, r.Version, "careful", s.shared, "Try $listing CALL:"+platform.ToolFileList)
		if err != nil {
			t.Fatal(err)
		}
		parked := settle(t, s.p, test.ID)
		if parked.Status != runs.Parked || parked.Question == nil || parked.Question.Tool == nil || parked.Question.Tool.Name != platform.ToolFileList {
			t.Fatalf("the test was not held at file_list: %s %+v", parked.Status, parked.Question)
		}
		calls, _, _ := s.model.take()
		if len(calls) != 1 || calls[0].tool != platform.ToolSkillFileRead || calls[0].isError || calls[0].result != draftedGuide {
			t.Fatalf("the read before the held call: %+v", calls)
		}
	})
}

// Whatever a turn's tools are narrowed to, by the agent's grants or by a
// skill's tool list, a turn whose text names a tool for reading skill files
// is offered that tool, and a turn that is offered it is told of it.
func TestTheTurnTextAndTheTurnsToolsAgree(t *testing.T) {
	s := skillShelfOf(t, func(*testing.T) state.Store { return state.NewMemory() })
	if _, err := s.p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: "granted", Provider: "reader", Model: "m", SystemPrompt: "Answer.", Tools: []string{platform.ToolFileRead}}, "test"); err != nil {
		t.Fatal(err)
	}
	type skill struct {
		name  string
		tools []string
		files bool
	}
	var skills []skill
	for _, tools := range [][]string{{}, {platform.ToolFileRead}, {platform.ToolWebSearch}} {
		for _, files := range []bool{true, false} {
			name := "shape-" + strings.Join(append([]string{"t"}, tools...), "-") + map[bool]string{true: "-files", false: "-bare"}[files]
			name = strings.ReplaceAll(name, "_", "")
			d := generated(name, draftedGuide)
			if !files {
				d.Files = d.Files[:1]
				d.Files[0].Data = []byte("---\nname: " + name + "\ndescription: Generated fixture skill " + name + "\n---\nAnswer briefly.\n")
			}
			d.Tools = tools
			r, err := s.p.DraftSkill(s.who, s.scope, d, 0, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.p.SkillLibrary.Tested(tenant, s.scope, r.Name, r.Version, "generated-fixture-test"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.p.SkillLibrary.Publish(tenant, "ada", s.scope, r.Name, r.Version, time.Now()); err != nil {
				t.Fatal(err)
			}
			skills = append(skills, skill{name, tools, files})
		}
	}
	for _, agent := range []string{"reader", "granted"} {
		for _, sk := range skills {
			posted, err := s.p.StartThread(t.Context(), s.who, platform.ThreadInput{Project: s.shared, Agents: []string{agent}, Text: "$" + sk.name + " Go"})
			if err != nil || len(posted.Runs) != 1 {
				t.Fatalf("%s %s: %v", agent, sk.name, err)
			}
			got := settle(t, s.p, posted.Runs[0].ID)
			calls, told, offered := s.model.take()
			if len(told) == 0 || len(offered) == 0 {
				t.Fatalf("%s %s: the model was not asked", agent, sk.name)
			}
			named := toldTool.FindStringSubmatch(told[0])
			has := slices.Contains(offered[0], platform.ToolSkillFileRead)
			switch {
			case named != nil && !slices.Contains(offered[0], named[1]):
				t.Errorf("%s %s: the text names %s, the turn is offered %v", agent, sk.name, named[1], offered[0])
			case named == nil && has:
				t.Errorf("%s %s: offered %s and not told of it", agent, sk.name, platform.ToolSkillFileRead)
			case sk.files != (named != nil):
				t.Errorf("%s %s: files %v, told %v", agent, sk.name, sk.files, named)
			case sk.files && (got.Status != runs.Completed || len(calls) != 1 || calls[0].isError):
				t.Errorf("%s %s: the read failed: %s %+v", agent, sk.name, got.Status, calls)
			}
		}
	}
}

// A skill's test that completed with a failed tool call, here a tool the
// model was told of and not offered, is not a passing test: the revision is
// not published on it, and its history says which test failed.
func TestASkillTestWithAFailedToolCallDoesNotPass(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		s := skillShelfOf(t, open)
		r, err := s.p.ImportSkill(s.who, s.scope, generated("imported", importedGuide), 0)
		if err != nil {
			t.Fatal(err)
		}
		// The model calls a tool nobody offers, as it did when the text named
		// file_read to a turn without it.
		test, err := s.p.TestSkill(t.Context(), s.who, r.ID, r.Version, "reader", s.shared, "Try $imported READ:skill:imported/missing.md")
		if err != nil {
			t.Fatal(err)
		}
		if got := settle(t, s.p, test.ID); got.Status != runs.Completed {
			logEvents(t, s.p, test.ID)
			t.Fatalf("the test did not complete: %s", got.Status)
		}
		s.model.take()
		saved := mustRevision(t, s.p, s.who, r)
		failed, err := s.p.FailedSkillTests(s.who, saved)
		if err != nil || !slices.Equal(failed, []string{test.ID}) {
			t.Fatalf("failed tests %v, %v; want %s", failed, err, test.ID)
		}
		if _, err := s.p.PublishSkill(s.who, r.ID, r.Version); !errors.Is(err, library.ErrConflict) || !strings.Contains(err.Error(), "a tool call failed in every finished test") {
			t.Fatalf("published on a test with a failed tool call: %v", err)
		}
		missing, err := s.p.TestSkill(t.Context(), s.who, r.ID, r.Version, "reader", s.shared, "Try $imported CALL:file_read")
		if err != nil {
			t.Fatal(err)
		}
		settle(t, s.p, missing.ID)
		calls, _, _ := s.model.take()
		if len(calls) != 2 || calls[1].tool != platform.ToolFileRead || !calls[1].isError || !strings.Contains(calls[1].result, "tool not found") {
			t.Fatalf("the call of a tool not offered: %+v", calls)
		}
		if _, err := s.p.PublishSkill(s.who, r.ID, r.Version); !errors.Is(err, library.ErrConflict) {
			t.Fatalf("published on a test with a tool not found: %v", err)
		}
		clean, err := s.p.TestSkill(t.Context(), s.who, r.ID, r.Version, "reader", s.shared, "Try $imported")
		if err != nil {
			t.Fatal(err)
		}
		if got := settle(t, s.p, clean.ID); got.Status != runs.Completed {
			t.Fatalf("the clean test: %s", got.Status)
		}
		if failed, _ := s.p.FailedSkillTests(s.who, mustRevision(t, s.p, s.who, r)); len(failed) != 2 || slices.Contains(failed, clean.ID) {
			t.Fatalf("failed tests after a clean one: %v", failed)
		}
		published, err := s.p.PublishSkill(s.who, r.ID, r.Version)
		if err != nil {
			t.Fatal(err)
		}
		if failed, _ := s.p.FailedSkillTests(s.who, published); failed != nil {
			t.Fatalf("a published revision lists failed tests: %v", failed)
		}
	})
}

// An imported skill limits a run's tools only as its SKILL.md declares: one
// that declares nothing leaves the agent's own tools, whether it was
// imported now (no list) or before (the empty list the import then wrote
// whatever the folder said); an empty allowed-tools allows none; and an
// empty list saved in the editor allows none. Every one still reads its own
// files.
func TestAnImportedSkillLimitsToolsOnlyAsItsFolderDeclares(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		s := skillShelfOf(t, open)
		if _, err := s.p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: "granted", Provider: "reader", Model: "m", SystemPrompt: "Answer.", Tools: []string{platform.ToolFileRead}}, "test"); err != nil {
			t.Fatal(err)
		}
		declares := func(name, allowed string) library.Definition {
			d := generated(name, importedGuide)
			d.Files[0].Data = []byte("---\nname: " + name + "\ndescription: Generated fixture skill " + name + "\n" + allowed + "---\nRead references/guide.md before answering.\n")
			return d
		}
		cases := []struct {
			name     string
			draft    func(d library.Definition) (library.Revision, error)
			d        library.Definition
			fileRead bool
		}{
			{"imported-now", func(d library.Definition) (library.Revision, error) {
				d.Tools = nil
				return s.p.ImportSkill(s.who, s.scope, d, 0)
			}, declares("imported-now", ""), true},
			{"imported-before", func(d library.Definition) (library.Revision, error) {
				// As the store holds a revision an earlier import wrote.
				d.Tools = []string{}
				return s.p.SkillLibrary.Import(tenant, "ada", s.scope, d, 0, time.Now())
			}, declares("imported-before", ""), true},
			{"imported-none", func(d library.Definition) (library.Revision, error) {
				d.Tools = nil
				return s.p.ImportSkill(s.who, s.scope, d, 0)
			}, declares("imported-none", "allowed-tools: \"\"\n"), false},
			{"imported-narrow", func(d library.Definition) (library.Revision, error) {
				d.Tools = nil
				return s.p.ImportSkill(s.who, s.scope, d, 0)
			}, declares("imported-narrow", "allowed-tools: file_read web_search\n"), true},
			{"edited-none", func(d library.Definition) (library.Revision, error) {
				return s.p.DraftSkill(s.who, s.scope, d, 0, nil)
			}, declares("edited-none", ""), false},
		}
		for _, c := range cases {
			r, err := c.draft(c.d)
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			if err := s.p.SkillLibrary.Tested(tenant, s.scope, r.Name, r.Version, "generated-fixture-test"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.p.SkillLibrary.Publish(tenant, "ada", s.scope, r.Name, r.Version, time.Now()); err != nil {
				t.Fatal(err)
			}
			posted, err := s.p.StartThread(t.Context(), s.who, platform.ThreadInput{Project: s.shared, Agents: []string{"granted"}, Text: "$" + c.name + " Go"})
			if err != nil || len(posted.Runs) != 1 {
				t.Fatalf("%s: %v", c.name, err)
			}
			got := settle(t, s.p, posted.Runs[0].ID)
			calls, _, offered := s.model.take()
			if got.Status != runs.Completed || len(calls) != 1 || calls[0].isError || calls[0].result != importedGuide {
				t.Errorf("%s: the skill's own file was not read: %s %+v", c.name, got.Status, calls)
			}
			if has := slices.Contains(offered[0], platform.ToolFileRead); has != c.fileRead {
				t.Errorf("%s: offered %v; the agent's file_read kept: %v, want %v", c.name, offered[0], has, c.fileRead)
			}
		}
	})
}
