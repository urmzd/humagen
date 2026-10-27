package artifacts_test

import (
	"encoding/json"
	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/state"
	"strings"
	"testing"
)

func TestSearchArtifactTextReplaysAndTracksCurrentVersionsAndDeletion(t *testing.T) {
	st := state.NewMemory()
	x, err := artifacts.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	old := put(t, x, "acme", artifacts.Artifact{Title: "report.docx", Thread: "th-1", SearchText: "Zephyr retired decision"})
	current := put(t, x, "acme", artifacts.Artifact{Title: "report.docx", Thread: "th-1", SearchText: "Saffron current decision"})
	again, err := artifacts.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	hits, _, err := again.Page("acme", artifacts.Query{Search: "saffron decision"})
	if err != nil || len(hits) != 1 || hits[0].ID != current.ID {
		t.Fatalf("%v %v", hits, err)
	}
	hits, _, _ = again.Page("acme", artifacts.Query{Search: "zephyr"})
	if len(hits) != 0 {
		t.Fatal(hits)
	}
	wire, _ := json.Marshal(current)
	if strings.Contains(string(wire), "Saffron") {
		t.Fatal("raw search text leaked into artifact API")
	}
	if _, err := again.Delete("acme", current.ID); err != nil {
		t.Fatal(err)
	}
	hits, _, _ = again.Page("acme", artifacts.Query{Search: "zephyr"})
	if len(hits) != 1 || hits[0].ID != old.ID {
		t.Fatal(hits)
	}
}
