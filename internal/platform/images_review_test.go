package platform_test

// Regression tests for the adversarial review of #486: how many checks run,
// which one's result is kept, when an image is warmed, and that a check
// ends with the platform.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/images"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/saige/flow/isolation"
	"github.com/urmzd/saige/flow/isolation/isolationtest"
)

// gatePuller pins like pinPuller, holding every pull block says to until
// gate closes, and counts the pulls under way.
type gatePuller struct {
	gate           chan struct{}
	block          func(n int, ref string) bool
	n              atomic.Int64
	inflight, peak atomic.Int64
	started        chan string
}

func newGatePuller(block func(n int, ref string) bool) *gatePuller {
	return &gatePuller{gate: make(chan struct{}), block: block, started: make(chan string, 100)}
}

func (g *gatePuller) Pull(ctx context.Context, ref, _ string) (string, error) {
	n := int(g.n.Add(1))
	cur := g.inflight.Add(1)
	defer g.inflight.Add(-1)
	for p := g.peak.Load(); cur > p && !g.peak.CompareAndSwap(p, cur); p = g.peak.Load() {
	}
	g.started <- ref
	if g.block(n, ref) {
		select {
		case <-g.gate:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if strings.Contains(ref, "@") {
		return ref, nil
	}
	return strings.Split(ref, ":")[0] + "@sha256:" + strings.Repeat("cd", 32), nil
}

func (g *gatePuller) waitStarted(t *testing.T) string {
	t.Helper()
	select {
	case ref := <-g.started:
		return ref
	case <-time.After(10 * time.Second):
		t.Fatal("no pull started")
		return ""
	}
}

// warmLeaser records the images it is asked to keep warm.
type warmLeaser struct {
	*isolationtest.Leaser
	mu     sync.Mutex
	warmed []string
}

func (w *warmLeaser) Warm(req isolation.Request) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.warmed = append(w.warmed, req.Image)
	return nil
}

func (w *warmLeaser) take() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := w.warmed
	w.warmed = nil
	return out
}

func imagesPlatform(t *testing.T, leaser isolation.Leaser, puller platform.ImagePuller) *platform.Platform {
	t.Helper()
	return startWith(t, platform.Config{
		Sandbox:          isolation.Sandbox{Leaser: leaser, Name: "test"},
		SandboxContained: true,
		SandboxImages:    &platform.ImageConfig{Registries: []string{"ghcr.io"}, Puller: puller},
	})
}

// Asking for a check while one runs starts none: it answers the image as it
// stands, checking. A person looping PATCH {check:true} holds one pull.
func TestACheckAskedForWhileOneRunsStartsNone(t *testing.T) {
	g := newGatePuller(func(n int, _ string) bool { return n > 1 })
	defer close(g.gate)
	p := imagesPlatform(t, &isolationtest.Leaser{Dir: t.TempDir()}, g)
	img, err := p.AddImage(t.Context(), admin, platform.ImageInput{Name: "A", Ref: "ghcr.io/acme/a:1"})
	if err != nil {
		t.Fatal(err)
	}
	readyImage(t, p, img.ID)
	g.waitStarted(t)
	for i := range 50 {
		got, err := p.ChangeImage(t.Context(), admin, img.ID, platform.ImageChange{Check: true})
		if err != nil {
			t.Fatal(err)
		}
		if !got.Rechecking || got.Status != images.Ready {
			t.Fatalf("check %d answered %s (rechecking %v), want ready and being checked again", i, got.Status, got.Rechecking)
		}
	}
	g.waitStarted(t)
	time.Sleep(200 * time.Millisecond)
	if n := g.inflight.Load(); n != 1 {
		t.Errorf("after 50 PATCH check: %d pulls under way, want 1", n)
	}
}

// However many images a workspace checks at once, at most
// MaxTenantImagePulls of its own pull at a time: they share the sandbox
// host's Docker daemon with every other workspace's.
func TestImagePullsAreBoundedAcrossImages(t *testing.T) {
	g := newGatePuller(func(int, string) bool { return true })
	p := imagesPlatform(t, &isolationtest.Leaser{Dir: t.TempDir()}, g)
	var ids []string
	for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
		img, err := p.AddImage(t.Context(), admin, platform.ImageInput{Name: name, Ref: "ghcr.io/acme/" + name + ":1"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, img.ID)
	}
	for range min(platform.MaxTenantImagePulls, len(ids)) {
		g.waitStarted(t)
	}
	time.Sleep(300 * time.Millisecond)
	if peak := g.peak.Load(); peak != platform.MaxTenantImagePulls {
		t.Errorf("%d pulls ran at once, want %d", peak, platform.MaxTenantImagePulls)
	}
	close(g.gate)
	for _, id := range ids {
		if got := readyImage(t, p, id); got.Status != images.Ready {
			t.Errorf("image %s: %s %s", id, got.Status, got.Problem)
		}
	}
}

// seqPuller pins its first pull to 1111 once slow closes, and every later
// one to 2222 at once.
type seqPuller struct {
	digest  string
	slow    chan struct{}
	started chan struct{}
}

func (s *seqPuller) Pull(ctx context.Context, ref, _ string) (string, error) {
	if s.slow != nil {
		close(s.started)
		select {
		case <-s.slow:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return "ghcr.io/acme/a@sha256:" + strings.Repeat(s.digest, 32), nil
}

// A slow check that ends after a newer one, here on another process on
// the same database, keeps nothing: the newer check's digest stands.
func TestAnOlderCheckNeverReplacesANewerOne(t *testing.T) {
	shared := state.NewMemory()
	on := func(puller platform.ImagePuller) *platform.Platform {
		return startWith(t, platform.Config{
			Backends:         platform.Backends{Shared: shared},
			Sandbox:          isolation.Sandbox{Leaser: &isolationtest.Leaser{Dir: t.TempDir()}, Name: "test"},
			SandboxContained: true,
			SandboxImages:    &platform.ImageConfig{Registries: []string{"ghcr.io"}, Puller: puller},
		})
	}
	slow := &seqPuller{digest: "11", slow: make(chan struct{}), started: make(chan struct{})}
	first, second := on(slow), on(&seqPuller{digest: "22"})
	img, err := first.AddImage(t.Context(), admin, platform.ImageInput{Name: "A", Ref: "ghcr.io/acme/a:1"})
	if err != nil {
		t.Fatal(err)
	}
	<-slow.started
	if _, err := second.ChangeImage(t.Context(), admin, img.ID, platform.ImageChange{Check: true}); err != nil {
		t.Fatal(err)
	}
	newer := readyImage(t, second, img.ID)
	if !strings.Contains(newer.Digest, "2222") {
		t.Fatalf("the newer check: %+v", newer)
	}
	close(slow.slow)
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		got, err := first.Images.Get(tenant, img.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Digest != newer.Digest || got.Status != images.Ready {
			t.Fatalf("the older check replaced the newer one: %s %s", got.Status, got.Digest)
		}
	}
}

// An image is warmed only while it is ready and kept warm: one set cold,
// or removed, while the start-up pull fetched it is not warmed after. And
// a workspace keeps at most MaxWarm images warm.
func TestImageWarmingFollowsTheImageAsItStands(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(p *platform.Platform, id string) error
	}{
		{"set cold", func(p *platform.Platform, id string) error {
			cold := false
			_, err := p.ChangeImage(context.Background(), admin, id, platform.ImageChange{Warm: &cold})
			return err
		}},
		{"removed", func(p *platform.Platform, id string) error { return p.RemoveImage(context.Background(), admin, id) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := newGatePuller(func(_ int, ref string) bool { return strings.Contains(ref, "@") })
			leaser := &warmLeaser{Leaser: &isolationtest.Leaser{Dir: t.TempDir()}}
			p := imagesPlatform(t, leaser, g)
			img, err := p.AddImage(t.Context(), admin, platform.ImageInput{Name: "A", Ref: "ghcr.io/acme/a:1", Warm: true})
			if err != nil {
				t.Fatal(err)
			}
			readyImage(t, p, img.ID)
			g.waitStarted(t)
			if got := leaser.take(); len(got) != 1 {
				t.Fatalf("the check warmed %v", got)
			}
			done := make(chan struct{})
			go func() { defer close(done); p.PullReadyImages(t.Context()) }()
			g.waitStarted(t)
			if err := c.change(p, img.ID); err != nil {
				t.Fatal(err)
			}
			close(g.gate)
			<-done
			if got := leaser.take(); len(got) != 0 {
				t.Errorf("an image %s during the start-up pull was warmed: %v", c.name, got)
			}
		})
	}

	p := imagesPlatform(t, &warmLeaser{Leaser: &isolationtest.Leaser{Dir: t.TempDir()}}, &pinPuller{})
	for i := range images.MaxWarm {
		name := string(rune('a' + i))
		if _, err := p.AddImage(t.Context(), admin, platform.ImageInput{Name: name, Ref: "ghcr.io/acme/" + name + ":1", Warm: true}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.AddImage(t.Context(), admin, platform.ImageInput{Name: "z", Ref: "ghcr.io/acme/z:1", Warm: true}); !errors.Is(err, images.ErrInvalid) {
		t.Errorf("a warm image past MaxWarm: %v, want ErrInvalid", err)
	}
}

// A check under way when the platform stops ends with it, and says so.
func TestAnImageCheckEndsWithThePlatform(t *testing.T) {
	ended := make(chan struct{}, 1)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	puller := platform.ImagePuller(pullFunc(func(ctx context.Context, _, _ string) (string, error) {
		select {
		case <-ctx.Done():
			ended <- struct{}{}
			return "", ctx.Err()
		case <-release:
			return "", errors.New("the test ended")
		}
	}))
	p, err := platform.New(platform.Config{
		Workspace:        t.TempDir(),
		Router:           func(string) join.Asker { return join.Words },
		Sandbox:          isolation.Sandbox{Leaser: &isolationtest.Leaser{Dir: t.TempDir()}, Name: "test"},
		SandboxContained: true,
		SandboxImages:    &platform.ImageConfig{Registries: []string{"ghcr.io"}, Puller: puller},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- p.Run(ctx) }()
	img, err := p.AddImage(t.Context(), admin, platform.ImageInput{Name: "A", Ref: "ghcr.io/acme/a:1"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("platform stopped with %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the platform did not stop")
	}
	select {
	case <-ended:
	default:
		t.Fatal("the image's pull was still running after the platform stopped")
	}
	got, err := p.Images.Get(tenant, img.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != images.Failed || !strings.Contains(got.Problem, "check it again") {
		t.Errorf("a check the platform stopped: %s %q", got.Status, got.Problem)
	}
}

type pullFunc func(ctx context.Context, ref, credential string) (string, error)

func (f pullFunc) Pull(ctx context.Context, ref, credential string) (string, error) {
	return f(ctx, ref, credential)
}

// Checking an image again checks its registry against the deployment's
// list as it is now: one the operator stopped allowing fails.
func TestACheckAgainRefusesARegistryNoLongerAllowed(t *testing.T) {
	cfg := &platform.ImageConfig{Registries: []string{"ghcr.io"}, Puller: &pinPuller{}}
	p := startWith(t, platform.Config{
		Sandbox:          isolation.Sandbox{Leaser: &isolationtest.Leaser{Dir: t.TempDir()}, Name: "test"},
		SandboxContained: true,
		SandboxImages:    cfg,
	})
	img, err := p.AddImage(t.Context(), admin, platform.ImageInput{Name: "A", Ref: "ghcr.io/acme/a:1"})
	if err != nil {
		t.Fatal(err)
	}
	readyImage(t, p, img.ID)
	cfg.Registries = []string{"quay.io"} // the operator narrows the list and restarts
	if _, err := p.ChangeImage(t.Context(), admin, img.ID, platform.ImageChange{Check: true}); err != nil {
		t.Fatal(err)
	}
	got := readyImage(t, p, img.ID)
	if got.Status != images.Failed || !strings.Contains(got.Problem, "ghcr.io") {
		t.Errorf("a check against a narrowed list: %s %q, want failed naming ghcr.io", got.Status, got.Problem)
	}
}

// A registry that hangs at start holds up one image's pull, not every
// other image's: each pull has a timeout of its own.
func TestStartUpPullsHaveATimeoutEach(t *testing.T) {
	defer platform.SetImageRepullTimeout(200 * time.Millisecond)()
	var repulls atomic.Int64
	puller := pullFunc(func(ctx context.Context, ref, _ string) (string, error) {
		if !strings.Contains(ref, "@") {
			return strings.Split(ref, ":")[0] + "@sha256:" + strings.Repeat("cd", 32), nil
		}
		if repulls.Add(1) == 1 {
			<-ctx.Done() // the registry never answers
			return "", ctx.Err()
		}
		return ref, nil
	})
	p := imagesPlatform(t, &isolationtest.Leaser{Dir: t.TempDir()}, puller)
	for _, name := range []string{"a", "b"} {
		img, err := p.AddImage(t.Context(), admin, platform.ImageInput{Name: name, Ref: "ghcr.io/acme/" + name + ":1"})
		if err != nil {
			t.Fatal(err)
		}
		readyImage(t, p, img.ID)
	}
	done := make(chan struct{})
	go func() { defer close(done); p.PullReadyImages(t.Context()) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("one hung pull held up the start-up pulls")
	}
	if n := repulls.Load(); n != 2 {
		t.Errorf("%d images pulled at start, want 2", n)
	}
}
