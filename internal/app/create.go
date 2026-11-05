package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/credref"
)

const createUsage = `usage: humagen create KIND [flags]

Kinds:
  github  Connect a GitHub App installation (default agent: code-reviewer)
  slack   Connect a Slack Events API app (default agent: workspace default)

Common flags: --server URL, --token REF, --name NAME, --agent NAME, --force, --json
Run humagen create KIND --help for kind-specific flags.`

type createOptions struct {
	kind, name, agent, server, token                               string
	appID, privateKey, installation, botToken, signingSecret, team string
	repos, owners                                                  []string
	force, json, explicitToken                                     bool
}

var createRepo = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func parseCreate(args []string, out io.Writer) (createOptions, error) {
	var c createOptions
	if len(args) == 0 {
		return c, errors.New(createUsage)
	}
	switch args[0] {
	case "help", "-h", "-help", "--help":
		if _, err := fmt.Fprintln(out, createUsage); err != nil {
			return c, err
		}
		return c, flag.ErrHelp
	case "github", "slack":
		c.kind = args[0]
	default:
		return c, errors.New("unknown create kind; choose github or slack")
	}
	fs := flag.NewFlagSet("create "+c.kind, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&c.name, "name", c.kind, "client name")
	defaultAgent := ""
	if c.kind == "github" {
		defaultAgent = "code-reviewer"
	}
	fs.StringVar(&c.agent, "agent", defaultAgent, "agent to answer messages; empty uses the workspace default")
	fs.StringVar(&c.server, "server", or(os.Getenv("HUMAGEN_URL"), "http://localhost:8080"), "Humagen API URL (HUMAGEN_URL)")
	fs.StringVar(&c.token, "token", "env:HUMAGEN_TOKEN", "API bearer token reference; empty disables authentication")
	fs.BoolVar(&c.force, "force", false, "update the client with this name and surface")
	fs.BoolVar(&c.json, "json", false, "print the API response as JSON")
	fs.StringVar(&c.appID, "app-id", "", "app id")
	if c.kind == "github" {
		fs.StringVar(&c.privateKey, "private-key", "", "private key reference: env:, keychain: or op:// (required)")
		fs.StringVar(&c.installation, "installation", "", "GitHub installation id (required)")
		fs.StringVar(&c.signingSecret, "webhook-secret", "", "webhook secret reference (required)")
		fs.Func("repo", "repository owner/name; repeatable", func(v string) error { c.repos = append(c.repos, v); return nil })
		fs.Func("owner", "GitHub account; repeatable; defaults to repository owners", func(v string) error { c.owners = append(c.owners, v); return nil })
	} else {
		fs.StringVar(&c.team, "team", "", "Slack team id (required)")
		fs.StringVar(&c.botToken, "bot-token", "", "bot token reference: env:, keychain: or op:// (required)")
		fs.StringVar(&c.signingSecret, "signing-secret", "", "Slack signing secret reference (required)")
	}
	fs.Usage = func() {
		fs.SetOutput(out)
		fmt.Fprintf(out, "usage: humagen create %s [flags]\n", c.kind)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args[1:]); err != nil {
		return c, err
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "token" {
			c.explicitToken = true
		}
	})
	if fs.NArg() != 0 {
		return c, errors.New("unexpected arguments; use humagen create " + c.kind + " --help")
	}
	if strings.TrimSpace(c.name) == "" || len(c.name) > 80 || strings.ContainsAny(c.name, "\r\n") {
		return c, errors.New("--name must be non-empty and at most 80 characters without newlines")
	}
	required := []struct{ name, value string }{{"team", c.team}, {"bot-token", c.botToken}, {"signing-secret", c.signingSecret}}
	if c.kind == "github" {
		required = []struct{ name, value string }{{"app-id", c.appID}, {"installation", c.installation}, {"private-key", c.privateKey}, {"webhook-secret", c.signingSecret}}
	}
	for _, f := range required {
		if strings.TrimSpace(f.value) == "" {
			return c, fmt.Errorf("--%s is required", f.name)
		}
	}
	if c.kind == "github" {
		for _, f := range required[:2] {
			if !clients.ValidGitHubID(f.value) {
				return c, fmt.Errorf("--%s must contain 1 to 20 digits", f.name)
			}
		}
	} else {
		if !clients.ValidSlackID(c.team) {
			return c, errors.New("--team must match ^[A-Z][A-Z0-9]{2,31}$ (for example T0123)")
		}
		if c.appID != "" && (!strings.HasPrefix(c.appID, "A") || !clients.ValidSlackID(c.appID)) {
			return c, errors.New("--app-id must match ^A[A-Z0-9]{2,31}$ (for example A0123)")
		}
	}
	secretName := "signing-secret"
	if c.kind == "github" {
		secretName = "webhook-secret"
	}
	for _, f := range []struct {
		name  string
		value *string
	}{{"private-key", &c.privateKey}, {"bot-token", &c.botToken}, {secretName, &c.signingSecret}, {"token", &c.token}} {
		if *f.value == "" {
			continue
		}
		ref, err := credref.Parse(*f.value)
		if err != nil {
			return c, fmt.Errorf("--%s must be a credential reference; use env:NAME, keychain:SERVICE/ACCOUNT or op://VAULT/ITEM/FIELD", f.name)
		}
		*f.value = ref.Raw
	}
	for _, repo := range c.repos {
		if !createRepo.MatchString(repo) {
			return c, errors.New("--repo must be owner/name")
		}
		owner, name, _ := strings.Cut(repo, "/")
		if owner == "." || owner == ".." || name == "." || name == ".." {
			return c, errors.New("--repo must be owner/name")
		}
	}
	if c.kind == "github" && len(c.owners) == 0 {
		for _, repo := range c.repos {
			owner, _, _ := strings.Cut(repo, "/")
			if !slices.Contains(c.owners, owner) {
				c.owners = append(c.owners, owner)
			}
		}
	}
	if c.kind == "github" && len(c.owners) == 0 {
		return c, errors.New("--owner is required (or provide --repo owner/name)")
	}
	u, err := url.Parse(c.server)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return c, errors.New("--server must be an http or https URL without credentials, query or fragment")
	}
	c.server = strings.TrimRight(c.server, "/")
	return c, nil
}

// createClientBody follows the API's ClientInput contract. Credential values
// are never resolved here: the server owns their interpretation and access.
type createClientBody struct {
	Name         string                 `json:"name"`
	Surface      clients.Surface        `json:"surface"`
	Identity     clients.Identity       `json:"identity"`
	DefaultAgent string                 `json:"default_agent"`
	Slack        *clients.SlackOptions  `json:"slack,omitempty"`
	GitHub       *clients.GitHubOptions `json:"github,omitempty"`
	Credentials  struct {
		Token         clients.SecretInput `json:"token"`
		SigningSecret clients.SecretInput `json:"signing_secret,omitempty"`
	} `json:"credentials"`
}

type createAPIError struct {
	method, path string
	status       int
	body         string
}

func (e *createAPIError) Error() string {
	return fmt.Sprintf("%s %s: HTTP %d: %s", e.method, e.path, e.status, e.body)
}

// secretRefHint uses only names supplied by the caller. The server intentionally
// does not disclose which reference failed or whether its variable is set.
func (c createOptions) secretRefHint(tenant string) error {
	var names []string
	for _, raw := range []string{c.privateKey, c.botToken, c.signingSecret} {
		ref, err := credref.Parse(raw)
		if err == nil && ref.Scheme == credref.Env && !slices.Contains(names, ref.Name) {
			names = append(names, ref.Name)
		}
	}
	if len(names) == 0 {
		return errors.New("the server cannot use these credential references; use env:HUMAGEN_* references granted to this workspace with -secret-refs (or spec.secret_env in the System document)")
	}
	if tenant == "" || strings.ContainsAny(tenant, "\";=\r\n") {
		tenant = "<tenant>"
	}
	return fmt.Errorf("the server does not let this workspace read %s; start it with -secret-refs \"%s=%s\" (or set spec.secret_env in the System document); ensure these variables are set on the server", strings.Join(names, ", "), tenant, strings.Join(names, ","))
}

func createCmd(args []string, out io.Writer) error {
	c, err := parseCreate(args, out)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	token := ""
	if c.token != "" {
		// An unset default token is the dev-mode path. An explicit reference
		// must resolve, so a typo cannot silently become an anonymous request.
		_, present := os.LookupEnv("HUMAGEN_TOKEN")
		if c.explicitToken || c.token != "env:HUMAGEN_TOKEN" || (present && strings.TrimSpace(os.Getenv("HUMAGEN_TOKEN")) != "") {
			token, err = credref.Default.Resolve(ctx, c.token)
			if err != nil {
				return fmt.Errorf("resolve --token: %w", err)
			}
		}
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request := func(method, path string, body any) (json.RawMessage, error) {
		var data []byte
		if body != nil {
			data, err = json.Marshal(body)
			if err != nil {
				return nil, err
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, c.server+path, bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", method, path, err)
		}
		defer resp.Body.Close()
		data, err = io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
		if err != nil {
			return nil, err
		}
		if len(data) > 1<<20 {
			return nil, errors.New("client API response exceeds 1 MiB")
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, &createAPIError{method: method, path: path, status: resp.StatusCode, body: strings.Join(strings.Fields(string(data)), " ")}
		}
		if !json.Valid(data) {
			return nil, errors.New("client API returned invalid JSON")
		}
		return json.RawMessage(data), nil
	}
	raw, err := request(http.MethodGet, "/api/clients", nil)
	if err != nil {
		return err
	}
	var list struct {
		Clients []clients.Client `json:"clients"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return fmt.Errorf("decode clients: %w", err)
	}
	method, path, verb := http.MethodPost, "/api/clients", "Created"
	for _, old := range list.Clients {
		if old.Name != c.name || string(old.Surface) != c.kind {
			continue
		}
		if !c.force {
			return fmt.Errorf("client %s exists; pass --force to update it", c.name)
		}
		if old.ID == "" {
			return errors.New("existing client has no id")
		}
		method, path, verb = http.MethodPut, "/api/clients/"+url.PathEscape(old.ID), "Updated"
		break
	}
	body := createClientBody{Name: c.name, Surface: clients.Surface(c.kind), Identity: clients.Identity{AppID: c.appID}, DefaultAgent: c.agent}
	body.Credentials.SigningSecret = clients.SecretInput{Ref: c.signingSecret}
	if c.kind == "github" {
		body.Identity.Installation, body.Identity.Owners = c.installation, c.owners
		body.GitHub = &clients.GitHubOptions{ReadRepositories: c.repos}
		body.Credentials.Token = clients.SecretInput{Ref: c.privateKey}
	} else {
		body.Identity.Team = c.team
		body.Slack = &clients.SlackOptions{}
		body.Credentials.Token = clients.SecretInput{Ref: c.botToken}
	}
	raw, err = request(method, path, body)
	if err != nil {
		var apiErr *createAPIError
		if errors.As(err, &apiErr) && apiErr.status == http.StatusBadRequest && strings.Contains(apiErr.body, "that secret reference cannot be used by this workspace") {
			// Read the authenticated principal rather than guessing its tenant
			// from local configuration or decoding an unverified token.
			var me struct {
				Principal struct {
					Tenant string `json:"tenant"`
				} `json:"principal"`
			}
			if raw, meErr := request(http.MethodGet, "/api/me", nil); meErr == nil {
				_ = json.Unmarshal(raw, &me)
			}
			return c.secretRefHint(me.Principal.Tenant)
		}
		return err
	}
	if c.json {
		_, err = fmt.Fprintln(out, string(raw))
		return err
	}
	var created clients.Client
	if err := json.Unmarshal(raw, &created); err != nil {
		return fmt.Errorf("decode client: %w", err)
	}
	answer := "the workspace default"
	next := "mention the app in Slack"
	if created.DefaultAgent != "" {
		answer = "@" + created.DefaultAgent
		next = "mention " + answer + " in Slack"
	}
	if c.kind == "github" {
		next = "mention " + answer + " on a pull request"
	}
	_, err = fmt.Fprintf(out, "%s %s client %s (id %s), answered by %s\nNext: %s\n", verb, c.kind, created.Name, created.ID, answer, next)
	return err
}
