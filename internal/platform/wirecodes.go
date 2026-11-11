package platform

// The runtime's sentinel errors are given their wire codes here, so that an
// error that crossed a process boundary (a durable record, an error envelope
// from an isolated instance) still answers errors.Is for the sentinel it
// wrapped. The flow and sandbox packages do not register themselves.
import (
	_ "github.com/urmzd/saige/flow/wirecodes"
	_ "github.com/urmzd/saige/sandbox/wirecodes"
)
