package app

import (
	"bytes"
	"context"
	"flag"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/playscape/pkg/isolate/egress"

	"github.com/urmzd/humagen/fiber/pkg/isolation"
	"github.com/urmzd/humagen/pkg/spec"
)

func sandboxArgs(t *testing.T, args ...string) sandboxFlags {
	t.Helper()
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	f := addSandboxFlags(fs, "", false)
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return f
}

func gated(block string) loaded {
	var l loaded
	l.cfg.System.Spec.Egress = spec.Slot{Kind: "gate", Block: []byte(block)}
	return l
}

// With the gate, a local sandbox's commands are sent to it, a host the run
// may not reach is refused, and the decision reaches the audit log with the
// run it was made for.
func TestALocalSandboxGoesOutThroughTheGate(t *testing.T) {
	decisions := newEgressLog(slog.New(slog.DiscardHandler))
	s, closeIt, err := openSandbox(t.Context(), Options{}, gated(`{"listen":"127.0.0.1:0"}`), sandboxArgs(t, "-sandbox=local", "-sandbox-work-root="+t.TempDir()), slog.New(slog.DiscardHandler), decisions)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeIt)
	if !s.Egress {
		t.Fatal("the sandbox does not say it has egress")
	}

	var mu sync.Mutex
	var got []string
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go decisions.run(ctx, func(req isolation.Request, d egress.Decision) error {
		mu.Lock()
		got = append(got, req.Tenant+"/"+req.Run+" "+d.Host+" "+map[bool]string{true: "allowed", false: "denied"}[d.Allowed])
		mu.Unlock()
		return nil
	})

	inst, err := s.Leaser.Lease(t.Context(), isolation.Request{Tenant: "acme", Run: "r1", Egress: []string{"api.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = inst.Close(context.Background()) }()
	var out bytes.Buffer
	if _, err := inst.Exec(t.Context(), isolation.Command{Argv: []string{"sh", "-c", `printf %s "$HTTP_PROXY"`}}, isolation.Stdio{Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	proxy, err := url.Parse(out.String())
	if err != nil || !strings.HasPrefix(proxy.Host, "127.0.0.1:") || strings.HasSuffix(proxy.Host, ":0") {
		t.Fatalf("the command's proxy = %q", out.String())
	}
	c := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}}
	resp, err := c.Get("http://elsewhere.example/")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a host the run may not reach = %d", resp.StatusCode)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != "acme/r1 elsewhere.example denied" {
		t.Fatalf("audited %q", got)
	}
}

// A docker sandbox needs an internal network and an address its instances
// can reach; unisolated starts no gate.
func TestTheGateIsRefusedWhereItCouldNotWork(t *testing.T) {
	open := func(l loaded, args ...string) (isolation.Sandbox, error) {
		t.Helper()
		s, closeIt, err := openSandbox(t.Context(), Options{}, l, sandboxArgs(t, args...), slog.New(slog.DiscardHandler), nil)
		if err == nil {
			t.Cleanup(closeIt)
		}
		return s, err
	}
	if _, err := open(gated(`{"listen":"127.0.0.1:0"}`), "-sandbox=docker"); err == nil || !strings.Contains(err.Error(), "needs network") {
		t.Fatalf("docker with no network = %v", err)
	}
	if _, err := open(gated(`{"listen":"127.0.0.1:0","network":"humagen-egress"}`), "-sandbox=docker"); err == nil || !strings.Contains(err.Error(), "can reach") {
		t.Fatalf("docker with a loopback address = %v", err)
	}
	s, err := open(gated(`{"listen":"127.0.0.1:0"}`), "-sandbox=unisolated")
	if err != nil || s.Egress || s.Isolated() {
		t.Fatalf("unisolated with a gate = %+v, %v", s, err)
	}
	// No egress slot: no network, as before.
	s, err = open(loaded{}, "-sandbox=local", "-sandbox-work-root="+t.TempDir())
	if err != nil || s.Egress {
		t.Fatalf("local with no gate = %+v, %v", s, err)
	}
}

// The gate never waits on the audit log: past the queue's bound a decision
// is counted and dropped.
func TestTheGateNeverWaitsOnTheAuditLog(t *testing.T) {
	e := newEgressLog(slog.New(slog.DiscardHandler))
	for range maxEgressQueue + 5 {
		e.add(egress.Decision{Host: "x"})
	}
	if n := e.dropped.Load(); n != 5 {
		t.Fatalf("dropped %d, want 5", n)
	}
	var nilLog *egressLog
	nilLog.add(egress.Decision{}) // a sandbox opened with no log drops them
}
