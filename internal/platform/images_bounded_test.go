package platform_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/saige/flow/isolation"
	"github.com/urmzd/saige/flow/isolation/isolationtest"
)

// imageReads counts what is read of the images' namespace: every record of
// it at once (loads), and each workspace's record.
type imageReads struct {
	state.Store
	mu      sync.Mutex
	loads   int
	lookups map[string]int
}

func (r *imageReads) Load(ctx context.Context, namespace string) ([]state.Record, error) {
	if namespace == "sandbox-images" {
		r.mu.Lock()
		r.loads++
		r.mu.Unlock()
	}
	return r.Store.Load(ctx, namespace)
}

func (r *imageReads) Lookup(ctx context.Context, namespace, tenant, key string) (state.Record, error) {
	if namespace == "sandbox-images" && tenant != "" {
		r.mu.Lock()
		r.lookups[tenant]++
		r.mu.Unlock()
	}
	return r.Store.Lookup(ctx, namespace, tenant, key)
}

// reset forgets what was counted, and returns it.
func (r *imageReads) reset() (loads int, lookups map[string]int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	loads, lookups = r.loads, r.lookups
	r.loads, r.lookups = 0, map[string]int{}
	return loads, lookups
}

// Nothing the platform does with images while it runs reads every
// workspace's record at once. Its sweep pages the workspaces that have
// images and reads one record each, and a change to one workspace's warm
// images reads that workspace's record and no other's.
func TestTheImagesSweepAndWarmChangesAreBoundedReads(t *testing.T) {
	reads := &imageReads{Store: state.NewMemory(), lookups: map[string]int{}}
	leaser := &warmLeaser{Leaser: &isolationtest.Leaser{Dir: t.TempDir()}}
	p := startWith(t, platform.Config{
		Backends:         platform.Backends{Shared: reads},
		Sandbox:          isolation.Sandbox{Leaser: leaser, Name: "test"},
		SandboxContained: true,
		SandboxImages:    &platform.ImageConfig{Registries: []string{"ghcr.io"}, Puller: &pinPuller{}},
	})
	ids := map[string]string{}
	digest := map[string]string{}
	for _, ws := range []string{"acme", "globex", "initech"} {
		img, err := p.AddImage(t.Context(), adminOf(ws), platform.ImageInput{Name: "A", Ref: "ghcr.io/" + ws + "/a:1", Warm: true})
		if err != nil {
			t.Fatal(err)
		}
		ids[ws] = img.ID
		for deadline := time.Now().Add(10 * time.Second); digest[ws] == "" && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if got, err := p.Images.Get(ws, img.ID); err == nil {
				digest[ws] = got.Digest
			}
		}
	}
	leaser.keeps(t, "with three workspaces' images warm", digest["acme"], digest["globex"], digest["initech"])
	if loads, _ := reads.reset(); loads > 1 {
		t.Errorf("every workspace's images were read at once %d times; only the directory's first fill may", loads)
	}

	off := false
	if _, err := p.ChangeImage(t.Context(), adminOf("globex"), ids["globex"], platform.ImageChange{Warm: &off}); err != nil {
		t.Fatal(err)
	}
	leaser.keeps(t, "with one workspace's image no longer warm", digest["acme"], digest["initech"])
	loads, lookups := reads.reset()
	if loads != 0 || lookups["acme"] != 0 || lookups["initech"] != 0 || lookups["globex"] == 0 {
		t.Errorf("a change to one workspace's warm image read: all at once %d times, each workspace %v; want only its own", loads, lookups)
	}

	platform.SweepImages(p)
	leaser.keeps(t, "after a sweep", digest["acme"], digest["initech"])
	loads, lookups = reads.reset()
	if loads != 0 {
		t.Errorf("a sweep read every workspace's images at once %d times", loads)
	}
	for ws, n := range lookups {
		// Once looking for stranded checks, once for what to keep warm.
		if n > 2 {
			t.Errorf("a sweep read %s's images %d times, want at most 2", ws, n)
		}
	}
	if len(lookups) != 3 {
		t.Errorf("a sweep read %v, want each of the three workspaces", lookups)
	}

	second := startWith(t, platform.Config{
		Backends:         platform.Backends{Shared: reads},
		Sandbox:          isolation.Sandbox{Leaser: &isolationtest.Leaser{Dir: t.TempDir()}, Name: "test"},
		SandboxContained: true,
		SandboxImages:    &platform.ImageConfig{Registries: []string{"ghcr.io"}, Puller: &pinPuller{}},
	})
	second.PullReadyImages(t.Context())
	if loads, _ := reads.reset(); loads != 0 {
		t.Errorf("a process that starts read every workspace's images at once %d times", loads)
	}
}
