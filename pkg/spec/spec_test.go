package spec

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The reference's own examples load, so docs/spec.md cannot drift from the
// language it describes.
func TestTheReferenceExamplesLoad(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile("(?s)```yaml\n(.*?)```").FindAllStringSubmatch(string(doc), -1)
	if len(blocks) < 2 {
		t.Fatalf("found %d yaml examples", len(blocks))
	}
	var all []string
	for _, b := range blocks {
		all = append(all, b[1])
	}
	path := filepath.Join(t.TempDir(), "warp.yaml")
	writeFile(t, path, strings.Join(all, "---\n"))
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.System.Spec.Stores.Kind != "postgres" || len(cfg.Platforms) != 1 || len(cfg.Platforms[0].Spec.Models) != 2 {
		t.Fatalf("config = %+v", cfg)
	}
	oidc, err := Block[OIDC](cfg.System.Spec.Auth.Slot)
	if err != nil || oidc.ClientSecret.Ref != "env:WARP_OIDC_CLIENT_SECRET" || cfg.System.Spec.Auth.Groups.Roles["admin"][0] != "platform-admins" {
		t.Fatalf("auth = %+v %+v %v", cfg.System.Spec.Auth, oidc, err)
	}
	if err := cfg.Check(Kinds{}); err != nil {
		t.Fatalf("strict check: %v", err)
	}
	w := cfg.Platforms[0].Spec.Workspace
	if w.Instructions == nil || len(w.Projects) != 3 {
		t.Fatalf("workspace = %+v", w)
	}
	if g := w.Projects[2]; g.Parent != "Field" || g.Kind != ProjectAccount || g.Account == nil || g.Account.URL != "https://crm.example.com/globex" {
		t.Fatalf("the example's account project = %+v", g)
	}
	if g := w.Projects[2]; len(w.Channels) != 1 || len(w.Experts) != 1 || len(g.Channels) != 1 || g.Channels[0].Default != "auto" || len(g.Experts) != 1 {
		t.Fatalf("the example's channels and experts = %+v %+v %+v", w.Channels, w.Experts, g)
	}
}

func TestNoFileIsAWorkingDefault(t *testing.T) {
	var s System
	s.Defaults()
	if s.Spec.Auth.Kind != "dev" || s.Spec.Server.Addr != "127.0.0.1:8080" || s.Spec.Stores.Kind != "memory" || s.Tenant() != "default" {
		t.Fatalf("defaults = %+v", s)
	}
}

func TestMistakesAreRefusedWithTheirPlace(t *testing.T) {
	sys := "api_version: warp/v1\nkind: System\nspec:\n"
	plat := "api_version: warp/v1\nkind: Platform\nmetadata: {name: default}\nspec:\n"
	for name, tc := range map[string]struct{ body, want string }{
		"a typo":                   {sys + "  stores: {kind: memory}\n  storse: {}\n", `unknown field "storse"`},
		"a secret as a value":      {sys + "  stores: {kind: postgres, postgres: {url: postgres://u:p@h/db}}\n", "a secret is a reference"},
		"a pasted key":             {plat + "  models: [{name: a, kind: anthropic, anthropic: {key: {ref: sk-ant-123}}}]\n", "use env:NAME"},
		"a block for another kind": {sys + "  blobs: {kind: disk, s3: {bucket: b}}\n", `unknown field "s3"`},
		"a field in a block":       {sys + "  blobs: {kind: s3, s3: {bucket: b, bukket: c}}\n", `unknown field "bukket"`},
		"no url":                   {sys + "  stores: {kind: postgres}\n", "url is required"},
		"half the s3 keys":         {sys + "  blobs: {kind: s3, s3: {bucket: b, access_key: {ref: env:A}}}\n", "go together"},
		"an old version":           {"api_version: warp/v0\nkind: System\n", "api_version"},
		"an unknown document":      {"api_version: warp/v1\nkind: Cluster\n", "kind is System or Platform"},
		"an unpinned tool":         {plat + "  tools: [{name: d, kind: mcp, mcp: {transport: http, endpoint: 'https://a', accept: [{name: s}]}}]\n", "schema_hash"},
		"slack without tokens":     {plat + "  surfaces: {slack: {enabled: true, team: T1}}\n", "bot_token"},
		"an unreachable tenant":    {plat[:len(plat)-len("metadata: {name: default}\nspec:\n")] + "metadata: {name: other}\n", "cannot be reached"},
		"two systems":              {sys + "---\n" + sys, "second System"},
	} {
		path := filepath.Join(t.TempDir(), "warp.yaml")
		writeFile(t, path, tc.body)
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

func TestAKindIsOneTheBinaryHas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warp.yaml")
	writeFile(t, path, "api_version: warp/v1\nkind: System\nspec:\n  blobs: {kind: gcs, gcs: {bucket: b}}\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load cannot know the binary's kinds, so it lets one through: %v", err)
	}
	if err := cfg.Check(Kinds{}); err == nil || !strings.Contains(err.Error(), "disk, memory, s3") {
		t.Fatalf("an unregistered kind: %v", err)
	}
	if err := cfg.Check(Kinds{SlotBlobs: {"gcs"}}); err != nil {
		t.Fatalf("a registered kind: %v", err)
	}
	var blk struct{ Bucket string }
	if err := cfg.System.Spec.Blobs.Decode(&blk); err != nil || blk.Bucket != "b" {
		t.Fatalf("its block: %+v %v", blk, err)
	}
}

func TestWhatWarpWritesItReads(t *testing.T) {
	baseline := "lead"
	cfg := Config{Platforms: []Platform{{Metadata: Metadata{Name: "default"}, Spec: PlatformSpec{
		Workspace: Workspace{Baseline: &baseline},
		Models:    []Model{{Slot: Slot{Kind: "anthropic", Block: MustJSON(ModelBlock{Key: &Ref{Ref: "env:ANTHROPIC_API_KEY"}})}, ModelShared: ModelShared{Name: "anthropic"}}},
		Surfaces:  Surfaces{Slack: &Slack{Enabled: true, Team: "T1", BotToken: &Ref{Ref: "env:B"}, AppToken: &Ref{Ref: "keychain:warp/slack"}}},
	}}}}
	cfg.System.Defaults()
	b, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "warp.yaml")
	writeFile(t, path, string(b))
	got, err := Load(path)
	if err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	if *got.Platforms[0].Spec.Workspace.Baseline != "lead" || got.Platforms[0].Spec.Surfaces.Slack.AppToken.Ref != "keychain:warp/slack" || got.System.Spec.Auth.Kind != "dev" {
		t.Fatalf("round trip lost something:\n%s", b)
	}
	if !strings.HasPrefix(string(b), "api_version: warp/v1\nkind: System\n") {
		t.Errorf("not in declaration order:\n%s", b)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
