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
	for _, name := range []string{"WARP_HOME", "XDG_CONFIG_HOME"} {
		if err := os.Setenv(name, home); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

// mustWriteFile writes a test's fixture and stops the test if it cannot.
func mustWriteFile(t *testing.T, path string, data []byte, perm os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, perm); err != nil {
		t.Fatal(err)
	}
}
