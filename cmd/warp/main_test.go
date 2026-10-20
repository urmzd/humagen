package main

import (
	"flag"
	"testing"

	"github.com/urmzd/warp/pkg/blob"
)

// Dev auth makes every request an admin, so it must never be reachable from
// another machine.
func TestDevAuthIsLoopbackOnly(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:8080": true,
		"localhost:8080": true,
		"[::1]:8080":     true,
		"0.0.0.0:8080":   false,
		":8080":          false,
		"10.0.0.5:8080":  false,
		"example.com:80": false,
		"nonsense":       false,
	} {
		if got := loopback(addr); got != want {
			t.Errorf("loopback(%q) = %v, want %v", addr, got, want)
		}
	}
	for _, addr := range []string{"0.0.0.0:8080", ":8080"} {
		if err := run([]string{"serve", "-auth=dev", "-addr", addr}); err == nil {
			t.Errorf("serve -auth=dev -addr %s started", addr)
		}
	}
}

func TestStorageFlags(t *testing.T) {
	open := func(env map[string]string, args ...string) (blob.Store, error) {
		t.Helper()
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		f := addStorageFlags(fs)
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
		return openBlobs(t.Context(), f, func(k string) string { return env[k] })
	}
	if s, err := open(nil); s != nil || err != nil {
		t.Fatalf("the default = %T, %v; want the platform's own", s, err)
	}
	dir := t.TempDir()
	if s, err := open(nil, "-storage-dir", dir); err != nil {
		t.Fatal(err)
	} else if _, ok := s.(*blob.Disk); !ok {
		t.Fatalf("-storage-dir = %T", s)
	}
	s, err := open(map[string]string{s3AccessEnv: "k", s3SecretEnv: "s"}, "-storage=s3", "-s3-bucket=warp", "-s3-endpoint=http://127.0.0.1:9000", "-s3-region=auto", "-s3-path-style", "-s3-prefix=prod")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(*blob.S3); !ok {
		t.Fatalf("-storage=s3 = %T", s)
	}
	for name, c := range map[string]struct {
		env  map[string]string
		args []string
	}{
		"no bucket":          {nil, []string{"-storage=s3"}},
		"half a credential":  {map[string]string{s3AccessEnv: "k"}, []string{"-storage=s3", "-s3-bucket=warp"}},
		"an unknown storage": {nil, []string{"-storage=tape"}},
	} {
		if _, err := open(c.env, c.args...); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
