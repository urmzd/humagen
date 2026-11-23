package github

import (
	"context"
	"sync"
	"time"

	transport "github.com/urmzd/saige/flow/connectors/github"
	"github.com/urmzd/saige/flow/deliverystate"
)

// layouts records, for each run's native review, that it was posted in the
// current layout. Recovery reproduces a review byte for byte to match the
// digest it was posted with, so it must know the layout: a review with no
// record was posted by the code from before reviews read as prose. The
// record goes in the delivery store, beside the review's receipt, so every
// replica reads it; without one it is kept in this process, as the
// receipt is.
type layouts struct {
	store deliverystate.Store
	scope string
	mu    sync.Mutex
	local map[deliverystate.Key]bool
}

// proseLayout is the value a run's record holds.
const proseLayout = "prose"

// newLayouts keeps the records in cfg's delivery store. A record names its
// tenant, run and channel, which no other connector instance shares, so it
// is not kept per instance: it outlives a receipt kept under another scope.
func newLayouts(cfg Config) *layouts {
	return &layouts{store: cfg.DeliveryState, scope: "review-layout", local: map[deliverystate.Key]bool{}}
}

func (l *layouts) key(v transport.View) deliverystate.Key {
	tenant := v.Tenant
	if tenant == "" {
		tenant = "-"
	}
	return deliverystate.Key{Tenant: tenant, Scope: l.scope, Run: v.RunID, Channel: v.Channel, Target: "review"}
}

// mark records that v's review is posted in the current layout.
func (l *layouts) mark(v transport.View) error {
	key := l.key(v)
	if l.store == nil {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.local[key] = true
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return l.store.Update(ctx, key, func([]byte) ([]byte, bool, error) { return []byte(proseLayout), false, nil })
}

// posted reports whether v's review was posted in the current layout.
func (l *layouts) posted(v transport.View) (bool, error) {
	key := l.key(v)
	if l.store == nil {
		l.mu.Lock()
		defer l.mu.Unlock()
		return l.local[key], nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	current := false
	err := l.store.Update(ctx, key, func(old []byte) ([]byte, bool, error) {
		current = string(old) == proseLayout
		return old, false, nil
	})
	return current, err
}
