package app

import "testing"

func TestPinnedPicksTheReferencesRepository(t *testing.T) {
	d := "@sha256:" + "ab"
	for _, c := range []struct {
		ref, want string
		digests   []string
	}{
		{"ghcr.io/acme/agent:1.4", "ghcr.io/acme/agent" + d, []string{"ghcr.io/other/x" + d, "ghcr.io/acme/agent" + d}},
		{"localhost:5000/agent:2", "localhost:5000/agent" + d, []string{"localhost:5000/agent" + d}},
		{"python:3", "python" + d, []string{"docker.io/library/python" + d}},
	} {
		got, err := pinned(c.ref, c.digests)
		if err != nil || got != c.want {
			t.Errorf("pinned(%q) = %q %v, want %q", c.ref, got, err, c.want)
		}
	}
	if _, err := pinned("ghcr.io/acme/agent:1", []string{"ghcr.io/other/x" + d}); err == nil {
		t.Error("a digest of another repository was taken")
	}
}
