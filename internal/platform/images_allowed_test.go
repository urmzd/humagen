package platform_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/images"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/flow/isolation"
	"github.com/urmzd/saige/flow/isolation/isolationtest"
)

// credPuller records every pull it is asked for, and the credential sent.
type credPuller struct {
	mu    sync.Mutex
	pulls []string
}

func (c *credPuller) Pull(_ context.Context, ref, credential string) (string, error) {
	c.mu.Lock()
	c.pulls = append(c.pulls, ref+" with "+credential)
	c.mu.Unlock()
	if strings.Contains(ref, "@") {
		return ref, nil
	}
	return strings.Split(ref, ":")[0] + "@sha256:" + strings.Repeat("cd", 32), nil
}

func (c *credPuller) got() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.pulls)
}

// deployment returns what starts a server process on one database with the
// registries it allows, and the leaser and puller it was given.
func deployment(t *testing.T) func(registries ...string) (*platform.Platform, *warmLeaser, *credPuller) {
	t.Helper()
	shared := state.NewMemory()
	return func(registries ...string) (*platform.Platform, *warmLeaser, *credPuller) {
		leaser := &warmLeaser{Leaser: &isolationtest.Leaser{Dir: t.TempDir()}}
		puller := &credPuller{}
		return startWith(t, platform.Config{
			Backends:         platform.Backends{Shared: shared},
			Sandbox:          isolation.Sandbox{Leaser: leaser, Name: "test"},
			SandboxContained: true,
			SandboxImages:    &platform.ImageConfig{Registries: registries, Puller: puller},
		}), leaser, puller
	}
}

// commandIn runs a person's command in a session, once its first run lets it.
func commandIn(t *testing.T, p *platform.Platform, thread string) error {
	t.Helper()
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		_, err := p.RunPersonCommand(t.Context(), admin, thread, platform.PersonCommand{Command: "true"})
		if err == nil || !strings.Contains(err.Error(), "wait for active") || time.Now().After(deadline) {
			return err
		}
	}
}

// refusedEverywhere checks every use of a ready, warm image on a process
// whose deployment does not allow its registry.
func refusedEverywhere(t *testing.T, p *platform.Platform, leaser *warmLeaser, puller *credPuller, id, session string) {
	t.Helper()
	if got := puller.got(); len(got) != 0 {
		t.Errorf("pulled from a registry no longer allowed: %v", got)
	}
	offered, err := p.SessionImages(admin)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(offered, func(img platform.SessionImage) bool { return img.ID == id })
	if i < 0 || offered[i].Status != images.Failed || offered[i].Problem != "this deployment no longer allows images from its registry" {
		t.Errorf("offered to a new session: %+v, want it failed with the reason", offered)
	}
	listed, err := p.ListImages(admin)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Status != images.Failed || listed[0].Problem != offered[i].Problem || listed[0].Rechecking {
		t.Errorf("listed in Settings: %+v, want it failed with the reason", listed)
	}
	if strings.Contains(offered[i].Problem, "ghcr.io") || strings.Contains(offered[i].Problem, "quay.io") {
		t.Errorf("the reason names a host: %q", offered[i].Problem)
	}
	if _, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "Build it", Image: id}); !errors.Is(err, threads.ErrInvalid) {
		t.Errorf("a new session in it: %v, want ErrInvalid", err)
	}
	// What a session's runs and a person's commands lease in.
	if digest, err := platform.SessionDigest(p, tenant, session); !errors.Is(err, platform.ErrImageGone) {
		t.Errorf("a running session's next lease: %q %v, want ErrImageGone", digest, err)
	}
	for _, inst := range leaser.Instances() {
		if strings.HasPrefix(inst.Request().Image, "ghcr.io/") {
			t.Errorf("an instance of it was started: %q", inst.Request().Image)
		}
	}
	if got := leaser.warm(); len(got) != 0 {
		t.Errorf("kept warm: %v", got)
	}
	// Nothing kept of it changed: reading it writes nothing.
	if kept, err := p.Images.Get(tenant, id); err != nil || kept.Status != images.Ready || kept.Problem != "" {
		t.Errorf("what is kept of it: %+v %v, want ready as it was", kept, err)
	}
}

// A deployment that stops allowing a registry and starts again has stopped
// using every image from it, with nobody checking anything again: none is
// pulled, so the workspace's credential goes nowhere, none is offered or
// kept warm, a new session in one is refused, and so is the next lease of a
// session already in one. Allowing the registry again brings the image
// back as it was, unchecked, because nothing kept of it changed.
func TestAnImageFromARegistryNoLongerAllowedIsNotUsedAfterARestart(t *testing.T) {
	on := deployment(t)
	first, _, _ := on("ghcr.io", "quay.io")
	img, err := first.AddImage(t.Context(), admin, platform.ImageInput{Name: "A", Ref: "ghcr.io/acme/a:1", Credential: "ws:tok", Warm: true})
	if err != nil {
		t.Fatal(err)
	}
	readyImage(t, first, img.ID)
	v, err := first.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "Build it", Image: img.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := commandIn(t, first, v.Thread.ID); err != nil {
		t.Fatalf("a command while the registry is allowed: %v", err)
	}

	second, leaser, puller := on("quay.io")
	second.PullReadyImages(t.Context()) // as the server does when it starts
	platform.SweepImages(second)
	refusedEverywhere(t, second, leaser, puller, img.ID, v.Thread.ID)

	third, leaser, puller := on("quay.io", "ghcr.io")
	third.PullReadyImages(t.Context())
	want := "ghcr.io/acme/a@sha256:" + strings.Repeat("cd", 32)
	if got := puller.got(); !slices.Equal(got, []string{want + " with ws:tok"}) {
		t.Errorf("pulled once the registry is allowed again: %v", got)
	}
	leaser.keeps(t, "once the registry is allowed again", want)
	if digest, err := platform.SessionDigest(third, tenant, v.Thread.ID); err != nil || digest != want {
		t.Errorf("the session's next lease once the registry is allowed again: %q %v", digest, err)
	}
}

// The same on a process that was already running when the image became
// ready on another whose registries are wider, as during a deploy that
// narrows them: it reads the image and refuses it, with no start in
// between. An administrator who checks it again there has it kept as
// failed, and then no process uses it until a check passes.
func TestAnImageFromARegistryNotAllowedHereIsNotUsedWithoutARestart(t *testing.T) {
	on := deployment(t)
	narrow, leaser, puller := on("quay.io")
	wide, _, _ := on("ghcr.io", "quay.io")
	img, err := wide.AddImage(t.Context(), admin, platform.ImageInput{Name: "A", Ref: "ghcr.io/acme/a:1", Credential: "ws:tok", Warm: true})
	if err != nil {
		t.Fatal(err)
	}
	readyImage(t, wide, img.ID)
	v, err := wide.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "Build it", Image: img.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := commandIn(t, wide, v.Thread.ID); err != nil {
		t.Fatalf("a command where the registry is allowed: %v", err)
	}
	platform.SweepImages(narrow)
	refusedEverywhere(t, narrow, leaser, puller, img.ID, v.Thread.ID)

	got, err := narrow.ChangeImage(t.Context(), admin, img.ID, platform.ImageChange{Check: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != images.Failed {
		t.Errorf("checked again where it is not allowed: %+v, want it shown failed", got)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		kept, err := wide.Images.Get(tenant, img.ID)
		if err != nil {
			t.Fatal(err)
		}
		if kept.Status == images.Failed && !kept.BeingChecked() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the check where it is not allowed left it %+v, want failed", kept)
		}
	}
	if got := puller.got(); len(got) != 0 {
		t.Errorf("the check pulled from a registry not allowed: %v", got)
	}
	if err := commandIn(t, wide, v.Thread.ID); !errors.Is(err, platform.ErrImageGone) {
		t.Errorf("a command where it is allowed, after it was kept failed: %v, want ErrImageGone", err)
	}
}
