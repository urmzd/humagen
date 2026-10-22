package app

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/spec"
	"github.com/urmzd/warp/pkg/state/statetest"
)

// releaseChangelog is three releases as the release tool writes them: one
// whose only entry is breaking, one with a feature, and one with only a fix.
const releaseChangelog = `# Changelog

## 0.17.0 (2026-11-09)

### Breaking

- **inbox**: a breaking change (#48) ([abcdef0](https://github.com/urmzd/warp/commit/abcdef01))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.16.0...v0.17.0)


## 0.16.0 (2026-11-02)

### Features

- **projects**: release notes in the workspace's inbox (#45) ([1234abc](https://github.com/urmzd/warp/commit/1234abcd))

### Bug Fixes

- **inbox**: a fix (#46) ([5678def](https://github.com/urmzd/warp/commit/5678defa))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.15.0...v0.16.0)


## 0.15.1 (2026-10-30)

### Bug Fixes

- **slack**: another fix (#47) ([9abc012](https://github.com/urmzd/warp/commit/9abc0123))

[Full Changelog](https://github.com/urmzd/warp/compare/v0.15.0...v0.15.1)
`

const releaseText = "- projects: release notes in the workspace's inbox (#45)\n\n" +
	"Full changelog: https://github.com/urmzd/warp/compare/v0.15.0...v0.16.0"

// Release notes are off unless the workspace turns them on, and only a
// release with breaking changes or features posts: a build that is not a
// release, a release the changelog does not list and a release of fixes post
// nothing. A release is posted once, from warp, in the workspace's own inbox.
func TestReleaseNotesArePostedWhereTurnedOn(t *testing.T) {
	p := newProjectsPlatform(t)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := spec.Config{Platforms: []spec.Platform{
		{Metadata: spec.Metadata{Name: "acme"}, Spec: spec.PlatformSpec{Workspace: spec.Workspace{ReleaseNotes: true}}},
		{Metadata: spec.Metadata{Name: "globex"}},
	}}
	for _, version := range []string{"", "dev", "0.14.0", "0.15.1"} {
		postReleaseNotes(p, cfg, version, releaseChangelog, quiet)
	}
	if got := p.Inbox.OpenOf("acme", inbox.KindAnnouncement); len(got) != 0 {
		t.Fatalf("posted with nothing to post: %+v", got)
	}
	for range 2 {
		postReleaseNotes(p, cfg, "0.16.0", releaseChangelog, quiet)
	}
	got := p.Inbox.OpenOf("acme", inbox.KindAnnouncement)
	if len(got) != 1 {
		t.Fatalf("posted %d times", len(got))
	}
	m := got[0]
	if m.Key != "release:0.16.0" || m.Subject != "What's new in warp 0.16.0" || m.From != (inbox.From{Kind: "system", Name: "warp"}) ||
		m.Source.Project != "" || len(m.Parts) != 1 || m.Parts[0].Text != releaseText {
		t.Fatalf("posted = %+v", m)
	}
	if got := p.Inbox.OpenOf("globex", inbox.KindAnnouncement); len(got) != 0 {
		t.Fatalf("a workspace that left it off was posted %+v", got)
	}
	// A release whose only entry is breaking, as the release tool files a
	// breaking feat, posts it.
	postReleaseNotes(p, cfg, "v0.17.0", releaseChangelog, quiet)
	got = p.Inbox.OpenOf("acme", inbox.KindAnnouncement)
	i := slices.IndexFunc(got, func(m inbox.Message) bool { return m.Key == "release:0.17.0" })
	if len(got) != 2 || i < 0 || got[i].Subject != "What's new in warp 0.17.0" || len(got[i].Parts) != 1 ||
		got[i].Parts[0].Text != "Breaking changes:\n- inbox: a breaking change (#48)\n\n"+
			"Full changelog: https://github.com/urmzd/warp/compare/v0.16.0...v0.17.0" {
		t.Fatalf("0.17.0 posted %+v", got)
	}

	// On a platform that has posted nothing, nothing posts where the
	// workspace leaves it off, or without a changelog.
	fresh := newProjectsPlatform(t)
	off := spec.Config{Platforms: []spec.Platform{{Metadata: spec.Metadata{Name: "acme"}}}}
	postReleaseNotes(fresh, off, "0.16.0", releaseChangelog, quiet)
	postReleaseNotes(fresh, spec.Config{}, "0.16.0", releaseChangelog, quiet)
	postReleaseNotes(fresh, cfg, "0.16.0", "", quiet)
	if got := fresh.Inbox.OpenOf("acme", inbox.KindAnnouncement); len(got) != 0 {
		t.Fatalf("posted %+v", got)
	}
}

// A real start on Postgres posts a release's notes once: a restart of the
// same release finds them posted and posts nothing, and a start of another
// release posts its own.
func TestReleaseNotesArePostedOnceAcrossRestartsOnPostgres(t *testing.T) {
	// A schema of its own, so what an earlier run posted is not read.
	pool := statetest.Pool(t)
	var schema string
	if err := pool.QueryRow(t.Context(), "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	url := os.Getenv(statetest.URLEnv)
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	t.Setenv("SPEC_TEST_PG", url+sep+"search_path="+schema)
	dir := t.TempDir()
	write := func(on bool) string {
		path := filepath.Join(dir, "warp.yaml")
		body := "api_version: warp/v1\nkind: System\nspec:\n  stores: {kind: postgres, postgres: {url: {ref: \"env:SPEC_TEST_PG\"}}}\n---\n" +
			"api_version: warp/v1\nkind: Platform\nmetadata: {name: default}\n"
		if on {
			body += "spec:\n  workspace:\n    release_notes: true\n"
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	type page struct {
		Messages []struct {
			ID      string     `json:"id"`
			Subject string     `json:"subject"`
			From    inbox.From `json:"from"`
		} `json:"messages"`
	}
	start := func(version string, on bool) page {
		t.Helper()
		o := Options{Version: Version{Version: version}, Changelog: releaseChangelog}
		addr, stop := serveUntil(t, o, "-f", write(on), "-addr", "127.0.0.1:0")
		defer stop()
		var p page
		if err := json.Unmarshal([]byte(get(t, "http://"+addr+"/api/projects/~/inbox?kind=announcement&state=all")), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// Off by default: a start of a release with features posts nothing.
	if got := start("v0.16.0", false); len(got.Messages) != 0 {
		t.Fatalf("off, posted %+v", got)
	}
	first := start("v0.16.0", true)
	if len(first.Messages) != 1 || first.Messages[0].Subject != "What's new in warp 0.16.0" || first.Messages[0].From != (inbox.From{Kind: "system", Name: "warp"}) {
		t.Fatalf("the first start = %+v", first)
	}
	if again := start("v0.16.0", true); len(again.Messages) != 1 || again.Messages[0].ID != first.Messages[0].ID {
		t.Fatalf("after a restart = %+v, was %+v", again, first)
	}
	// A dev build and a release of fixes post nothing.
	for _, version := range []string{"dev", "v0.15.1"} {
		if got := start(version, true); len(got.Messages) != 1 {
			t.Fatalf("%s posted: %+v", version, got)
		}
	}
}
