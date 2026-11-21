package images_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/images"
	"github.com/urmzd/humagen/pkg/state"
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

// Docker takes the first part of a path for a host when it has a dot, a
// port, is localhost, or has a capital letter. The allowlist is checked
// against the host Docker contacts, so a capital or a port is no way round
// it, and an image from Docker Hub passes only when docker.io is allowed.
func TestCheckRefAllowsOnlyTheHostDockerContacts(t *testing.T) {
	for ref, want := range map[string]string{
		"LOCALHOST/agent:1": "localhost", "EVILHOST/agent:1": "evilhost", "Metadata/x:1": "metadata",
		"Ghcr.IO/x": "ghcr.io", "ghcr.io:443/x": "ghcr.io:443", "10.0.0.5/x": "10.0.0.5",
		"index.docker.io/acme/x": "docker.io", "docker.io/library/python:3": "docker.io",
	} {
		if got := images.Registry(ref); got != want {
			t.Errorf("Registry(%q) = %q, want %q", ref, got, want)
		}
	}
	allowed := []string{"docker.io", "ghcr.io"}
	for _, ref := range []string{
		"LOCALHOST/agent:1", "EVILHOST/agent:1", "Metadata/x:1", "LOCALHOST/x", "Ghcr.IO/x", "GHCR.IO/acme/x",
		"ghcr.io:443/x", "10.0.0.5/x", "127.0.0.1:5000/x", "[::1]:5000/x",
	} {
		if err := images.CheckRef(ref, allowed); !errors.Is(err, images.ErrInvalid) {
			t.Errorf("CheckRef(%q, %v) = %v, want ErrInvalid (Docker pulls it from %s)", ref, allowed, err, images.Registry(ref))
		}
	}
	for _, ref := range []string{"ghcr.io/x", "acme/agent:1", "python:3", "docker.io/acme/x", "index.docker.io/acme/x", "library/python:3"} {
		if err := images.CheckRef(ref, allowed); err != nil {
			t.Errorf("CheckRef(%q, %v): %v", ref, allowed, err)
		}
	}
}

// A workspace keeps at most MaxWarm images warm: the sandbox's pool is
// shared, and what it warms stays warm.
func TestAWorkspaceKeepsFewImagesWarm(t *testing.T) {
	s := images.Open(nil)
	var kept []images.Image
	for i := range images.MaxWarm {
		img, err := s.Create("acme", images.Image{ID: images.NewID(), Name: fmt.Sprintf("Warm %d", i), Warm: true})
		if err != nil {
			t.Fatal(err)
		}
		kept = append(kept, img)
	}
	if _, err := s.Create("acme", images.Image{ID: images.NewID(), Name: "One more", Warm: true}); !errors.Is(err, images.ErrInvalid) {
		t.Errorf("a warm image past MaxWarm: %v, want ErrInvalid", err)
	}
	cold, err := s.Create("acme", images.Image{ID: images.NewID(), Name: "Cold"})
	if err != nil {
		t.Fatal(err)
	}
	warmUp := func(img images.Image) images.Image { img.Warm = true; return img }
	if _, err := s.Update("acme", cold.ID, warmUp); !errors.Is(err, images.ErrInvalid) {
		t.Errorf("warming one past MaxWarm: %v, want ErrInvalid", err)
	}
	if _, err := s.Update("acme", kept[0].ID, func(img images.Image) images.Image { img.Warm = false; return img }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update("acme", cold.ID, warmUp); err != nil {
		t.Errorf("warming one once another went cold: %v", err)
	}
	if _, err := s.Create("globex", images.Image{ID: images.NewID(), Name: "Theirs", Warm: true}); err != nil {
		t.Errorf("another workspace's warm image: %v", err)
	}
}

// neverLoaded is a store that fails the test when every record of a
// namespace is read after the one read that fills the directory.
type neverLoaded struct {
	state.Store
	t     *testing.T
	loads int
}

func (n *neverLoaded) Load(ctx context.Context, namespace string) ([]state.Record, error) {
	if n.loads++; n.loads > 1 {
		n.t.Errorf("every record of %s was read %d times; only the directory's first fill may", namespace, n.loads)
	}
	return n.Store.Load(ctx, namespace)
}

// The read across workspaces is bounded: it pages a directory of the
// workspaces that have added an image and reads one record for each, never
// the whole namespace, and stops at the bound it is given, saying so.
func TestReadingEveryWorkspacesImagesIsBounded(t *testing.T) {
	st := &neverLoaded{Store: state.NewMemory(), t: t}
	s := images.Open(st)
	for i := range 5 {
		tenant := fmt.Sprintf("ws-%d", i)
		if _, err := s.Create(tenant, images.Image{ID: images.NewID(), Name: "A"}); err != nil {
			t.Fatal(err)
		}
	}
	gone, err := s.Create("ws-9", images.Image{ID: images.NewID(), Name: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("ws-9", gone.ID); err != nil {
		t.Fatal(err)
	}
	read := func(s *images.Store, most int) ([]string, bool) {
		t.Helper()
		var got []string
		more, err := s.Tenants(most, func(tenant string, imgs []images.Image) {
			if len(imgs) != 1 {
				t.Errorf("%s: %d images, want 1", tenant, len(imgs))
			}
			got = append(got, tenant)
		})
		if err != nil {
			t.Fatal(err)
		}
		return got, more
	}
	all := []string{"ws-0", "ws-1", "ws-2", "ws-3", "ws-4"}
	// Another process on the same database reads the same.
	for _, s := range []*images.Store{s, images.Open(st)} {
		if got, more := read(s, 100); !slices.Equal(got, all) || more {
			t.Errorf("every workspace: %v more=%v, want %v and no more", got, more, all)
		}
		if got, more := read(s, 6); !slices.Equal(got, all) || more {
			t.Errorf("at the bound: %v more=%v, want %v and no more", got, more, all)
		}
		if got, more := read(s, 3); !slices.Equal(got, all[:3]) || !more {
			t.Errorf("past the bound: %v more=%v, want %v and more", got, more, all[:3])
		}
	}
	// More workspaces than one page of the directory holds.
	for i := range state.MaxList + 100 {
		if _, err := s.Create(fmt.Sprintf("big-%04d", i), images.Image{ID: images.NewID(), Name: "A"}); err != nil {
			t.Fatal(err)
		}
	}
	if got, more := read(s, 2*state.MaxList); len(got) != state.MaxList+105 || more || !slices.IsSorted(got) {
		t.Errorf("across pages: %d workspaces more=%v, want %d in order and no more", len(got), more, state.MaxList+105)
	}
	if got, more := read(s, state.MaxList+1); len(got) != state.MaxList+1 || !more {
		t.Errorf("across pages, past the bound: %d workspaces more=%v, want %d and more", len(got), more, state.MaxList+1)
	}
}
