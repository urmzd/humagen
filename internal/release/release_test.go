package release

import (
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// changelog is three releases as the release tool writes them: headings,
// groups, entries with and without a scope, commit links and the compare
// link.
const changelog = `# Changelog

## 0.15.0 (2026-10-22)

### Breaking

- **projects**: recaps, read confinement, Slack and GitHub coworking (#44) ([b102fe3](https://github.com/urmzd/humagen/commit/b102fe3dc37130127615860c14ca311758879be7))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.14.0...v0.15.0)


## 0.14.0 (2026-10-22)

### Breaking

- one spec, humagen start and humagen deploy (#36) ([6b6fae9](https://github.com/urmzd/humagen/commit/6b6fae9138e7453b773f5f7085ede6d9a4342fd7))

### Features

- **projects**: project roles, project inboxes and announcements (#43) ([f00fb8a](https://github.com/urmzd/humagen/commit/f00fb8a8c5a91b258bea30787a8a4eeb7691ef1a))
- inbox templates, Monitor and Context ([71c9898](https://github.com/urmzd/humagen/commit/71c9898669c2ad778ab816ab734ed1d9adaf11b6))

### Bug Fixes

- **deps**: fiber v0.22.1 and anchor v0.5.1 (#42) ([04e6271](https://github.com/urmzd/humagen/commit/04e6271d6cb43276abd43aed3648d57f149c1cb7))

### Misc

- **deps**: fiber v0.20.0 (#34) ([1abd413](https://github.com/urmzd/humagen/commit/1abd413e65c87dcd4399623bac0b832722f259d8))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.13.1...v0.14.0)


## 0.13.1 (2026-10-22)

### Bug Fixes

- **auth**: a machine carries the deployment's policy (#40) ([527e553](https://github.com/urmzd/humagen/commit/527e55383993fff207987cc566893cf30113222f))

[Full Changelog](https://github.com/urmzd/humagen/compare/v0.13.0...v0.13.1)
`

// A release's news is its Breaking group, where the release tool files a
// breaking feat, and its Features group, each entry without its markup or
// commit link, and its compare link is kept. Fixes and the rest are not news.
func TestAReleaseIsItsBreakingChangesAndFeatures(t *testing.T) {
	all := Parse(changelog)
	var versions []string
	for _, n := range all {
		versions = append(versions, n.Version)
	}
	if !slices.Equal(versions, []string{"0.15.0", "0.14.0", "0.13.1"}) {
		t.Fatalf("releases = %q", versions)
	}
	n, ok := Find(changelog, "v0.14.0")
	wantBreaking := []string{"one spec, humagen start and humagen deploy (#36)"}
	wantFeatures := []string{
		"projects: project roles, project inboxes and announcements (#43)",
		"inbox templates, Monitor and Context",
	}
	if !ok || !n.News() || !slices.Equal(n.Breaking, wantBreaking) || !slices.Equal(n.Features, wantFeatures) ||
		n.Compare != "https://github.com/urmzd/humagen/compare/v0.13.1...v0.14.0" {
		t.Fatalf("0.14.0 = %+v, %v", n, ok)
	}
	if n.Subject() != "What's new in humagen 0.14.0" {
		t.Errorf("subject = %q", n.Subject())
	}
	wantText := "Breaking changes:\n" +
		"- one spec, humagen start and humagen deploy (#36)\n\n" +
		"Features:\n" +
		"- projects: project roles, project inboxes and announcements (#43)\n" +
		"- inbox templates, Monitor and Context\n\n" +
		"Full changelog: https://github.com/urmzd/humagen/compare/v0.13.1...v0.14.0"
	if got := n.Text(4000); got != wantText {
		t.Errorf("text = %q, want %q", got, wantText)
	}
	// A release of breaking changes alone is news, and lists them.
	n, ok = Find(changelog, "0.15.0")
	if !ok || !n.News() || !slices.Equal(n.Breaking, []string{"projects: recaps, read confinement, Slack and GitHub coworking (#44)"}) || len(n.Features) != 0 {
		t.Fatalf("0.15.0 = %+v, %v", n, ok)
	}
	wantText = "Breaking changes:\n" +
		"- projects: recaps, read confinement, Slack and GitHub coworking (#44)\n\n" +
		"Full changelog: https://github.com/urmzd/humagen/compare/v0.14.0...v0.15.0"
	if got := n.Text(4000); got != wantText {
		t.Errorf("0.15.0 text = %q, want %q", got, wantText)
	}
	// Fixes are not news.
	if n, ok := Find(changelog, "0.13.1"); !ok || n.News() || len(n.Breaking)+len(n.Features) != 0 {
		t.Errorf("0.13.1 = %+v, %v", n, ok)
	}
	for _, v := range []string{"0.12.0", "dev", ""} {
		if _, ok := Find(changelog, v); ok {
			t.Errorf("%q was found", v)
		}
	}
}

// A heading that names no version starts no release and ends the one
// before it, and a release with no compare link has none.
func TestAnUnversionedHeadingStartsNoRelease(t *testing.T) {
	all := Parse("## 0.2.0\n\n### Features\n\n- one\n\n## Unreleased\n\n### Breaking\n\n- not yet\n\n### Features\n\n- nor this\n")
	if len(all) != 1 || !slices.Equal(all[0].Features, []string{"one"}) || len(all[0].Breaking) != 0 || all[0].Compare != "" {
		t.Fatalf("releases = %+v", all)
	}
	if got := all[0].Text(4000); got != "- one" {
		t.Errorf("text = %q", got)
	}
}

// Entries past the limit are counted, never cut mid-line.
func TestTheTextFitsItsLimit(t *testing.T) {
	n := Notes{Version: "1.0.0", Compare: "https://example.com/compare"}
	for range 300 {
		n.Features = append(n.Features, strings.Repeat("x", 40))
	}
	text := n.Text(4000)
	if c := utf8.RuneCountInString(text); c > 4000 {
		t.Fatalf("text is %d characters", c)
	}
	lines := strings.Split(text, "\n")
	shown := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "- ") {
			if l != "- "+strings.Repeat("x", 40) {
				t.Fatalf("a cut line: %q", l)
			}
			shown++
		}
	}
	if more := lines[shown]; more != "And "+strconv.Itoa(300-shown)+" more." || lines[len(lines)-1] != "Full changelog: https://example.com/compare" {
		t.Fatalf("after %d features: %q", shown, lines[shown:])
	}

	// Breaking changes come first, and what does not fit of either group
	// is counted together: each text below is the longest that fits in
	// fewer characters than the one before it.
	b, f := "- "+strings.Repeat("b", 20), "- "+strings.Repeat("f", 20)
	n = Notes{Version: "1.0.0", Breaking: []string{b[2:], b[2:]}, Features: []string{f[2:], f[2:], f[2:]}}
	wants := []string{
		"Breaking changes:\n" + b + "\n" + b + "\n\nFeatures:\n" + f + "\n" + f + "\n" + f,
		"Breaking changes:\n" + b + "\n" + b + "\n\nFeatures:\n" + f + "\n" + f + "\nAnd 1 more.",
		"Breaking changes:\n" + b + "\n" + b + "\n\nFeatures:\n" + f + "\nAnd 2 more.",
		"Breaking changes:\n" + b + "\n" + b + "\nAnd 3 more.",
		"Breaking changes:\n" + b + "\nAnd 4 more.",
		"And 5 more.",
		"5 changes.",
	}
	for i, want := range wants {
		limit := utf8.RuneCountInString(want)
		if i > 0 {
			limit = utf8.RuneCountInString(wants[i-1]) - 1
		}
		if got := n.Text(limit); got != want {
			t.Errorf("at %d: %q, want %q", limit, got, want)
		}
	}
}

// The running release is what the release build stamped, else the version
// Go recorded for humagen's module; a build that is not a release has none.
func TestTheRunningRelease(t *testing.T) {
	own := func(version string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Path: Module, Version: version}}
	}
	imported := func(dep debug.Module) *debug.BuildInfo {
		dep.Path = Module
		return &debug.BuildInfo{Main: debug.Module{Path: "example.com/house", Version: "v3.1.0"}, Deps: []*debug.Module{{Path: "example.com/other", Version: "v0.15.0"}, &dep}}
	}
	for name, tc := range map[string]struct {
		stamped string
		info    *debug.BuildInfo
		want    string
	}{
		"a release image, stamped":            {"v0.15.0", own("(devel)"), "0.15.0"},
		"stamped without the v":               {"0.15.0", own("(devel)"), "0.15.0"},
		"go install at a tag":                 {"dev", own("v0.15.0"), "0.15.0"},
		"a local build":                       {"dev", own("(devel)"), ""},
		"a build past a tag":                  {"dev", own("v0.15.1-0.20261022120000-b102fe3dc371"), ""},
		"a build with local changes":          {"dev", own("v0.15.0+dirty"), ""},
		"no build information":                {"v0.15.0", nil, ""},
		"imported at a release":               {"v3.1.0", imported(debug.Module{Version: "v0.14.0"}), "0.14.0"},
		"imported at a pseudo-version":        {"v3.1.0", imported(debug.Module{Version: "v0.14.1-0.20261022120000-f00fb8a8c5a9"}), ""},
		"imported from a local directory":     {"v3.1.0", imported(debug.Module{Version: "v0.14.0", Replace: &debug.Module{Path: "../humagen"}}), ""},
		"imported from another release":       {"v3.1.0", imported(debug.Module{Version: "v0.14.0", Replace: &debug.Module{Path: Module, Version: "v0.13.1"}}), "0.13.1"},
		"a program that does not use humagen": {"v3.1.0", &debug.BuildInfo{Main: debug.Module{Path: "example.com/house"}}, ""},
	} {
		if got := running(tc.stamped, tc.info); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
	// A test binary is a local build.
	if got := Running(""); got != "" {
		t.Errorf("this test binary runs %q", got)
	}
}
