package wire

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/envelope"
)

func TestText(t *testing.T) {
	tag := NewInput(envelope.Tag{ID: "t1"}, "hello")
	if TextOf(tag) != "hello" || tag.ID != "t1" {
		t.Fatalf("NewInput = %+v", tag)
	}
	SetText(&tag, "again")
	if TextOf(tag) != "again" {
		t.Fatalf("SetText left %q", TextOf(tag))
	}
}

func TestSurfaceOf(t *testing.T) {
	tag := envelope.Tag{Meta: map[string]string{
		"slack_event": "app_mention", "slack_channel": "C1", "slack_command": "/ask",
		"github_event": "issue_comment", "github_owner": "o", "github_repo": "r",
		"github_labels": "a,b", "github_author": "u", "cron_schedule": "daily",
	}}
	SetGitHubHeadSHA(&tag, "abc")
	want := Surface{
		SlackEvent: "app_mention", SlackChannel: "C1", SlackCommand: "/ask",
		GitHubEvent: "issue_comment", GitHubOwner: "o", GitHubRepo: "r",
		GitHubLabels: "a,b", GitHubAuthor: "u", GitHubHeadSHA: "abc", CronSchedule: "daily",
	}
	if got := SurfaceOf(tag); got != want {
		t.Fatalf("SurfaceOf = %+v, want %+v", got, want)
	}
	if got := SurfaceOf(envelope.Tag{}); got != (Surface{}) {
		t.Fatalf("SurfaceOf(empty) = %+v", got)
	}
}

func TestKinds(t *testing.T) {
	for k, want := range map[Kind][3]bool{
		KindCompleted:   {true, false, false},
		KindFailed:      {true, false, false},
		KindStopped:     {true, false, false},
		KindParked:      {false, true, false},
		KindChildParked: {false, true, false},
		KindAccepted:    {false, false, true},
		KindStarted:     {false, false, true},
		KindResumed:     {false, false, true},
		KindText:        {false, false, false},
	} {
		if got := [3]bool{IsTerminal(k), IsParked(k), IsActive(k)}; got != want {
			t.Errorf("%s: terminal, parked, active = %v, want %v", k, got, want)
		}
	}
}

func TestUsageOf(t *testing.T) {
	raw, _ := json.Marshal(payload.UsageCall{AccountingID: "a1"})
	c, child, err := UsageOf(envelope.Event{Kind: KindUsageCall, Payload: raw})
	if err != nil || c == nil || child != nil || c.AccountingID != "a1" {
		t.Fatalf("usage.call = %v %v %v", c, child, err)
	}
	raw, _ = json.Marshal(payload.UsageChild{AccountingID: "a2"})
	c, child, err = UsageOf(envelope.Event{Kind: KindUsageChild, Payload: raw})
	if err != nil || c != nil || child == nil || child.AccountingID != "a2" {
		t.Fatalf("usage.child = %v %v %v", c, child, err)
	}
	if _, _, err := UsageOf(envelope.Event{Kind: KindText}); err == nil {
		t.Fatal("a text delta read as usage")
	}
	if _, _, err := UsageOf(envelope.Event{Kind: KindUsageCall, Payload: []byte("{")}); err == nil {
		t.Fatal("a malformed usage body read")
	}
}

func TestPolicyOf(t *testing.T) {
	spec := AgentSpec{TaggedOnly: true, Approvers: []string{"a"}, Access: Access{Spawn: []string{"b"}}}
	p := PolicyOf(spec)
	if !p.TaggedOnly || p.Approvers[0] != "a" || p.Access.Spawn[0] != "b" {
		t.Fatalf("PolicyOf = %+v", p)
	}
}

// TestSeam holds the rest of the module to this package: only here and in
// pkg/call may code import fiber's agent registry or definitions, decode an
// event body, or name a connector's meta key.
func TestSeam(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", root, "ls-files", "-z", "*.go").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	banned := map[string]bool{
		"github.com/urmzd/fiber/pkg/registry":  true,
		"github.com/urmzd/fiber/pkg/agentspec": true,
	}
	keys := map[string]bool{}
	for _, k := range []string{MetaSlackEvent, MetaSlackChannel, MetaSlackCommand, MetaGitHubEvent, MetaGitHubOwner, MetaGitHubRepo, MetaGitHubLabels, MetaGitHubAuthor, MetaGitHubHeadSHA, MetaCronSchedule, MetaCronExpression} {
		keys[k] = true
	}
	var bad []string
	for _, p := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if p == "" || strings.HasSuffix(p, "_test.go") || strings.HasPrefix(p, "internal/wire/") || strings.HasPrefix(p, "pkg/call/") || strings.HasPrefix(p, "fiber/") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(root, p), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		payloadName := ""
		for _, im := range f.Imports {
			path, _ := strconv.Unquote(im.Path.Value)
			if banned[path] {
				bad = append(bad, fset.Position(im.Pos()).String()+": imports "+path)
			}
			if path == "github.com/urmzd/fiber/pkg/agentrt/payload" {
				payloadName = "payload"
				if im.Name != nil {
					payloadName = im.Name.Name
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && payloadName != "" && id.Name == payloadName && (x.Sel.Name == "Decode" || x.Sel.Name == "Of") {
					bad = append(bad, fset.Position(x.Pos()).String()+": payload."+x.Sel.Name)
				}
			case *ast.BasicLit:
				if x.Kind == token.STRING {
					if v, err := strconv.Unquote(x.Value); err == nil && keys[v] {
						bad = append(bad, fset.Position(x.Pos()).String()+": meta key "+x.Value)
					}
				}
			}
			return true
		})
	}
	if len(bad) > 0 {
		t.Errorf("fiber's run model is read outside internal/wire:\n%s", strings.Join(bad, "\n"))
	}
}
