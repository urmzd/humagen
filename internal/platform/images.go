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
	"errors"
	"fmt"
	"log/slog"
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
)

// imageRepullTimeout bounds one pull of a ready image at start, so a
// registry that hangs holds up no other image.
var imageRepullTimeout = 5 * time.Minute

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
// asked for while one is under way starts none: the image is answered as
// it stands, checking.
func (p *Platform) ChangeImage(_ context.Context, who auth.Principal, id string, in ImageChange) (images.Image, error) {
	if !p.imagesOn() {
		// Nothing is kept where nothing may be added.
		return images.Image{}, images.ErrNotFound
	}
	check := in.Check && p.imageChecks.claim(who.Tenant, id)
	img, err := p.Images.Update(who.Tenant, id, func(img images.Image) images.Image {
		if in.Warm != nil {
			img.Warm = *in.Warm
		}
		if check {
			img.Status, img.Problem = images.Checking, ""
			img.Checks++
		}
		return img
	})
	if err != nil {
		if check {
			p.imageChecks.release(who.Tenant, id)
		}
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
// is refused a sandbox from then on: its image is gone.
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
// per image, at most MaxImagePulls pulling at once, and all of them ended
// when the platform stops.
type imageChecks struct {
	mu      sync.Mutex
	ctx     context.Context // every check's; cancelled by stop
	cancel  context.CancelFunc
	stopped bool
	running map[string]bool // tenant and image id
	pulls   chan struct{}
	wg      sync.WaitGroup
}

func (c *imageChecks) initLocked() {
	if c.ctx == nil {
		c.ctx, c.cancel = context.WithCancel(context.Background())
		c.running = map[string]bool{}
		c.pulls = make(chan struct{}, MaxImagePulls)
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

// pull waits for room to pull, and returns what gives it back.
func (c *imageChecks) pull(ctx context.Context) (func(), error) {
	c.mu.Lock()
	c.initLocked()
	pulls := c.pulls
	c.mu.Unlock()
	select {
	case pulls <- struct{}{}:
		return func() { <-pulls }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
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
		ctx, cancel := context.WithTimeout(p.imageChecks.context(), ImageCheckTimeout)
		defer cancel()
		p.checkImage(ctx, tenant, id, n)
	}()
}

// errStopped is a check the server stopped before it finished.
var errStopped = errors.New("the server stopped before the check finished; check it again")

// checkImage pulls an image, pins it and starts it once, and keeps what
// came of it while it is the image's latest check, n.
func (p *Platform) checkImage(ctx context.Context, tenant, id string, n int64) {
	img, err := p.Images.Get(tenant, id)
	if err != nil || img.Checks != n {
		return
	}
	var digest string
	// The registries allowed may have narrowed since it was added.
	if err = images.CheckRef(img.Ref, p.cfg.SandboxImages.Registries); err != nil {
		err = fmt.Errorf("this deployment no longer allows it: %s", strings.TrimPrefix(err.Error(), images.ErrInvalid.Error()+": "))
	} else if done, perr := p.imageChecks.pull(ctx); perr != nil {
		err = perr
	} else {
		digest, err = p.probeImage(ctx, tenant, img)
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
		if err != nil {
			cur.Status, cur.Problem = images.Failed, err.Error()
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
		slog.Warn("images: check failed", "tenant", tenant, "image", id, "error", err)
		return
	}
	if saved.Warm {
		p.warmImage(tenant, saved)
	}
}

// probeImage pulls img and runs the command every run's shell relies on in
// it, as the sandbox's non-root user. It returns the pinned digest.
func (p *Platform) probeImage(ctx context.Context, tenant string, img images.Image) (string, error) {
	cred, _ := p.secrets.Get(tenant, imageOwner(img.ID), "pull")
	digest, err := p.cfg.SandboxImages.Puller.Pull(ctx, img.Ref, cred)
	if err != nil {
		return "", pullProblem(img.Ref, img.Credential, err)
	}
	inst, err := p.cfg.Sandbox.Leaser.Lease(ctx, isolation.Request{Tenant: tenant, Run: "image-check-" + img.ID, Image: digest, Egress: []string{}})
	if err != nil {
		return "", fmt.Errorf("could not start it: %w", err)
	}
	defer func() { _ = inst.Close(context.WithoutCancel(ctx)) }()
	st, err := inst.Exec(ctx, isolation.Command{Argv: []string{"sh", "-c", "command -v sleep >/dev/null"}, Timeout: imageProbeTimeout}, isolation.Stdio{})
	if err != nil {
		return "", fmt.Errorf("could not run sh in it: %w", err)
	}
	if st.Code != 0 {
		return "", errors.New("it has no sleep on the shell's path; an image needs sh and sleep")
	}
	return digest, nil
}

// pullProblem says why a pull failed in words a person can act on: the
// registry's own message is kept for what it does not cover.
func pullProblem(ref string, credential bool, err error) error {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "unauthorized"), strings.Contains(msg, "403"), strings.Contains(msg, "401"),
		strings.Contains(msg, "denied"), strings.Contains(msg, "not found"), strings.Contains(msg, "manifest unknown"):
		if credential {
			return fmt.Errorf("the registry has no %s that this credential can read", ref)
		}
		return fmt.Errorf("the registry has no %s, or it is private and needs a pull credential", ref)
	case strings.Contains(msg, "no matching manifest"), strings.Contains(msg, "platform"):
		return fmt.Errorf("%s has no build for the sandbox host's platform", ref)
	case strings.Contains(msg, "deadline"), strings.Contains(msg, "timeout"):
		return fmt.Errorf("pulling %s took too long", ref)
	}
	return fmt.Errorf("could not pull %s: %w", ref, err)
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

// repull pulls a ready image by its digest, within imageRepullTimeout.
func (p *Platform) repull(ctx context.Context, tenant string, img images.Image) error {
	ctx, cancel := context.WithTimeout(ctx, imageRepullTimeout)
	defer cancel()
	done, err := p.imageChecks.pull(ctx)
	if err != nil {
		return err
	}
	defer done()
	cred, _ := p.secrets.Get(tenant, imageOwner(img.ID), "pull")
	_, err = p.cfg.SandboxImages.Puller.Pull(ctx, img.Digest, cred)
	return err
}

// ErrImageGone reports a session whose image was removed or no longer
// passes its check.
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
