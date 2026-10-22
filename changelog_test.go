package warp

import (
	"regexp"
	"slices"
	"testing"

	"github.com/urmzd/warp/internal/release"
)

// The embedded changelog is the one the release tool writes, and reads as
// release notes: every release heading in it names a version, a release's
// features are its Features group without commit links, and a release of
// breaking changes or fixes alone has none.
func TestTheEmbeddedChangelogReadsAsReleaseNotes(t *testing.T) {
	headings := regexp.MustCompile(`(?m)^## `).FindAllStringIndex(changelog, -1)
	all := release.Parse(changelog)
	if len(all) < 10 || len(all) != len(headings) {
		t.Fatalf("%d releases read from %d headings", len(all), len(headings))
	}
	n, ok := release.Find(changelog, "0.12.0")
	if !ok || !slices.Equal(n.Features, []string{"credentials: each person's own credential for the tools that act as them (#38)"}) ||
		n.Compare != "https://github.com/urmzd/warp/compare/v0.11.0...v0.12.0" {
		t.Fatalf("0.12.0 = %+v, %v", n, ok)
	}
	for _, v := range []string{"0.13.1", "0.15.0"} {
		if n, ok := release.Find(changelog, v); !ok || len(n.Features) != 0 {
			t.Errorf("%s = %+v, %v", v, n, ok)
		}
	}
}
