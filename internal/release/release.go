// Package release reads warp's changelog, the CHANGELOG.md the release tool
// writes at each release, and which release of warp a binary runs. It is
// what workspace.release_notes posts in a workspace's inbox.
package release

import (
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"
	"unicode/utf8"
)

// Module is warp's module path.
const Module = "github.com/urmzd/warp"

// Notes is what one release brings, as the changelog lists it. Each entry
// is plain text: the scope, then the summary, without the commit link
// ("projects: project roles (#43)").
type Notes struct {
	// Version is the release as its heading writes it, such as "0.15.0".
	Version string
	// Breaking are its breaking changes, the changelog's Breaking group.
	// The release tool files a breaking feat there and not under Features,
	// and does not write an entry's type, so a breaking change of another
	// type is here too.
	Breaking []string
	// Features are its other feat entries, the changelog's Features group.
	Features []string
	// Compare links to the release's full changelog, when it has one.
	Compare string
}

var (
	// heading is a release: "## 0.15.0 (2026-10-22)".
	heading = regexp.MustCompile(`^## v?(\d+\.\d+\.\d+\S*)(?: \(\d{4}-\d{2}-\d{2}\))?$`)
	// commitLink ends an entry: " ([b102fe3](https://.../commit/b102fe3...))".
	commitLink = regexp.MustCompile(`\s*\(\[[0-9a-f]{7,40}\]\([^)\s]*\)\)$`)
	// scope starts an entry that has one: "**projects**: ".
	scope = regexp.MustCompile(`^\*\*([^*]+)\*\*:\s*`)
	// compare ends a release: "[Full Changelog](https://.../compare/v0.14.0...v0.15.0)".
	compare = regexp.MustCompile(`^\[Full Changelog\]\((\S+)\)$`)
	// plain is a release's version as Go or -ldflags write it.
	plain = regexp.MustCompile(`^v?(\d+\.\d+\.\d+)$`)
)

// The groups the release tool writes a release's news under: a breaking
// change of any type, and every other feat.
const (
	breakingGroup = "Breaking"
	featuresGroup = "Features"
)

// Parse reads every release in a changelog the release tool wrote, in the
// order it lists them, newest first. A second-level heading that names no
// version ends the release before it and starts none.
func Parse(changelog string) []Notes {
	var out []Notes
	in, group := false, ""
	for line := range strings.Lines(changelog) {
		line = strings.TrimRight(line, " \t\r\n")
		if strings.HasPrefix(line, "## ") {
			m := heading.FindStringSubmatch(line)
			in, group = m != nil, ""
			if in {
				out = append(out, Notes{Version: m[1]})
			}
			continue
		}
		if !in {
			continue
		}
		cur := &out[len(out)-1]
		switch {
		case strings.HasPrefix(line, "### "):
			group = strings.TrimSpace(line[len("### "):])
		case strings.HasPrefix(line, "- ") && strings.EqualFold(group, breakingGroup):
			if e := entry(line[len("- "):]); e != "" {
				cur.Breaking = append(cur.Breaking, e)
			}
		case strings.HasPrefix(line, "- ") && strings.EqualFold(group, featuresGroup):
			if e := entry(line[len("- "):]); e != "" {
				cur.Features = append(cur.Features, e)
			}
		default:
			if m := compare.FindStringSubmatch(line); m != nil {
				cur.Compare = m[1]
			}
		}
	}
	return out
}

// entry is one changelog entry as plain text.
func entry(s string) string {
	s = commitLink.ReplaceAllString(strings.TrimSpace(s), "")
	return strings.TrimSpace(scope.ReplaceAllString(s, "$1: "))
}

// Find returns one release's notes, the version written with or without its
// "v", and whether the changelog lists it.
func Find(changelog, version string) (Notes, bool) {
	version = strings.TrimPrefix(version, "v")
	if version == "" {
		return Notes{}, false
	}
	for _, n := range Parse(changelog) {
		if n.Version == version {
			return n, true
		}
	}
	return Notes{}, false
}

// News is whether the release brings anything to post: a breaking change
// or a feature. A release of fixes alone does not.
func (n Notes) News() bool { return len(n.Breaking)+len(n.Features) > 0 }

// Subject is the announcement's subject line.
func (n Notes) Subject() string { return "What's new in warp " + n.Version }

// Text is the announcement's text: the breaking changes under "Breaking
// changes:", then the features, under "Features:" when there are breaking
// changes before them, each entry on a line of its own, then the link to
// the full changelog. It is at most limit characters: the entries that do
// not fit, the last ones first, are counted on a last line instead.
func (n Notes) Text(limit int) string {
	link := ""
	if n.Compare != "" {
		link = "\n\nFull changelog: " + n.Compare
	}
	all := len(n.Breaking) + len(n.Features)
	for shown := all; shown >= 0; shown-- {
		breaking := n.Breaking[:min(shown, len(n.Breaking))]
		features := n.Features[:shown-len(breaking)]
		var groups []string
		if len(breaking) > 0 {
			groups = append(groups, "Breaking changes:\n"+lines(breaking))
		}
		if len(features) > 0 {
			group := lines(features)
			if len(n.Breaking) > 0 {
				group = "Features:\n" + group
			}
			groups = append(groups, group)
		}
		text := strings.Join(groups, "\n\n")
		if more := all - shown; more > 0 {
			if text != "" {
				text += "\n"
			}
			text += fmt.Sprintf("And %d more.", more)
		}
		if text += link; utf8.RuneCountInString(text) <= limit {
			return text
		}
	}
	return fmt.Sprintf("%d changes.", all)
}

// lines is entries as a list, each on a line of its own.
func lines(entries []string) string { return "- " + strings.Join(entries, "\n- ") }

// Running is the release of warp this binary runs, as the changelog's
// headings write it, or "" for a build that is not a release: "dev", a
// local build or a pseudo-version. stamped is the version a release build
// sets with -ldflags, which is warp's only in warp's own binary. A program
// that imports warp has a version of its own, and there warp's is the one Go
// recorded for warp's module.
func Running(stamped string) string {
	info, _ := debug.ReadBuildInfo()
	return running(stamped, info)
}

func running(stamped string, info *debug.BuildInfo) string {
	if info == nil {
		return ""
	}
	var candidates []string
	if info.Main.Path == Module {
		candidates = append(candidates, stamped, info.Main.Version)
	}
	for _, d := range info.Deps {
		if d.Path != Module {
			continue
		}
		if d.Replace != nil {
			// A module replaced by a directory has no version: it is a
			// local build, whatever version it was required at.
			candidates = append(candidates, d.Replace.Version)
		} else {
			candidates = append(candidates, d.Version)
		}
	}
	for _, v := range candidates {
		if m := plain.FindStringSubmatch(v); m != nil {
			return m[1]
		}
	}
	return ""
}
