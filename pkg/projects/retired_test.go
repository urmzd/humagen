package projects

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
)

// A project stored while approve_reviews existed may list it among the file's
// fields. Reading it drops the name, so the project still validates and saves.
func TestAStoredProjectDropsARetiredFileField(t *testing.T) {
	p, err := NewStore().Create("acme", Project{Name: "Field"}, Writer{By: "ada"}, time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(stored{Project: p, Owns: []string{"approve_reviews", FieldTitle}})
	if err != nil {
		t.Fatal(err)
	}
	var s stored
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	got := s.project()
	if !slices.Equal(got.FileOwns, []string{FieldTitle}) {
		t.Fatalf("file owns %v", got.FileOwns)
	}
	if err := got.validate(); err != nil {
		t.Fatalf("a stored project with a retired field is invalid: %v", err)
	}
}
