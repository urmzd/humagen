package app

import (
	"os"
	"testing"
)

// TestMain keeps every test away from the machine's own warp.yaml and cache.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "warp-test-home-")
	if err != nil {
		panic(err)
	}
	for _, v := range []string{"WARP_HOME", "XDG_CONFIG_HOME"} {
		if err := os.Setenv(v, home); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
