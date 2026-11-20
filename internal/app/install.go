package app

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/credref"
	githubapi "github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/spec"
)

const installUsage = `usage: humagen install KIND [flags]

Kinds:
  github  Register a GitHub App in your browser, install it on your repositories, and connect it
  slack   Create the Slack app from Humagen's manifest in your browser, then keep its two tokens

Run humagen install KIND --help for kind-specific flags.`

// installer is what install reads of the world, so a test can give its own:
// the browser, the two services, the keychain and the terminal.
type installer struct {
	in  *bufio.Reader
	out io.Writer
	// open shows a page to the person. It is their browser.
	open func(url string) error
	http *http.Client
	// Where GitHub and Slack are: their pages, and their APIs.
	githubWeb, githubAPI, slackWeb, slackAPI string
	// keep writes a value to the operating system's keychain and returns its reference.
	keep func(ctx context.Context, service, account, value string) (string, error)
	// secret reads one line the terminal does not show.
	secret func(prompt string) (string, error)
	// wait is how long the person has in the browser.
	wait time.Duration
}

func installCmd(o Options, args []string, in io.Reader, out io.Writer) error {
	r := bufio.NewReader(in)
	i := installer{
		in: r, out: out, open: openBrowser,
		http:      &http.Client{Timeout: 30 * time.Second},
		githubWeb: "https://github.com", githubAPI: "https://api.github.com",
		slackWeb: "https://api.slack.com", slackAPI: "https://slack.com/api",
		keep: keepInKeychain, wait: 15 * time.Minute,
	}
	i.secret = func(prompt string) (string, error) {
		fmt.Fprint(out, prompt)
		// A terminal reads without echo. Anything else, such as a pipe, is read as it comes.
		if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
			b, err := term.ReadPassword(int(f.Fd()))
			fmt.Fprintln(out)
			return strings.TrimSpace(string(b)), err
		}
		line, err := r.ReadString('\n')
		if err != nil && line == "" {
			return "", err
		}
		return strings.TrimSpace(line), nil
	}
	if len(args) == 0 {
		fmt.Fprintln(out, installUsage)
		return errors.New("humagen install needs a kind: github or slack")
	}
	switch args[0] {
	case "help", "-h", "-help", "--help":
		fmt.Fprintln(out, installUsage)
		return flag.ErrHelp
	case "github":
		return i.github(args[1:])
	case "slack":
		return i.slack(o, args[1:])
	}
	return errors.New("unknown install kind; choose github or slack")
}

// openBrowser shows url in the person's browser.
func openBrowser(target string) error {
	switch goruntime.GOOS {
	case "darwin":
		return exec.Command("open", target).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
	}
	return exec.Command("xdg-open", target).Start()
}

// keychainSafe is a value that can be given to security(1) as one word.
var keychainSafe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// keepInKeychain writes value under service and account and returns the
// reference that reads it back (credref.Keychain). The value goes to the tool
// on its standard input, never in its arguments, where other processes could
// read it.
func keepInKeychain(ctx context.Context, service, account, value string) (string, error) {
	if !keychainSafe.MatchString(value) {
		return "", errors.New("the value has characters the keychain command cannot be given")
	}
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "security", "-i")
		cmd.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n", service, account, value))
	case "linux", "freebsd", "openbsd":
		cmd = exec.CommandContext(ctx, "secret-tool", "store", "--label", "Humagen "+account, "service", service, "account", account)
		cmd.Stdin = strings.NewReader(value)
	default:
		return "", fmt.Errorf("no keychain on %s", goruntime.GOOS)
	}
	if err := cmd.Run(); err != nil {
		return "", errors.New("the keychain could not be written")
	}
	return "keychain:" + service + "/" + account, nil
}

// randomHex is n random bytes as hex.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// page is what the person sees in the browser between steps. It never holds a credential.
var page = template.Must(template.New("page").Parse(`<!doctype html><meta charset="utf-8"><title>Humagen</title>
<body style="font:16px system-ui;max-width:32rem;margin:15vh auto;padding:0 1rem">
<h1 style="font-size:1.25rem">{{.Title}}</h1><p>{{.Text}}</p>
{{if .Action}}<form id="f" method="post" action="{{.Action}}"><input type="hidden" name="manifest" value="{{.Manifest}}"><button>Continue to GitHub</button></form><script>document.getElementById("f").submit()</script>{{end}}
</body>`))

type pageData struct{ Title, Text, Action, Manifest string }

var (
	githubCode = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)
	githubSlug = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
)

// githubApp is what GitHub answers when a manifest becomes an app.
type githubApp struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	HTMLURL       string `json:"html_url"`
	PEM           string `json:"pem"`
	WebhookSecret string `json:"webhook_secret"`
}

// github registers a GitHub App from a manifest, has the person install it,
// and connects that installation as a client. The app's key and webhook secret
// go from GitHub to this command to the server, and are never printed, shown
// in the browser or written to disk here.
func (i installer) github(args []string) error {
	fs := flag.NewFlagSet("install github", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	name := fs.String("name", "github", "client name in Humagen")
	agent := fs.String("agent", "code-reviewer", "agent to answer; empty uses the workspace default")
	server := fs.String("server", or(os.Getenv("HUMAGEN_URL"), "http://localhost:8080"), "Humagen API URL (HUMAGEN_URL)")
	token := fs.String("token", "env:HUMAGEN_TOKEN", "API bearer token reference; empty disables authentication")
	appName := fs.String("app-name", "", "the app's name on GitHub, which must be unused there. Empty means Humagen and a few random letters; GitHub lets you change it before you confirm")
	org := fs.String("org", "", "register the app under this organization instead of your own account")
	hook := fs.String("webhook-url", "", "public https URL that reaches this server's /connectors/github (required)")
	force := fs.Bool("force", false, "update the client with this name")
	quiet := fs.Bool("no-browser", false, "print the link instead of opening it")
	var repos []string
	fs.Func("repo", "repository owner/name another surface may read; repeatable", func(v string) error { repos = append(repos, v); return nil })
	fs.Usage = func() {
		fs.SetOutput(i.out)
		fmt.Fprintln(i.out, "usage: humagen install github --webhook-url URL [flags]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	explicit := false
	fs.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "token" })
	h, err := url.Parse(*hook)
	if err != nil || h.Scheme != "https" || h.Host == "" || h.User != nil {
		return errors.New("--webhook-url is required: the public https URL that reaches this server's /connectors/github, which GitHub sends events to (docs/guides/github.md#a-public-address)")
	}
	if *org != "" && !githubSlug.MatchString(*org) {
		return errors.New("--org is a GitHub organization's login")
	}
	for _, repo := range repos {
		if !createRepo.MatchString(repo) {
			return errors.New("--repo must be owner/name")
		}
	}
	if *appName == "" {
		suffix, err := randomHex(3)
		if err != nil {
			return err
		}
		*appName = "Humagen " + suffix
	}

	ctx, cancel := context.WithTimeout(context.Background(), i.wait)
	defer cancel()
	request, err := apiCaller(ctx, strings.TrimRight(*server, "/"), *token, explicit)
	if err != nil {
		return err
	}
	// Ask the server first, so nothing is registered at GitHub that it would then refuse.
	method, path, err := clientTarget(request, *name, "github", *force)
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	here := "http://" + ln.Addr().String()
	state, err := randomHex(16)
	if err != nil {
		return err
	}
	manifest, err := json.Marshal(map[string]any{
		"name": *appName, "url": "https://github.com/urmzd/humagen", "public": false,
		"description":     "Reviews pull requests and answers on issues, through Humagen.",
		"hook_attributes": map[string]any{"url": *hook, "active": true},
		"redirect_url":    here + "/callback", "setup_url": here + "/installed",
		"default_permissions": map[string]string{"issues": "write", "pull_requests": "write", "checks": "write", "contents": "read", "metadata": "read", "actions": "read"},
		"default_events":      []string{"issues", "issue_comment", "pull_request", "pull_request_review", "pull_request_review_comment"},
	})
	if err != nil {
		return err
	}
	register := i.githubWeb + "/settings/apps/new?state=" + state
	if *org != "" {
		register = i.githubWeb + "/organizations/" + *org + "/settings/apps/new?state=" + state
	}

	type result struct {
		app          githubApp
		installation string
		owner        string
		err          error
	}
	done := make(chan result, 1)
	finish := func(r result) {
		select {
		case done <- r:
		default:
		}
	}
	// The browser asks one page after another, but nothing promises that.
	var mu sync.Mutex
	var app githubApp
	show := func(w http.ResponseWriter, status int, title, text string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = page.Execute(w, pageData{Title: title, Text: text})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = page.Execute(w, pageData{Title: "Registering the app with GitHub", Text: "GitHub asks you to confirm the app's name.", Action: register, Manifest: string(manifest)})
	})
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		code := r.URL.Query().Get("code")
		if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(state)) != 1 || !githubCode.MatchString(code) || app.ID != 0 {
			show(w, http.StatusBadRequest, "That link is not from this setup", "Run humagen install github again.")
			return
		}
		got, err := i.githubConvert(r.Context(), code)
		if err != nil {
			finish(result{err: err})
			show(w, http.StatusBadGateway, "GitHub did not finish registering the app", "Return to the terminal.")
			return
		}
		app = got
		http.Redirect(w, r, app.HTMLURL+"/installations/new", http.StatusFound)
	})
	mux.HandleFunc("GET /installed", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		id := r.URL.Query().Get("installation_id")
		if app.ID == 0 || !clients.ValidGitHubID(id) {
			show(w, http.StatusBadRequest, "That link is not from this setup", "Run humagen install github again.")
			return
		}
		// The address bar is not evidence: GitHub is asked, as the app, whose installation this is.
		owner, err := i.githubInstallationOwner(r.Context(), app, id)
		if err != nil {
			show(w, http.StatusBadRequest, "GitHub does not know that installation of this app", "Install the app from the page GitHub opened, then return here.")
			return
		}
		finish(result{app: app, installation: id, owner: owner})
		show(w, http.StatusOK, "GitHub is connected", "Return to the terminal. You can close this tab.")
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	fmt.Fprintf(i.out, "Opening GitHub to register %q. Confirm its name, then choose the repositories it may work in.\n", *appName)
	if *quiet || i.open(here+"/") != nil {
		fmt.Fprintf(i.out, "Open this link in your browser: %s/\n", here)
	}
	var r result
	select {
	case r = <-done:
	case <-ctx.Done():
		return errors.New("timed out waiting for GitHub; nothing was connected. If the app was registered, delete it on GitHub before running this again")
	}
	if r.err != nil {
		return r.err
	}

	body := createClientBody{Name: *name, Surface: clients.GitHub, DefaultAgent: *agent}
	body.Identity = clients.Identity{AppID: fmt.Sprint(r.app.ID), Installation: r.installation, Owners: []string{r.owner}}
	body.GitHub = &clients.GitHubOptions{ReadRepositories: repos, Login: r.app.Slug + "[bot]"}
	body.Credentials.Token = clients.SecretInput{Value: r.app.PEM}
	body.Credentials.SigningSecret = clients.SecretInput{Value: r.app.WebhookSecret}
	raw, err := request(method, path, body)
	if err != nil {
		return fmt.Errorf("the app %s is registered and installed, but Humagen did not take it: %w. Delete the app on GitHub (its Advanced settings) before running this again; its key was not kept", r.app.HTMLURL, err)
	}
	var created clients.Client
	if err := json.Unmarshal(raw, &created); err != nil {
		return fmt.Errorf("decode client: %w", err)
	}
	answer := "the workspace default"
	if created.DefaultAgent != "" {
		answer = "@" + created.DefaultAgent
	}
	next := "mention @" + r.app.Slug + " on a pull request in " + r.owner
	if title := storePackageFor(request, created.DefaultAgent); title != "" {
		next = "install the " + title + " package from the Store, then " + next
	}
	fmt.Fprintf(i.out, "Connected GitHub App %s for %s as client %s (id %s), answered by %s\nNext: %s\n", r.app.HTMLURL, r.owner, created.Name, created.ID, answer, next)
	return nil
}

// clientTarget is the call that creates the client called name on surface, or
// with force updates the one there is.
func clientTarget(request func(method, path string, body any) (json.RawMessage, error), name, surface string, force bool) (method, path string, err error) {
	raw, err := request(http.MethodGet, "/api/clients", nil)
	if err != nil {
		return "", "", err
	}
	var list struct {
		Clients []clients.Client `json:"clients"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return "", "", fmt.Errorf("decode clients: %w", err)
	}
	for _, old := range list.Clients {
		if old.Name != name || string(old.Surface) != surface {
			continue
		}
		if !force {
			return "", "", fmt.Errorf("client %s exists; pass --force to update it, or --name for another", name)
		}
		if old.ID == "" {
			return "", "", errors.New("existing client has no id")
		}
		return http.MethodPut, "/api/clients/" + url.PathEscape(old.ID), nil
	}
	return http.MethodPost, "/api/clients", nil
}

// githubJSON calls GitHub's API and decodes its answer into v.
func (i installer) githubJSON(ctx context.Context, method, path, bearer string, v any) error {
	req, err := http.NewRequestWithContext(ctx, method, i.githubAPI+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := i.http.Do(req)
	if err != nil {
		return errors.New("could not reach GitHub")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("an answer of HTTP %d from GitHub", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v)
}

// githubConvert trades the code GitHub sent back for the app it registered.
func (i installer) githubConvert(ctx context.Context, code string) (githubApp, error) {
	var app githubApp
	if err := i.githubJSON(ctx, http.MethodPost, "/app-manifests/"+code+"/conversions", "", &app); err != nil {
		return githubApp{}, fmt.Errorf("registering the app: %w", err)
	}
	// Its page is where the person is sent next, so it must be GitHub's own.
	if app.ID <= 0 || !githubSlug.MatchString(app.Slug) || !githubapi.IsPrivateKeyPEM(app.PEM) || app.WebhookSecret == "" || !strings.HasPrefix(app.HTMLURL, i.githubWeb+"/") {
		return githubApp{}, errors.New("registering the app: GitHub's answer is not an app")
	}
	return app, nil
}

// githubInstallationOwner is the account an installation of app is on, asked of GitHub as the app.
func (i installer) githubInstallationOwner(ctx context.Context, app githubApp, id string) (string, error) {
	key, err := githubapi.ParsePrivateKeyPEM(app.PEM)
	if err != nil {
		return "", err
	}
	jwt, err := githubapi.SignAppJWT(fmt.Sprint(app.ID), key, time.Now())
	if err != nil {
		return "", err
	}
	var got struct {
		Account struct {
			Login string `json:"login"`
		} `json:"account"`
	}
	if err := i.githubJSON(ctx, http.MethodGet, "/app/installations/"+id, jwt, &got); err != nil {
		return "", err
	}
	if !githubSlug.MatchString(got.Account.Login) {
		return "", errors.New("no account named by GitHub for the installation")
	}
	return got.Account.Login, nil
}

var (
	slackBotToken = regexp.MustCompile(`^xoxb-[A-Za-z0-9-]{10,250}$`)
	slackAppToken = regexp.MustCompile(`^xapp-[A-Za-z0-9-]{10,250}$`)
)

// slackCall calls one Slack Web API method as token and decodes its answer.
func (i installer) slackCall(ctx context.Context, method, token string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, i.slackAPI+"/"+method, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := i.http.Do(req)
	if err != nil {
		return errors.New("could not reach Slack")
	}
	defer resp.Body.Close()
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v)
}

// slack opens Slack's own page with Humagen's manifest filled in, takes the
// two tokens Slack then shows, checks each with Slack, keeps them in the
// keychain and writes where they are into the spec. Slack has no way to hand
// a token to a program, so the person copies two values and nothing else.
func (i installer) slack(o Options, args []string) error {
	fs := flag.NewFlagSet("install slack", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("o", "", "the spec file to write. Empty means humagen.yaml in HUMAGEN_HOME, else the user config directory")
	name := fs.String("name", "", `the app's name in Slack. Empty means the spec's, else "Humagen"`)
	store := fs.String("store", "keychain", "where the tokens are kept: keychain, or env to export SLACK_BOT_TOKEN and SLACK_APP_TOKEN yourself")
	quiet := fs.Bool("no-browser", false, "print the link instead of opening it")
	fs.Usage = func() {
		fs.SetOutput(i.out)
		fmt.Fprintln(i.out, "usage: humagen install slack [flags]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *store != "keychain" && *store != "env" {
		return errors.New("--store is keychain or env")
	}
	if *path == "" {
		p, err := specPath(o.notes)
		if err != nil {
			return err
		}
		*path = p
	}
	cfg, err := openSpec(*path)
	if err != nil {
		return err
	}
	sys, plat := &cfg.System, &cfg.Platforms[0]
	if sys.Spec.Auth.Kind == "tailscale" {
		return errors.New("the Slack app cannot sign in to Humagen through Tailscale Serve; use oidc, or dev on one machine")
	}
	sl := plat.Spec.Surfaces.Slack
	if sl == nil {
		sl = &spec.Slack{}
	}
	if *name == "" {
		*name = or(sl.AppName, or(plat.Metadata.DisplayName, or(sys.Metadata.DisplayName, "Humagen")))
	}
	manifest, err := json.Marshal(slackManifest(*name, slackCopy{Description: sl.Description, BackgroundColor: sl.BackgroundColor}))
	if err != nil {
		return err
	}
	link := i.slackWeb + "/apps?new_app=1&manifest_json=" + url.QueryEscape(string(manifest))

	fmt.Fprintf(i.out, `Opening Slack to create %q, with everything it needs already filled in.
  1. Pick your workspace, then Next and Create.
  2. Install to Workspace, then Allow.
  3. OAuth & Permissions: copy the Bot User OAuth Token.
  4. Basic Information, App-Level Tokens: Generate one with the scope connections:write, and copy it.
`, *name)
	if *quiet || i.open(link) != nil {
		fmt.Fprintf(i.out, "Open this link in your browser: %s\n", link)
	}

	ctx, cancel := context.WithTimeout(context.Background(), i.wait)
	defer cancel()
	bot, err := i.secret("Bot token (xoxb-): ")
	if err != nil {
		return err
	}
	if !slackBotToken.MatchString(bot) {
		return errors.New("that is not a bot token: it starts with xoxb-")
	}
	var who struct {
		OK     bool   `json:"ok"`
		Error  string `json:"error"`
		Team   string `json:"team"`
		TeamID string `json:"team_id"`
	}
	if err := i.slackCall(ctx, "auth.test", bot, &who); err != nil {
		return err
	}
	if !who.OK || !clients.ValidSlackID(who.TeamID) {
		return fmt.Errorf("the bot token was refused by Slack (%s)", or(who.Error, "no workspace named"))
	}
	app, err := i.secret("App-level token (xapp-): ")
	if err != nil {
		return err
	}
	if !slackAppToken.MatchString(app) {
		return errors.New("that is not an app-level token: it starts with xapp-")
	}
	var socket struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := i.slackCall(ctx, "apps.connections.open", app, &socket); err != nil {
		return err
	}
	if !socket.OK {
		return fmt.Errorf("the app-level token was refused by Slack (%s); it needs the scope connections:write", or(socket.Error, "no reason given"))
	}

	botRef, appRef := "env:SLACK_BOT_TOKEN", "env:SLACK_APP_TOKEN"
	if *store == "keychain" {
		// One workspace's tokens per account, so a second workspace does not replace the first.
		if botRef, err = i.keep(ctx, "humagen", "slack-bot-"+who.TeamID, bot); err == nil {
			appRef, err = i.keep(ctx, "humagen", "slack-app-"+who.TeamID, app)
		}
		if err != nil {
			return fmt.Errorf("%w; run again with --store env to export the tokens yourself", err)
		}
	}
	for _, ref := range []string{botRef, appRef} {
		if _, err := credref.Parse(ref); err != nil {
			return err
		}
	}
	sl.Enabled, sl.Team, sl.AppName = true, who.TeamID, *name
	sl.BotToken, sl.AppToken = &spec.Ref{Ref: botRef}, &spec.Ref{Ref: appRef}
	plat.Spec.Surfaces.Slack = sl
	if err := cfg.Check(nil); err != nil {
		return err
	}
	if err := saveSpec(cfg, *path); err != nil {
		return err
	}
	fmt.Fprintf(i.out, "Slack workspace %s (%s) is set up in %s. The tokens are at %s and %s.\n", or(who.Team, who.TeamID), who.TeamID, *path, botRef, appRef)
	if *store == "env" {
		fmt.Fprintln(i.out, "Export SLACK_BOT_TOKEN and SLACK_APP_TOKEN where Humagen runs.")
	}
	if err := sl.CheckSignIn(sys.Spec.Auth.Kind); err != nil {
		fmt.Fprintf(i.out, "One thing is left: %v. `humagen setup` asks for it.\n", err)
	}
	fmt.Fprintf(i.out, "Next: run `humagen start`, then message %s in Slack.\n", *name)
	return nil
}
