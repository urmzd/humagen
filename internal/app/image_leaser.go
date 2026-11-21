package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/urmzd/saige/flow/isolation"
	poolleaser "github.com/urmzd/saige/flow/isolation/isolate"
	"github.com/urmzd/saige/sandbox/isolate"
)

// imageLeaser is the docker sandbox's Leaser where workspaces run images of
// their own. It keeps a workspace's image warm only while it is told to
// (KeepWarm), which saige's pool alone cannot do: a pool keeps an instance
// warm of every spec it has ever leased, until it is closed, so one pool
// for everything would hold an idle instance of every image any workspace
// ever checked or ran.
//
// So there are three ways an instance is had:
//
//   - The deployment's own image comes from the deployment's pool, as
//     before: warm, and bounded by the pool's Max.
//   - An image kept warm has a pool of its own, started by KeepWarm and
//     closed, with its idle instances, when a later KeepWarm leaves the
//     image out.
//   - Any other image (one not kept warm, and every image's check) is
//     started for the lease and destroyed after it, in a pool of one that
//     is closed as soon as the lease is out. Nothing of it stays.
//
// Instances of workspaces' images out at once are bounded by room, which is
// as large as the deployment pool's Max: a lease past it waits.
type imageLeaser struct {
	*poolleaser.Leaser // the deployment's own image
	driver             isolate.Driver
	cfg                poolleaser.Config
	room               chan struct{}
	out                atomic.Int64 // leases of workspaces' images not yet released

	mu      sync.Mutex
	closed  bool
	kept    map[string]*poolleaser.Leaser // the pools of the images kept warm, by image
	closing sync.WaitGroup
	owners  sync.Map // lease id to the *poolleaser.Leaser that granted its egress
}

var _ isolation.Leaser = (*imageLeaser)(nil)

func newImageLeaser(d isolate.Driver, cfg poolleaser.Config) (*imageLeaser, error) {
	base, err := poolleaser.New(d, cfg)
	if err != nil {
		return nil, err
	}
	room := cfg.Pool.Max
	if room <= 0 {
		room = isolate.DefaultPoolMax
	}
	return &imageLeaser{Leaser: base, driver: d, cfg: cfg, room: make(chan struct{}, room), kept: map[string]*poolleaser.Leaser{}}, nil
}

// own reports whether req asks for the deployment's own image.
func (l *imageLeaser) own(req isolation.Request) bool {
	return req.Image == "" || req.Image == l.cfg.Image
}

// with is the deployment's configuration with a pool of its own.
func (l *imageLeaser) with(pool isolate.PoolConfig) poolleaser.Config {
	cfg := l.cfg
	pool.OnError = l.cfg.Pool.OnError
	cfg.Pool = pool
	return cfg
}

// Lease implements isolation.Leaser.
func (l *imageLeaser) Lease(ctx context.Context, req isolation.Request) (isolation.Instance, error) {
	if l.own(req) {
		return l.Leaser.Lease(ctx, req)
	}
	select {
	case l.room <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("sandbox: waiting for room to start a workspace's image: %w", ctx.Err())
	}
	inst, from, err := l.lease(ctx, req)
	if err != nil {
		<-l.room
		return nil, err
	}
	id := inst.ID()
	l.owners.Store(id, from)
	l.out.Add(1)
	return &imageInstance{Instance: inst, done: func() {
		l.owners.Delete(id)
		l.out.Add(-1)
		<-l.room
	}}, nil
}

// lease has an instance of a workspace's image from the image's own pool
// when it is kept warm, and otherwise starts one that nothing keeps.
func (l *imageLeaser) lease(ctx context.Context, req isolation.Request) (inst isolation.Instance, from *poolleaser.Leaser, err error) {
	l.mu.Lock()
	closed, pool := l.closed, l.kept[req.Image]
	l.mu.Unlock()
	if closed {
		return nil, nil, isolate.ErrClosed
	}
	if pool != nil {
		inst, err := pool.Lease(ctx, req)
		if !errors.Is(err, isolate.ErrClosed) {
			return inst, pool, err
		}
		// It stopped being kept warm as this lease asked.
	}
	// A pool of one: the lease is its only instance, so it starts no
	// other, and closed, it starts none when the lease is released either.
	once, err := poolleaser.New(l.driver, l.with(isolate.PoolConfig{Warm: 1, Max: 1}))
	if err != nil {
		return nil, nil, err
	}
	inst, err = once.Lease(ctx, req)
	_ = once.Close(context.WithoutCancel(ctx))
	return inst, once, err
}

// KeepWarm names every workspace image to keep warm from now on: each has
// instances started before a session asks, and an image kept warm before
// and not named now has its idle instances destroyed. Instances leased from
// it live until their runs release them.
func (l *imageLeaser) KeepWarm(reqs []isolation.Request) {
	want := map[string]bool{}
	for _, req := range reqs {
		if !l.own(req) {
			want[req.Image] = true
		}
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	var drop []*poolleaser.Leaser
	for image, pool := range l.kept {
		if !want[image] {
			delete(l.kept, image)
			drop = append(drop, pool)
		}
	}
	for image := range want {
		if l.kept[image] != nil {
			continue
		}
		// Room bounds the leases; the pool's own Max is past it, so a
		// lease never waits on the image's warm instances.
		warm := l.cfg.Pool.Warm
		if warm <= 0 {
			warm = isolate.DefaultPoolWarm
		}
		pool, err := poolleaser.New(l.driver, l.with(isolate.PoolConfig{Warm: warm, Max: cap(l.room) + 2*warm}))
		if err == nil {
			if err = pool.Warm(isolation.Request{Image: image}); err != nil {
				drop = append(drop, pool)
			}
		}
		if err != nil {
			if l.cfg.Pool.OnError != nil {
				l.cfg.Pool.OnError(fmt.Errorf("sandbox: keeping %s warm: %w", image, err))
			}
			continue
		}
		l.kept[image] = pool
	}
	l.mu.Unlock()
	for _, pool := range drop {
		l.closing.Go(func() { l.shut(pool) })
	}
}

func (l *imageLeaser) shut(pool *poolleaser.Leaser) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := pool.Close(ctx); err != nil && l.cfg.Pool.OnError != nil {
		l.cfg.Pool.OnError(err)
	}
}

// Granted returns the request an egress grant was made for while its lease
// is out, whichever pool the lease came from.
func (l *imageLeaser) Granted(id string) (isolation.Request, bool) {
	if req, ok := l.Leaser.Granted(id); ok {
		return req, true
	}
	if from, ok := l.owners.Load(id); ok {
		return from.(*poolleaser.Leaser).Granted(id)
	}
	return isolation.Request{}, false
}

// Stats is a snapshot of every instance this Leaser holds: the deployment
// pool's, the idle ones of the images kept warm, and every lease of a
// workspace's image that is out.
func (l *imageLeaser) Stats() isolate.PoolStats {
	s := l.Leaser.Stats()
	l.mu.Lock()
	for _, pool := range l.kept {
		k := pool.Stats()
		s.Live, s.Warm, s.Specs = s.Live+k.Live-k.Leased, s.Warm+k.Warm, s.Specs+k.Specs
	}
	l.mu.Unlock()
	n := int(l.out.Load())
	s.Live, s.Leased = s.Live+n, s.Leased+n
	return s
}

// Close stops warming and destroys every warm instance, of the deployment's
// image and of each image kept warm. Leases still out are destroyed when
// their runs release them.
func (l *imageLeaser) Close(ctx context.Context) error {
	l.mu.Lock()
	l.closed = true
	kept := l.kept
	l.kept = map[string]*poolleaser.Leaser{}
	l.mu.Unlock()
	errs := []error{l.Leaser.Close(ctx)}
	for _, pool := range kept {
		errs = append(errs, pool.Close(ctx))
	}
	l.closing.Wait()
	return errors.Join(errs...)
}

// imageInstance is a lease of a workspace's image; done gives its room back
// once it is closed.
type imageInstance struct {
	isolation.Instance
	done func()
	once sync.Once
}

func (i *imageInstance) Close(ctx context.Context) error {
	err := i.Instance.Close(ctx)
	i.once.Do(i.done)
	return err
}
