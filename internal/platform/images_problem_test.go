package platform_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/images"
	"github.com/urmzd/saige/flow/isolation/isolationtest"
)

// syncBuffer is a log a test reads while the platform writes it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// A failed pull tells the image's readers one of a few reasons, in words
// made from the image's own reference. What the sandbox host printed (its
// Docker address, the registry's answer) goes to the server's log only, and
// the pull credential goes to neither, however the host wrote it.
func TestAFailedPullTellsReadersAReasonAndTheLogTheRest(t *testing.T) {
	const credential = "ws-user:SECRET-TOKEN"
	encoded := base64.StdEncoding.EncodeToString([]byte(credential))
	log := &syncBuffer{}
	was := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(log, nil)))
	defer slog.SetDefault(was)

	host := "failed to connect to the docker API at tcp://sandbox-7.internal.acme.net:2376; check if the path is correct and if the daemon is running: dial tcp 10.12.0.7:2376: connect: connection refused"
	said := map[string]string{
		"daemon":       "docker pull: " + host,
		"private":      "docker pull: Error response from daemon: Head \"https://ghcr.io/v2/acme/private/manifests/1\": unauthorized (sent " + credential + " as " + encoded + ", token SECRET-TOKEN)",
		"missing":      "docker pull: Error response from daemon: manifest unknown",
		"platform":     "docker pull: no matching manifest for linux/amd64 in the manifest list entries",
		"unreachable":  "docker pull: Error response from daemon: Get \"https://ghcr.io/v2/\": dial tcp: lookup ghcr.io on 10.12.0.2:53: no such host",
		"slow":         "docker pull: Error response from daemon: context deadline exceeded",
		"unclassified": "docker pull: /var/lib/docker/overlay2 on sandbox-7.internal.acme.net is full",
	}
	p := imagesPlatform(t, &isolationtest.Leaser{Dir: t.TempDir()}, pullFunc(func(_ context.Context, ref, _ string) (string, error) {
		name := strings.TrimSuffix(strings.TrimPrefix(ref, "ghcr.io/acme/"), ":1")
		return "", errors.New(said[name])
	}))
	want := map[string]string{
		"daemon":       "the sandbox host could not pull ghcr.io/acme/daemon:1; the server's log says why",
		"private":      "the registry has no ghcr.io/acme/private:1 that this credential can read",
		"missing":      "the registry has no ghcr.io/acme/missing:1 that this credential can read",
		"platform":     "ghcr.io/acme/platform:1 has no build for the sandbox host's platform",
		"unreachable":  "the sandbox host could not reach ghcr.io",
		"slow":         "pulling ghcr.io/acme/slow:1 took too long",
		"unclassified": "the sandbox host could not pull ghcr.io/acme/unclassified:1; the server's log says why",
	}
	for name := range said {
		img, err := p.AddImage(t.Context(), admin, platform.ImageInput{Name: name, Ref: "ghcr.io/acme/" + name + ":1", Credential: credential})
		if err != nil {
			t.Fatal(err)
		}
		readyImage(t, p, img.ID)
	}
	imgs, err := p.ListImages(viewer)
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != len(said) {
		t.Fatalf("%d images, want %d", len(imgs), len(said))
	}
	for _, img := range imgs {
		if img.Status != images.Failed || img.Problem != want[img.Name] {
			t.Errorf("%s: %s %q, want failed %q", img.Name, img.Status, img.Problem, want[img.Name])
		}
	}
	logged := log.String()
	for _, part := range []string{"sandbox-7.internal.acme.net:2376", "10.12.0.7", "/var/lib/docker/overlay2", "lookup ghcr.io on 10.12.0.2:53"} {
		if !strings.Contains(logged, part) {
			t.Errorf("the server's log does not hold %q", part)
		}
	}
	for _, secret := range []string{credential, "SECRET-TOKEN", encoded} {
		if strings.Contains(logged, secret) {
			t.Errorf("the server's log holds the credential as %q", secret)
		}
		for _, img := range imgs {
			if strings.Contains(img.Problem, secret) {
				t.Errorf("%s's problem holds the credential", img.Name)
			}
		}
	}
}
