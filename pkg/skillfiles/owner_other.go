//go:build !unix

package skillfiles

import "io/fs"

// own reports whether a directory belongs to this user and no one else
// opens it. Where that cannot be read, no directory is taken, so no files
// are placed: the programs that read them run on macOS and Linux only.
func own(fs.FileInfo) bool { return false }
