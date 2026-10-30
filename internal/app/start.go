package app

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/urmzd/humagen/internal/legacy"
)

// envWebDir names a built console, overriding the search.
const envWebDir = "HUMAGEN_WEB_DIR"

// start runs the platform with nothing configured: dev sign-in, memory
// stores, disk files, seeded example agents and the console. It reads
// humagen.yaml from the config directory when there is one, and on a terminal
// with none it asks the first-run questions, every one with a default Enter
// accepts. Every serve flag works here too and wins over the file. These are
// a laptop's defaults; serve keeps a server's.
func start(o Options, args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ask := fs.Bool("setup", false, "ask the first-run questions again before starting")
	yes := fs.Bool("yes", false, "never ask: run on defaults when no spec is written down")
	// start's own flags come first; everything else is serve's.
	var rest []string
	for i, a := range args {
		name := strings.TrimLeft(strings.SplitN(a, "=", 2)[0], "-")
		if name != "setup" && name != "yes" {
			rest = args[i:]
			break
		}
		if err := fs.Parse([]string{a}); err != nil {
			return err
		}
	}
	if i := indexOf(rest, "--"); i >= 0 {
		rest = append(rest[:i:i], rest[i+1:]...)
	}
	// Example agents are seeded only when nothing is configured: a spec
	// declares its own agents.
	// On the developer's own machine runc is the runtime Docker has, and
	// playscape refuses it unless told it is for development; a server
	// defaults to runsc and refuses runc.
	d := serveDefaults{auth: "dev", seed: true, web: webDir(os.Getenv), notes: out, sandboxRuntime: "runc", sandboxInsecure: true}
	if len(scanFiles(rest)) > 0 {
		d.seed = false
	} else {
		// The default spec is looked for only when -f names none.
		path, err := specPath(o.notes)
		if err != nil {
			return err
		}
		_, err = os.Stat(path)
		exists := err == nil
		// The questions are asked, and humagen.yaml written, only once serve
		// knows its command line parses: -h, or a flag it does not know,
		// asks and writes nothing.
		setup := *ask || (!exists && !*yes && interactive(in))
		if exists || setup {
			d.files, d.seed = []string{path}, false
			d.prepare = func() error {
				if setup {
					if err := runSetup(bufio.NewReader(in), out, path); err != nil {
						return err
					}
				}
				fmt.Fprintf(out, "humagen: reading %s\n", path)
				return nil
			}
		}
	}
	return serve(o, rest, d)
}

// webDir finds a built console: HUMAGEN_WEB_DIR, else apps/web/dist under the
// working directory or beside the binary.
func webDir(getenv func(string) string) string {
	if d := strings.TrimSpace(getenv(envWebDir)); d != "" {
		return d
	}
	for _, r := range searchRoots() {
		for _, c := range []string{filepath.Join(r, "apps", "web", "dist"), filepath.Join(r, "web")} {
			if st, err := os.Stat(filepath.Join(c, "index.html")); err == nil && !st.IsDir() {
				return c
			}
		}
	}
	return ""
}

// searchRoots are where a checkout or an install keeps humagen's other parts.
func searchRoots() []string {
	var roots []string
	if wd, err := os.Getwd(); err == nil {
		roots = append(roots, wd)
	}
	if exe, err := os.Executable(); err == nil {
		// share/warp is the old install directory, from before the rename.
		roots = append(roots, filepath.Dir(exe), filepath.Join(filepath.Dir(exe), "share", "humagen"), filepath.Join(filepath.Dir(exe), "share", legacy.ShareDir))
	}
	return roots
}

func indexOf(args []string, s string) int {
	for i, a := range args {
		if a == s {
			return i
		}
	}
	return -1
}

// interactive reports whether in is a terminal a person can answer on. A
// character device is not enough: /dev/null is one.
func interactive(in io.Reader) bool {
	f, ok := in.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// probe parses args as fs would, without setting any of fs's flags, and
// returns what parsing would: flag.ErrHelp for -h, an error for a flag fs
// does not define or one missing its value. A value fs would refuse is left
// to fs.
func probe(fs *flag.FlagSet, args []string) error {
	dry := flag.NewFlagSet(fs.Name(), flag.ContinueOnError)
	dry.SetOutput(io.Discard)
	fs.VisitAll(func(f *flag.Flag) {
		b, ok := f.Value.(interface{ IsBoolFlag() bool })
		dry.Var(ignored{isBool: ok && b.IsBoolFlag()}, f.Name, "")
	})
	return dry.Parse(args)
}

// ignored is a flag's value that keeps nothing, for probe.
type ignored struct{ isBool bool }

func (ignored) String() string     { return "" }
func (ignored) Set(string) error   { return nil }
func (v ignored) IsBoolFlag() bool { return v.isBool }

// given reports whether a parse set the flag name, to anything.
func given(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}
