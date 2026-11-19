package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/httpapi"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/auth/authtest"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/sessionimport"
)

// importPlatform serves a platform behind OIDC verification against an
// in-process issuer, as serve wires the API, and returns it with its URL
// and an administrator's token.
func importPlatform(t *testing.T) (*platform.Platform, string, string) {
	t.Helper()
	const clientID, audience = "import-client", "api://humagen"
	iss := authtest.New(t)
	v, err := auth.NewVerifier(context.Background(), auth.OIDCConfig{
		Issuer: iss.URL, ClientID: clientID, Audience: audience, TenantClaim: "org",
		GroupRoles: map[string]auth.Role{"humagen-admins": auth.Admin},
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := platform.New(platform.Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	authn, err := httpapi.NewAuthn(v, clientID, "secret", srv.URL, []string{"openid", "groups"})
	if err != nil {
		t.Fatal(err)
	}
	mux.Handle("/", (&httpapi.Server{P: p, Authn: authn, Log: slog.New(slog.DiscardHandler)}).Handler())
	t.Cleanup(func() {
		srv.Close()
		cancel()
		if err := <-done; err != nil {
			t.Errorf("platform shutdown: %v", err)
		}
	})
	return p, srv.URL, iss.Sign(t, "00u-importer", audience, map[string]any{"org": "acme", "groups": []any{"humagen-admins"}, "name": "Importer"})
}

// syntheticSessions reads the synthetic records each format's test data
// holds, as the import reads them.
func syntheticSessions(t *testing.T, home, chatgpt, claudeAI string) map[string]sessionimport.Session {
	t.Helper()
	out := map[string]sessionimport.Session{}
	for tool, path := range map[string]string{
		sessionimport.Claude: filepath.Join(home, ".claude/projects/-synthetic-work-alpha-site/cc0rig1d-aaaa-4bbb-8ccc-000000000001.jsonl"),
		sessionimport.Codex:  filepath.Join(home, ".codex/sessions/2026/10/06/rollout-2026-10-06T09-00-00-c0dex0r1-bbbb-4ccc-8ddd-000000000002.jsonl"),
	} {
		s, err := sessionimport.Open(tool, path)
		if err != nil {
			t.Fatal(err)
		}
		out[s.URI()] = s
	}
	for _, path := range []string{chatgpt, claudeAI} {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = sessionimport.ReadExport(f, func(s sessionimport.Session) error {
			out[s.URI()] = s
			return nil
		})
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// An import into a running server keeps with each session's document the
// structure the session makes, for all four formats: read back as the
// reader is let read it, it is the structure sent, field for field. A second
// import writes nothing and leaves every structure as it was. A session that
// grew has its document and structure replaced together; a document edited
// in the console has no trusted structure until an import writes both again.
func TestImportSessionsKeepsEachSessionsStructureOnTheServer(t *testing.T) {
	p, url, token := importPlatform(t)
	t.Setenv("GENERATED_IMPORT_TOKEN", token)
	home, chatgpt, claudeAI := syntheticHome(t)
	args := []string{"sessions", "--home", home, "--export", chatgpt, "--export", claudeAI, "--server", url, "--token", "env:GENERATED_IMPORT_TOKEN", "--json"}
	run := func(want string) {
		t.Helper()
		got, err := runImport(t, args...)
		if c := counts(t, got); err != nil || c != want {
			t.Fatalf("got %s (%v), want %s\n%s", c, err, want, got)
		}
	}
	run("added 4 updated 0 present 0 failed []")

	reader := auth.Principal{Tenant: "acme", Subject: "00u-importer", Kind: auth.User, Scopes: []auth.Permission{auth.KnowledgeRead}}
	sessions := syntheticSessions(t, home, chatgpt, claudeAI)
	lim := sessionimport.Limits{Bytes: knowledge.MaxImportedBytes, Turns: knowledge.MaxImportedTurns, TurnBytes: knowledge.MaxImportedTurnBytes, ID: knowledge.MaxImportedID}
	// read is each document's structure as JSON by its uri, or why it is
	// unknown, and the source and id of each.
	where := map[string][2]string{}
	read := func() map[string]string {
		t.Helper()
		kept := map[string]string{}
		srcs, err := p.SourcesAs(reader)
		if err != nil {
			t.Fatal(err)
		}
		for _, src := range srcs {
			docs, err := p.Knowledge.Documents("acme", src.Name)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range docs {
				o, err := p.ImportedOrigin(reader, src.Name, d.ID)
				if err != nil {
					t.Fatalf("%s: %v", d.URI, err)
				}
				where[d.URI] = [2]string{src.Name, d.ID}
				if o.Session == nil {
					kept[d.URI] = "unknown: " + string(o.Unknown)
					continue
				}
				raw, _ := json.Marshal(o.Session)
				kept[d.URI] = string(raw)
			}
		}
		return kept
	}
	sent := func(uri string) string {
		t.Helper()
		st, ok := syntheticSessions(t, home, chatgpt, claudeAI)[uri].Structure(lim)
		raw, _ := json.Marshal(st)
		if !ok {
			t.Fatalf("%s makes no structure", uri)
		}
		return string(raw)
	}
	kept := read()
	if len(kept) != 4 || len(sessions) != 4 {
		t.Fatalf("kept %d structures of %d sessions: %v", len(kept), len(sessions), kept)
	}
	for uri := range sessions {
		if want := sent(uri); kept[uri] != want {
			t.Errorf("%s:\nkept %s\nsent %s", uri, kept[uri], want)
		}
	}

	run("added 0 updated 0 present 4 failed []")
	if again := read(); len(again) != 4 {
		t.Fatalf("after a second run: %v", again)
	} else {
		for uri, s := range again {
			if s != kept[uri] {
				t.Errorf("%s changed on a second run", uri)
			}
		}
	}

	const claude = "claude:cc0rig1d-aaaa-4bbb-8ccc-000000000001"
	grow(t, home)
	run("added 0 updated 1 present 3 failed []")
	grown := read()
	if grown[claude] == kept[claude] || grown[claude] != sent(claude) || !strings.Contains(grown[claude], "CC-GREW one more thing") {
		t.Fatalf("grown: %s", grown[claude])
	}

	// Edited as the console edits: no structure is sent, and the one kept
	// is not trusted.
	o, err := p.ImportedOrigin(reader, where[claude][0], where[claude][1])
	if err != nil {
		t.Fatal(err)
	}
	doc := o.Document
	src, err := p.SourceAs(reader, where[claude][0])
	if err != nil {
		t.Fatal(err)
	}
	var cfg *knowledge.SourceConfig
	if src.Lifecycle != nil {
		c := src.Lifecycle.SourceConfig
		cfg = &c
	}
	if _, err := p.Knowledge.ReplaceForScope("acme", where[claude][0], where[claude][1], doc.Title+", renamed", "A note somebody wrote instead.", doc.Digest, time.Now(), cfg); err != nil {
		t.Fatal(err)
	}
	if got := read()[claude]; got != "unknown: changed" {
		t.Fatalf("edited: %s", got)
	}
	run("added 0 updated 1 present 3 failed []")
	if got := read()[claude]; got != sent(claude) {
		t.Fatalf("imported again: %s", got)
	}
}
