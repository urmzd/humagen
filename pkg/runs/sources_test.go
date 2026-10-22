package runs

import (
	"slices"
	"testing"
)

// A run's sources are read from the private key, and from the old public one
// for a run pinned before it, the private key first.
func TestSourcesAreReadFromThePrivateKeyThenTheOldOne(t *testing.T) {
	for _, c := range []struct {
		meta map[string]string
		want []string
	}{
		{map[string]string{MetaSources: "a,b"}, []string{"a", "b"}},
		{map[string]string{MetaSourcesV0: "a"}, []string{"a"}},
		{map[string]string{MetaSources: "a", MetaSourcesV0: "b"}, []string{"a"}},
		{map[string]string{MetaSources: "", MetaSourcesV0: "b"}, nil},
		{nil, nil},
	} {
		if got := SourcesOf(c.meta); !slices.Equal(got, c.want) {
			t.Errorf("SourcesOf(%v) = %v, want %v", c.meta, got, c.want)
		}
	}
	if MetaSources != "_warp.sources" {
		t.Errorf("MetaSources = %q, want a private key", MetaSources)
	}
}
