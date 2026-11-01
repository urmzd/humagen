package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/ready"
)

// readyz reads the readiness probe.
func readyz(t *testing.T, addr string) (int, ready.Report) {
	t.Helper()
	resp, err := http.Get("http://" + addr + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var r ready.Report
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, r
}

// A server is ready once it has started, and on SIGTERM drains and stops
// without being cancelled.
func TestServeIsReadyAndStopsOnSIGTERM(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	sigs := make(chan os.Signal, 2)
	addrs, done := make(chan string, 1), make(chan error, 1)
	o := Options{ctx: context.Background(), ready: func(a string) { addrs <- a }, signals: sigs}
	go func() {
		done <- serve(o, []string{"-auth=dev", "-addr=127.0.0.1:0", "-drain-grace=5s", "-shutdown-timeout=2s"}, serveDefaults{})
	}()
	var addr string
	select {
	case addr = <-addrs:
	case err := <-done:
		t.Fatalf("serve returned before listening: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("serve did not listen")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		code, r := readyz(t, addr)
		if code == http.StatusOK && r.Status == ready.Ready {
			break
		}
		if code != http.StatusServiceUnavailable || r.Status != ready.Starting || time.Now().After(deadline) {
			t.Fatalf("readyz = %d %+v", code, r)
		}
		time.Sleep(10 * time.Millisecond)
	}
	sigs <- syscall.SIGTERM
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve after SIGTERM: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not stop after SIGTERM with nothing in flight")
	}
}

// A drain takes the process out of rotation, stops new turns, and returns
// at once when nothing is in flight.
func TestDrainPlatformTakesTheProcessOutOfRotation(t *testing.T) {
	p, err := platform.New(platform.Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	gate := ready.New(slog.New(slog.DiscardHandler))
	gate.Started()
	start := time.Now()
	drainPlatform(t.Context(), p, gate, time.Minute, slog.New(slog.DiscardHandler))
	if time.Since(start) > 5*time.Second {
		t.Errorf("an idle drain took %v", time.Since(start))
	}
	if !p.Draining() || !gate.Draining() {
		t.Fatalf("draining: platform %v, gate %v", p.Draining(), gate.Draining())
	}
	if r, ok := gate.Check(t.Context()); ok || r.Status != ready.Draining {
		t.Errorf("readiness while draining = %+v", r)
	}
}
