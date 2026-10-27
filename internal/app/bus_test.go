package app

import (
	"strings"
	"testing"

	"github.com/urmzd/fiber/pkg/bus"

	"github.com/urmzd/humagen/internal/platform"
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
