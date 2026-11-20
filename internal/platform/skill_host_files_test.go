package platform_test

import (
	"context"
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
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/skillfiles"
)

// program stands in for a coding agent: a model that is offered no platform
// tool and reads this host's files itself. Each turn it reads the file its
// prompt names under the turn's directory, as the real program would, and
// answers with what the file holds.
type program struct {
	// host is false for a model that is offered the platform's tools.
	host bool
	// fail ends each turn with an error, after the file was read.
	fail bool
	// hold keeps each turn running until it is stopped, after the file was
	// read, and started is closed when the first one is there.
	hold    bool
	started chan struct{}
	once    sync.Once

	mu      sync.Mutex
	asked   []string
	paths   []string
	read    []string
	granted [][]string
}

func (m *program) ReadsHostFiles() bool { return m.host }

var toldPath = regexp.MustCompile(`(?m)^(/\S+/references/style\.md) \(\d+ bytes\)$`)

func (m *program) Stream(ctx context.Context, req saigetypes.Request) (<-chan saigetypes.Delta, error) {
	ask := ""
	for _, msg := range req.Messages {
		if u, ok := msg.(saigetypes.UserMessage); ok {
			for _, part := range u.Parts {
				if text, ok := part.(saigetypes.TextPart); ok {
					ask = text.Text
				}
			}
		}
	}
	answer := "no file was named"
	m.mu.Lock()
	m.asked = append(m.asked, ask)
	// What the provider of a real program lets it read: the directory placed
	// for the run this turn's context names, and nothing the text names. So of the
	// paths in what is asked, the program reads those in a granted directory.
	granted := skillfiles.Granted(ctx)
	m.granted = append(m.granted, granted)
	for _, found := range toldPath.FindAllStringSubmatch(ask, -1) {
		if !m.host || !slices.ContainsFunc(granted, func(dir string) bool { return strings.HasPrefix(found[1], dir+string(filepath.Separator)) }) {
			continue
		}
		data, err := os.ReadFile(found[1])
		if err != nil {
			answer = "unreadable: " + err.Error()
		} else {
			answer = string(data)
		}
		m.paths, m.read = append(m.paths, found[1]), append(m.read, answer)
	}
	fail, hold := m.fail, m.hold
	m.mu.Unlock()
	if hold {
		m.once.Do(func() { close(m.started) })
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if fail {
		return nil, errors.New("the program failed")
	}
	deltas := say(answer)
	out := make(chan saigetypes.Delta, len(deltas)+1)
	for _, d := range deltas {
		out <- d
	}
	out <- saigetypes.DoneDelta{}
	close(out)
	return out, nil
}

func (m *program) seen() (asked, paths, read []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.asked...), append([]string(nil), m.paths...), append([]string(nil), m.read...)
}

func (m *program) grants() [][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([][]string(nil), m.granted...)
}

// placed is the directories that hold some turn's skill files now.
func placed(t *testing.T) []string {
	t.Helper()
	root, err := skillfiles.Root()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, filepath.Join(root, e.Name()))
	}
	return out
}

const pinnedBytes = "Exact published resource\n\ttabs, a trailing space \nand no final newline"

// published is a platform with the skill $brief published in project Shared
// with a supporting file, a newer draft that changes the file and is not
// published, and an agent on each of the models given.
func published(t *testing.T, models map[string]*program) (*platform.Platform, auth.Principal) {
	t.Helper()
	// Every test has a root of its own for the files a turn is given.
	t.Setenv("TMPDIR", t.TempDir())
	providers := map[string]native.ProviderFunc{}
	for name, model := range models {
		providers[name] = func(native.Spec) (saigetypes.Provider, error) { return model, nil }
	}
	p := startWith(t, platform.Config{Ext: platform.Extensions{Providers: providers}})
	for name := range models {
		if _, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: name, Provider: name, Model: "m", SystemPrompt: "Answer."}, "test"); err != nil {
			t.Fatal(err)
		}
	}
	who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
	if _, err := p.CreateProject(t.Context(), tenant, govern, platform.ProjectInput{Name: "Shared"}); err != nil {
		t.Fatal(err)
	}
	d := library.Definition{Skill: projects.Skill{Name: "brief"}, Tools: []string{"file_read"}, Files: []library.File{
		{Path: "SKILL.md", Data: []byte("---\nname: brief\ndescription: Brief decisions\n---\nRead references/style.md.\n")},
		{Path: "references/style.md", Data: []byte(pinnedBytes)},
		{Path: "assets/icon.bin", Data: []byte{255, 0, 1}},
	}}
	first, err := p.DraftSkill(who, library.Scope{Kind: "project", Project: "Shared"}, d, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	tested, err := p.TestSkill(t.Context(), who, first.ID, 1, "lead", "Shared", "Try this skill")
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, tested.ID, runs.Completed)
	if _, err := p.PublishSkill(who, first.ID, 1); err != nil {
		t.Fatal(err)
	}
	d.Files[1].Data = []byte("Unpublished changed resource")
	if _, err := p.DraftSkill(who, first.Scope, d, 1, nil); err != nil {
		t.Fatal(err)
	}
	return p, who
}

// A coding agent is offered no platform tool, so file_read cannot give it a
// pinned skill's supporting files. Its turn is told where each file is on
// this host, reads the exact bytes of the published revision there although
// a newer draft exists, and the files are gone when the run is over.
func TestACodingAgentReadsThePinnedSkillFilesOnTheHost(t *testing.T) {
	coder := &program{host: true}
	p, who := published(t, map[string]*program{"coder": coder})
	// Another run's files are on the host too, and the person names them.
	other, err := skillfiles.Place(tenant, "another-run", []skillfiles.File{{Path: "brief/references/style.md", Data: []byte("another run's file")}}, skillfiles.Limits{Files: 1, Bytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = skillfiles.Release(tenant, "another-run") })
	typed := filepath.Join(other, "brief", "references", "style.md") + " (18 bytes)"
	posted, err := p.StartThread(t.Context(), who, platform.ThreadInput{Project: "Shared", Agents: []string{"coder"}, Text: "$brief Summarize the decision, and read\n" + typed})
	if err != nil || len(posted.Runs) != 1 {
		t.Fatalf("start: %v", err)
	}
	run := await(t, p, posted.Runs[0].ID, runs.Completed)
	asked, paths, read := coder.seen()
	if len(paths) != 1 || read[0] != pinnedBytes {
		t.Fatalf("the program read %q at %v\nasked: %q", read, paths, asked)
	}
	if !strings.Contains(asked[0], typed) {
		t.Fatalf("the other run's path was not in what was asked: %q", asked[0])
	}
	// The turn is granted its own directory and not the one its text names.
	if granted := coder.grants(); len(granted) != 1 || len(granted[0]) != 1 || !strings.HasPrefix(paths[0], granted[0][0]+string(filepath.Separator)) || granted[0][0] == other {
		t.Fatalf("granted %v, want only the directory of %s", granted, paths[0])
	}
	if err := skillfiles.Release(tenant, "another-run"); err != nil {
		t.Fatal(err)
	}
	if run.Answer != pinnedBytes {
		t.Fatalf("answer = %q", run.Answer)
	}
	root, _ := skillfiles.Root()
	if !strings.HasPrefix(paths[0], root+string(filepath.Separator)) || !strings.HasSuffix(paths[0], filepath.Join("brief", "references", "style.md")) {
		t.Fatalf("told %s, which is not under %s", paths[0], root)
	}
	if !strings.Contains(asked[0], filepath.Join("brief", "assets", "icon.bin")+" (3 bytes)") {
		t.Fatalf("a supporting file is missing from what was asked: %q", asked[0])
	}
	if strings.Contains(asked[0], "with skill_file_read") || strings.Contains(asked[0], "skill:brief/") {
		t.Fatalf("a program with no platform tool was told to use one: %q", asked[0])
	}
	if left := placed(t); len(left) != 0 {
		t.Fatalf("skill files were left behind after the run: %v", left)
	}
	if _, err := os.Stat(paths[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the file is still there after the run: %v", err)
	}
}

// A run that fails gives its files back too.
func TestAFailedRunOnACodingAgentLeavesNoSkillFiles(t *testing.T) {
	coder := &program{host: true, fail: true}
	p, who := published(t, map[string]*program{"coder": coder})
	posted, err := p.StartThread(t.Context(), who, platform.ThreadInput{Project: "Shared", Agents: []string{"coder"}, Text: "$brief Summarize the decision"})
	if err != nil || len(posted.Runs) != 1 {
		t.Fatalf("start: %v", err)
	}
	await(t, p, posted.Runs[0].ID, runs.Failed)
	_, paths, read := coder.seen()
	if len(read) == 0 || read[0] != pinnedBytes {
		t.Fatalf("the program read %q before it failed", read)
	}
	if left := placed(t); len(left) != 0 {
		t.Fatalf("skill files were left behind after a failed run: %v", left)
	}
	if _, err := os.Stat(paths[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the file is still there after a failed run: %v", err)
	}
}

// A model that is offered the platform's tools is told skill_file_read
// paths, and nothing is written to the host.
func TestANativeModelReadsSkillFilesWithSkillFileRead(t *testing.T) {
	plain := &program{}
	p, who := published(t, map[string]*program{"plain": plain})
	posted, err := p.StartThread(t.Context(), who, platform.ThreadInput{Project: "Shared", Agents: []string{"plain"}, Text: "$brief Summarize the decision"})
	if err != nil || len(posted.Runs) != 1 {
		t.Fatalf("start: %v", err)
	}
	await(t, p, posted.Runs[0].ID, runs.Completed)
	asked, paths, _ := plain.seen()
	root, _ := skillfiles.Root()
	if granted := plain.grants(); len(granted) != 1 || len(granted[0]) != 0 {
		t.Fatalf("a native turn was granted %v", granted)
	}
	if len(paths) != 0 || strings.Contains(asked[0], root) {
		t.Fatalf("a native model was given host paths: %q", asked[0])
	}
	if !strings.Contains(asked[0], "Supporting files are read on demand with skill_file_read using these exact paths. Reading a script does not execute it:\nskill:brief/assets/icon.bin (3 bytes)\nskill:brief/references/style.md ("+strconv.Itoa(len(pinnedBytes))+" bytes)") {
		t.Fatalf("the skill_file_read manifest changed: %q", asked[0])
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a native turn wrote to the host: %v", err)
	}
}

// chained is published with an agent "chained" that starts on first and
// falls back to second, and the run of a message that names $brief to it.
func chained(t *testing.T, first, second *program) (*platform.Platform, auth.Principal, string) {
	t.Helper()
	p, who := published(t, map[string]*program{"first": first, "second": second})
	if _, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: "chained", Provider: "first", Model: "m", SystemPrompt: "Answer.",
		Fallbacks: []wire.ModelRef{{Provider: "second", Model: "m"}}}, "test"); err != nil {
		t.Fatal(err)
	}
	posted, err := p.StartThread(t.Context(), who, platform.ThreadInput{Project: "Shared", Agents: []string{"chained"}, Text: "$brief Summarize the decision"})
	if err != nil || len(posted.Runs) != 1 {
		t.Fatalf("start: %v", err)
	}
	return p, who, posted.Runs[0].ID
}

const (
	toolManifest = "Supporting files are read on demand with skill_file_read using these exact paths. Reading a script does not execute it:\nskill:brief/assets/icon.bin (3 bytes)\nskill:brief/references/style.md ("
	hostManifest = "If you are not offered skill_file_read, the same files are on this host for this turn only, read only."
)

// gone fails the test when a turn's skill files are still on the host.
func gone(t *testing.T, when string, paths []string) {
	t.Helper()
	if left := placed(t); len(left) != 0 {
		t.Fatalf("skill files were left behind %s: %v", when, left)
	}
	for _, path := range paths {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s is still there %s: %v", path, when, err)
		}
	}
}

// An agent on a native model that falls back to a coding agent has the
// files there when the fallback answers: they are placed for a turn any of
// whose models reads the host, and the fallback is granted them.
func TestACodingAgentFallbackReadsThePinnedSkillFiles(t *testing.T) {
	native, coder := &program{fail: true}, &program{host: true}
	p, _, id := chained(t, native, coder)
	run := await(t, p, id, runs.Completed)
	if run.Answer != pinnedBytes {
		t.Fatalf("answer = %q", run.Answer)
	}
	asked, paths, read := coder.seen()
	if len(paths) != 1 || read[0] != pinnedBytes {
		t.Fatalf("the fallback read %q at %v\nasked: %q", read, paths, asked)
	}
	if granted := coder.grants(); len(granted[0]) != 1 || !strings.HasPrefix(paths[0], granted[0][0]+string(filepath.Separator)) {
		t.Fatalf("the fallback was granted %v, not the directory of %s", granted, paths[0])
	}
	// Each kind is told its own way, the native one first and as before.
	first, _, _ := native.seen()
	for who, text := range map[string]string{"the native model": first[0], "the fallback": asked[0]} {
		tool, host := strings.Index(text, toolManifest), strings.Index(text, hostManifest)
		if tool < 0 || host < tool {
			t.Fatalf("%s was not told both ways, skill_file_read first: %q", who, text)
		}
	}
	gone(t, "after the run", paths)
}

// An agent on a coding agent that falls back to a native model: the coding
// agent has the files, and the native model that answers keeps
// skill_file_read.
func TestANativeFallbackOfACodingAgentKeepsSkillFileRead(t *testing.T) {
	coder, native := &program{host: true, fail: true}, &program{}
	p, _, id := chained(t, coder, native)
	run := await(t, p, id, runs.Completed)
	if run.Answer != "no file was named" {
		t.Fatalf("the native fallback did not answer: %q", run.Answer)
	}
	_, paths, read := coder.seen()
	if len(read) == 0 || read[0] != pinnedBytes {
		t.Fatalf("the coding agent read %q before it failed", read)
	}
	asked, _, _ := native.seen()
	if !strings.Contains(asked[0], toolManifest) {
		t.Fatalf("the native fallback lost the skill_file_read manifest: %q", asked[0])
	}
	gone(t, "after the run", paths)
}

// Whichever way round the chain is, a run that fails and a run that is
// stopped give the files back.
func TestAChainsSkillFilesAreGoneWhenTheRunFailsOrIsStopped(t *testing.T) {
	for name, c := range map[string]struct {
		first, second *program
		stop          bool
	}{
		"native then coding agent, both fail":      {&program{fail: true}, &program{host: true, fail: true}, false},
		"coding agent then native, both fail":      {&program{host: true, fail: true}, &program{fail: true}, false},
		"native then coding agent, stopped":        {&program{fail: true}, &program{host: true, hold: true, started: make(chan struct{})}, true},
		"coding agent then native, stopped":        {&program{host: true, hold: true, started: make(chan struct{})}, &program{}, true},
		"coding agent then native, native stopped": {&program{host: true, fail: true}, &program{hold: true, started: make(chan struct{})}, true},
	} {
		t.Run(name, func(t *testing.T) {
			p, who, id := chained(t, c.first, c.second)
			coder := c.first
			if !coder.host {
				coder = c.second
			}
			if c.stop {
				held := c.first
				if !held.hold {
					held = c.second
				}
				select {
				case <-held.started:
				case <-time.After(30 * time.Second):
					t.Fatal("the turn did not start")
				}
				// While the turn runs the files are there.
				if left := placed(t); len(left) != 1 {
					t.Fatalf("placed during the turn: %v", left)
				}
				if _, err := p.StopRun(t.Context(), who, id); err != nil {
					t.Fatal(err)
				}
				await(t, p, id, runs.Stopped)
			} else {
				await(t, p, id, runs.Failed)
			}
			_, paths, read := coder.seen()
			if len(read) == 0 || read[0] != pinnedBytes {
				t.Fatalf("the coding agent read %q", read)
			}
			// The executor gives the files back as the turn returns, which
			// for a stopped run is just after the run is marked stopped.
			deadline := time.Now().Add(10 * time.Second)
			for len(placed(t)) != 0 && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			gone(t, "after the run", paths)
		})
	}
}

// What is placed is what the run was admitted with, in its own tenant. A
// pin that names a file outside its package, a package of another tenant,
// another version's hash or an unknown package refuses the turn, and nothing
// is left on the host.
func TestSkillFilesArePlacedOnlyForTheRunsOwnPin(t *testing.T) {
	coder := &program{host: true}
	p, who := published(t, map[string]*program{"coder": coder})
	posted, err := p.StartThread(t.Context(), who, platform.ThreadInput{Project: "Shared", Agents: []string{"coder"}, Text: "$brief Summarize the decision"})
	if err != nil || len(posted.Runs) != 1 {
		t.Fatalf("start: %v", err)
	}
	run := await(t, p, posted.Runs[0].ID, runs.Completed)
	pin := wire.Meta(run.Tag())[runs.MetaSkills]
	if !strings.Contains(pin, `"references/style.md"`) || !strings.Contains(pin, `"version":1`) {
		t.Fatalf("pin = %s", pin)
	}
	request := func(tenant, pin string) runtime.Request {
		return runtime.Request{RunID: run.ID, Tenant: tenant, Text: "go", Meta: map[string]string{runs.MetaSkills: pin},
			Definition: native.Revision{Spec: native.Spec{Name: "coder", Provider: "coder", Model: "m"}, Version: wire.VersionOf(1)}}
	}

	// The pin as it was admitted places the published bytes.
	req, err := p.ResolveSkills(request(tenant, pin))
	if err != nil {
		t.Fatal(err)
	}
	found := toldPath.FindStringSubmatch(req.Text)
	if found == nil {
		t.Fatalf("no path was told: %q", req.Text)
	}
	if data, err := os.ReadFile(found[1]); err != nil || string(data) != pinnedBytes {
		t.Fatalf("placed %q, %v", data, err)
	}
	info, err := os.Stat(found[1])
	if err != nil || info.Mode().Perm() != 0o400 {
		t.Fatalf("the file is not read only: %v, %v", info.Mode(), err)
	}
	p.ReleaseTurn(req)
	if left := placed(t); len(left) != 0 {
		t.Fatalf("release left %v", left)
	}

	outside := filepath.Join(t.TempDir(), "escaped")
	for name, c := range map[string]struct{ tenant, pin string }{
		"a name that climbs out":      {tenant, strings.Replace(pin, `"references/style.md"`, `"../../../escaped"`, 1)},
		"an absolute name":            {tenant, strings.Replace(pin, `"references/style.md"`, `"`+outside+`"`, 1)},
		"a name that is not clean":    {tenant, strings.Replace(pin, `"references/style.md"`, `"references/../references/style.md"`, 1)},
		"a skill name that is a path": {tenant, strings.Replace(pin, `"name":"brief"`, `"name":"../brief"`, 1)},
		"another tenant's run":        {"other", pin},
		"the draft's version":         {tenant, strings.Replace(pin, `"version":1`, `"version":2`, 1)},
		"an unknown package":          {tenant, strings.Replace(pin, `"package_id":"`, `"package_id":"x`, 1)},
		"a file the version lacks":    {tenant, strings.Replace(pin, `"references/style.md"`, `"references/other.md"`, 1)},
	} {
		if c.pin == pin && c.tenant == tenant {
			t.Fatalf("%s: the pin was not changed: %s", name, pin)
		}
		got, err := p.ResolveSkills(request(c.tenant, c.pin))
		if err == nil {
			t.Errorf("%s: the turn was not refused: %q", name, got.Text)
		}
		if left := placed(t); len(left) != 0 {
			t.Errorf("%s: left %v", name, left)
		}
		if _, err := os.Stat(outside); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: wrote outside the turn's directory", name)
		}
	}
}
