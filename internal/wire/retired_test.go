package wire

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRetiredAgentFieldsAreDroppedWithANote(t *testing.T) {
	kept, notes, err := DropRetiredAgentFields([]byte(`{"name":"picker","choices":{"models":["a/b"]}}`))
	if err != nil || len(notes) != 1 || !strings.HasPrefix(notes[0], "choices is no longer read") {
		t.Fatalf("notes = %v, %v", notes, err)
	}
	var fields map[string]any
	if err := json.Unmarshal(kept, &fields); err != nil || fields["choices"] != nil || fields["name"] != "picker" {
		t.Fatalf("kept = %s, %v", kept, err)
	}
	for _, as := range []string{`{"name":"plain"}`, `not json`, `[1]`} {
		if out, notes, err := DropRetiredAgentFields([]byte(as)); err != nil || len(notes) != 0 || string(out) != as {
			t.Errorf("%s = %s, %v, %v", as, out, notes, err)
		}
	}
}
