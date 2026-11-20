// Command humagen runs the platform: API, sign-in, event stream, schedules and
// agent execution, in one process. Every turn runs on saige. A program that
// adds providers, tools or stores calls humagen.Main with options of its
// own. See internal/app for the composition root.
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
