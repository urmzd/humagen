// Package images keeps the sandbox images a workspace added of its own.
//
// Every session runs its tools in an image: the deployment's default, which
// everyone has, or one a workspace added here. A workspace's image is a
// reference to a registry the deployment allows (ghcr.io/acme/agent:1.4),
// checked once when it is added: pulled, pinned to the digest it resolved to,
// and started. Sessions then run the digest, so every sandbox host runs the
// same bytes however many there are.
//
// The default image is not kept here: it is the deployment's, and Default
// names it. A pull credential is not kept here either: it goes to the
// secrets store the platform keeps client secrets in, and an image records
// only that it has one.
//
// It reads and writes the database per request, so every process on one
// database agrees on every image.
package images

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/state"
)

// Default is the id of the deployment's own image. It is never stored.
const Default = "default"

// Bounds on what a workspace keeps.
const (
	// Limit bounds the images one workspace keeps.
	Limit = 20
	// MaxName bounds an image's name, in characters.
	MaxName = 60
	// MaxRef bounds a reference, in bytes.
	MaxRef = 512
	// MaxProblem bounds why a check failed, in bytes.
	MaxProblem = 512
)

// ErrInvalid reports an image that will not be kept. Callers match with
// errors.Is.
var ErrInvalid = errors.New("images: invalid")

// ErrNotFound reports an image the workspace does not have.
var ErrNotFound = errors.New("images: not found")

// ErrNameTaken reports a name another image in the workspace has.
var ErrNameTaken = errors.New("images: name taken")

// Status is where an image's check stands.
type Status string

const (
	// Checking: it is being pulled, pinned and started.
	Checking Status = "checking"
	// Ready: sessions may run it.
	Ready Status = "ready"
	// Failed: the check did not pass; Problem says why.
	Failed Status = "failed"
)

// Image is one image a workspace added.
type Image struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Ref is the reference as it was added.
	Ref string `json:"ref"`
	// Digest is Ref pinned, repo@sha256:..., once the check resolved it.
	Digest  string `json:"digest,omitempty"`
	Status  Status `json:"status"`
	Problem string `json:"problem,omitempty"`
	// Warm keeps an instance of it started on each sandbox host, so a
	// session does not wait for one.
	Warm bool `json:"warm,omitempty"`
	// Credential says a pull credential is kept for it.
	Credential bool      `json:"credential,omitempty"`
	CreatedBy  string    `json:"createdBy"`
	CreatedAt  time.Time `json:"createdAt"`
	CheckedAt  time.Time `json:"checkedAt,omitzero"`
}

// NewID returns a fresh image id.
func NewID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "img_" + hex.EncodeToString(b[:])
}

// ValidID reports whether s may name a stored image.
func ValidID(s string) bool {
	if !strings.HasPrefix(s, "img_") || len(s) != len("img_")+12 {
		return false
	}
	_, err := hex.DecodeString(s[len("img_"):])
	return err == nil
}

// host is a reference's registry part as written, when it has one. It
// follows Docker's own rule (distribution/reference): the first part of the
// path names a host when it holds a dot or a port, is localhost, or has an
// upper-case letter. Anything else pulls from Docker Hub.
func host(ref string) (string, bool) {
	first, _, ok := strings.Cut(ref, "/")
	if ok && (strings.ContainsAny(first, ".:") || first == "localhost" || strings.ToLower(first) != first) {
		return first, true
	}
	return "", false
}

// Registry is the host a reference pulls from, in lower case: the first
// part of its path when it names a host, and docker.io otherwise. Docker's
// legacy index.docker.io is docker.io.
func Registry(ref string) string {
	h, ok := host(ref)
	if !ok {
		return "docker.io"
	}
	h = strings.ToLower(h)
	if h == "index.docker.io" {
		return "docker.io"
	}
	return h
}

// CheckRef refuses a reference that is malformed or whose registry is not
// in allowed. An empty allowed allows none: custom images are off.
func CheckRef(ref string, allowed []string) error {
	if ref == "" || len(ref) > MaxRef || strings.HasPrefix(ref, "-") ||
		strings.ContainsAny(ref, " \t\r\n\x00\"'`$\\") || strings.HasSuffix(ref, "/") {
		return fmt.Errorf("%w: a reference looks like ghcr.io/acme/agent:1.4", ErrInvalid)
	}
	if _, digest, ok := strings.Cut(ref, "@"); ok {
		hexPart, found := strings.CutPrefix(digest, "sha256:")
		if _, err := hex.DecodeString(hexPart); !found || len(hexPart) != 64 || err != nil {
			return fmt.Errorf("%w: a digest is sha256: and 64 hex digits", ErrInvalid)
		}
	}
	if len(allowed) == 0 {
		return fmt.Errorf("%w: this deployment allows no images of a workspace's own", ErrInvalid)
	}
	// A host written in capitals is still a host to Docker, and an
	// allowlist compared on one spelling would let another through.
	if h, ok := host(ref); ok && h != strings.ToLower(h) {
		return fmt.Errorf("%w: write the registry %s in lower case", ErrInvalid, h)
	}
	if reg := Registry(ref); !slices.Contains(allowed, reg) {
		return fmt.Errorf("%w: %s is not a registry this deployment allows (%s)", ErrInvalid, reg, strings.Join(allowed, ", "))
	}
	return nil
}

// CheckName refuses an empty or overlong name.
func CheckName(name string) error {
	if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > MaxName || strings.ContainsAny(name, "\r\n\x00") {
		return fmt.Errorf("%w: a name is 1 to %d characters on one line", ErrInvalid, MaxName)
	}
	return nil
}

const key = "images"

// Store keeps each workspace's images, one record per workspace.
type Store struct {
	t *state.Table[map[string]Image]
}

// Open returns a store on st, which may be shared with other processes. A
// nil st keeps images in this process's memory.
func Open(st state.Store) *Store {
	if st == nil {
		st = state.NewMemory()
	}
	return &Store{t: state.NewTable[map[string]Image](st, "sandbox-images")}
}

func (s *Store) all(tenant string) (map[string]Image, error) {
	item, err := s.t.Lookup(tenant, key)
	if errors.Is(err, state.ErrNotFound) {
		return map[string]Image{}, nil
	}
	if err != nil {
		return nil, err
	}
	return item.Value, nil
}

// List returns the workspace's images, oldest first.
func (s *Store) List(tenant string) ([]Image, error) {
	all, err := s.all(tenant)
	if err != nil {
		return nil, err
	}
	out := make([]Image, 0, len(all))
	for _, img := range all {
		out = append(out, img)
	}
	slices.SortFunc(out, func(a, b Image) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

// Get returns one of the workspace's images.
func (s *Store) Get(tenant, id string) (Image, error) {
	all, err := s.all(tenant)
	if err != nil {
		return Image{}, err
	}
	img, ok := all[id]
	if !ok {
		return Image{}, ErrNotFound
	}
	return img, nil
}

// Create keeps a new image, checking, under its own id. A name is unique in
// a workspace, ignoring case.
func (s *Store) Create(tenant string, img Image) (Image, error) {
	if !ValidID(img.ID) {
		return Image{}, fmt.Errorf("%w: id %q", ErrInvalid, img.ID)
	}
	img.Name = strings.TrimSpace(img.Name)
	if err := CheckName(img.Name); err != nil {
		return Image{}, err
	}
	img.Status, img.Digest, img.Problem = Checking, "", ""
	_, err := s.t.Update(tenant, key, func(all map[string]Image, _ bool) (map[string]Image, error) {
		if len(all) >= Limit {
			return nil, fmt.Errorf("%w: a workspace keeps at most %d images", ErrInvalid, Limit)
		}
		next := make(map[string]Image, len(all)+1)
		for k, v := range all {
			if strings.EqualFold(v.Name, img.Name) {
				return nil, fmt.Errorf("%w: %q", ErrNameTaken, img.Name)
			}
			next[k] = v
		}
		if _, ok := next[img.ID]; ok {
			return nil, fmt.Errorf("%w: id %q is taken", ErrInvalid, img.ID)
		}
		next[img.ID] = img
		return next, nil
	})
	if err != nil {
		return Image{}, err
	}
	return img, nil
}

// Update changes one image with fn and returns it as kept. Its id, name,
// reference, creator and creation time do not change.
func (s *Store) Update(tenant, id string, fn func(Image) Image) (Image, error) {
	var out Image
	_, err := s.t.Update(tenant, key, func(all map[string]Image, _ bool) (map[string]Image, error) {
		cur, ok := all[id]
		if !ok {
			return nil, ErrNotFound
		}
		next := fn(cur)
		next.ID, next.Name, next.Ref, next.CreatedBy, next.CreatedAt = cur.ID, cur.Name, cur.Ref, cur.CreatedBy, cur.CreatedAt
		if len(next.Problem) > MaxProblem {
			next.Problem = next.Problem[:MaxProblem]
			for !utf8.ValidString(next.Problem) {
				next.Problem = next.Problem[:len(next.Problem)-1]
			}
		}
		if next == cur {
			out = cur
			return nil, state.ErrUnchanged
		}
		copied := make(map[string]Image, len(all))
		for k, v := range all {
			copied[k] = v
		}
		copied[id] = next
		out = next
		return copied, nil
	})
	if errors.Is(err, state.ErrUnchanged) {
		return out, nil
	}
	if err != nil {
		return Image{}, err
	}
	return out, nil
}

// Delete removes one image.
func (s *Store) Delete(tenant, id string) error {
	_, err := s.t.Update(tenant, key, func(all map[string]Image, _ bool) (map[string]Image, error) {
		if _, ok := all[id]; !ok {
			return nil, ErrNotFound
		}
		next := make(map[string]Image, len(all))
		for k, v := range all {
			if k != id {
				next[k] = v
			}
		}
		return next, nil
	})
	return err
}

// Tenants calls fn with every workspace's images, for the work the platform
// does across them: pulling ready images onto a sandbox host when it starts.
func (s *Store) Tenants(fn func(tenant string, imgs []Image) error) error {
	return s.t.Load(func(tenant, _ string, all map[string]Image) error {
		imgs := make([]Image, 0, len(all))
		for _, img := range all {
			imgs = append(imgs, img)
		}
		return fn(tenant, imgs)
	})
}
