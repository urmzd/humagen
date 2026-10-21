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
	for k, v := range map[string]string{"WARP_HOME": home, "XDG_CONFIG_HOME": home} {
		if err := os.Setenv(k, v); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
