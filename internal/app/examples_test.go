package app

import (
	"bytes"
	"context"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/saige/agent/provider/fallback"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/attachment"
	"github.com/urmzd/saige/flow/attachment/resolve"
	"github.com/urmzd/saige/flow/blob"
	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/spec"
)

// The example profiles are configuration people copy and start. When the
// claude-code and codex model kinds were removed, twelve of them stopped
// starting and nothing noticed. These tests read every one the way
// `humagen config check` does, hold each agent to tools that exist and to
// the media its guide says it reads, and hold the docs and the examples'
// own comments to files that exist.

const examplesDir = "../../examples"

// exampleProfiles is every YAML file under examples/.
func exampleProfiles(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(examplesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "node_modules" {
			return filepath.SkipDir
		}
		if ext := filepath.Ext(path); !d.IsDir() && (ext == ".yaml" || ext == ".yml") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("no example profiles found under " + examplesDir)
	}
	return out
}

func exampleName(path string) string { return strings.TrimPrefix(path, examplesDir+"/") }

func TestEveryExampleProfilePassesConfigCheck(t *testing.T) {
	for _, path := range exampleProfiles(t) {
		t.Run(exampleName(path), func(t *testing.T) {
			var out bytes.Buffer
			if err := configCmd(Options{}, []string{"check", "-f", path}, &out); err != nil {
				t.Fatalf("humagen config check -f %s: %v", path, err)
			}
			if !strings.Contains(out.String(), "ok: ") {
				t.Fatalf("humagen config check -f %s said:\n%s", path, out.String())
			}
		})
	}
}

// grantable is every name an agent of plat may grant: the tools the
// platform's registry offers to runs, person_tools (the grant a run's context
// turns into the person's own approved servers), and each tool server plat
// declares, read-only or with writes.
func grantable(t *testing.T, p *platform.Platform, plat spec.Platform) []string {
	t.Helper()
	tools, err := p.SystemTools()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools {
		if tool.Exposure.Runs() {
			names = append(names, tool.Name)
		}
	}
	names = append(names, "person_tools")
	for _, tool := range plat.Spec.Tools {
		names = append(names, wire.MCPGroupPrefix+tool.Name, wire.MCPGroupPrefix+tool.Name+":write")
	}
	return names
}

// Neither config check nor start refuses a tool name no registry has: the
// agent just goes without it. So each example agent's tools are held to the
// platform's own registry and the tool servers its file declares.
func TestExampleAgentsGrantOnlyToolsThatExist(t *testing.T) {
	p, err := platform.New(platform.Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	// The registry is the real one: tools an example does not use are in
	// it, and a misspelt one is not.
	names := grantable(t, p, spec.Platform{})
	for _, real := range []string{"ask_person", "memory_search", "web_search", "file_read", "shell", "person_tools"} {
		if !slices.Contains(names, real) {
			t.Errorf("the registry has no %q", real)
		}
	}
	for _, wrong := range []string{"web_serch", "file_reed", "mcp:browser"} {
		if slices.Contains(names, wrong) {
			t.Errorf("the registry offers %q", wrong)
		}
	}

	for _, path := range exampleProfiles(t) {
		cfg, err := spec.Load(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for _, plat := range cfg.Platforms {
			names := grantable(t, p, plat)
			for _, a := range plat.Spec.Agents {
				if a.Spec == nil {
					continue
				}
				for _, tool := range a.Spec.Tools {
					if !slices.Contains(names, tool) {
						t.Errorf("%s: agent %q grants %q, which no registry or tool server in the file has", path, a.Spec.Name, tool)
					}
				}
			}
		}
	}
}

// exampleChains builds every example agent's model the way a turn does: each
// model of its chain through the file's own provider construction
// (fileExtensions), with a fake key and no network, joined as saige's native
// runtime joins them (its modelChain): one model alone, or a fallback chain,
// which accepts only the media every member accepts.
func exampleChains(t *testing.T) map[string]saigetypes.Provider {
	t.Helper()
	for _, env := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GOOGLE_API_KEY", "GROQ_API_KEY"} {
		t.Setenv(env, "fake-not-a-key")
	}
	out := map[string]saigetypes.Provider{}
	for _, path := range exampleProfiles(t) {
		cfg, err := spec.Load(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		ext, err := fileExtensions(context.Background(), Options{}, cfg, credref.Default)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for _, plat := range cfg.Platforms {
			for _, a := range plat.Spec.Agents {
				if a.Spec == nil {
					continue
				}
				refs := append([]wire.ModelRef{{Provider: a.Spec.Provider, Model: a.Spec.Model}}, a.Spec.Fallbacks...)
				var members []saigetypes.Provider
				for _, ref := range refs {
					build, ok := ext.Providers[ref.Provider]
					if !ok {
						t.Fatalf("%s: agent %q names provider %q, which the file does not declare", path, a.Spec.Name, ref.Provider)
					}
					m, err := build(native.Spec{Name: a.Spec.Name, Provider: ref.Provider, Model: ref.Model})
					if err != nil {
						t.Fatalf("%s: agent %q: %v", path, a.Spec.Name, err)
					}
					members = append(members, m)
				}
				chain := members[0]
				if len(members) > 1 {
					if chain, err = fallback.New(fallback.Config{Providers: members}); err != nil {
						t.Fatalf("%s: agent %q: %v", path, a.Spec.Name, err)
					}
				}
				out[exampleName(path)+"#"+a.Spec.Name] = chain
			}
		}
	}
	return out
}

func nativeMedia(t *testing.T, p saigetypes.Provider) saigetypes.ContentSupport {
	t.Helper()
	caps, ok := saigetypes.ProviderCapabilities(p)
	if !ok {
		t.Fatal("the model reports no capabilities, so a turn treats it as text only")
	}
	return caps.Media
}

// What each example's guide says its agent reads of an attachment: the
// media types its model chain must take natively, and those it must not.
var exampleMediaClaims = map[string]struct{ takes, refuses []saigetypes.MediaType }{
	// docs/guides/media-evidence.md: PDF pages, images, MP3, WAV and MP4 on
	// Gemini; M4A not as audio.
	"media-evidence/humagen.yaml#analyst": {
		takes:   []saigetypes.MediaType{"application/pdf", "image/png", "image/jpeg", "image/gif", "image/webp", "audio/mpeg", "audio/wav", "video/mp4"},
		refuses: []saigetypes.MediaType{"audio/mp4"},
	},
	// docs/guides/records-research.md: a scan, photo or PDF record is attached.
	"records-research/humagen.yaml#records": {takes: []saigetypes.MediaType{"application/pdf", "image/png", "image/jpeg"}},
	// The browser and desktop guides: the model reads returned screenshots.
	"browser/humagen.yaml#browser": {takes: []saigetypes.MediaType{"image/png"}},
	"desktop/humagen.yaml#desktop": {takes: []saigetypes.MediaType{"image/png"}},
}

func TestExampleAgentsReadTheMediaTheirGuidesSay(t *testing.T) {
	chains := exampleChains(t)
	agents := slices.Sorted(maps.Keys(chains))
	for _, agent := range agents {
		var types []string
		for mt, on := range nativeMedia(t, chains[agent]).NativeTypes {
			if on {
				types = append(types, string(mt))
			}
		}
		slices.Sort(types)
		t.Logf("%s takes %v", agent, types)
	}
	for agent, claim := range exampleMediaClaims {
		chain, ok := chains[agent]
		if !ok {
			t.Errorf("%s: no such example agent", agent)
			continue
		}
		media := nativeMedia(t, chain)
		for _, mt := range claim.takes {
			if !media.NativeTypes[mt] {
				t.Errorf("%s: its model chain does not take %s natively, though its guide says it reads it", agent, mt)
			}
		}
		for _, mt := range claim.refuses {
			if media.NativeTypes[mt] {
				t.Errorf("%s: its model chain takes %s natively, though its guide says it does not", agent, mt)
			}
		}
	}
}

// A fallback to a model that cannot hear would make every recording a note
// for the media agent: a chain takes only what all its members take.
func TestMediaEvidenceAgentHearsAudio(t *testing.T) {
	chains := exampleChains(t)
	chain := chains["media-evidence/humagen.yaml#analyst"]
	if chain == nil {
		t.Fatal("the media-evidence profile has no analyst")
	}
	media := nativeMedia(t, chain)

	// A generated WAV, as the attachment resolver a turn uses sees it.
	wav := []byte("RIFF\x24\x1f\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x40\x1f\x00\x00\x80\x3e\x00\x00\x02\x00\x10\x00data\x00\x1f\x00\x00")
	wav = append(wav, make([]byte, 0x1f00)...)
	store := blob.NewMemory()
	info, err := store.Put(context.Background(), "t", bytes.NewReader(wav), blob.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	a := attachment.Attachment{Digest: string(info.Digest), MediaType: attachment.Sniff(wav, "recording.wav"), Name: "recording.wav", Size: info.Size}
	if a.MediaType != "audio/wav" {
		t.Fatalf("the generated recording sniffs as %s", a.MediaType)
	}
	res, err := resolve.New(store).Resolve(context.Background(), resolve.Request{Tenant: "t", Attachments: []attachment.Attachment{a}, Model: media})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Outcomes[0]; got.Form != resolve.FormNative {
		t.Fatalf("a WAV reaches the media agent as %s (%s), not as audio", got.Form, got.Reason)
	}

	// The trap itself: the same agent with a fallback that cannot hear.
	anthropic, err := modelProvider("anthropic", "anthropic", spec.ModelBlock{}, credref.Default)(native.Spec{Name: "a", Model: "claude-sonnet-5-5"})
	if err != nil {
		t.Fatal(err)
	}
	mixed, err := fallback.New(fallback.Config{Providers: []saigetypes.Provider{chain, anthropic}})
	if err != nil {
		t.Fatal(err)
	}
	if nativeMedia(t, mixed).NativeTypes["audio/wav"] {
		t.Fatal("a chain with a model that cannot hear still takes audio; this test no longer shows why the profile has no such fallback")
	}
}

// pathIn is a path under examples/ or docs/ as prose or a comment names it.
var pathIn = regexp.MustCompile(`(?:examples|docs)/[A-Za-z0-9_./-]+`)

// namedPaths reports each path name names under examples/ or docs/ that does
// not exist.
func namedPaths(t *testing.T, file string, only func(string) bool) {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range pathIn.FindAllString(string(b), -1) {
		name = strings.TrimRight(name, "./")
		if !only(name) {
			continue
		}
		if _, err := os.Stat(filepath.Join("../..", name)); err != nil {
			t.Errorf("%s names %s, which does not exist", file, name)
		}
	}
}

func TestDocsNameOnlyExampleFilesThatExist(t *testing.T) {
	examplesOnly := func(name string) bool { return strings.HasPrefix(name, "examples/") }
	everyPath := func(string) bool { return true }

	// The docs and READMEs: the example files they name.
	docs := []string{"../../README.md", "../../AGENTS.md"}
	apps, err := filepath.Glob("../../apps/*/README.md")
	if err != nil {
		t.Fatal(err)
	}
	docs = append(docs, apps...)
	err = filepath.WalkDir("../../docs", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Assets are retained evidence of earlier states, not instructions.
		if d.IsDir() && d.Name() == "assets" {
			return filepath.SkipDir
		}
		if !d.IsDir() && filepath.Ext(path) == ".md" {
			docs = append(docs, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range docs {
		namedPaths(t, doc, examplesOnly)
	}

	// The examples' own files and comments: every example file and guide
	// they name.
	err = filepath.WalkDir(examplesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == "__pycache__") {
			return filepath.SkipDir
		}
		switch filepath.Ext(path) {
		case ".yaml", ".yml", ".sh", ".py", ".mjs", ".json", ".md", ".swift":
			namedPaths(t, path, everyPath)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// The docs site's navigation, whose pages are under docs/.
	toml, err := os.ReadFile("../../zensical.toml")
	if err != nil {
		t.Fatal(err)
	}
	pages := regexp.MustCompile(`"([A-Za-z0-9_./-]+\.md)"`).FindAllStringSubmatch(string(toml), -1)
	if len(pages) == 0 {
		t.Fatal("zensical.toml names no pages")
	}
	for _, page := range pages {
		if _, err := os.Stat(filepath.Join("../../docs", page[1])); err != nil {
			t.Errorf("zensical.toml names %s, which is not under docs/", page[1])
		}
	}
	namedPaths(t, "../../zensical.toml", everyPath)
}
