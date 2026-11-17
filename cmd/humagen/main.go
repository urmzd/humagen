// Command humagen runs the platform: API, sign-in, event stream, schedules and
// agent execution, in one process. It is humagen.Main with the model kinds
// that run a turn on Claude Code or Codex (pkg/vendorcli); a program that
// adds providers, tools or stores calls humagen.Main with options of its
// own. See internal/app for the composition root.
package main

import (
	"os"

	"github.com/urmzd/humagen"
	"github.com/urmzd/humagen/pkg/vendorcli"
)

// Set at release time with -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(humagen.Main(humagen.Version{Version: version, Commit: commit, Date: date},
		humagen.WithKind(humagen.Models, vendorcli.ClaudeCode, vendorcli.Factory(vendorcli.ClaudeCode)),
		humagen.WithKind(humagen.Models, vendorcli.Codex, vendorcli.Factory(vendorcli.Codex)),
	))
}
