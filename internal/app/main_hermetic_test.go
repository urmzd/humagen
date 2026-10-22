package app

import (
	"os"
	"strings"
	"testing"
)

// testCommand, when set, makes this test binary run the humagen command it
// holds instead of the tests, with the environment the test gave it, so a
// test can send a real process a signal.
const testCommand = "HUMAGEN_TEST_COMMAND"

// TestMain keeps every test away from the machine's own humagen.yaml and cache.
func TestMain(m *testing.M) {
	if args := os.Getenv(testCommand); args != "" {
		os.Exit(Execute(Options{}, strings.Fields(args)))
	}
	home, err := os.MkdirTemp("", "humagen-test-home-")
	if err != nil {
		panic(err)
	}
	for _, v := range []string{"HUMAGEN_HOME", "XDG_CONFIG_HOME"} {
		if err := os.Setenv(v, home); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
