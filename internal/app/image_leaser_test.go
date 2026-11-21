package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/saige/flow/isolation"
	poolleaser "github.com/urmzd/saige/flow/isolation/isolate"
	"github.com/urmzd/saige/sandbox/isolate"
	"github.com/urmzd/saige/sandbox/isolate/local"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/images"
	"github.com/urmzd/humagen/pkg/join"
)

func testImageLeaser(t *testing.T, pool isolate.PoolConfig) *imageLeaser {
	t.Helper()
	l, err := newImageLeaser(local.Driver{Dir: t.TempDir(), Log: io.Discard}, poolleaser.Config{Image: "local", Pool: pool})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close(context.Background()) })
	return l
}

// settled is the leaser's instances once they have stopped changing.
func settled(l *imageLeaser) isolate.PoolStats {
	var s isolate.PoolStats
	for range 100 {
		time.Sleep(50 * time.Millisecond)
		n := l.Stats()
		if n == s {
			return n
		}
		s = n
	}
	return s
}

// An image leased and released leaves nothing behind: no idle instance and
// no spec that would start one, however many images are leased. Only an
// image the leaser is told to keep warm has idle instances, and telling it
// to keep fewer destroys the rest's.
func TestOnlyAnImageToldToBeKeptWarmStaysStarted(t *testing.T) {
	l := testImageLeaser(t, isolate.PoolConfig{Warm: 1, Max: 64})
	ctx := context.Background()
	for i := range 60 {
		inst, err := l.Lease(ctx, isolation.Request{Tenant: "acme", Run: "r", Image: fmt.Sprintf("ghcr.io/acme/i%d@sha256:%064d", i, i)})
		if err != nil {
			t.Fatal(err)
		}
		if s := l.Stats(); s.Leased != 1 || s.Live != 1 {
			t.Fatalf("with one lease out: %+v", s)
		}
		if err := inst.Close(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if s := settled(l); s != (isolate.PoolStats{}) {
		t.Fatalf("after 60 images were leased and released: %+v, want nothing kept", s)
	}

	warm := []isolation.Request{{Image: "ghcr.io/acme/a@sha256:1"}, {Image: "ghcr.io/acme/b@sha256:2"}, {Image: "ghcr.io/acme/c@sha256:3"}}
	l.KeepWarm(warm)
	if s := settled(l); s.Warm != 3 || s.Live != 3 || s.Specs != 3 {
		t.Fatalf("with three images kept warm: %+v", s)
	}
	// A lease of one is a warm instance, and another is started in its place.
	inst, err := l.Lease(ctx, isolation.Request{Tenant: "acme", Run: "r", Image: warm[0].Image})
	if err != nil {
		t.Fatal(err)
	}
	if s := settled(l); s.Warm != 3 || s.Leased != 1 {
		t.Errorf("with a kept image leased: %+v, want 3 warm and 1 leased", s)
	}
	// Told to keep one: the other two are let go, and so is the lease's
	// image, whose lease lives until it is released.
	l.KeepWarm(warm[2:])
	if s := settled(l); s.Warm != 1 || s.Leased != 1 || s.Specs != 1 {
		t.Errorf("told to keep one: %+v, want 1 warm, and the lease still out", s)
	}
	if st, err := inst.Exec(ctx, isolation.Command{Argv: []string{"true"}}, isolation.Stdio{}); err != nil || st.Code != 0 {
		t.Errorf("a lease of an image no longer kept warm: %v, exit %d", err, st.Code)
	}
	if err := inst.Close(ctx); err != nil {
		t.Fatal(err)
	}
	l.KeepWarm(nil)
	if s := settled(l); s != (isolate.PoolStats{}) {
		t.Errorf("told to keep none: %+v, want nothing kept", s)
	}
}

// The deployment's own image is the deployment pool's, as it was: leased
// from it, and kept warm by it.
func TestTheDeploymentsImageStaysInItsPool(t *testing.T) {
	l := testImageLeaser(t, isolate.PoolConfig{Warm: 1, Max: 8})
	inst, err := l.Lease(context.Background(), isolation.Request{Tenant: "acme", Run: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s, base := settled(l), l.Leaser.Stats(); s.Warm != 1 || s.Specs != 1 || base != s {
		t.Errorf("after a lease of the deployment's image: %+v (its pool: %+v), want one kept warm there", s, base)
	}
}

// Instances of workspaces' images out at once are bounded: a lease past the
// bound waits for one to be released.
func TestLeasesOfWorkspaceImagesAreBounded(t *testing.T) {
	l := testImageLeaser(t, isolate.PoolConfig{Warm: 1, Max: 2})
	ctx := context.Background()
	var out []isolation.Instance
	for i := range 2 {
		inst, err := l.Lease(ctx, isolation.Request{Run: "r", Image: fmt.Sprintf("ghcr.io/acme/i@sha256:%d", i)})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, inst)
	}
	short, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if _, err := l.Lease(short, isolation.Request{Run: "r", Image: "ghcr.io/acme/i@sha256:3"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a third lease with two out: %v, want it to wait", err)
	}
	if err := out[0].Close(ctx); err != nil {
		t.Fatal(err)
	}
	inst, err := l.Lease(ctx, isolation.Request{Run: "r", Image: "ghcr.io/acme/i@sha256:3"})
	if err != nil {
		t.Fatalf("a lease once one was released: %v", err)
	}
	_ = inst.Close(ctx)
	_ = out[1].Close(ctx)
}

// digestPuller pins each reference to a digest of its own.
type digestPuller struct{}

func (digestPuller) Pull(_ context.Context, ref, _ string) (string, error) {
	if strings.Contains(ref, "@") {
		return ref, nil
	}
	sum := sha256.Sum256([]byte(ref))
	return strings.Split(ref, ":")[0] + "@sha256:" + hex.EncodeToString(sum[:]), nil
}

// The bound a workspace is told holds on the real pool: a workspace that
// adds, checks and removes its limit of images over and over, none marked
// warm, leaves no instance started, and one that marks images warm holds
// instances of at most images.MaxWarm, which unmarking gives back.
func TestAWorkspaceHoldsWarmInstancesOnlyOfItsWarmImages(t *testing.T) {
	l := testImageLeaser(t, isolate.PoolConfig{Warm: 1, Max: 64})
	p, err := platform.New(platform.Config{
		Workspace:        t.TempDir(),
		Router:           func(string) join.Asker { return join.Words },
		Sandbox:          isolation.Sandbox{Leaser: l, Name: "test"},
		SandboxContained: true,
		SandboxImages:    &platform.ImageConfig{Registries: []string{"ghcr.io"}, Puller: digestPuller{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- p.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-stopped })
	admin := auth.Principal{Subject: "ana", Tenant: "acme", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	ready := func(id string) images.Image {
		t.Helper()
		for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if img, err := p.Images.Get("acme", id); err == nil && !img.BeingChecked() {
				if img.Status != images.Ready {
					t.Fatalf("image %s: %s %s", img.Name, img.Status, img.Problem)
				}
				return img
			}
		}
		t.Fatalf("image %s is still checking", id)
		return images.Image{}
	}
	var ids []string
	for round := range 3 {
		ids = ids[:0]
		for i := range images.Limit {
			img, err := p.AddImage(ctx, admin, platform.ImageInput{Name: fmt.Sprintf("r%d-%d", round, i), Ref: fmt.Sprintf("ghcr.io/acme/r%d-%d:1", round, i)})
			if err != nil {
				t.Fatal(err)
			}
			ids = append(ids, img.ID)
		}
		for _, id := range ids {
			ready(id)
		}
		if s := settled(l); s != (isolate.PoolStats{}) {
			t.Fatalf("round %d, %d images checked and none marked warm: %+v, want nothing started", round, images.Limit, s)
		}
		if round == 2 {
			break
		}
		for _, id := range ids {
			if err := p.RemoveImage(ctx, admin, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	on, off := true, false
	for i, id := range ids {
		_, err := p.ChangeImage(ctx, admin, id, platform.ImageChange{Warm: &on})
		if (err == nil) != (i < images.MaxWarm) {
			t.Fatalf("marking image %d of %d warm: %v; a workspace keeps at most %d", i+1, len(ids), err, images.MaxWarm)
		}
	}
	if s := settled(l); s.Warm != images.MaxWarm || s.Live != images.MaxWarm {
		t.Fatalf("with every image the workspace may mark warm: %+v, want %d warm", s, images.MaxWarm)
	}
	if _, err := p.ChangeImage(ctx, admin, ids[0], platform.ImageChange{Warm: &off}); err != nil {
		t.Fatal(err)
	}
	if err := p.RemoveImage(ctx, admin, ids[1]); err != nil {
		t.Fatal(err)
	}
	if s := settled(l); s.Warm != 1 || s.Live != 1 || s.Specs != 1 {
		t.Fatalf("with one unmarked and one removed: %+v, want 1 warm", s)
	}
}
