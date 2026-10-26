package runs

import "fmt"

// UsesPrivateContext is internal policy metadata, never part of run JSON or
// the immutable accepted tag. It survives journal expiry and replay.
func (r Run) UsesPrivateContext() bool { return r.privateContext }

// MarkPrivateContext writes through before a tool may obtain private data.
// Flush serialization prevents an older pending snapshot from erasing the bit.
func (x *Index) MarkPrivateContext(tenant, id string) error {
	p := x.kept
	if p != nil {
		p.flushing.Lock()
		defer p.flushing.Unlock()
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	run, ok := x.runs[id]
	if !ok || run.Tenant != tenant || run.Status.Terminal() {
		return fmt.Errorf("%w: private context requires a live run", ErrNotFound)
	}
	if run.privateContext {
		return nil
	}
	next := *run
	next.privateContext = true
	if p != nil {
		if err := p.runs.Put(tenant, id, p.record(&next)); err != nil {
			return fmt.Errorf("runs: private context write: %w", err)
		}
		delete(p.dirty, id)
	}
	run.privateContext = true
	return nil
}
