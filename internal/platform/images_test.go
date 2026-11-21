package platform_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/images"
	"github.com/urmzd/saige/flow/isolation"
	"github.com/urmzd/saige/flow/isolation/isolationtest"
)

// pinPuller pins a reference to a made-up digest.
type pinPuller struct {
	mu    sync.Mutex
	pulls []string
}

func (p *pinPuller) Pull(_ context.Context, ref, _ string) (string, error) {
	p.mu.Lock()
	p.pulls = append(p.pulls, ref)
	p.mu.Unlock()
	if strings.Contains(ref, "@") {
		return ref, nil
	}
	return strings.Split(ref, ":")[0] + "@sha256:" + strings.Repeat("cd", 32), nil
}

func readyImage(t *testing.T, p *platform.Platform, id string) images.Image {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		imgs, err := p.ListImages(admin)
		if err != nil {
			t.Fatal(err)
		}
		for _, img := range imgs {
			if img.ID == id && img.Status != images.Checking {
				return img
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("image %s is still checking", id)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A session started in a workspace's image leases its digest: the image's
// check leased it first, and a person's command in the session leases it
// again. Once the image is removed the session is refused a sandbox, and a
// conversation that chose none leases the deployment's own.
func TestASessionsSandboxRunsTheImageItChose(t *testing.T) {
	leaser := &isolationtest.Leaser{Dir: t.TempDir()}
	puller := &pinPuller{}
	p := startWith(t, platform.Config{
		Sandbox:          isolation.Sandbox{Leaser: leaser, Name: "test"},
		SandboxContained: true,
		SandboxImages:    &platform.ImageConfig{Default: "python:3.13-alpine", Registries: []string{"ghcr.io"}, Puller: puller},
	})
	img, err := p.AddImage(t.Context(), admin, platform.ImageInput{Name: "Agent", Ref: "ghcr.io/acme/agent:2"})
	if err != nil {
		t.Fatal(err)
	}
	img = readyImage(t, p, img.ID)
	digest := "ghcr.io/acme/agent@sha256:" + strings.Repeat("cd", 32)
	if img.Status != images.Ready || img.Digest != digest {
		t.Fatalf("image = %+v", img)
	}
	if probe := leaser.Instances(); len(probe) != 1 || probe[0].Request().Image != digest || !probe[0].Closed() {
		t.Fatalf("the check leased %d instances", len(probe))
	}

	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "Build it", Image: img.ID})
	if err != nil {
		t.Fatal(err)
	}
	if v.Thread.Session == nil || v.Thread.Session.Image != img.ID {
		t.Fatalf("session = %+v", v.Thread.Session)
	}
	// The session's first run is under way; a command waits for it.
	for deadline := time.Now().Add(20 * time.Second); ; {
		_, err := p.RunPersonCommand(t.Context(), admin, v.Thread.ID, platform.PersonCommand{Command: "true"})
		if err == nil {
			break
		}
		if !strings.Contains(err.Error(), "wait for active") || time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	all := leaser.Instances()
	if last := all[len(all)-1].Request(); last.Image != digest {
		t.Errorf("the command leased %q, want the session's image", last.Image)
	}

	// A conversation with no image leases the deployment's own.
	th := commandThread(t, p)
	if _, err := p.RunPersonCommand(t.Context(), operator, th.ID, platform.PersonCommand{Command: "true"}); err != nil {
		t.Fatal(err)
	}
	all = leaser.Instances()
	if last := all[len(all)-1].Request(); last.Image != "" {
		t.Errorf("a conversation with no image leased %q", last.Image)
	}

	if err := p.RemoveImage(t.Context(), admin, img.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.RunPersonCommand(t.Context(), admin, v.Thread.ID, platform.PersonCommand{Command: "true"}); !errors.Is(err, platform.ErrImageGone) {
		t.Errorf("a removed image: %v, want ErrImageGone", err)
	}
}
