package platform

import (
	"reflect"
	"testing"

	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/runs"
)

// What a reader sees, by every combination of role, govern, thread and
// participation: a role in the project is needed for anything in it, a
// private thread also needs taking part, and govern sees all.
func TestSeesNeedsARoleAndInAPrivateThreadToTakePart(t *testing.T) {
	shared := &threadAccess{Participants: []string{"ada"}}
	private := &threadAccess{Private: true, Participants: []string{"ada"}}
	for _, c := range []struct {
		name           string
		viewer, govern bool
		th             *threadAccess
		subject        string
		want           bool
	}{
		{"a viewer, no thread", true, false, nil, "bob", true},
		{"a viewer, a shared thread", true, false, shared, "bob", true},
		{"a viewer in a private thread", true, false, private, "ada", true},
		{"a viewer outside a private thread", true, false, private, "bob", false},
		{"no subject in a private thread", true, false, &threadAccess{Private: true, Participants: []string{""}}, "", false},
		{"no role, no thread", false, false, nil, "bob", false},
		{"no role, a shared thread", false, false, shared, "bob", false},
		{"no role, though taking part", false, false, private, "ada", false},
		{"govern, no role", false, true, nil, "root", true},
		{"govern, a private thread", false, true, private, "root", true},
	} {
		if got := seesPlace(c.viewer, c.govern, c.th, c.subject); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// Each kind of object names the same place for the same project and thread.
func TestObjectsNameTheirPlace(t *testing.T) {
	want := Place{Project: "Globex", Thread: "r1"}
	for name, got := range map[string]Place{
		"run":      placeOfRun(runs.Run{ID: "r2", Project: "Globex", Thread: "r1"}),
		"message":  placeOfSource(inbox.Source{Run: "r2", Project: "Globex", Thread: "r1"}),
		"artifact": placeOfArtifact(artifacts.Artifact{ID: "art-1", Project: "Globex", Thread: "r1"}),
	} {
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %+v", name, got)
		}
	}
	if !reflect.DeepEqual(placeOfRun(runs.Run{}), Place{}) {
		t.Error("a run filed nowhere is not at the root")
	}
}
