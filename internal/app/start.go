package app

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/urmzd/warp/pkg/credref"
)

// StartFile is what `warp setup` writes and `warp start` reads: the
// deployment's choices, as opposed to its providers. Everything in it has a
// working default, so a missing file is a working platform. Keys are never in
// it, only references to where they are kept.
type StartFile struct {
	// Auth is dev, tailscale or oidc. Empty is dev.
	Auth string `json:"auth,omitempty"`
	// Addr is the listen address. Empty is 127.0.0.1:8080.
	Addr string `json:"addr,omitempty"`
	// PublicURL is where browsers reach the server.
	PublicURL string `json:"public_url,omitempty"`
	// Tenant is the workspace every caller is confined to. Empty is default.
	Tenant string `json:"tenant,omitempty"`
	// Seed creates the example agents. Nil is on.
	Seed *bool `json:"seed,omitempty"`
	// Slack runs the Slack app as a client of this server.
	Slack SlackChoice `json:"slack"`
}

// SlackChoice is whether the Slack app runs, and where its credentials are.
type SlackChoice struct {
	Enabled bool `json:"enabled"`
	// Team is the one Slack team the app answers in.
	Team string `json:"team,omitempty"`
	// BotTokenRef and AppTokenRef name where the bot token (xoxb-) and the
	// Socket Mode app token (xapp-) are kept.
	BotTokenRef string `json:"bot_token_ref,omitempty"`
	AppTokenRef string `json:"app_token_ref,omitempty"`
	// Operators are Slack user ids who may settle any inbox item from Slack.
	Operators []string `json:"operators,omitempty"`
}

// Check validates the choices.
func (f StartFile) Check() error {
	switch f.Auth {
	case "", "dev", "tailscale", "oidc", "okta", "mock":
	default:
		return fmt.Errorf("auth is dev, tailscale or oidc, not %q", f.Auth)
	}
	if !f.Slack.Enabled {
		return nil
	}
	if strings.TrimSpace(f.Slack.Team) == "" {
		return errors.New("slack: team is required")
	}
	for name, ref := range map[string]string{"bot_token_ref": f.Slack.BotTokenRef, "app_token_ref": f.Slack.AppTokenRef} {
		if _, err := credref.Parse(ref); err != nil {
			return fmt.Errorf("slack: %s: %w", name, err)
		}
	}
	return nil
}

// ReadStartFile reads path. A missing file is the zero StartFile.
func ReadStartFile(path string) (StartFile, bool, error) {
	var f StartFile
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, false, nil
	}
	if err != nil {
		return f, false, err
	}
	if err := decodeStrict(b, &f); err != nil {
		return f, true, fmt.Errorf("%s: %w", path, err)
	}
	if err := f.Check(); err != nil {
		return f, true, fmt.Errorf("%s: %w", path, err)
	}
	return f, true, nil
}

// configDir is where start and setup keep their files: WARP_HOME, else the
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

// Environment overrides for start, so the same command runs on a laptop and
// on someone's own infrastructure. Stores, buses and buckets are configured by
// their own variables (WARP_POSTGRES_URL, WARP_REDIS_URL, the -storage flags),
// which start passes through untouched.
const (
	envAuth      = "WARP_AUTH"
	envAddr      = "WARP_ADDR"
	envPublicURL = "WARP_PUBLIC_URL"
	envTenant    = "WARP_TENANT"
	envWebDir    = "WARP_WEB_DIR"
)

// start runs the platform with nothing configured: dev auth, memory stores,
// disk files, seeded example agents and the console. On a terminal with no
// choices written yet it asks for them first, every question having a default
// that Enter accepts. Arguments after "--" go to serve unchanged.
func start(o Options, args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	dir := fs.String("config", "", "directory of warp.json and providers.json. Empty means WARP_HOME, else the user config directory")
	ask := fs.Bool("setup", false, "ask the first-run questions again before starting")
	yes := fs.Bool("yes", false, "never ask: take the defaults for anything not written down")
	pass := args
	if i := indexOf(args, "--"); i >= 0 {
		args, pass = args[:i], args[i+1:]
	} else {
		pass = nil
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		*dir = configDir(os.Getenv)
	}
	startPath, providersPath := filepath.Join(*dir, "warp.json"), filepath.Join(*dir, "providers.json")
	sf, exists, err := ReadStartFile(startPath)
	if err != nil {
		return err
	}
	if *ask || (!exists && !*yes && interactive(in)) {
		if sf, err = runSetup(bufio.NewReader(in), out, startPath, providersPath); err != nil {
			return err
		}
	}

	d := serveDefaults{auth: "dev", seed: true, providers: providersPath, web: webDir(os.Getenv)}
	var flags []string
	set := func(name, file, env string) {
		v := file
		if e := strings.TrimSpace(os.Getenv(env)); e != "" {
			v = e
		}
		if v != "" {
			flags = append(flags, "-"+name, v)
		}
	}
	set("auth", sf.Auth, envAuth)
	set("addr", sf.Addr, envAddr)
	set("public-url", sf.PublicURL, envPublicURL)
	set("tenant", sf.Tenant, envTenant)
	if sf.Seed != nil {
		d.seed = *sf.Seed
	}
	if sf.Slack.Enabled {
		d.slack = &slackApp{choice: sf.Slack, keys: credref.Default, dir: slackDir(os.Getenv)}
	}
	if d.web == "" {
		fmt.Fprintln(out, "warp: no built console found (run pnpm --filter @warp/console build, or set WARP_WEB_DIR); serving the API only")
	}
	fmt.Fprintf(out, "warp: choices in %s, providers in %s\n", startPath, providersPath)
	return serve(o, append(flags, pass...), d)
}

// webDir finds a built console: WARP_WEB_DIR, else apps/web/dist under the
// working directory or beside the binary.
func webDir(getenv func(string) string) string {
	if d := strings.TrimSpace(getenv(envWebDir)); d != "" {
		return d
	}
	var roots []string
	if wd, err := os.Getwd(); err == nil {
		roots = append(roots, wd)
	}
	if exe, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Dir(exe), filepath.Join(filepath.Dir(exe), "share", "warp"))
	}
	for _, r := range roots {
		for _, c := range []string{filepath.Join(r, "apps", "web", "dist"), filepath.Join(r, "web")} {
			if st, err := os.Stat(filepath.Join(c, "index.html")); err == nil && !st.IsDir() {
				return c
			}
		}
	}
	return ""
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
