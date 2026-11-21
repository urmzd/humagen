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

// Docker prints a Docker Hub repository by its familiar name in
// RepoDigests (["python@sha256:..."]), however the reference spelled it.
func TestPinnedReadsDockerHubsFamiliarNames(t *testing.T) {
	d := "@sha256:" + "ab"
	for _, c := range []struct {
		ref, want, digest string
	}{
		{"docker.io/library/python:3.13-alpine", "docker.io/library/python" + d, "python" + d},
		{"library/python:3", "library/python" + d, "python" + d},
		{"docker.io/acme/agent:1", "docker.io/acme/agent" + d, "acme/agent" + d},
		{"index.docker.io/acme/agent:1", "index.docker.io/acme/agent" + d, "acme/agent" + d},
	} {
		got, err := pinned(c.ref, []string{c.digest})
		if err != nil || got != c.want {
			t.Errorf("pinned(%q, [%s]) = %q %v, want %q", c.ref, c.digest, got, err, c.want)
		}
	}
	// A Docker Hub name is not another registry's.
	if _, err := pinned("ghcr.io/library/python:3", []string{"python" + d}); err == nil {
		t.Error("ghcr.io/library/python took Docker Hub's python digest")
	}
}
