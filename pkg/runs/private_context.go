package runs

import (
	"errors"
	"fmt"
)

// UsesPrivateContext is internal policy metadata, never part of run JSON or
// the immutable accepted tag. It survives journal expiry and replay.
func (r Run) UsesPrivateContext() bool { return r.privateContext }

// MarkPrivateContext writes through before a tool may obtain private data.
// Flush serialization prevents an older pending snapshot from erasing the bit.
func (x *Index) MarkPrivateContext(tenant, id string) error {
	err := x.do(tenant, func(t *txn) error {
		run, err := t.run(id)
		if err != nil {
			return err
		}
		if run == nil || run.Status.Terminal() {
			return fmt.Errorf("%w: private context requires a live run", ErrNotFound)
		}
		if run.privateContext {
			return errSkip
		}
		run.privateContext = true
		t.touch(run)
		return nil
	})
	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("runs: private context write: %w", err)
	}
	return err
}
