package platform_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/images"
	"github.com/urmzd/saige/flow/isolation/isolationtest"
)

// Checking a ready image again never takes it away. While the check runs,
// and after one that fails, the image is ready on the digest its last good
// check pinned: a session in it keeps its sandbox and a new one may start.
// The failure is shown. A check that passes moves it to the new digest.
// Only an image whose first check never passed refuses a session.
func TestACheckAgainNeverTakesAWorkingImageAway(t *testing.T) {
	digest := func(hex string) string { return "ghcr.io/acme/agent@sha256:" + strings.Repeat(hex, 32) }
	type answer struct {
		digest string
		err    error
	}
	answers := make(chan answer)
	started := make(chan struct{}, 10)
	leaser := &isolationtest.Leaser{Dir: t.TempDir()}
	p := imagesPlatform(t, leaser, pullFunc(func(ctx context.Context, ref, _ string) (string, error) {
		if strings.Contains(ref, "never") {
			return "", errors.New("docker pull: manifest unknown")
		}
		started <- struct{}{}
		select {
		case a := <-answers:
			return a.digest, a.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}))
	wait := func() {
		t.Helper()
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("no pull started")
		}
	}
	img, err := p.AddImage(t.Context(), admin, platform.ImageInput{Name: "Agent", Ref: "ghcr.io/acme/agent:2"})
	if err != nil {
		t.Fatal(err)
	}
	wait()
	answers <- answer{digest: digest("11")}
	if img = readyImage(t, p, img.ID); img.Status != images.Ready || img.Digest != digest("11") {
		t.Fatalf("the first check: %+v", img)
	}
	session, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "Build it", Image: img.ID})
	if err != nil {
		t.Fatal(err)
	}
	// runs is the image the session's next command leased.
	runs := func(when string) string {
		t.Helper()
		for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(50 * time.Millisecond) {
			_, err := p.RunPersonCommand(t.Context(), admin, session.Thread.ID, platform.PersonCommand{Command: "true"})
			if err == nil {
				all := leaser.Instances()
				return all[len(all)-1].Request().Image
			}
			if !strings.Contains(err.Error(), "wait for active") || time.Now().After(deadline) {
				t.Fatalf("%s, the session's command: %v", when, err)
			}
		}
	}
	if got := runs("before any check again"); got != digest("11") {
		t.Fatalf("the session ran %q", got)
	}
	again := func() {
		t.Helper()
		got, err := p.ChangeImage(t.Context(), admin, img.ID, platform.ImageChange{Check: true})
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != images.Ready || !got.Rechecking || got.Digest != digest("11") {
			t.Fatalf("an image being checked again: %s, rechecking %v, digest %s; want ready on the digest it had", got.Status, got.Rechecking, got.Digest)
		}
		wait()
	}

	again()
	if got := runs("while it is checked again"); got != digest("11") {
		t.Errorf("while it is checked again the session ran %q", got)
	}
	if _, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "Another", Image: img.ID}); err != nil {
		t.Errorf("a new session while it is checked again: %v", err)
	}

	answers <- answer{err: errors.New("docker pull: Get \"https://ghcr.io/v2/\": dial tcp: lookup ghcr.io: no such host")}
	failed := readyImage(t, p, img.ID)
	if failed.Status != images.Ready || failed.Digest != digest("11") || failed.Problem != "the sandbox host could not reach ghcr.io" {
		t.Errorf("after a check again that failed: %s %s %q; want ready on the digest it had, with the reason", failed.Status, failed.Digest, failed.Problem)
	}
	if got := runs("after a check again that failed"); got != digest("11") {
		t.Errorf("after a check again that failed the session ran %q", got)
	}
	if _, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "A third", Image: img.ID}); err != nil {
		t.Errorf("a new session after a check again that failed: %v", err)
	}

	again()
	answers <- answer{digest: digest("22")}
	if passed := readyImage(t, p, img.ID); passed.Status != images.Ready || passed.Digest != digest("22") || passed.Problem != "" {
		t.Errorf("after a check again that passed: %+v", passed)
	}
	if got := runs("after a check again that passed"); got != digest("22") {
		t.Errorf("after a check again that passed the session ran %q, want the new digest", got)
	}

	// An image whose first check never passed has nothing to run.
	never, err := p.AddImage(t.Context(), admin, platform.ImageInput{Name: "Never", Ref: "ghcr.io/acme/never:1"})
	if err != nil {
		t.Fatal(err)
	}
	if never = readyImage(t, p, never.ID); never.Status != images.Failed {
		t.Fatalf("an image whose first check failed: %+v", never)
	}
	if _, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "No", Image: never.ID}); err == nil {
		t.Error("a session started in an image that never passed its check")
	}
	if got, err := p.ChangeImage(t.Context(), admin, never.ID, platform.ImageChange{Check: true}); err != nil || got.Rechecking {
		t.Errorf("checking a failed image again: %+v %v; it has no digest to stay ready on", got, err)
	}
}
