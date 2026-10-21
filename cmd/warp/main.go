// Command warp runs the platform: API, sign-in, event stream, schedules and
// agent execution, in one process. It is warp.Main with no options; a
// program that adds providers, tools or stores calls warp.Main with
// options instead. See internal/app for the composition root.
package main

import (
	"os"

	"github.com/urmzd/warp"
)

// Set at release time with -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(warp.Main(warp.Version{Version: version, Commit: commit, Date: date}))
}
