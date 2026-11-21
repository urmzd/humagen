package platform

// The images a session's tools may run in.
//
// Every session runs in an image: the deployment's default, which everyone
// has, or one its workspace added (pkg/images). Adding one starts a check:
// the image is pulled onto the sandbox host, pinned to the digest it
// resolved to, and started once to prove a run's shell works in it. Only a
// ready image is offered, and a session runs its digest, so every host runs
// the same bytes. When the platform starts, every ready image is pulled
// again by digest, so a new sandbox host has them before a session asks.
//
// The terminal a person opens in a session runs in the session's image too
// (person_command.go). The session's browser is a tool server reached over
// HTTP (browser_view.go), not part of the image.

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/saige/flow/isolation"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/images"
	"github.com/urmzd/humagen/pkg/threads"
)

// Bounds on an image's check.
const (
	// ImageCheckTimeout bounds one image's pull and first start.
	ImageCheckTimeout = 10 * time.Minute
	// imageProbeTimeout bounds the command the check runs in the image.
	imageProbeTimeout = 30 * time.Second
	// MaxImageCredential bounds a pull credential, in bytes.
	MaxImageCredential = 4 << 10
	// MaxImagePulls bounds the pulls this process runs at once, for every
	// workspace: they share the sandbox host's Docker daemon.
	MaxImagePulls = 3
	// MaxTenantImagePulls bounds how many of them one workspace has, so
	// one workspace's slow pulls never hold every place.
	MaxTenantImagePulls = MaxImagePulls - 1
)

// imageRepullTimeout bounds one pull of a ready image at start, so a
// registry that hangs holds up no other image. imageCheckTimeout is
// ImageCheckTimeout, which a test shortens.
var (
	imageRepullTimeout = 5 * time.Minute
	imageCheckTimeout  = ImageCheckTimeout
)

// A check under way says so on the image every imageBeatEvery (its beat).
// One silent for imageBeatStale has no process left, and every process
// looks for those each imageSweepEvery and starts the check again. Stale is
// several beats, so a slow write is not a dead process; the clocks of two
// processes on one database may differ by less than that.
var (
	imageBeatEvery  = 20 * time.Second
	imageBeatStale  = 90 * time.Second
	imageSweepEvery = time.Minute
)

// stranded reports a check under way that no process is running any more.
func stranded(img images.Image) bool {
	return img.BeingChecked() && time.Since(img.Beat) > imageBeatStale
}

// ImagePuller fetches images onto the sandbox host.
type ImagePuller interface {
	// Pull fetches ref, with credential when it is not empty, and returns
	// it pinned as repo@sha256:..., the digest the host now holds.
	Pull(ctx context.Context, ref, credential string) (digest string, err error)
}

// ImageConfig lets a deployment's workspaces add images of their own.
type ImageConfig struct {
	// Default is the deployment's own image, as it is shown.
	Default string
	// Registries are the hosts an image may come from ("ghcr.io"). Empty
	// allows none: workspaces have only the default.
	Registries []string
	// Puller fetches images. Nil allows none.
	Puller ImagePuller
}

// ErrImages reports a deployment that takes no images of a workspace's own.
var ErrImages = errors.New("platform: this deployment runs only its default image")

// ImageInput is what a person sends to add an image.
type ImageInput struct {
	Name string `json:"name"`
	Ref  string `json:"ref"`
	// Credential is the registry's pull secret ("user:token"), kept apart
	// from the image and never shown again. Empty pulls anonymously.
	Credential string `json:"credential,omitempty"`
	Warm       bool   `json:"warm,omitempty"`
}

// ImageChange is what a person may change of an image after adding it.
type ImageChange struct {
	Warm *bool `json:"warm,omitempty"`
	// Check pulls and checks it again: to take a newer push of its tag,
	// or after a failure.
	Check bool `json:"check,omitempty"`
}

// SessionImage is one image a session may run in, as SessionOptions offers it.
type SessionImage struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Ref  string `json:"ref"`
	// Digest is Ref pinned, once its check resolved it.
	Digest  string        `json:"digest,omitempty"`
	Status  images.Status `json:"status"`
	Problem string        `json:"problem,omitempty"`
	Warm    bool          `json:"warm"`
	Default bool          `json:"default"`
}

// imagesOn reports whether a workspace may add images here: the sandbox
// starts instances, a puller fetches images, and some registry is allowed.
func (p *Platform) imagesOn() bool {
	c := p.cfg.SandboxImages
	return p.cfg.Sandbox.Isolated() && c != nil && c.Puller != nil && len(c.Registries) > 0
}

// ImageRegistries are the registries a workspace may add images from here;
// empty when it may add none.
func (p *Platform) ImageRegistries() []string {
	if !p.imagesOn() {
		return []string{}
	}
	return append([]string(nil), p.cfg.SandboxImages.Registries...)
}

func (p *Platform) defaultImage() SessionImage {
	ref := ""
	if c := p.cfg.SandboxImages; c != nil {
		ref = c.Default
	}
	return SessionImage{ID: images.Default, Name: "Default", Ref: ref, Status: images.Ready, Warm: true, Default: true}
}

// SessionImages are the images the caller's workspace may start a session
// in: the default first, then its own, oldest first, with those not ready
// marked so.
func (p *Platform) SessionImages(who auth.Principal) ([]SessionImage, error) {
	out := []SessionImage{p.defaultImage()}
	if !p.imagesOn() {
		return out, nil
	}
	own, err := p.Images.List(who.Tenant)
	if err != nil {
		return nil, err
	}
	for _, img := range own {
		out = append(out, SessionImage{ID: img.ID, Name: img.Name, Ref: img.Ref, Digest: img.Digest, Status: img.Status, Problem: img.Problem, Warm: img.Warm})
	}
	return out, nil
}

// ListImages returns the workspace's own images.
func (p *Platform) ListImages(who auth.Principal) ([]images.Image, error) {
	if !p.imagesOn() {
		return []images.Image{}, nil
	}
	imgs, err := p.Images.List(who.Tenant)
	for i := range imgs {
		imgs[i] = imgs[i].Shown()
	}
	return imgs, err
}

// imageOwner is who an image's pull credential is kept under in the
// secrets store: a client's id and a person's owner hold no "image/".
func imageOwner(id string) string { return "image/" + id }

// AddImage keeps a new image for the caller's workspace and starts its
// check in the background. It answers at once, checking.
func (p *Platform) AddImage(ctx context.Context, who auth.Principal, in ImageInput) (images.Image, error) {
	if !p.imagesOn() {
		return images.Image{}, ErrImages
	}
	in.Ref = strings.TrimSpace(in.Ref)
	if err := images.CheckRef(in.Ref, p.cfg.SandboxImages.Registries); err != nil {
		return images.Image{}, err
	}
	if len(in.Credential) > MaxImageCredential || strings.ContainsAny(in.Credential, "\r\n\x00") {
		return images.Image{}, fmt.Errorf("%w: a credential is one line of at most %d bytes", images.ErrInvalid, MaxImageCredential)
	}
	img := images.Image{
		ID: images.NewID(), Name: in.Name, Ref: in.Ref, Warm: in.Warm,
		Credential: in.Credential != "", CreatedBy: who.Subject, CreatedAt: time.Now().UTC(),
	}
	if img.Credential {
		if err := p.secrets.Put(who.Tenant, imageOwner(img.ID), "pull", in.Credential); err != nil {
			return images.Image{}, err
		}
	}
	saved, err := p.Images.Create(who.Tenant, img)
	if err != nil {
		if img.Credential {
			_ = p.secrets.Drop(who.Tenant, imageOwner(img.ID))
		}
		return images.Image{}, err
	}
	if p.imageChecks.claim(who.Tenant, saved.ID) {
		p.checkImageLater(who.Tenant, saved.ID, saved.Checks)
	}
	return saved.Shown(), nil
}

// ChangeImage keeps an image warm or not, or checks it again. A check
// asked for while one is under way, in this process or another, starts
// none: the image is answered as it stands, checking. One asked for while
// the image's check is stranded (its process died) starts it again. A ready
// image stays ready while it is checked again, on the digest it has: its
// sessions go on running.
func (p *Platform) ChangeImage(_ context.Context, who auth.Principal, id string, in ImageChange) (images.Image, error) {
	if !p.imagesOn() {
		// Nothing is kept where nothing may be added.
		return images.Image{}, images.ErrNotFound
	}
	claimed := in.Check && p.imageChecks.claim(who.Tenant, id)
	check := false
	img, err := p.Images.Update(who.Tenant, id, func(img images.Image) images.Image {
		if in.Warm != nil {
			img.Warm = *in.Warm
		}
		// Another process's check that still beats is left to finish.
		check = claimed && (!img.BeingChecked() || stranded(img))
		if check {
			img.Beat = time.Now().UTC()
			if img.Status == images.Ready && img.Digest != "" {
				img.Rechecking = true
			} else {
				img.Status = images.Checking
			}
			img.Problem = ""
			img.Checks++
		}
		return img
	})
	if claimed && (err != nil || !check) {
		p.imageChecks.release(who.Tenant, id)
	}
	if err != nil {
		return images.Image{}, err
	}
	switch {
	case check:
		p.checkImageLater(who.Tenant, id, img.Checks)
	case img.Warm && img.Status == images.Ready:
		p.warmImage(who.Tenant, img)
	}
	return img.Shown(), nil
}

// RemoveImage forgets an image and its credential. A session started in it
// is refused a sandbox from then on: its image is gone. Removing it, and
// the deployment no longer allowing its registry, are the only ways a
// session loses an image that once passed its check.
func (p *Platform) RemoveImage(_ context.Context, who auth.Principal, id string) error {
	if !p.imagesOn() {
		return images.ErrNotFound
	}
	if err := p.Images.Delete(who.Tenant, id); err != nil {
		return err
	}
	return p.secrets.Drop(who.Tenant, imageOwner(id))
}

// imageChecks are the image checks under way in this process: at most one
// per image, at most MaxImagePulls pulling at once and MaxTenantImagePulls
// of one workspace, and all of them ended when the platform stops.
type imageChecks struct {
	mu      sync.Mutex
	ctx     context.Context // every check's; cancelled by stop
	cancel  context.CancelFunc
	stopped bool
	running map[string]bool // tenant and image id
	pulling map[string]int  // the pulls under way, by tenant
	pulls   int             // all of them
	waiting []*pullWaiter   // the pulls waiting for a place, oldest first
	wg      sync.WaitGroup
}

// pullWaiter is one pull waiting for a place; ready closes when it has one.
type pullWaiter struct {
	tenant string
	ready  chan struct{}
}

func (c *imageChecks) initLocked() {
	if c.ctx == nil {
		c.ctx, c.cancel = context.WithCancel(context.Background())
		c.running = map[string]bool{}
		c.pulling = map[string]int{}
	}
}

// claim reports whether a check of the image may start: none is under way
// and the platform has not stopped. The caller then releases it.
func (c *imageChecks) claim(tenant, id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.initLocked()
	key := tenant + "\x00" + id
	if c.stopped || c.running[key] {
		return false
	}
	c.running[key] = true
	c.wg.Add(1)
	return true
}

func (c *imageChecks) release(tenant, id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.running, tenant+"\x00"+id)
	c.wg.Done()
}

// context is the one every check runs under.
func (c *imageChecks) context() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.initLocked()
	return c.ctx
}

// pull waits for a place to pull in, and returns what gives it back. A
// place that frees goes to the waiting workspace with the fewest pulls
// under way, the longest waiting first among equals, and never to one that
// already has MaxTenantImagePulls: a workspace with many slow pulls waits
// behind its own, not in front of everyone else's.
func (c *imageChecks) pull(ctx context.Context, tenant string) (func(), error) {
	w := &pullWaiter{tenant: tenant, ready: make(chan struct{})}
	c.mu.Lock()
	c.initLocked()
	c.waiting = append(c.waiting, w)
	c.placeLocked()
	c.mu.Unlock()
	done := sync.OnceFunc(func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.pulling[tenant]--
		if c.pulling[tenant] == 0 {
			delete(c.pulling, tenant)
		}
		c.pulls--
		c.placeLocked()
	})
	select {
	case <-w.ready:
		return done, nil
	case <-ctx.Done():
	}
	c.mu.Lock()
	i := slices.Index(c.waiting, w)
	if i >= 0 {
		c.waiting = slices.Delete(c.waiting, i, i+1)
	}
	c.mu.Unlock()
	if i < 0 {
		done() // it was given a place as ctx ended
	}
	return nil, ctx.Err()
}

// placeLocked gives the free places to those waiting.
func (c *imageChecks) placeLocked() {
	for c.pulls < MaxImagePulls {
		next := -1
		for i, w := range c.waiting {
			if c.pulling[w.tenant] < MaxTenantImagePulls && (next < 0 || c.pulling[w.tenant] < c.pulling[c.waiting[next].tenant]) {
				next = i
			}
		}
		if next < 0 {
			return
		}
		w := c.waiting[next]
		c.waiting = slices.Delete(c.waiting, next, next+1)
		c.pulling[w.tenant]++
		c.pulls++
		close(w.ready)
	}
}

// stop ends every check under way and waits for them.
func (c *imageChecks) stop() {
	c.mu.Lock()
	c.initLocked()
	c.stopped = true
	c.cancel()
	c.mu.Unlock()
	c.wg.Wait()
}

// checkImageLater runs check n of an image in the background, past the
// request that asked for it. Its claim is the caller's, and is released
// when it ends.
func (p *Platform) checkImageLater(tenant, id string, n int64) {
	go func() {
		defer p.imageChecks.release(tenant, id)
		p.checkImage(p.imageChecks.context(), tenant, id, n)
	}()
}

// errStopped is a check the server stopped before it finished.
var errStopped = errors.New("the server stopped before the check finished; check it again")

// checkImage pulls an image, pins it and starts it once, and keeps what
// came of it while it is the image's latest check, n. Its ImageCheckTimeout
// starts when it has a place to pull in, not while it waits for one: a
// check is never failed for the time other pulls took.
func (p *Platform) checkImage(ctx context.Context, tenant, id string, n int64) {
	img, err := p.Images.Get(tenant, id)
	if err != nil || img.Checks != n {
		return
	}
	ctx, superseded := context.WithCancel(ctx)
	defer superseded()
	defer p.beatImage(ctx, tenant, id, n, superseded)()
	var digest string
	// The registries allowed may have narrowed since it was added. That
	// is the deployment's decision, not a check that went wrong: the image
	// is refused, where any other failure leaves a ready image ready.
	disallowed := false
	if err = images.CheckRef(img.Ref, p.cfg.SandboxImages.Registries); err != nil {
		disallowed = true
		err = fmt.Errorf("this deployment no longer allows it: %s", strings.TrimPrefix(err.Error(), images.ErrInvalid.Error()+": "))
	} else if done, perr := p.imageChecks.pull(ctx, tenant); perr != nil {
		err = perr
	} else {
		timed, cancel := context.WithTimeout(ctx, imageCheckTimeout)
		digest, err = p.probeImage(timed, tenant, img)
		cancel()
		done()
	}
	if err != nil && p.imageChecks.context().Err() != nil {
		err = errStopped
	}
	stale := false
	saved, uerr := p.Images.Update(tenant, id, func(cur images.Image) images.Image {
		if cur.Checks != n {
			// A newer check started; its result is the one kept.
			stale = true
			return cur
		}
		cur.CheckedAt = time.Now().UTC()
		cur.Rechecking = false
		if err != nil {
			// A check that fails never takes a working image away: one
			// that was ready stays ready on the digest its last good
			// check pinned, which the host still holds, and shows why
			// this check failed.
			if cur.Status != images.Ready || cur.Digest == "" || disallowed {
				cur.Status = images.Failed
			}
			cur.Problem = err.Error()
			return cur
		}
		cur.Status, cur.Problem, cur.Digest = images.Ready, "", digest
		return cur
	})
	if uerr != nil || stale {
		// Removed, or checked again, while it was checked.
		return
	}
	if err != nil {
		var cause error
		if f := (*checkFailure)(nil); errors.As(err, &f) {
			cause = f.cause
		}
		slog.Warn("images: check failed", "tenant", tenant, "image", id, "problem", err, "cause", cause)
		return
	}
	if saved.Warm {
		p.warmImage(tenant, saved)
	}
}

// beatImage says on the image, every imageBeatEvery until the returned stop
// is called, that its check n is still under way here, so no other process
// takes it for stranded. When the image has a newer check, or is gone, it
// calls superseded: this check's result would be dropped, so its pull ends.
func (p *Platform) beatImage(ctx context.Context, tenant, id string, n int64, superseded func()) (stop func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		tick := time.NewTicker(imageBeatEvery)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			current := false
			_, err := p.Images.Update(tenant, id, func(cur images.Image) images.Image {
				current = cur.Checks == n && cur.BeingChecked()
				if current {
					cur.Beat = time.Now().UTC()
				}
				return cur
			})
			if errors.Is(err, images.ErrNotFound) || (err == nil && !current) {
				superseded()
				return
			}
		}
	})
	return func() { close(done); wg.Wait() }
}

// adoptStrandedChecks starts again every check that no process is running:
// one whose process died mid-check, on this database's every workspace. The
// check it starts is a new one (the image's count of checks goes up), so
// whatever the dead check's process might still write is dropped, and of
// two processes that find the same stranded check only the first starts it:
// the second finds its beat fresh.
func (p *Platform) adoptStrandedChecks() {
	type found struct{ tenant, id string }
	var all []found
	_ = p.Images.Tenants(func(tenant string, imgs []images.Image) error {
		for _, img := range imgs {
			if stranded(img) {
				all = append(all, found{tenant, img.ID})
			}
		}
		return nil
	})
	for _, f := range all {
		if !p.imageChecks.claim(f.tenant, f.id) {
			continue
		}
		adopted := false
		img, err := p.Images.Update(f.tenant, f.id, func(cur images.Image) images.Image {
			if adopted = stranded(cur); adopted {
				cur.Checks++
				cur.Beat = time.Now().UTC()
			}
			return cur
		})
		if err != nil || !adopted {
			p.imageChecks.release(f.tenant, f.id)
			continue
		}
		slog.Info("images: a check no process was running starts again", "tenant", f.tenant, "image", f.id)
		p.checkImageLater(f.tenant, f.id, img.Checks)
	}
}

// sweepImages looks, when the platform starts and every imageSweepEvery
// after, for what a process that died left behind.
func (p *Platform) sweepImages(ctx context.Context) error {
	tick := time.NewTicker(imageSweepEvery)
	defer tick.Stop()
	for {
		p.adoptStrandedChecks()
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// checkFailure is why a check failed: problem is what the image's readers
// are shown, and cause, when there is one, is what the sandbox host said,
// which only the server's log gets. A host's words name the host: its
// Docker address, its paths, the registry's answer in full.
type checkFailure struct {
	problem string
	cause   error
}

func (f *checkFailure) Error() string { return f.problem }

// probeImage pulls img and runs the command every run's shell relies on in
// it, as the sandbox's non-root user. It returns the pinned digest, or a
// *checkFailure.
func (p *Platform) probeImage(ctx context.Context, tenant string, img images.Image) (string, error) {
	cred, _ := p.secrets.Get(tenant, imageOwner(img.ID), "pull")
	fail := func(problem string, cause error) (string, error) {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			problem = fmt.Sprintf("checking %s took longer than %s", img.Ref, imageCheckTimeout)
		}
		return "", &checkFailure{problem: problem, cause: errors.New(redact(cause.Error(), cred))}
	}
	digest, err := p.cfg.SandboxImages.Puller.Pull(ctx, img.Ref, cred)
	if err != nil {
		return fail(pullProblem(img.Ref, img.Credential, err), err)
	}
	inst, err := p.cfg.Sandbox.Leaser.Lease(ctx, isolation.Request{Tenant: tenant, Run: "image-check-" + img.ID, Image: digest, Egress: []string{}})
	if err != nil {
		return fail("it was pulled, and the sandbox could not start it", err)
	}
	defer func() { _ = inst.Close(context.WithoutCancel(ctx)) }()
	st, err := inst.Exec(ctx, isolation.Command{Argv: []string{"sh", "-c", "command -v sleep >/dev/null"}, Timeout: imageProbeTimeout}, isolation.Stdio{})
	if err != nil {
		return fail("sh could not be run in it; an image needs sh and sleep", err)
	}
	if st.Code != 0 {
		return "", &checkFailure{problem: "it has no sleep on the shell's path; an image needs sh and sleep"}
	}
	return digest, nil
}

// redact takes a pull credential ("user:token") out of s however it is
// written there: whole, its token alone, or in base64 as Docker keeps it.
func redact(s, credential string) string {
	if credential == "" {
		return s
	}
	hide := []string{
		credential,
		base64.StdEncoding.EncodeToString([]byte(credential)),
		base64.URLEncoding.EncodeToString([]byte(credential)),
		base64.RawStdEncoding.EncodeToString([]byte(credential)),
		base64.RawURLEncoding.EncodeToString([]byte(credential)),
	}
	if _, token, ok := strings.Cut(credential, ":"); ok && token != "" {
		hide = append(hide, token)
	}
	for _, h := range hide {
		s = strings.ReplaceAll(s, h, "[credential]")
	}
	return s
}

// pullProblem says why a pull failed, to everyone who may read the image:
// one of a few reasons, worded from the image's own reference and never
// from what the host printed.
func pullProblem(ref string, credential bool, err error) string {
	msg := strings.ToLower(err.Error())
	has := func(parts ...string) bool {
		return slices.ContainsFunc(parts, func(part string) bool { return strings.Contains(msg, part) })
	}
	switch {
	case has("docker api", "docker daemon", "cannot connect"):
		// The sandbox host's own Docker could not be reached.
	case has("unauthorized", "authentication required", "denied", "forbidden", "not found", "manifest unknown", "name unknown"):
		if credential {
			return fmt.Sprintf("the registry has no %s that this credential can read", ref)
		}
		return fmt.Sprintf("the registry has no %s, or it is private and needs a pull credential", ref)
	case has("no matching manifest", "platform"):
		return fmt.Sprintf("%s has no build for the sandbox host's platform", ref)
	case has("no such host", "dial tcp", "i/o timeout", "handshake", "connection refused", "connection reset", "x509", "network is unreachable", "no route to host", "server misbehaving"):
		return fmt.Sprintf("the sandbox host could not reach %s", images.Registry(ref))
	case errors.Is(err, context.DeadlineExceeded), has("deadline", "timeout", "timed out"):
		return fmt.Sprintf("pulling %s took too long", ref)
	}
	return fmt.Sprintf("the sandbox host could not pull %s; the server's log says why", ref)
}

// warmer is a Leaser that can start an image before a run asks for it, as
// saige's pool leaser can.
type warmer interface {
	Warm(isolation.Request) error
}

func (p *Platform) warmImage(tenant string, img images.Image) {
	w, ok := p.cfg.Sandbox.Leaser.(warmer)
	if !ok || img.Digest == "" {
		return
	}
	if err := w.Warm(isolation.Request{Tenant: tenant, Image: img.Digest}); err != nil {
		slog.Warn("images: could not keep warm", "tenant", tenant, "image", img.ID, "error", err)
	}
}

// PullReadyImages fetches every workspace's ready image onto the sandbox
// host by digest, and warms those kept warm. The platform calls it when it
// starts, so a host that has never run an image has it before a session
// asks; a failure is logged and the image stays ready for the next host.
// Each pull has imageRepullTimeout, and an image is warmed only if it is
// still ready and kept warm once it is pulled.
func (p *Platform) PullReadyImages(ctx context.Context) {
	if !p.imagesOn() {
		return
	}
	_ = p.Images.Tenants(func(tenant string, imgs []images.Image) error {
		for _, img := range imgs {
			if img.Status != images.Ready || img.Digest == "" {
				continue
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := p.repull(ctx, tenant, img); err != nil {
				slog.Warn("images: could not pull a ready image", "tenant", tenant, "image", img.ID, "error", err)
				continue
			}
			cur, err := p.Images.Get(tenant, img.ID)
			if err == nil && cur.Warm && cur.Status == images.Ready && cur.Digest == img.Digest {
				p.warmImage(tenant, cur)
			}
		}
		return nil
	})
}

// repull pulls a ready image by its digest, within imageRepullTimeout of
// having a place to pull in.
func (p *Platform) repull(ctx context.Context, tenant string, img images.Image) error {
	done, err := p.imageChecks.pull(ctx, tenant)
	if err != nil {
		return err
	}
	defer done()
	ctx, cancel := context.WithTimeout(ctx, imageRepullTimeout)
	defer cancel()
	cred, _ := p.secrets.Get(tenant, imageOwner(img.ID), "pull")
	if _, err = p.cfg.SandboxImages.Puller.Pull(ctx, img.Digest, cred); err != nil {
		return errors.New(redact(err.Error(), cred))
	}
	return nil
}

// ErrImageGone reports a session whose image was removed, or is one the
// deployment no longer allows. An image being checked again, or whose
// latest check failed, is not gone: it runs the digest it had.
var ErrImageGone = errors.New("platform: this session's image is not available")

// imageOf is the digest a session's runs lease: empty for the default
// image, a conversation that is not a session, or a session that chose none.
func (p *Platform) imageOf(tenant, thread string) (string, error) {
	if thread == "" {
		return "", nil
	}
	th, err := p.Threads.Get(tenant, thread)
	if errors.Is(err, threads.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if th.Session == nil || th.Session.Image == "" || th.Session.Image == images.Default {
		return "", nil
	}
	img, err := p.Images.Get(tenant, th.Session.Image)
	if err != nil || img.Status != images.Ready || img.Digest == "" || !p.imagesOn() {
		return "", fmt.Errorf("%w: %s", ErrImageGone, th.Session.Image)
	}
	return img.Digest, nil
}

// leaserFor is the sandbox's Leaser for a request, or one that refuses
// with err when the request's image cannot be had.
func (p *Platform) leaserFor(err error) isolation.Leaser {
	if err == nil {
		return p.cfg.Sandbox.Leaser
	}
	return isolation.LeaserFunc(func(context.Context, isolation.Request) (isolation.Instance, error) {
		return nil, err
	})
}
