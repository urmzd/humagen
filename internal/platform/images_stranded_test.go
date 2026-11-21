package platform_test

import (
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/images"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/saige/flow/isolation"
	"github.com/urmzd/saige/flow/isolation/isolationtest"
)

// processes returns what starts a platform on one database shared by all
// of them, as replicas and a restarted process are.
func processes(t *testing.T) func(platform.ImagePuller) *platform.Platform {
	t.Helper()
	shared := state.NewMemory()
	return func(puller platform.ImagePuller) *platform.Platform {
		return startWith(t, platform.Config{
			Backends:         platform.Backends{Shared: shared},
			Sandbox:          isolation.Sandbox{Leaser: &isolationtest.Leaser{Dir: t.TempDir()}, Name: "test"},
			SandboxContained: true,
			SandboxImages:    &platform.ImageConfig{Registries: []string{"ghcr.io"}, Puller: puller},
		})
	}
}

// A check whose process died leaves the image checking. The process that
// replaces it finds the check has stopped beating and starts it again, with
// no one asking: the image does not stay checking for good. What the dead
// process's check would have written is dropped.
func TestACheckStrandedByADeadProcessIsStartedAgain(t *testing.T) {
	// The first process never beats, as a dead one does not.
	t.Cleanup(platform.SetImageBeats(time.Hour, 300*time.Millisecond, 50*time.Millisecond))
	on := processes(t)
	dead := newGatePuller(func(int, string) bool { return true })
	first := on(dead)
	img, err := first.AddImage(t.Context(), admin, platform.ImageInput{Name: "A", Ref: "ghcr.io/acme/a:1"})
	if err != nil {
		t.Fatal(err)
	}
	dead.waitStarted(t)

	second := on(&seqPuller{digest: "22"})
	got := readyImage(t, second, img.ID)
	if got.Status != images.Ready || !strings.Contains(got.Digest, "2222") {
		t.Fatalf("the image a dead process left checking: %s %q %s, want ready", got.Status, got.Problem, got.Digest)
	}
	// The dead process's pull ends at last; its result is not kept.
	close(dead.gate)
	for deadline := time.Now().Add(500 * time.Millisecond); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if now, err := second.Images.Get(tenant, img.ID); err != nil || now.Status != images.Ready || now.Digest != got.Digest {
			t.Fatalf("the dead process's check replaced the one that took over: %+v %v", now, err)
		}
	}
}

// The same for a ready image a dead process was checking again: it was
// ready throughout, and its check again is finished by another process.
func TestACheckAgainStrandedByADeadProcessIsStartedAgain(t *testing.T) {
	t.Cleanup(platform.SetImageBeats(time.Hour, 300*time.Millisecond, 50*time.Millisecond))
	on := processes(t)
	dead := newGatePuller(func(n int, _ string) bool { return n > 1 })
	defer close(dead.gate)
	first := on(dead)
	img, err := first.AddImage(t.Context(), admin, platform.ImageInput{Name: "A", Ref: "ghcr.io/acme/a:1"})
	if err != nil {
		t.Fatal(err)
	}
	readyImage(t, first, img.ID)
	dead.waitStarted(t)
	if _, err := first.ChangeImage(t.Context(), admin, img.ID, platform.ImageChange{Check: true}); err != nil {
		t.Fatal(err)
	}
	dead.waitStarted(t)

	second := on(&seqPuller{digest: "22"})
	got := readyImage(t, second, img.ID)
	if got.Status != images.Ready || !strings.Contains(got.Digest, "2222") || got.Rechecking {
		t.Fatalf("the image a dead process was checking again: %+v", got)
	}
}

// A check that is running on another live process is left alone: it beats,
// so no sweep takes it for stranded however long it runs, and a person's
// Check Again on the other process starts no second pull either.
func TestACheckRunningOnAnotherProcessIsNotStartedTwice(t *testing.T) {
	const stale = time.Second
	t.Cleanup(platform.SetImageBeats(50*time.Millisecond, stale, 50*time.Millisecond))
	on := processes(t)
	slow := newGatePuller(func(int, string) bool { return true })
	first := on(slow)
	other := newGatePuller(func(int, string) bool { return false })
	second := on(other)
	img, err := first.AddImage(t.Context(), admin, platform.ImageInput{Name: "A", Ref: "ghcr.io/acme/a:1"})
	if err != nil {
		t.Fatal(err)
	}
	slow.waitStarted(t)
	// The check runs for several times what makes a silent one stranded.
	for deadline := time.Now().Add(3 * stale); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		got, err := second.ChangeImage(t.Context(), admin, img.ID, platform.ImageChange{Check: true})
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != images.Checking {
			t.Fatalf("a check under way on another process answered %s", got.Status)
		}
	}
	if n := other.n.Load() + slow.n.Load(); n != 1 {
		t.Fatalf("%d pulls of one image's one check, want 1", n)
	}
	close(slow.gate)
	if got := readyImage(t, second, img.ID); got.Status != images.Ready {
		t.Errorf("the check the first process ran: %s %q", got.Status, got.Problem)
	}
	if n := other.n.Load(); n != 0 {
		t.Errorf("the other process pulled %d times", n)
	}
}

// A person may also start a stranded check again themselves: asking for a
// check of an image whose check stopped beating starts one.
func TestAPersonMayCheckAStrandedImageAgain(t *testing.T) {
	// No sweep but the one at start, so only the person's asking starts it.
	t.Cleanup(platform.SetImageBeats(time.Hour, 200*time.Millisecond, time.Hour))
	on := processes(t)
	dead := newGatePuller(func(int, string) bool { return true })
	defer close(dead.gate)
	first := on(dead)
	img, err := first.AddImage(t.Context(), admin, platform.ImageInput{Name: "A", Ref: "ghcr.io/acme/a:1"})
	if err != nil {
		t.Fatal(err)
	}
	dead.waitStarted(t)
	second := on(&seqPuller{digest: "22"})
	time.Sleep(300 * time.Millisecond)
	if _, err := second.ChangeImage(t.Context(), admin, img.ID, platform.ImageChange{Check: true}); err != nil {
		t.Fatal(err)
	}
	if got := readyImage(t, second, img.ID); got.Status != images.Ready || !strings.Contains(got.Digest, "2222") {
		t.Fatalf("a stranded image a person checked again: %s %q", got.Status, got.Problem)
	}
}
