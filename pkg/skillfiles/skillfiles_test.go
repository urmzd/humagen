package skillfiles

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var limits = Limits{Files: 8, Bytes: 1 << 10}

// own root for each test: the host's temporary directory is the test's.
func isolate(t *testing.T) string {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// place writes one small set and removes it when the test ends.
func place(t *testing.T) string {
	t.Helper()
	dir, err := Write([]File{{Path: "brief/a.md", Data: []byte("a")}}, limits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Remove(dir) })
	return dir
}

func TestWritePlacesExactBytesReadOnlyAndRemoveTakesThemAway(t *testing.T) {
	root := isolate(t)
	binary := []byte{0, 255, '\n', 0}
	dir, err := Write([]File{{Path: "brief/references/style.md", Data: []byte("one\n")}, {Path: "brief/assets/icon.bin", Data: binary}, {Path: "other/notes.txt", Data: nil}}, limits)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dir) != root || !name.MatchString(filepath.Base(dir)) {
		t.Fatalf("placed in %s, not directly under %s", dir, root)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "brief", "assets", "icon.bin")); err != nil || !bytes.Equal(got, binary) {
		t.Fatalf("read %v, %v", got, err)
	}
	if info, _ := os.Stat(root); info.Mode().Perm() != 0o700 {
		t.Fatalf("the root is open to others: %v", info.Mode())
	}
	for path, want := range map[string]os.FileMode{"": 0o500, "brief": 0o500, "brief/references": 0o500, "brief/references/style.md": 0o400, "other/notes.txt": 0o400} {
		if info, err := os.Stat(filepath.Join(dir, path)); err != nil || info.Mode().Perm() != want {
			t.Errorf("%q: mode %v, %v, want %v", path, info.Mode().Perm(), err, want)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "brief", "added.md"), []byte("x"), 0o600); err == nil {
		t.Error("a file was added to a read-only directory")
	}
	if err := Remove(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the directory is still there: %v", err)
	}
}

func TestWriteRefusesAPathThatLeavesTheDirectoryAndLeavesNothing(t *testing.T) {
	root := isolate(t)
	outside := filepath.Join(t.TempDir(), "out.md")
	for _, path := range []string{"../out.md", "brief/../../out.md", "brief/../x.md", outside, "", ".", "brief//x.md", "./x.md", "brief/", `brief\..\x.md` + "\x00"} {
		if dir, err := Write([]File{{Path: "brief/ok.md", Data: []byte("ok")}, {Path: path, Data: []byte("no")}}, limits); !errors.Is(err, ErrFiles) {
			t.Errorf("%q was placed in %s: %v", path, dir, err)
		}
	}
	if _, err := Write([]File{{Path: "a.md"}, {Path: "a.md"}}, limits); !errors.Is(err, ErrFiles) {
		t.Errorf("a repeated path: %v", err)
	}
	// A file where a directory is needed fails after the directory is made:
	// nothing of it is left.
	if _, err := Write([]File{{Path: "a", Data: []byte("x")}, {Path: "a/b.md", Data: []byte("y")}}, limits); !errors.Is(err, ErrFiles) {
		t.Errorf("a file used as a directory: %v", err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("a refused set left %v", entries)
	}
	if _, err := os.Stat(outside); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("wrote outside the root")
	}
}

func TestWriteIsBounded(t *testing.T) {
	root := isolate(t)
	if _, err := Write(nil, limits); !errors.Is(err, ErrFiles) {
		t.Errorf("no files: %v", err)
	}
	if _, err := Write([]File{{Path: "a"}, {Path: "b"}, {Path: "c"}}, Limits{Files: 2, Bytes: 10}); !errors.Is(err, ErrFiles) {
		t.Errorf("too many files: %v", err)
	}
	if _, err := Write([]File{{Path: "a", Data: make([]byte, 6)}, {Path: "b", Data: make([]byte, 5)}}, Limits{Files: 2, Bytes: 10}); !errors.Is(err, ErrFiles) {
		t.Errorf("too many bytes: %v", err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("a refused set left %v", entries)
	}
}

func TestADirectoryNobodyRemovedIsSweptWhenItIsOld(t *testing.T) {
	isolate(t)
	left, recent := place(t), place(t)
	old := time.Now().Add(-stale - time.Hour)
	if err := os.Chtimes(left, old, old); err != nil {
		t.Fatal(err)
	}
	place(t)
	if _, err := os.Stat(left); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the old directory was kept: %v", err)
	}
	if _, err := os.Stat(recent); err != nil {
		t.Fatalf("a directory in use was swept: %v", err)
	}
}

func TestDirsTakesOnlyADirectoryThatIsPlacedNow(t *testing.T) {
	root := isolate(t)
	dir := place(t)
	text := "Read these:\n" + filepath.Join(dir, "brief", "a.md") + " (1 bytes)\n" + filepath.Join(dir, "brief", "b.md") + " (2 bytes)"
	if got := Dirs(text); len(got) != 1 || got[0] != dir {
		t.Fatalf("Dirs = %v, want %s", got, dir)
	}
	// What a person could type names nothing: a directory that was never
	// placed, the root itself, a path elsewhere, and one that climbs out.
	never := filepath.Join(root, "0123456789abcdef0123456789abcdef")
	plain := filepath.Join(root, "notes")
	if err := os.Mkdir(plain, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, typed := range []string{never + "/x.md", root + "/x.md", plain + "/x.md", "/etc/passwd", root + "/../../etc/passwd", dir + "x/a.md", filepath.Base(dir) + "/a.md"} {
		if got := Dirs("please read " + typed); len(got) != 0 {
			t.Errorf("%q named %v", typed, got)
		}
	}
	if err := Remove(plain); err == nil {
		t.Error("Remove took a directory Write did not make")
	}
	if err := Remove(filepath.Dir(root)); err == nil {
		t.Error("Remove took a directory outside the root")
	}
	if err := Remove(dir); err != nil {
		t.Fatal(err)
	}
	if got := Dirs(text); len(got) != 0 {
		t.Fatalf("a removed directory is still named: %v", got)
	}
}

func TestTextThatNamesManyDirectoriesNamesNone(t *testing.T) {
	isolate(t)
	text := ""
	for range maxDirs + 1 {
		text += filepath.Join(place(t), "brief", "a.md") + "\n"
	}
	if got := Dirs(text); len(got) != 0 {
		t.Fatalf("Dirs = %v", got)
	}
}
