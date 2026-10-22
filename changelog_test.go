package warp

import (
	"regexp"
	"slices"
	"testing"

	"github.com/urmzd/warp/internal/release"
)

// The embedded changelog is the one the release tool writes, and reads as
// release notes: every release heading in it names a version, and a
// release's news is its Breaking group, where the tool files a breaking
// feat, and its Features group, without commit links. A release of fixes
// alone has none.
func TestTheEmbeddedChangelogReadsAsReleaseNotes(t *testing.T) {
	headings := regexp.MustCompile(`(?m)^## `).FindAllStringIndex(changelog, -1)
	all := release.Parse(changelog)
	if len(all) < 10 || len(all) != len(headings) {
		t.Fatalf("%d releases read from %d headings", len(all), len(headings))
	}
	n, ok := release.Find(changelog, "0.12.0")
	if !ok || !slices.Equal(n.Breaking, []string{"projects: explicit addressing, @auto, agents in projects, and the directory (#39)"}) ||
		!slices.Equal(n.Features, []string{"credentials: each person's own credential for the tools that act as them (#38)"}) ||
		n.Compare != "https://github.com/urmzd/warp/compare/v0.11.0...v0.12.0" {
		t.Fatalf("0.12.0 = %+v, %v", n, ok)
	}
	// A release whose one entry is a breaking feat is news.
	n, ok = release.Find(changelog, "0.15.0")
	if !ok || !n.News() || !slices.Equal(n.Breaking, []string{"projects: recaps, read confinement, Slack and GitHub coworking (#44)"}) {
		t.Fatalf("0.15.0 = %+v, %v", n, ok)
	}
	if n, ok := release.Find(changelog, "0.13.1"); !ok || n.News() {
		t.Errorf("0.13.1 = %+v, %v", n, ok)
	}
}
