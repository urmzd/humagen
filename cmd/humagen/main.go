// Command humagen runs the platform: API, sign-in, event stream, schedules and
// agent execution, in one process. It is humagen.Main with no options; a
// program that adds providers, tools or stores calls humagen.Main with
// options instead. See internal/app for the composition root.
package main

import (
	"os"

	"github.com/urmzd/humagen"
)

// Set at release time with -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(humagen.Main(humagen.Version{Version: version, Commit: commit, Date: date}))
}
