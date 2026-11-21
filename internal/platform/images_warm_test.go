package platform_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/images"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/saige/flow/isolation"
	"github.com/urmzd/saige/flow/isolation/isolationtest"
)

// The sandbox is told to keep warm exactly the ready images marked warm,
// each time as they all stand: checking an image, or running a session in
// it, does not make it warm, and one unmarked, moved to another digest by
// a check again, or removed is no longer named, so the sandbox lets it go.
func TestOnlyImagesMarkedWarmAreKeptWarm(t *testing.T) {
	var mu sync.Mutex
	tag := map[string]string{} // the digest each reference resolves to now
	puller := pullFunc(func(_ context.Context, ref, _ string) (string, error) {
		if strings.Contains(ref, "@") {
			return ref, nil
		}
		mu.Lock()
		defer mu.Unlock()
		hex := tag[ref]
		if hex == "" {
			hex = "aa"
		}
		return strings.Split(ref, ":")[0] + "@sha256:" + strings.Repeat(hex, 32), nil
	})
	digest := func(name, hex string) string { return "ghcr.io/acme/" + name + "@sha256:" + strings.Repeat(hex, 32) }
	leaser := &warmLeaser{Leaser: &isolationtest.Leaser{Dir: t.TempDir()}}
	p := imagesPlatform(t, leaser, puller)
	add := func(name string, warm bool) images.Image {
		t.Helper()
		img, err := p.AddImage(t.Context(), admin, platform.ImageInput{Name: name, Ref: "ghcr.io/acme/" + name + ":1", Warm: warm})
		if err != nil {
			t.Fatal(err)
		}
		return readyImage(t, p, img.ID)
	}
	set := func(id string, warm bool) {
		t.Helper()
		if _, err := p.ChangeImage(t.Context(), admin, id, platform.ImageChange{Warm: &warm}); err != nil {
			t.Fatal(err)
		}
	}

	// Checked, and run in a session, and never marked warm: not kept warm.
	cold := add("cold", false)
	if _, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "Build it", Image: cold.ID}); err != nil {
		t.Fatal(err)
	}
	leaser.keeps(t, "with one image, not marked warm,")

	a, b := add("a", true), add("b", true)
	leaser.keeps(t, "with two images marked warm", digest("a", "aa"), digest("b", "aa"))

	set(cold.ID, true)
	leaser.keeps(t, "with a third marked warm", digest("a", "aa"), digest("b", "aa"), digest("cold", "aa"))
	set(cold.ID, false)
	leaser.keeps(t, "with it unmarked again", digest("a", "aa"), digest("b", "aa"))

	// A check again moves a to a new digest: the one it had is let go.
	mu.Lock()
	tag["ghcr.io/acme/a:1"] = "bb"
	mu.Unlock()
	if _, err := p.ChangeImage(t.Context(), admin, a.ID, platform.ImageChange{Check: true}); err != nil {
		t.Fatal(err)
	}
	leaser.keeps(t, "after a check again moved a", digest("a", "bb"), digest("b", "aa"))

	if err := p.RemoveImage(t.Context(), admin, b.ID); err != nil {
		t.Fatal(err)
	}
	leaser.keeps(t, "with b removed", digest("a", "bb"))
	if err := p.RemoveImage(t.Context(), admin, a.ID); err != nil {
		t.Fatal(err)
	}
	leaser.keeps(t, "with every warm image removed")
}

// What one process changes of the images kept warm reaches every other
// process on the database: each reads the images again as it sweeps, and
// lets go of what is no longer warm. A process keeps warm only what it
// pulled onto its own sandbox host.
func TestAnImageUnmarkedOnOneProcessIsLetGoOnEveryOther(t *testing.T) {
	t.Cleanup(platform.SetImageBeats(time.Hour, time.Hour, 50*time.Millisecond))
	shared := state.NewMemory()
	on := func() (*platform.Platform, *warmLeaser) {
		leaser := &warmLeaser{Leaser: &isolationtest.Leaser{Dir: t.TempDir()}}
		return startWith(t, platform.Config{
			Backends:         platform.Backends{Shared: shared},
			Sandbox:          isolation.Sandbox{Leaser: leaser, Name: "test"},
			SandboxContained: true,
			SandboxImages:    &platform.ImageConfig{Registries: []string{"ghcr.io"}, Puller: &pinPuller{}},
		}), leaser
	}
	first, firstLeaser := on()
	img, err := first.AddImage(t.Context(), admin, platform.ImageInput{Name: "A", Ref: "ghcr.io/acme/a:1", Warm: true})
	if err != nil {
		t.Fatal(err)
	}
	img = readyImage(t, first, img.ID)
	firstLeaser.keeps(t, "on the process that checked it", img.Digest)

	// A second process starts, and pulls what is ready as it does.
	second, secondLeaser := on()
	time.Sleep(200 * time.Millisecond)
	secondLeaser.keeps(t, "on a process that has not pulled it")
	second.PullReadyImages(t.Context())
	secondLeaser.keeps(t, "on a process that pulled it at start", img.Digest)

	// Unmarked through the second: the first lets it go without being asked.
	cold := false
	if _, err := second.ChangeImage(t.Context(), admin, img.ID, platform.ImageChange{Warm: &cold}); err != nil {
		t.Fatal(err)
	}
	secondLeaser.keeps(t, "on the process that unmarked it")
	firstLeaser.keeps(t, "on the other process, after its sweep,")
}
