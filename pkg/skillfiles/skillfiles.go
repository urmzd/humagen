// Package skillfiles puts the supporting files of the skills one run is
// pinned to on this host, for a model whose program reads files itself.
//
// A native model reads a pinned skill's supporting text through the
// platform's file_read tool. A program that is the model and the tools at
// once (pkg/vendorcli) is offered no platform tool, so the platform writes
// the files where the program can read them: one directory a run, under a
// root only this user can open, named by 128 random bits, read only, and
// removed when the turn ends. The platform tells the program each file's
// absolute path in what it asks. Which directory the program may read is
// not taken from that text: the provider asks for the directory placed for
// the run its turn belongs to (Granted), and the run is the one the runtime
// names on the turn's context, set from the run's durable record.
//
// So a path a person types grants nothing, another run's included, and a
// turn is granted the directory of its own run in its own tenant and no
// other. The record of which run has which directory is this process's:
// the process that places a turn's files is the one that runs its program.
package skillfiles

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/urmzd/saige/flow/isolation"
)

// ErrFiles is a set of files that was not written.
var ErrFiles = errors.New("skillfiles: the skill files could not be placed")

// HostReader is a model provider whose program reads files on this host
// itself and is offered no platform tool. A run on one is given its pinned
// skills' supporting files as host paths.
type HostReader interface {
	ReadsHostFiles() bool
}

// File is one file to place: a slash-separated path under the run's
// directory, and its exact bytes.
type File struct {
	Path string
	Data []byte
}

// Limits bound what one run's directory holds.
type Limits struct {
	Files int
	Bytes int
}

// stale is how long a directory nobody removed is kept: one left by a
// process that was killed. It is far longer than a turn runs.
const stale = 7 * 24 * time.Hour

// Root is the directory every run's directory is under: one in the host's
// temporary directory that only this user opens, with symbolic links
// resolved so a program is told the path it will see.
func Root() (string, error) {
	tmp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrFiles, err)
	}
	return filepath.Join(tmp, "humagen-skill-files-"+strconv.Itoa(os.Getuid())), nil
}

// root makes Root and refuses one that is not this user's own directory: in
// a temporary directory several users share, another could have made it.
func root() (string, error) {
	dir, err := Root()
	if err != nil {
		return "", err
	}
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", fmt.Errorf("%w: %v", ErrFiles, err)
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || !own(info) {
		return "", fmt.Errorf("%w: %s is not a directory only this user opens", ErrFiles, dir)
	}
	return dir, nil
}

// Write places files in a new directory for one run and returns it. Every
// file is written whole or the directory is not left behind. Files and the
// directories that hold them are read only. A path that is not a plain
// relative one, that repeats, or that would leave the directory is refused,
// as is a set over the limits.
func Write(files []File, lim Limits) (string, error) {
	if len(files) == 0 || len(files) > lim.Files {
		return "", fmt.Errorf("%w: %d files, of at most %d", ErrFiles, len(files), lim.Files)
	}
	total := 0
	seen := map[string]bool{}
	for _, f := range files {
		if !fs.ValidPath(f.Path) || f.Path == "." || path.Clean(f.Path) != f.Path || filepath.FromSlash(f.Path) != f.Path || seen[f.Path] {
			return "", fmt.Errorf("%w: %q is not a path inside the run's directory", ErrFiles, f.Path)
		}
		seen[f.Path] = true
		if total += len(f.Data); total > lim.Bytes {
			return "", fmt.Errorf("%w: more than %d bytes", ErrFiles, lim.Bytes)
		}
	}
	base, err := root()
	if err != nil {
		return "", err
	}
	sweep(base)
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("%w: %v", ErrFiles, err)
	}
	dir := filepath.Join(base, hex.EncodeToString(id[:]))
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", fmt.Errorf("%w: %v", ErrFiles, err)
	}
	if err := fill(dir, files); err != nil {
		_ = Remove(dir)
		return "", fmt.Errorf("%w: %v", ErrFiles, err)
	}
	return dir, nil
}

// fill writes files under dir through an os.Root, which follows nothing out
// of dir, then takes write permission off every directory.
func fill(dir string, files []File) error {
	in, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	for _, f := range files {
		if parent := path.Dir(f.Path); parent != "." {
			if err := in.MkdirAll(parent, 0o700); err != nil {
				return err
			}
		}
		out, err := in.OpenFile(f.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400)
		if err != nil {
			return err
		}
		_, err = out.Write(f.Data)
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
	}
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		return os.Chmod(p, 0o500)
	})
}

// Remove deletes a run's directory. It removes only a directory Write made:
// one directly under the root with a name of that form.
func Remove(dir string) error {
	base, err := Root()
	if err != nil {
		return err
	}
	if filepath.Dir(dir) != base || !name.MatchString(filepath.Base(dir)) {
		return fmt.Errorf("%w: %q is not a run's directory", ErrFiles, dir)
	}
	// A read-only directory does not let what is in it be removed.
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o700)
		}
		return nil
	})
	return os.RemoveAll(dir)
}

// sweep removes directories left by a process that ended without removing
// them, so the root does not grow without bound.
func sweep(base string) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !e.IsDir() || !name.MatchString(e.Name()) || time.Since(info.ModTime()) < stale {
			continue
		}
		_ = Remove(filepath.Join(base, e.Name()))
	}
}

var name = regexp.MustCompile(`^[0-9a-f]{32}$`)

// placed is the directory of each run that has one in this process, by
// tenant and run: what a turn of that run is granted.
var placed = struct {
	mu   sync.Mutex
	dirs map[string]string
}{dirs: map[string]string{}}

func key(tenant, run string) string { return tenant + "\x00" + run }

// Place writes files to a new directory for one run of one tenant, as Write
// does, and records the directory as that run's: the one a turn of the run
// is granted (Granted). A directory the run already had is removed. Only
// the platform calls it, for a run it admitted.
func Place(tenant, run string, files []File, lim Limits) (string, error) {
	if tenant == "" || run == "" {
		return "", fmt.Errorf("%w: files are placed for a run of a tenant", ErrFiles)
	}
	dir, err := Write(files, lim)
	if err != nil {
		return "", err
	}
	placed.mu.Lock()
	old := placed.dirs[key(tenant, run)]
	placed.dirs[key(tenant, run)] = dir
	placed.mu.Unlock()
	if old != "" {
		_ = Remove(old)
	}
	return dir, nil
}

// Release removes the directory placed for a run, if it has one.
func Release(tenant, run string) error {
	placed.mu.Lock()
	dir := placed.dirs[key(tenant, run)]
	delete(placed.dirs, key(tenant, run))
	placed.mu.Unlock()
	if dir == "" {
		return nil
	}
	return Remove(dir)
}

// Granted is the directories a provider lets its program read for the turn
// ctx runs: the one placed for that turn's own run, or none.
//
// The run is read from the claim the runtime puts on every turn's context
// (isolation.RunFrom), whose tenant and run the platform sets from the run's
// durable record when the turn starts. Nothing in a turn's text is read, so
// a path a person types, another run's included, grants nothing, and a turn
// of one run is never granted another's directory. A caller that is not a
// platform turn carries no claim and is granted nothing.
func Granted(ctx context.Context) []string {
	claim, ok := isolation.RunFrom(ctx)
	if !ok || claim == nil {
		return nil
	}
	req := claim.Request()
	if req.Tenant == "" || req.Run == "" {
		return nil
	}
	placed.mu.Lock()
	dir := placed.dirs[key(req.Tenant, req.Run)]
	placed.mu.Unlock()
	if dir == "" {
		return nil
	}
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return nil
	}
	return []string{dir}
}
