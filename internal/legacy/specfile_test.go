package legacy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/legacy"
)

func TestSpecFile(t *testing.T) {
	write := func(t *testing.T, path string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, tc := range map[string]struct {
		home    bool
		files   []string
		want    string
		note    bool
		wantErr bool
	}{
		"neither: the new place":      {false, nil, "humagen/humagen.yaml", false, false},
		"the new one":                 {false, []string{"humagen/humagen.yaml"}, "humagen/humagen.yaml", false, false},
		"the old one, where it is":    {false, []string{"warp/warp.yaml"}, "warp/warp.yaml", true, false},
		"both: an error":              {false, []string{"humagen/humagen.yaml", "warp/warp.yaml"}, "", false, true},
		"home, neither":               {true, nil, "home/humagen.yaml", false, false},
		"home, the old file":          {true, []string{"home/warp.yaml"}, "home/warp.yaml", true, false},
		"home, both":                  {true, []string{"home/warp.yaml", "home/humagen.yaml"}, "", false, true},
		"home looks nowhere else":     {true, []string{"warp/warp.yaml"}, "home/humagen.yaml", false, false},
		"home, new beside an old dir": {true, []string{"home/humagen.yaml", "warp/warp.yaml"}, "home/humagen.yaml", false, false},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tc.files {
				write(t, filepath.Join(dir, f))
			}
			getenv := func(k string) string {
				if tc.home && k == "HUMAGEN_HOME" {
					return filepath.Join(dir, "home")
				}
				return ""
			}
			n := legacy.NewNotes(nil)
			got, err := legacy.SpecFile(getenv, func() (string, error) { return dir, nil }, n)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v", err)
			}
			if tc.wantErr {
				return
			}
			if want := filepath.Join(dir, tc.want); got != want {
				t.Errorf("got %s, want %s", got, want)
			}
			if notes := n.All(); (len(notes) == 1) != tc.note || (tc.note && !strings.Contains(notes[0], "the old place")) {
				t.Errorf("notes %q", notes)
			}
		})
	}
}

// WARP_HOME reaches SpecFile as HUMAGEN_HOME, through AdoptEnv.
func TestOldHomeIsReadThroughAdoptEnv(t *testing.T) {
	dir := t.TempDir()
	env := map[string]string{}
	legacy.AdoptEnv([]string{"WARP_HOME=" + dir}, func(k, v string) error { env[k] = v; return nil }, nil)
	got, err := legacy.SpecFile(func(k string) string { return env[k] }, func() (string, error) { return "/nowhere", nil }, nil)
	if err != nil || got != filepath.Join(dir, "humagen.yaml") {
		t.Fatalf("got %s, %v", got, err)
	}
}

func TestNotesAndCommand(t *testing.T) {
	var buf strings.Builder
	n := legacy.NewNotes(&buf)
	legacy.FromCommand("/usr/local/bin/warp", n)
	legacy.FromCommand("warp", n)
	legacy.FromCommand("/usr/local/bin/humagen", n)
	if buf.String() != "humagen: the command is humagen now; warp still runs it\n" {
		t.Errorf("printed %q", buf.String())
	}
	var nilNotes *legacy.Notes
	nilNotes.Add("dropped")
	if nilNotes.All() != nil {
		t.Error("a nil Notes kept a note")
	}
}
