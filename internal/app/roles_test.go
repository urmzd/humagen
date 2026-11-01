package app

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/urmzd/humagen/internal/platform"
)

func TestProcessRole(t *testing.T) {
	for _, c := range []struct {
		flag  string
		args  []string
		fixed platform.ProcessRole
		want  platform.ProcessRole
		err   string
	}{
		{flag: "all", want: platform.RoleAll},
		{flag: "api", want: platform.RoleAPI},
		{flag: "worker", want: platform.RoleWorker},
		{flag: "", want: platform.RoleAll},
		{flag: "scheduler", err: "not all, api or worker"},
		// humagen worker is the worker role whatever the spec says.
		{flag: "api", fixed: platform.RoleWorker, want: platform.RoleWorker},
		{flag: "worker", args: []string{"-role=worker"}, fixed: platform.RoleWorker, want: platform.RoleWorker},
		{flag: "api", args: []string{"-role", "api"}, fixed: platform.RoleWorker, err: "runs the worker role"},
		{flag: "api", args: []string{"--", "-role=api"}, fixed: platform.RoleWorker, want: platform.RoleWorker},
	} {
		got, err := processRole(c.flag, c.args, c.fixed)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%+v: err = %v, want %q", c, err, c.err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%+v = %q, %v; want %q", c, got, err, c.want)
		}
	}
}

func TestASplitRoleNeedsPostgres(t *testing.T) {
	t.Setenv(postgresURLEnv, "")
	o := Options{ctx: t.Context(), ready: func(string) { t.Error("a refused role listened") }}
	err := serve(o, []string{"-addr", "127.0.0.1:0", "-auth", "dev"}, serveDefaults{role: platform.RoleWorker})
	if err == nil || !strings.Contains(err.Error(), "-state=postgres") {
		t.Fatalf("humagen worker on memory: %v", err)
	}
	err = serve(o, []string{"-addr", "127.0.0.1:0", "-auth", "dev", "-role", "api"}, serveDefaults{})
	if err == nil || !strings.Contains(err.Error(), "-state=postgres") {
		t.Fatalf("serve -role api on memory: %v", err)
	}
}

// humagen worker runs the platform, leads as the only process, and starts no
// listener: not the API, not sign-in.
func TestAWorkerStartsNoHTTPServer(t *testing.T) {
	base := os.Getenv("HUMAGEN_POSTGRES_TEST_URL")
	if base == "" {
		t.Skip("HUMAGEN_POSTGRES_TEST_URL is not set")
	}
	t.Setenv(postgresURLEnv, inSchema(t, base))

	// An address nothing listens on, to show the worker does not either.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ran := make(chan *platform.Platform, 1)
	o := Options{
		ctx:     ctx,
		ready:   func(string) { t.Error("a worker listened") },
		running: func(p *platform.Platform) { ran <- p },
	}
	done := make(chan error, 1)
	// oidc with no issuer: a worker does not sign anyone in, so it does not
	// need one.
	go func() {
		done <- serve(o, []string{"-addr", addr, "-auth", "oidc"}, serveDefaults{role: platform.RoleWorker})
	}()

	var p *platform.Platform
	select {
	case p = <-ran:
	case err := <-done:
		t.Fatalf("the worker stopped before it ran: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("the worker did not start")
	}
	if got := p.Process().Role; got != platform.RoleWorker {
		t.Fatalf("role = %q, want worker", got)
	}
	// A process that wins the lock waits out a predecessor's last check
	// (three intervals of two seconds) before it leads.
	deadline := time.Now().Add(30 * time.Second)
	for !p.Process().Leader {
		if time.Now().After(deadline) {
			t.Fatal("the only process on its database does not lead")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		_ = c.Close()
		t.Fatalf("something listens on %s", addr)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("worker: %v", err)
	}
}

// inSchema is base in a new schema of its own, dropped when the test ends,
// so the test does not contend with others for the database.
func inSchema(t *testing.T, base string) string {
	t.Helper()
	schema := fmt.Sprintf("humagen_roles_%d", time.Now().UnixNano())
	conn, err := pgx.Connect(t.Context(), base)
	if err != nil {
		t.Fatal(err)
	}
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err := conn.Exec(t.Context(), "CREATE SCHEMA "+ident); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "DROP SCHEMA "+ident+" CASCADE")
		_ = conn.Close(context.Background())
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

// serve with no role is all of it, as before roles: it listens, leads, and
// runs turns.
func TestServeIsAllInOneByDefault(t *testing.T) {
	t.Setenv(postgresURLEnv, "")
	ran := make(chan *platform.Platform, 1)
	addr, stop := serveUntil(t, Options{running: func(p *platform.Platform) { ran <- p }}, "-addr", "127.0.0.1:0", "-auth", "dev")
	defer stop()
	p := <-ran
	if got := p.Process(); got.Role != platform.RoleAll {
		t.Fatalf("role = %q, want all", got.Role)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !p.Process().Leader {
		if time.Now().After(deadline) {
			t.Fatal("a process on memory does not lead")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if body := get(t, "http://"+addr+"/healthz"); !strings.Contains(body, "ok") {
		t.Fatalf("healthz = %q", body)
	}
}
