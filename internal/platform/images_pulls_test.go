package platform_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/images"
	"github.com/urmzd/saige/flow/isolation/isolationtest"
)

// heldPuller holds each pull until its reference is let go, and says which
// started, in order.
type heldPuller struct {
	mu      sync.Mutex
	held    map[string]chan struct{}
	started chan string
}

func newHeldPuller() *heldPuller {
	return &heldPuller{held: map[string]chan struct{}{}, started: make(chan string, 100)}
}

func (h *heldPuller) gate(ref string) chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.held[ref] == nil {
		h.held[ref] = make(chan struct{})
	}
	return h.held[ref]
}

func (h *heldPuller) letGo(ref string) {
	gate := h.gate(ref)
	h.mu.Lock()
	defer h.mu.Unlock()
	select {
	case <-gate:
	default:
		close(gate)
	}
}

func (h *heldPuller) Pull(ctx context.Context, ref, _ string) (string, error) {
	h.started <- ref
	select {
	case <-h.gate(ref):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return strings.Split(ref, ":")[0] + "@sha256:" + strings.Repeat("cd", 32), nil
}

func (h *heldPuller) next(t *testing.T) string {
	t.Helper()
	select {
	case ref := <-h.started:
		return ref
	case <-time.After(10 * time.Second):
		t.Fatal("no pull started")
		return ""
	}
}

func (h *heldPuller) none(t *testing.T) {
	t.Helper()
	select {
	case ref := <-h.started:
		t.Fatalf("%s started pulling, and no place was free for it", ref)
	case <-time.After(300 * time.Millisecond):
	}
}

func adminOf(tenant string) auth.Principal {
	return auth.Principal{Subject: "admin-" + tenant, Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Admin}}
}

// One workspace's slow pulls never hold every place: it has at most
// MaxTenantImagePulls, so another workspace's check starts at once. And a
// place that frees goes to the workspace with the fewest pulls under way,
// not to the one that queued the most.
func TestOneWorkspacesPullsLeaveRoomForAnothers(t *testing.T) {
	h := newHeldPuller()
	p := imagesPlatform(t, &isolationtest.Leaser{Dir: t.TempDir()}, h)
	add := func(who auth.Principal, name string) string {
		t.Helper()
		ref := "ghcr.io/" + who.Tenant + "/" + name + ":1"
		if _, err := p.AddImage(t.Context(), who, platform.ImageInput{Name: name, Ref: ref}); err != nil {
			t.Fatal(err)
		}
		return ref
	}
	// acme asks for five; two start.
	var acme []string
	for i := range 5 {
		acme = append(acme, add(admin, fmt.Sprint("slow", i)))
	}
	running := map[string]bool{h.next(t): true, h.next(t): true}
	h.none(t)
	// globex's starts at once, in the place acme may not have.
	globex := add(adminOf("globex"), "quick")
	if got := h.next(t); got != globex {
		t.Fatalf("%s started, want globex's %s", got, globex)
	}
	// Every place is taken. initech waits behind three of acme's that
	// queued before it, and has the next place all the same.
	initech := add(adminOf("initech"), "late")
	h.none(t)
	for ref := range running {
		h.letGo(ref)
		break
	}
	if got := h.next(t); got != initech {
		t.Fatalf("%s took the freed place, want initech's %s", got, initech)
	}
	for _, ref := range append(acme, globex, initech) {
		h.letGo(ref)
	}
}

// A check's time starts when it has a place to pull in. One that waited
// for others' slow pulls still pulls, and is never failed unpulled.
func TestACheckIsNotTimedWhileItWaitsForAPlace(t *testing.T) {
	t.Cleanup(platform.SetImageCheckTimeout(400 * time.Millisecond))
	g := newGatePuller(func(int, string) bool { return true }) // every pull runs out its time
	defer close(g.gate)
	p := imagesPlatform(t, &isolationtest.Leaser{Dir: t.TempDir()}, g)
	const n = 3 * platform.MaxTenantImagePulls
	var ids []string
	for i := range n {
		img, err := p.AddImage(t.Context(), admin, platform.ImageInput{Name: fmt.Sprint("slow", i), Ref: fmt.Sprintf("ghcr.io/acme/slow%d:1", i)})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, img.ID)
	}
	for _, id := range ids {
		got := readyImage(t, p, id)
		if got.Status != images.Failed || !strings.Contains(got.Problem, "took longer than") {
			t.Errorf("a pull that ran out its time: %s %q", got.Status, got.Problem)
		}
	}
	if got := g.n.Load(); got != n {
		t.Errorf("%d of %d checks pulled; the rest were failed for the time they waited", got, n)
	}
}
