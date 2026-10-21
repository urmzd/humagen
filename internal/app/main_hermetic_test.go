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
	os.Setenv("WARP_HOME", home)
	os.Setenv("XDG_CONFIG_HOME", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
