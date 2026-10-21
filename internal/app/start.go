package app

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// configDir is where start and setup keep warp.yaml: WARP_HOME, else the
// user's config directory.
func configDir(getenv func(string) string) string {
	if d := strings.TrimSpace(getenv("WARP_HOME")); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "warp")
}

// envWebDir names a built console, overriding the search.
const envWebDir = "WARP_WEB_DIR"

// start runs the platform with nothing configured: dev sign-in, memory
// stores, disk files, seeded example agents and the console. It reads
// warp.yaml from the config directory when there is one, and on a terminal
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
	d := serveDefaults{auth: "dev", seed: true, web: webDir(os.Getenv)}
	path := filepath.Join(configDir(os.Getenv), "warp.yaml")
	if len(scanFiles(rest)) > 0 {
		d.seed = false
	} else {
		_, err := os.Stat(path)
		exists := err == nil
		if *ask || (!exists && !*yes && interactive(in)) {
			if err := runSetup(bufio.NewReader(in), out, path); err != nil {
				return err
			}
			exists = true
		}
		if exists {
			d.files, d.seed = []string{path}, false
			fmt.Fprintf(out, "warp: reading %s\n", path)
		}
	}
	if d.web == "" {
		fmt.Fprintln(out, "warp: no built console found (run pnpm --filter @warp/console build, or set WARP_WEB_DIR); serving the API only")
	}
	return serve(o, rest, d)
}

// webDir finds a built console: WARP_WEB_DIR, else apps/web/dist under the
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

// searchRoots are where a checkout or an install keeps warp's other parts.
func searchRoots() []string {
	var roots []string
	if wd, err := os.Getwd(); err == nil {
		roots = append(roots, wd)
	}
	if exe, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Dir(exe), filepath.Join(filepath.Dir(exe), "share", "warp"))
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

// interactive reports whether in is a terminal a person can answer on.
func interactive(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
