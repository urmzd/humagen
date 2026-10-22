package legacy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SpecFile is the spec start, setup, config and deploy read when none is
// named. In $HUMAGEN_HOME (which AdoptEnv fills from $WARP_HOME) it is
// humagen.yaml, else warp.yaml; with no home it is
// UserConfigDir()/humagen/humagen.yaml, else UserConfigDir()/warp/warp.yaml.
// An old one found is used where it is, with a note, so an older binary
// still finds it. Both found is an error rather than a guess at which one is
// live. Neither found is the new path, where first-run setup writes.
func SpecFile(getenv func(string) string, userConfigDir func() (string, error), n *Notes) (string, error) {
	var newPath, oldPath string
	if d := strings.TrimSpace(getenv(Prefix + "HOME")); d != "" {
		newPath, oldPath = filepath.Join(d, "humagen.yaml"), filepath.Join(d, ConfigFile)
	} else {
		base, err := userConfigDir()
		if err != nil {
			base = "."
		}
		newPath, oldPath = filepath.Join(base, "humagen", "humagen.yaml"), filepath.Join(base, ConfigDir, ConfigFile)
	}
	haveNew, haveOld := exists(newPath), exists(oldPath)
	switch {
	case haveNew && haveOld:
		return "", fmt.Errorf("%s and %s both exist; keep one (%s is the old place)", newPath, oldPath, oldPath)
	case haveOld:
		n.Add("reading %s, the old place; it still works, move it to %s", oldPath, newPath)
		return oldPath, nil
	}
	return newPath, nil
}

// IsOldSpecFile reports whether path is a spec file under its old name,
// which setup writes in the old format so an older binary can still read it.
func IsOldSpecFile(path string) bool { return filepath.Base(path) == ConfigFile }

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !errors.Is(err, fs.ErrNotExist)
}
