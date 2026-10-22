package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/spec"
)

// setupCmd is `humagen setup`: the first-run questions on their own.
func setupCmd(o Options, args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	path := fs.String("o", "", "the spec file to write. Empty means humagen.yaml in HUMAGEN_HOME, else the user config directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		p, err := specPath(o.notes)
		if err != nil {
			return err
		}
		*path = p
	}
	return runSetup(bufio.NewReader(in), out, *path)
}

// modelPresets are the model providers setup offers by name.
var modelPresets = []struct {
	label, kind, name, baseURL, key string
}{
	{"Anthropic", "anthropic", "anthropic", "", "env:ANTHROPIC_API_KEY"},
	{"OpenAI", "openai", "openai", "", "env:OPENAI_API_KEY"},
	{"Ollama on this machine (no key)", "ollama", "ollama", "", ""},
	{"Another OpenAI-compatible server", "openai", "custom", "http://127.0.0.1:8000/v1", ""},
}

// runSetup asks for the deployment's choices and writes them to path as a
// System and a Platform document, keeping what is already there as each
// question's default. It never asks for a key, only where one is kept.
func runSetup(r *bufio.Reader, out io.Writer, path string) error {
	var cfg spec.Config
	if _, err := os.Stat(path); err == nil {
		if cfg, err = spec.Load(path); err != nil {
			return err
		}
	}
	cfg.System.Defaults()
	if len(cfg.Platforms) == 0 {
		cfg.Platforms = []spec.Platform{{Metadata: spec.Metadata{Name: cfg.System.Tenant()}}}
		if cfg.Platforms[0].Metadata.Name == "" {
			cfg.Platforms[0].Metadata.Name = "default"
		}
	}
	sys, plat := &cfg.System, &cfg.Platforms[0]
	q := asker{r: r, out: out}
	fmt.Fprintln(out, "Setting up Humagen. Press Enter to take the default in brackets. Nothing here asks for a key; you name where each one is kept.")

	sys.Metadata.DisplayName = q.text("What should people see it called?", sys.Metadata.DisplayName, nonEmpty)
	kind := q.choose("Who may sign in? dev (this machine only, no sign-in), tailscale or oidc", sys.Spec.Auth.Kind, "dev", "tailscale", "oidc")
	if kind != sys.Spec.Auth.Kind {
		sys.Spec.Auth.Slot = spec.Slot{Kind: kind}
	}
	if kind == "oidc" {
		b, _ := spec.Block[spec.OIDC](sys.Spec.Auth.Slot)
		b.Issuer = q.text("Issuer URL", b.Issuer, nonEmpty)
		b.ClientID = q.text("Client id", or(b.ClientID, "humagen"), nonEmpty)
		secret := ""
		if b.ClientSecret.Set() {
			secret = b.ClientSecret.Ref
		}
		b.ClientSecret = &spec.Ref{Ref: q.text("Where is the client secret?", or(secret, "env:HUMAGEN_OIDC_CLIENT_SECRET"), parseRef)}
		b.Audience = q.text("Audience of the API's access tokens", or(b.Audience, "humagen"), nonEmpty)
		sys.Spec.Auth.Block = spec.MustJSON(b)
	}

	have := map[string]bool{}
	for _, m := range plat.Spec.Models {
		have[m.Name] = true
	}
	if len(have) > 0 {
		fmt.Fprintf(out, "Model providers already declared: %d\n", len(have))
	}
	for q.yes("Add a model provider?", len(plat.Spec.Models) == 0) {
		for i, p := range modelPresets {
			fmt.Fprintf(out, "  %d. %s\n", i+1, p.label)
		}
		pr := modelPresets[q.number("Which one", 1, len(modelPresets))-1]
		name := q.text("Name agents will use for it", pr.name, func(v string) error {
			if have[v] {
				return fmt.Errorf("%q is already declared", v)
			}
			return nil
		})
		blk := spec.ModelBlock{}
		if pr.baseURL != "" {
			blk.BaseURL = q.text("Base URL", pr.baseURL, nil)
		}
		if pr.kind != "ollama" {
			ref := q.text("Where is its key? env:NAME, keychain:SERVICE[/ACCOUNT], op://VAULT/ITEM/FIELD, or blank for none", pr.key, func(v string) error {
				if v == "" {
					return nil
				}
				return parseRef(v)
			})
			if ref != "" {
				blk.Key = &spec.Ref{Ref: ref}
			}
		}
		plat.Spec.Models = append(plat.Spec.Models, spec.Model{
			Slot: spec.Slot{Kind: pr.kind, Block: spec.MustJSON(blk)}, ModelShared: spec.ModelShared{Name: name},
		})
		have[name] = true
	}

	for q.yes("Add an MCP tool provider?", false) {
		b := spec.MCP{Transport: q.choose("Transport", "http", "http", "stdio")}
		name := q.text("Name", "", nonEmpty)
		if b.Transport == "http" {
			b.Endpoint = q.text("URL", "", nonEmpty)
		} else {
			b.Endpoint = q.text("Command", "", nonEmpty)
			if a := q.text("Arguments, space separated", "", nil); a != "" {
				b.Args = strings.Fields(a)
			}
		}
		if ref := q.text("Where is its credential? (blank for none)", "", func(v string) error {
			if v == "" {
				return nil
			}
			return parseRef(v)
		}); ref != "" {
			b.Credential = &spec.Ref{Ref: ref}
			def := ""
			if b.Transport == "stdio" {
				def = "API_KEY"
			}
			b.CredentialAs = q.text("Sent as which variable or header (blank for a bearer token)", def, nil)
		}
		plat.Spec.Tools = append(plat.Spec.Tools, spec.Tool{Slot: spec.Slot{Kind: "mcp", Block: spec.MustJSON(b)}, ToolShared: spec.ToolShared{Name: name}})
		fmt.Fprintln(out, "  Its tools start pending. Humagen logs each one with its schema hash; add the ones you accept under accept.")
	}

	sl := plat.Spec.Surfaces.Slack
	switch {
	case kind == "tailscale":
		// The Slack app calls humagen's API, and a process cannot sign in
		// through Tailscale Serve.
		if sl != nil && sl.Enabled {
			fmt.Fprintln(out, "Slack is off: its app cannot sign in to Humagen through Tailscale Serve.")
			sl.Enabled = false
		}
	case q.yes("Answer in Slack too?", sl != nil && sl.Enabled):
		if sl == nil {
			sl = &spec.Slack{BotToken: &spec.Ref{Ref: "env:SLACK_BOT_TOKEN"}, AppToken: &spec.Ref{Ref: "env:SLACK_APP_TOKEN"}}
		}
		sl.Enabled = true
		fmt.Fprintln(out, "  Create the app from `humagen slack manifest`, install it, and make an app-level token with connections:write.")
		sl.Team = q.text("Slack team id (T...)", sl.Team, nonEmpty)
		sl.BotToken = &spec.Ref{Ref: q.text("Where is the bot token (xoxb-)?", sl.BotToken.Ref, parseRef)}
		sl.AppToken = &spec.Ref{Ref: q.text("Where is the app token (xapp-)?", sl.AppToken.Ref, parseRef)}
		if kind == "oidc" && !sl.HumagenToken.Set() {
			fmt.Fprintln(out, "  The app signs in to Humagen as a machine client of the identity provider, with client credentials.")
			c := sl.HumagenClient
			if c == nil {
				c = &spec.ClientCredentials{}
			}
			c.TokenURL = q.text("Its token URL", c.TokenURL, httpURL)
			c.ClientID = q.text("Its client id", or(c.ClientID, "humagen-slack"), nonEmpty)
			secret := "env:HUMAGEN_SLACK_CLIENT_SECRET"
			if c.ClientSecret.Set() {
				secret = c.ClientSecret.Ref
			}
			c.ClientSecret = &spec.Ref{Ref: q.text("Where is its client secret?", secret, parseRef)}
			sl.HumagenClient = c
		}
		plat.Spec.Surfaces.Slack = sl
	case sl != nil:
		sl.Enabled = false
	}
	if q.err != nil {
		return q.err
	}
	if err := cfg.Check(nil); err != nil {
		return err
	}
	// A spec read in the language from before the rename is written back in
	// it, whatever its name, so that release still reads it.
	write := spec.Marshal
	if cfg.Old {
		write = spec.MarshalOld
	}
	b, err := write(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	fmt.Fprintf(out, "Wrote %s. Edit it any time (docs/spec.md); `humagen setup` asks again.\n", path)
	return nil
}

func parseRef(v string) error { _, err := credref.Parse(v); return err }

func httpURL(v string) error {
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("an http or https URL")
	}
	return nil
}

func nonEmpty(v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("required")
	}
	return nil
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// asker reads answers one line at a time. At the end of input every question
// takes its default, so a script can pipe nothing and get the defaults.
type asker struct {
	r   *bufio.Reader
	out io.Writer
	eof bool
	err error
}

func (q *asker) line(prompt string) string {
	fmt.Fprint(q.out, prompt)
	if q.eof {
		fmt.Fprintln(q.out)
		return ""
	}
	s, err := q.r.ReadString('\n')
	if err == io.EOF {
		q.eof = true
		if s == "" {
			fmt.Fprintln(q.out)
		}
	} else if err != nil {
		q.eof, q.err = true, err
	}
	return strings.TrimSpace(s)
}

func (q *asker) text(prompt, def string, check func(string) error) string {
	for {
		v := q.line(fmt.Sprintf("%s [%s]: ", prompt, def))
		if v == "" {
			v = def
		}
		if check == nil {
			return v
		}
		err := check(v)
		if err == nil {
			return v
		}
		fmt.Fprintf(q.out, "  %v\n", err)
		if q.eof {
			q.err = fmt.Errorf("%s: %v", prompt, err)
			return v
		}
	}
}

func (q *asker) choose(prompt, def string, options ...string) string {
	return q.text(prompt, def, func(v string) error {
		for _, o := range options {
			if v == o {
				return nil
			}
		}
		return fmt.Errorf("one of %s", strings.Join(options, ", "))
	})
}

func (q *asker) yes(prompt string, def bool) bool {
	d := "y/N"
	if def {
		d = "Y/n"
	}
	for {
		switch strings.ToLower(q.line(fmt.Sprintf("%s [%s]: ", prompt, d))) {
		case "":
			return def
		case "y", "yes":
			return true
		case "n", "no":
			return false
		}
	}
}

func (q *asker) number(prompt string, lo, hi int) int {
	var n int
	q.text(prompt, fmt.Sprint(lo), func(v string) error {
		if _, err := fmt.Sscan(v, &n); err != nil || n < lo || n > hi {
			return fmt.Errorf("a number from %d to %d", lo, hi)
		}
		return nil
	})
	if n < lo || n > hi {
		n = lo
	}
	return n
}

func decodeStrict(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
