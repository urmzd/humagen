//go:build unix

package skillfiles

import (
	"io/fs"
	"os"
	"syscall"
)

// own reports whether a directory belongs to this user and no one else
// opens it.
func own(info fs.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid() && info.Mode().Perm() == 0o700
}
