package app

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/spec"
)

// The example profiles are configuration people copy and start. When the
// claude-code and codex model kinds were removed, twelve of them stopped
// starting and nothing noticed. These tests read every one the way
// `humagen config check` does, and hold the docs to the example files that
// exist.

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

func TestEveryExampleProfilePassesConfigCheck(t *testing.T) {
	for _, path := range exampleProfiles(t) {
		t.Run(strings.TrimPrefix(path, examplesDir+"/"), func(t *testing.T) {
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

// Neither config check nor start refuses a tool name no registry has: the
// agent just goes without it. So each example agent's tools are held to the
// names the platform registers that an example uses, and to the tool
// servers its own file declares. Grant another platform tool in an example
// and add its name here.
func TestExampleAgentsGrantOnlyToolsThatExist(t *testing.T) {
	known := slices.Concat(platform.FileTools(), platform.WebTools(), []string{platform.ToolShell, "person_tools"})
	for _, path := range exampleProfiles(t) {
		cfg, err := spec.Load(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for _, p := range cfg.Platforms {
			names := slices.Clone(known)
			for _, tool := range p.Spec.Tools {
				names = append(names, "mcp:"+tool.Name, "mcp:"+tool.Name+":write")
			}
			for _, a := range p.Spec.Agents {
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

// examplePath is a path under examples/ as prose names it.
var examplePath = regexp.MustCompile(`examples/[A-Za-z0-9_./-]+`)

func TestDocsNameOnlyExampleFilesThatExist(t *testing.T) {
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
		b, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range examplePath.FindAllString(string(b), -1) {
			name = strings.TrimRight(name, "./")
			if _, err := os.Stat(filepath.Join("../..", name)); err != nil {
				t.Errorf("%s names %s, which does not exist", doc, name)
			}
		}
	}
}
