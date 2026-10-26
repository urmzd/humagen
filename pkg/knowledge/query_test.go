package knowledge

import (
	"strings"
	"testing"
)

func TestRepeatedQueryWordsCountEachPassageOnce(t *testing.T) {
	s := NewStore()
	_, _ = s.Create("tenant", "docs", "", "owner", now)
	_, err := s.Add("tenant", "docs", "Supplier ETA", "", "Packaging arrives in November.", "owner", now)
	if err != nil {
		t.Fatal(err)
	}
	one, err := s.Search("tenant", []string{"docs"}, "packaging", 6, "")
	if err != nil || len(one) != 1 {
		t.Fatal(one, err)
	}
	repeated, err := s.Search("tenant", []string{"docs"}, strings.Repeat("packaging ", 20), 6, "")
	if err != nil || len(repeated) != 1 || repeated[0].Score <= 0 {
		t.Fatalf("repeated query lost an actual match: %+v %v", repeated, err)
	}
}
