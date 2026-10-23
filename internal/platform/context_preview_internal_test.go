package platform

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestContextNamePreviewIsBoundedWithoutChangingRuntimeOmissions(t *testing.T) {
	names := make([]string, 200)
	for i := range names {
		names[i] = strings.Repeat("s", 200)
	}
	got := nameLayer("agent_knowledge", "helper", names, 0)
	if utf8.RuneCountInString(got.Text) > 16128 || len(strings.Split(got.Text, "\n")) > 128 || got.Used != len(names) || got.PreviewOmitted == 0 || got.Omitted != 0 {
		t.Fatalf("unbounded or misleading name layer: %+v", got)
	}
	long := nameLayer("agent_knowledge", "helper", []string{strings.Repeat("n", 16001), "fits"}, 0)
	if long.Text != "fits" || long.PreviewOmitted != 1 || long.Used != 2 {
		t.Fatalf("oversized source: %+v", long)
	}
}
