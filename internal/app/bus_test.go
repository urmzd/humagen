package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/orchagent/flow/bus"

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
