package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/toolprovider"
	"github.com/urmzd/humagen/internal/wire"
)

// The reference's own examples load, so docs/reference/spec.md cannot drift from the
// language it describes.
func TestTheReferenceExamplesLoad(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "reference", "spec.md"))
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
	path := filepath.Join(t.TempDir(), "humagen.yaml")
	writeFile(t, path, strings.Join(all, "---\n"))
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.System.Spec.Stores.Kind != "postgres" || len(cfg.Platforms) != 1 || len(cfg.Platforms[0].Spec.Models) != 3 {
		t.Fatalf("config = %+v", cfg)
	}
	oidc, err := Block[OIDC](cfg.System.Spec.Auth.Slot)
	if err != nil || oidc.ClientSecret.Ref != "env:HUMAGEN_OIDC_CLIENT_SECRET" || cfg.System.Spec.Auth.Groups.Roles["admin"][0] != "platform-admins" {
		t.Fatalf("auth = %+v %+v %v", cfg.System.Spec.Auth, oidc, err)
	}
	if err := cfg.Check(Kinds{}); err != nil {
		t.Fatalf("strict check: %v", err)
	}
	if r, err := cfg.System.Spec.Rooms.Config(); err != nil || r.CoalesceAbove != 200 || r.TailInterval != 3*time.Second || r.RoomRunsPerMinute != 600 || r.PersonRuns != 25 {
		t.Fatalf("rooms = %+v %v", r, err)
	}
	w := cfg.Platforms[0].Spec.Workspace
	if w.Instructions == nil || len(w.Projects) != 3 || !w.ReleaseNotes || w.Retention() != 180*24*time.Hour {
		t.Fatalf("workspace = %+v", w)
	}
	if g := w.Projects[2]; g.Parent != "Field" || g.Kind != ProjectAccount || g.Account == nil || g.Account.URL != "https://crm.example.com/globex" {
		t.Fatalf("the example's account project = %+v", g)
	}
	if g := w.Projects[2]; len(w.Channels) != 1 || len(w.Experts) != 1 || len(g.Channels) != 2 || g.Channels[0].Default != "*" || g.Channels[1].Slack == nil || len(g.Experts) != 1 {
		t.Fatalf("the example's channels and experts = %+v %+v %+v", w.Channels, w.Experts, g)
	}
	if f := w.Projects[1]; len(w.Knowledge) != 1 || len(f.Knowledge) != 1 || f.Knowledge[0] != "field-playbook" {
		t.Fatalf("the example's knowledge = %v %v", w.Knowledge, f.Knowledge)
	}
	if f := w.Projects[1]; len(w.Skills) != 1 || len(f.Skills) != 1 || f.Skills[0].Name != "review" || f.Skills[0].ArgumentHint != "[account]" ||
		len(w.MentionKinds) != 1 || w.MentionKinds[0].Resolves != "role" {
		t.Fatalf("the example's skills and mention kinds = %+v %+v %+v", w.Skills, f.Skills, w.MentionKinds)
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
	sys := "api_version: humagen/v1\nkind: System\nspec:\n"
	plat := "api_version: humagen/v1\nkind: Platform\nmetadata: {name: default}\nspec:\n"
	for name, tc := range map[string]struct{ body, want string }{
		"a typo":                   {sys + "  stores: {kind: memory}\n  storse: {}\n", `unknown field "storse"`},
		"a secret as a value":      {sys + "  stores: {kind: postgres, postgres: {url: postgres://u:p@h/db}}\n", "a secret is a reference"},
		"a pasted key":             {plat + "  models: [{name: a, kind: anthropic, anthropic: {key: {ref: sk-ant-123}}}]\n", "use env:NAME"},
		"a block for another kind": {sys + "  blobs: {kind: disk, s3: {bucket: b}}\n", `unknown field "s3"`},
		"a field in a block":       {sys + "  blobs: {kind: s3, s3: {bucket: b, bukket: c}}\n", `unknown field "bukket"`},
		"no url":                   {sys + "  stores: {kind: postgres}\n", "url is required"},
		"half the s3 keys":         {sys + "  blobs: {kind: s3, s3: {bucket: b, access_key: {ref: env:A}}}\n", "go together"},
		"an old version":           {"api_version: humagen/v0\nkind: System\n", "api_version"},
		"an unknown document":      {"api_version: humagen/v1\nkind: Cluster\n", "kind is System or Platform"},
		"an unpinned tool":         {plat + "  tools: [{name: d, kind: mcp, mcp: {transport: http, endpoint: 'https://a', accept: [{name: s}]}}]\n", "schema_hash"},
		"slack without tokens":     {plat + "  surfaces: {slack: {enabled: true, team: T1}}\n", "bot_token"},
		"an unreachable tenant":    {plat[:len(plat)-len("metadata: {name: default}\nspec:\n")] + "metadata: {name: other}\n", "cannot be reached"},
		"two systems":              {sys + "---\n" + sys, "second System"},
		"a person-scoped tool with a credential": {plat + "  tools: [{name: d, kind: mcp, mcp: {transport: http, endpoint: 'https://a', credential_scope: person, credential: {ref: env:X}}}]\n",
			"names no credential"},
		"an unknown credential scope": {plat + "  tools: [{name: d, kind: mcp, mcp: {transport: http, endpoint: 'https://a', credential_scope: team}}]\n", "shared or person"},
		"a person-scoped stdio tool with nowhere to send it": {plat + "  tools: [{name: d, kind: mcp, mcp: {transport: stdio, endpoint: srv, credential_scope: person}}]\n",
			"credential_as"},
		"a person-scoped tool without its form": {plat + "  tools: [{name: d, kind: mcp, mcp: {transport: http, endpoint: 'https://a', credential_scope: person, accept: [{name: s, schema_hash: abc}]}}]\n",
			"tools[0] \"d\": accept[0] \"s\""},
		"a person reference the server cannot confine": {sys + "  person_refs: {default: ['env:ANTHROPIC_']}\n", "person_refs.default"},
		"a person reference that is nobody's own":      {sys + "  person_refs: {default: ['op://People/']}\n", "person_refs.default"},
		"two workspaces' people in one place": {sys + "  person_refs: {default: ['op://People/{subject}/'], globex: ['op://People/{subject}/']}\n",
			"could name one reference for two people"},
		"an inbox kept less than a week":  {plat + "  workspace: {inbox_retention: 6d}\n", "workspace.inbox_retention: \"6d\" is less than the least retention, 7d"},
		"a call depth past the hop limit": {sys + "  execution: {max_depth: 8}\n", "execution.max_depth: 8"},
		"an inbox kept for no time":       {plat + "  workspace: {inbox_retention: forever}\n", "workspace.inbox_retention: \"forever\" is whole days"},
	} {
		path := filepath.Join(t.TempDir(), "humagen.yaml")
		writeFile(t, path, tc.body)
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

func TestAKindIsOneTheBinaryHas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "humagen.yaml")
	writeFile(t, path, "api_version: humagen/v1\nkind: System\nspec:\n  blobs: {kind: gcs, gcs: {bucket: b}}\n")
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

func TestWhatHumagenWritesItReads(t *testing.T) {
	cfg := Config{Platforms: []Platform{{Metadata: Metadata{Name: "default"}, Spec: PlatformSpec{
		Workspace: Workspace{Agents: &Agents{Enable: []string{EveryAgent}, Default: "lead"}},
		Agents:    []Agent{{Spec: &wire.AgentSpec{Name: "lead", Provider: "anthropic", Model: "m", SystemPrompt: "Lead."}}},
		Models:    []Model{{Slot: Slot{Kind: "anthropic", Block: MustJSON(ModelBlock{Key: &Ref{Ref: "env:ANTHROPIC_API_KEY"}})}, ModelShared: ModelShared{Name: "anthropic"}}},
		Surfaces:  Surfaces{Slack: &Slack{Enabled: true, Team: "T1", BotToken: &Ref{Ref: "env:B"}, AppToken: &Ref{Ref: "keychain:humagen/slack"}}},
	}}}}
	cfg.System.Defaults()
	b, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "humagen.yaml")
	writeFile(t, path, string(b))
	got, err := Load(path)
	if err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	if got.Platforms[0].Spec.Workspace.Agents.Default != "lead" || got.Platforms[0].Spec.Surfaces.Slack.AppToken.Ref != "keychain:humagen/slack" || got.System.Spec.Auth.Kind != "dev" {
		t.Fatalf("round trip lost something:\n%s", b)
	}
	if !strings.HasPrefix(string(b), "api_version: humagen/v1\nkind: System\n") {
		t.Errorf("not in declaration order:\n%s", b)
	}
}

// A person-scoped tool is declared with each tool's whole form, and what
// humagen writes it reads back.
func TestAPersonScopedToolDeclaresItsWholeForm(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)
	full, err := toolprovider.NewTool("search", "Search issues", schema)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Platforms: []Platform{{Metadata: Metadata{Name: "default"}, Spec: PlatformSpec{
		Workspace: Workspace{PersonCredentials: &PersonCredentials{Values: true}},
		Tools: []Tool{{ToolShared: ToolShared{Name: "issues"}, Slot: Slot{Kind: "mcp", Block: MustJSON(MCP{
			Transport: "http", Endpoint: "https://issues.example.com/mcp", CredentialScope: ScopePerson,
			Accept: []AcceptTool{{Name: "search", Description: "Search issues", InputSchema: schema, SchemaHash: full.SchemaHash, Class: "read"}},
		})}}},
	}}}}
	cfg.System.Defaults()
	cfg.System.Spec.PersonRefs = map[string][]string{"default": {"op://People/{subject}/"}}
	b, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "humagen.yaml")
	writeFile(t, path, string(b))
	got, err := Load(path)
	if err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	tool, err := Block[MCP](got.Platforms[0].Spec.Tools[0].Slot)
	if err != nil || tool.CredentialScope != ScopePerson || len(tool.Accept) != 1 || tool.Accept[0].Description != "Search issues" {
		t.Fatalf("the tool came back as %+v, %v\n%s", tool, err, b)
	}
	again, err := toolprovider.NewTool("search", tool.Accept[0].Description, tool.Accept[0].InputSchema)
	if err != nil || again.SchemaHash != full.SchemaHash {
		t.Fatalf("the schema came back differently: %s", tool.Accept[0].InputSchema)
	}
	if !got.Platforms[0].Spec.Workspace.PersonCredentials.Values || got.System.Spec.PersonRefs["default"][0] != "op://People/{subject}/" {
		t.Fatalf("round trip lost something:\n%s", b)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// An inbox's retention is whole days or a duration, 90 days when left out,
// and never less than a week.
func TestAnInboxRetentionIsDaysOrADuration(t *testing.T) {
	day := 24 * time.Hour
	for in, want := range map[string]time.Duration{"": 90 * day, "90d": 90 * day, "2160h": 90 * day, "7d": 7 * day, "168h": 7 * day, "365d": 365 * day} {
		if got, err := ParseRetention(in); err != nil || got != want {
			t.Errorf("%q = %v, %v, want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"6d", "167h", "0d", "-30d", "1.5d", "d", "90", "90days", "36501d", "forever", "+30d", " 30d", "876001h"} {
		if got, err := ParseRetention(in); err == nil {
			t.Errorf("%q = %v, want it refused", in, got)
		}
	}
	if got := (Workspace{InboxRetention: "30d"}).Retention(); got != 30*day {
		t.Errorf("a workspace's retention = %v", got)
	}
}

// Postgres first: stores on Postgres put the bus on the same database, so
// replicas share one event stream with nothing else to run.
func TestTheBusFollowsTheStores(t *testing.T) {
	var mem System
	mem.Defaults()
	if mem.Spec.Bus.Kind != "memory" {
		t.Fatalf("memory stores: bus = %q, want memory", mem.Spec.Bus.Kind)
	}
	var pg System
	pg.Spec.Stores.Kind = "postgres"
	pg.Defaults()
	if pg.Spec.Bus.Kind != "postgres" {
		t.Fatalf("postgres stores: bus = %q, want postgres", pg.Spec.Bus.Kind)
	}
	var pinned System
	pinned.Spec.Stores.Kind = "postgres"
	pinned.Spec.Bus.Kind = "memory"
	pinned.Defaults()
	if pinned.Spec.Bus.Kind != "memory" {
		t.Fatalf("an explicit bus is kept: %q", pinned.Spec.Bus.Kind)
	}
}

func TestABusTheStoresCannotCarryIsRefused(t *testing.T) {
	sys := "api_version: humagen/v1\nkind: System\nspec:\n"
	pgStores := "  stores: {kind: postgres, postgres: {url: {ref: env:HUMAGEN_POSTGRES_URL}}}\n"
	for name, tc := range map[string]struct{ body, want string }{
		"the removed redis bus":       {sys + pgStores + "  bus: {kind: redis, redis: {url: {ref: env:R}}}\n", "redis bus was removed"},
		"postgres without its stores": {sys + "  bus: {kind: postgres}\n", "set stores.kind: postgres"},
	} {
		path := filepath.Join(t.TempDir(), "humagen.yaml")
		writeFile(t, path, tc.body)
		cfg, err := Load(path)
		if err == nil {
			err = cfg.Check(Kinds{})
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
	path := filepath.Join(t.TempDir(), "humagen.yaml")
	writeFile(t, path, sys+pgStores+"  bus: {kind: postgres}\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Check(Kinds{}); err != nil {
		t.Fatalf("postgres bus on postgres stores: %v", err)
	}
}

// A policy goes only with an .agent.md file, and never says what wakes the
// agent.
func TestAnAgentPolicyNeedsAnAgentMdFile(t *testing.T) {
	plat := "api_version: humagen/v1\nkind: Platform\nmetadata: {name: default}\nspec:\n"
	for body, want := range map[string]string{
		"  agents: [{file: a.json, policy: {tagged_only: true}}]\n":                                   "policy goes with an .agent.md file",
		"  agents: [{file: a.agent.md, policy: {subscriptions: [{channel: x, types: [message]}]}}]\n": "the platform decides what wakes an agent",
	} {
		path := filepath.Join(t.TempDir(), "humagen.yaml")
		writeFile(t, path, plat+body)
		cfg, err := Load(path)
		if err == nil {
			err = cfg.Check(Kinds{})
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s= %v, want %q", body, err, want)
		}
	}
}

// The reference's .agent.md example reads as the agent it describes.
func TestTheReferenceAgentFileReads(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "reference", "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("(?s)```markdown\n(---\napiVersion: saige/v1.*?)```").FindStringSubmatch(string(doc))
	if m == nil {
		t.Fatal("docs/reference/spec.md has no .agent.md example")
	}
	s, err := wire.SpecOfAgentFile(m[1], wire.DefinitionPolicy{TaggedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "reviewer" || s.Provider != "anthropic" || s.SystemPrompt == "" || !s.TaggedOnly {
		t.Fatalf("the example reads as %+v", s)
	}
}

func TestModelKinds(t *testing.T) {
	key := &Ref{Ref: "env:K"}
	for name, c := range map[string]struct {
		kind string
		b    ModelBlock
		ok   bool
	}{
		"gemini":              {"google", ModelBlock{Key: key}, true},
		"vertex":              {"google", ModelBlock{Vertex: &Vertex{Project: "p", Location: "us-central1"}}, true},
		"vertex half":         {"google", ModelBlock{Vertex: &Vertex{Project: "p"}}, false},
		"vertex not google":   {"openai", ModelBlock{Vertex: &Vertex{Project: "p", Location: "l"}}, false},
		"local openai server": {"openai", ModelBlock{BaseURL: "http://127.0.0.1:11434/v1"}, true},
		"hosted with no key":  {"anthropic", ModelBlock{}, false},
	} {
		if err := checkModel(c.kind, c.b); (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRoomsAreBounded(t *testing.T) {
	for _, bad := range []Rooms{{TailInterval: "soon"}, {TailInterval: "10ms"}, {CoalesceAbove: -1}, {MaxRunsPerPerson: 5000}} {
		if _, err := bad.Config(); err == nil {
			t.Errorf("%+v was accepted", bad)
		}
	}
	if c, err := (Rooms{}).Config(); err != nil || c.CoalesceAbove != 200 {
		t.Fatalf("left out = %+v %v", c, err)
	}
}
