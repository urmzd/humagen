package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/urmzd/playscape/pkg/isolate/egress"

	"github.com/urmzd/humagen/fiber/pkg/isolation"
	poolleaser "github.com/urmzd/humagen/fiber/pkg/isolation/isolate"
	"github.com/urmzd/humagen/pkg/spec"
)

// The egress gate (docs/reference/spec.md, Egress): playscape's forward
// proxy, served by this process, and a sandbox instance's only way out.
// The leaser grants each lease the hosts its run's egress ceiling allows;
// this file starts the gate and carries its decisions to the audit log.

// egressGateOf is the spec's gate block, and whether the egress slot asks
// for a gate at all. The block was checked when the file was read.
func egressGateOf(l loaded) (spec.EgressGate, bool) {
	slot := l.cfg.System.Spec.Egress
	if slot.Kind != "gate" {
		return spec.EgressGate{}, false
	}
	b, _ := spec.Block[spec.EgressGate](slot)
	if b.Listen == "" {
		b.Listen = spec.DefaultEgressListen
	}
	return b, true
}

// dockerEgress refuses a gate docker instances could not use: one with no
// internal network to join, or one they would look for on their own
// loopback.
func dockerEgress(g spec.EgressGate) error {
	if g.Network == "" {
		return errors.New("egress: gate: the docker sandbox needs network, an --internal Docker network the gate is reachable on")
	}
	addr := cmp.Or(g.Address, g.Listen)
	host, _, _ := net.SplitHostPort(addr)
	if ip := net.ParseIP(host); host == "localhost" || ip != nil && (ip.IsLoopback() || ip.IsUnspecified()) {
		return fmt.Errorf("egress: gate: address %q is not one a docker instance can reach the gate at; give this server's address on network %s", addr, g.Network)
	}
	return nil
}

// runningGate is a started gate and how to stop it.
type runningGate struct {
	egress *poolleaser.Egress
	stop   func()
}

// startEgress listens on g.Listen now, so a port in use fails the start,
// and serves the gate there until stop. Instances reach it at g.Address,
// or where it listens when that is empty.
func startEgress(g spec.EgressGate, decisions *egressLog, log *slog.Logger) (runningGate, error) {
	gate := egress.New(egress.Config{AllowNonPublic: g.AllowNonPublic, OnDecision: decisions.add})
	ln, err := net.Listen("tcp", g.Listen)
	if err != nil {
		return runningGate{}, fmt.Errorf("egress: gate: %w", err)
	}
	srv := &http.Server{Handler: gate, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			log.Error("egress: the gate stopped: instances have no way out", "error", err)
		}
	}()
	addr := cmp.Or(g.Address, ln.Addr().String())
	log.Info("egress: gate listening", "listen", ln.Addr().String(), "address", addr, "network", g.Network)
	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	return runningGate{egress: &poolleaser.Egress{Gate: gate, Address: addr}, stop: stop}, nil
}

// egressLog carries the gate's decisions to the audit log. The gate must
// not block, so a decision waits in a bounded queue and one that finds it
// full is counted and dropped, and the count is logged.
type egressLog struct {
	log     *slog.Logger
	queue   chan egressDecision
	dropped atomic.Int64
	leaser  atomic.Pointer[poolleaser.Leaser]
}

type egressDecision struct {
	req   isolation.Request
	known bool
	d     egress.Decision
}

// maxEgressQueue bounds the decisions waiting for the audit log.
const maxEgressQueue = 1024

func newEgressLog(log *slog.Logger) *egressLog {
	return &egressLog{log: log, queue: make(chan egressDecision, maxEgressQueue)}
}

// from names the leaser whose grants decisions belong to.
func (e *egressLog) from(l *poolleaser.Leaser) {
	if e != nil {
		e.leaser.Store(l)
	}
}

// add is the gate's OnDecision. It finds the run the decision's grant was
// made for while the lease is still out.
func (e *egressLog) add(d egress.Decision) {
	if e == nil {
		return
	}
	x := egressDecision{d: d}
	if l := e.leaser.Load(); l != nil && d.Grant != "" {
		x.req, x.known = l.Granted(d.Grant)
	}
	select {
	case e.queue <- x:
	default:
		e.dropped.Add(1)
	}
}

// run records decisions with record until ctx ends.
func (e *egressLog) run(ctx context.Context, record func(isolation.Request, egress.Decision) error) {
	for {
		select {
		case <-ctx.Done():
			return
		case x := <-e.queue:
			if n := e.dropped.Swap(0); n > 0 {
				e.log.Warn("egress: decisions were not audited: the queue was full", "dropped", n)
			}
			if !x.known {
				// No grant: a token that was never valid or was revoked.
				e.log.Warn("egress: a request with no valid grant was refused", "method", x.d.Method, "host", x.d.Host, "port", x.d.Port, "reason", x.d.Reason)
				continue
			}
			if err := record(x.req, x.d); err != nil {
				e.log.Warn("egress: a decision was not audited", "run", x.req.Run, "host", x.d.Host, "error", err)
			}
		}
	}
}
