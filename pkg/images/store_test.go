package images_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/images"
)

func TestRegistryAndCheckRef(t *testing.T) {
	for ref, want := range map[string]string{
		"ghcr.io/acme/agent:1": "ghcr.io", "GHCR.io/a/b": "ghcr.io", "localhost:5000/a": "localhost:5000",
		"localhost/a": "localhost", "python:3": "docker.io", "acme/agent": "docker.io",
	} {
		if got := images.Registry(ref); got != want {
			t.Errorf("Registry(%q) = %q, want %q", ref, got, want)
		}
	}
	allowed := []string{"ghcr.io"}
	digest := "ghcr.io/a/b@sha256:" + strings.Repeat("0f", 32)
	for _, ref := range []string{"ghcr.io/a/b:1", "ghcr.io/a/b", digest} {
		if err := images.CheckRef(ref, allowed); err != nil {
			t.Errorf("%q: %v", ref, err)
		}
	}
	for _, ref := range []string{"", "-v", "ghcr.io/a b", "ghcr.io/a/$(x)", "ghcr.io/a/", "python:3", "quay.io/a/b",
		"ghcr.io/a/b@sha256:abc", "ghcr.io/a/b@md5:" + strings.Repeat("0", 64), "ghcr.io/" + strings.Repeat("a", images.MaxRef)} {
		if err := images.CheckRef(ref, allowed); !errors.Is(err, images.ErrInvalid) {
			t.Errorf("%q: %v, want ErrInvalid", ref, err)
		}
	}
	if err := images.CheckRef("ghcr.io/a/b:1", nil); !errors.Is(err, images.ErrInvalid) {
		t.Errorf("no registries allowed: %v", err)
	}
}

func add(t *testing.T, s *images.Store, tenant, name string) images.Image {
	t.Helper()
	img, err := s.Create(tenant, images.Image{ID: images.NewID(), Name: name, Ref: "ghcr.io/a/" + strings.ToLower(name), CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// A workspace's images are its own, unique by name ignoring case, at most
// Limit of them, and an update keeps who added them and what.
func TestTheStoreKeepsEachWorkspacesImages(t *testing.T) {
	s := images.Open(nil)
	a := add(t, s, "acme", "Agent")
	if a.Status != images.Checking {
		t.Errorf("a new image is %q, want checking", a.Status)
	}
	if _, err := s.Create("acme", images.Image{ID: images.NewID(), Name: "agent"}); !errors.Is(err, images.ErrNameTaken) {
		t.Errorf("same name: %v", err)
	}
	if _, err := s.Create("acme", images.Image{ID: "nope", Name: "X"}); !errors.Is(err, images.ErrInvalid) {
		t.Errorf("bad id: %v", err)
	}
	add(t, s, "globex", "Agent")
	if _, err := s.Get("globex", a.ID); !errors.Is(err, images.ErrNotFound) {
		t.Errorf("another tenant's image: %v", err)
	}
	if err := s.Delete("globex", a.ID); !errors.Is(err, images.ErrNotFound) {
		t.Errorf("another tenant deletes: %v", err)
	}

	up, err := s.Update("acme", a.ID, func(img images.Image) images.Image {
		img.Status, img.Digest, img.Name, img.Problem = images.Ready, "d", "Renamed", strings.Repeat("é", images.MaxProblem)
		return img
	})
	if err != nil {
		t.Fatal(err)
	}
	if up.Name != "Agent" || up.Status != images.Ready || len(up.Problem) > images.MaxProblem || !strings.HasPrefix(up.Problem, "é") {
		t.Errorf("updated = %+v", up)
	}

	for i := 1; i < images.Limit; i++ {
		add(t, s, "acme", fmt.Sprintf("Image %d", i))
	}
	if _, err := s.Create("acme", images.Image{ID: images.NewID(), Name: "One more"}); !errors.Is(err, images.ErrInvalid) {
		t.Errorf("past the limit: %v", err)
	}
	list, err := s.List("acme")
	if err != nil || len(list) != images.Limit || list[0].ID != a.ID {
		t.Fatalf("list = %d %v", len(list), err)
	}
	if err := s.Delete("acme", a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("acme", a.ID); !errors.Is(err, images.ErrNotFound) {
		t.Errorf("deleted: %v", err)
	}
}
