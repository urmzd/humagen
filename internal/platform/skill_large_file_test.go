package platform_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

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
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// fixtureMarker is in every generated file, so a test can tell whether a
// file's content reached what a model was asked.
const fixtureMarker = "generated test fixture"

// generatedText is a generated stand-in for a large schema or source file:
// exactly n bytes of text.
func generatedText(n int, seed string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "<!-- %s %s -->\n", fixtureMarker, seed)
	for i := 0; b.Len() < n; i++ {
		fmt.Fprintf(&b, "<element name=\"%s-%d\"/>\n", seed, i)
	}
	b.Truncate(n)
	return b.Bytes()
}

var toldFile = regexp.MustCompile(`(?m)^(/\S+) \((\d+) bytes\)$`)

// hasher stands in for a model. As a coding agent (host) it reads every
// file its turn names inside the directory granted to it and keeps the hash
// of each, as the real program reads them; either way it keeps what it was
// asked.
type hasher struct {
	host bool

	mu     sync.Mutex
	asked  []string
	hashes map[string][32]byte
}

func (m *hasher) ReadsHostFiles() bool { return m.host }

func (m *hasher) Stream(ctx context.Context, req saigetypes.Request) (<-chan saigetypes.Delta, error) {
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
	granted := skillfiles.Granted(ctx)
	m.mu.Lock()
	m.asked = append(m.asked, ask)
	for _, found := range toldFile.FindAllStringSubmatch(ask, -1) {
		if !m.host || !slices.ContainsFunc(granted, func(dir string) bool { return strings.HasPrefix(found[1], dir+string(filepath.Separator)) }) {
			continue
		}
		if data, err := os.ReadFile(found[1]); err == nil {
			m.hashes[filepath.Base(found[1])] = sha256.Sum256(data)
		}
	}
	m.mu.Unlock()
	deltas := say("Checked.")
	out := make(chan saigetypes.Delta, len(deltas)+1)
	for _, d := range deltas {
		out <- d
	}
	out <- saigetypes.DoneDelta{}
	close(out)
	return out, nil
}

// A supporting file as large as the library takes reaches a coding agent
// whole, on the host, and its bytes are never put into what a model is
// asked. A native model is told the file is over what file_read returns,
// and file_read refuses it while it still reads a file at that bound.
func TestALargeSupportingFileReachesACodingAgentWholeAndNeverThePrompt(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		t.Setenv("TMPDIR", t.TempDir())
		coder, plain := &hasher{host: true, hashes: map[string][32]byte{}}, &hasher{hashes: map[string][32]byte{}}
		providers := map[string]native.ProviderFunc{
			"coder": func(native.Spec) (saigetypes.Provider, error) { return coder, nil },
			"plain": func(native.Spec) (saigetypes.Provider, error) { return plain, nil },
		}
		p := startWith(t, platform.Config{Backends: platform.Backends{State: open(t)}, Ext: platform.Extensions{Providers: providers}})
		for _, name := range []string{"coder", "plain"} {
			if _, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: name, Provider: name, Model: "m", SystemPrompt: "Answer."}, "test"); err != nil {
				t.Fatal(err)
			}
		}
		who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
		if _, err := p.CreateProject(t.Context(), tenant, govern, platform.ProjectInput{Name: "Shared"}); err != nil {
			t.Fatal(err)
		}
		const readBound = 128 << 10 // what file_read returns of a skill file
		large := generatedText(library.MaxPackageFileBytes, "large")
		small := generatedText(readBound, "small")
		d := library.Definition{Skill: projects.Skill{Name: "schemas"}, Tools: []string{"file_read"}, Files: []library.File{
			{Path: "SKILL.md", Data: []byte("---\nname: schemas\ndescription: Validates documents against a generated schema\n---\nRead schemas/large.xsd on demand.\n")},
			{Path: "schemas/large.xsd", Data: large},
			{Path: "schemas/small.xsd", Data: small},
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

		posted, err := p.StartThread(t.Context(), who, platform.ThreadInput{Project: "Shared", Agents: []string{"coder"}, Text: "$schemas Validate the document"})
		if err != nil || len(posted.Runs) != 1 {
			t.Fatalf("start: %v", err)
		}
		await(t, p, posted.Runs[0].ID, runs.Completed)
		coder.mu.Lock()
		got, asked := coder.hashes["large.xsd"], strings.Join(coder.asked, "\n")
		coder.mu.Unlock()
		if got != sha256.Sum256(large) {
			t.Fatalf("the coding agent did not read the large file whole: %x\nasked: %.2000s", got, asked)
		}
		if !strings.Contains(asked, fmt.Sprintf("large.xsd (%d bytes)", library.MaxPackageFileBytes)) {
			t.Fatalf("the large file was not named with its size: %.2000s", asked)
		}
		if strings.Contains(asked, fixtureMarker) || len(asked) > 16<<10 {
			t.Fatalf("a supporting file's bytes reached what the coding agent was asked (%d bytes)", len(asked))
		}

		posted, err = p.StartThread(t.Context(), who, platform.ThreadInput{Project: "Shared", Agents: []string{"plain"}, Text: "$schemas Validate the document"})
		if err != nil || len(posted.Runs) != 1 {
			t.Fatalf("start: %v", err)
		}
		run := await(t, p, posted.Runs[0].ID, runs.Completed)
		plain.mu.Lock()
		asked = strings.Join(plain.asked, "\n")
		plain.mu.Unlock()
		for _, line := range []string{
			fmt.Sprintf("skill:schemas/schemas/large.xsd (%d bytes), over the %d bytes file_read returns\n", library.MaxPackageFileBytes, readBound),
			fmt.Sprintf("skill:schemas/schemas/small.xsd (%d bytes)\n", readBound),
		} {
			if !strings.Contains(asked, line) {
				t.Fatalf("the native manifest lacks %q: %.2000s", line, asked)
			}
		}
		if strings.Contains(asked, fixtureMarker) {
			t.Fatal("a supporting file's bytes reached what the native model was asked")
		}
		req := runtime.Request{RunID: run.ID, Tenant: tenant, Definition: native.Revision{Spec: native.Spec{Name: "plain"}, Version: wire.VersionOf(1)}}
		out, err := use(t, p, req, platform.ToolFileRead, map[string]any{"path": "skill:schemas/schemas/small.xsd"})
		if err != nil || out != string(small) {
			t.Fatalf("file_read of a file at the bound: %d bytes, %v", len(out), err)
		}
		out, err = use(t, p, req, platform.ToolFileRead, map[string]any{"path": "skill:schemas/schemas/large.xsd"})
		if !errors.Is(err, platform.ErrSandbox) || !strings.Contains(err.Error(), fmt.Sprintf("over the %d bytes file_read returns", readBound)) || out != "" {
			t.Fatalf("file_read of the large file = %d bytes, %v", len(out), err)
		}
	})
}
