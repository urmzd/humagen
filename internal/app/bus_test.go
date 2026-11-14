package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/saige/flow/bus"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/credref"
)

func TestOpenBusIsPostgresFirst(t *testing.T) {
	ctx := t.Context()
	pg := platform.Backends{Bus: bus.NewMemory()} // stands in for the stores' Postgres bus

	if b, err := openBus(ctx, Options{}, loaded{}, "", pg); err != nil || b != pg.Bus {
		t.Fatalf("empty with postgres stores: %v, %v; want the stores' bus", b, err)
	}
	if b, err := openBus(ctx, Options{}, loaded{}, "", platform.Backends{}); err != nil || b != nil {
		t.Fatalf("empty with memory stores: %v, %v; want memory", b, err)
	}
	if b, err := openBus(ctx, Options{}, loaded{}, "memory", pg); err != nil || b != nil {
		t.Fatalf("memory: %v, %v; want memory", b, err)
	}
	if b, err := openBus(ctx, Options{}, loaded{}, "postgres", pg); err != nil || b != pg.Bus {
		t.Fatalf("postgres: %v, %v", b, err)
	}
	for kind, want := range map[string]string{
		"postgres": "-state=postgres",
		"redis":    "redis bus was removed",
		"kafka":    "not memory or postgres",
	} {
		if _, err := openBus(ctx, Options{}, loaded{}, kind, platform.Backends{}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", kind, err, want)
		}
	}
}

// The start refuses more than one replica on the in-memory bus (a nil bus),
// whichever of the file, the flags and the environment chose it, and says
// what to set. One replica on memory and any number on the stores' bus start.
func TestTheStartRefusesReplicasOnTheMemoryBus(t *testing.T) {
	pg := bus.NewMemory() // stands in for the stores' Postgres bus
	err := checkReplicas(2, nil)
	if err == nil {
		t.Fatal("2 replicas on the memory bus started")
	}
	for _, want := range []string{"the in-memory bus reaches one process only", "2 replicas", "bus.kind: postgres", "server.replicas: 1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to say %q", err, want)
		}
	}
	if err := checkReplicas(1, nil); err != nil {
		t.Errorf("1 replica on the memory bus: %v", err)
	}
	if err := checkReplicas(3, pg); err != nil {
		t.Errorf("3 replicas on the stores' bus: %v", err)
	}
	if err := checkReplicas(0, pg); err == nil || !strings.Contains(err.Error(), "1 or more") {
		t.Errorf("0 replicas: err = %v", err)
	}
}

// config check refuses the combination a file declares, and the file's
// replica count reaches the start as -replicas.
func TestConfigCheckRefusesReplicasOnTheMemoryBus(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		path := filepath.Join(dir, "humagen.yaml")
		if err := os.WriteFile(path, []byte("api_version: humagen/v1\nkind: System\nmetadata: {name: humagen}\nspec:\n"+body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	var out strings.Builder
	err := configCmd(Options{}, []string{"check", "-f", write("  server: {replicas: 2}\n  bus: {kind: memory}\n")}, &out)
	if err == nil || !strings.Contains(err.Error(), "the in-memory bus reaches one process only") {
		t.Fatalf("config check: err = %v, out %q", err, out.String())
	}
	if err := configCmd(Options{}, []string{"check", "-f", write("  server: {replicas: 1}\n  bus: {kind: memory}\n")}, &out); err != nil {
		t.Fatalf("config check, one replica: %v", err)
	}
	t.Setenv("HUMAGEN_POSTGRES_URL", "postgres://unused") // named, never dialed: loadFiles only reads it
	l, err := loadFiles(t.Context(), Options{}, []string{write("  server: {replicas: 3}\n  stores: {kind: postgres, postgres: {url: {ref: env:HUMAGEN_POSTGRES_URL}}}\n")}, credref.Resolver{})
	if err != nil || !slices.Contains(l.args, "-replicas=3") {
		t.Fatalf("a declared replica count: args %v, %v", l.args, err)
	}
}

// A file that names no stores and no bus leaves -bus unset, so a server on
// $HUMAGEN_POSTGRES_URL puts its bus on Postgres beside the stores (openBus)
// and replicas share one stream. A bus the file names is still passed on.
func TestTheSpecSetsTheBusOnlyWhenItSays(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		path := filepath.Join(dir, "humagen.yaml")
		if err := os.WriteFile(path, []byte("api_version: humagen/v1\nkind: System\nmetadata: {name: humagen}\nspec:\n"+body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	l, err := loadFiles(t.Context(), Options{}, []string{write("  server: {addr: \"127.0.0.1:9\"}\n")}, credref.Resolver{})
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range l.args {
		if strings.HasPrefix(arg, "-bus=") || strings.HasPrefix(arg, "-state=") {
			t.Errorf("a file naming no stores or bus set %s (args %v)", arg, l.args)
		}
	}
	l, err = loadFiles(t.Context(), Options{}, []string{write("  bus: {kind: memory}\n")}, credref.Resolver{})
	if err != nil || !slices.Contains(l.args, "-bus=memory") {
		t.Fatalf("a declared bus: args %v, %v", l.args, err)
	}
}
