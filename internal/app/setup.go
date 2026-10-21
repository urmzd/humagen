package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/urmzd/warp/pkg/credref"
)

// setupCmd is `warp setup`: the first-run questions on their own.
func setupCmd(args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	dir := fs.String("config", "", "directory to write warp.json and providers.json in. Empty means WARP_HOME, else the user config directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		*dir = configDir(os.Getenv)
	}
	_, err := runSetup(bufio.NewReader(in), out, filepath.Join(*dir, "warp.json"), filepath.Join(*dir, "providers.json"))
	return err
}

// modelPresets are the providers setup offers by name. Each is a providers
// file entry with its usual key reference filled in.
var modelPresets = []struct {
	label string
	entry ModelProvider
}{
	{"Anthropic", ModelProvider{Name: "anthropic", Kind: KindAnthropic, APIKeyRef: "env:ANTHROPIC_API_KEY"}},
	{"OpenAI", ModelProvider{Name: "openai", Kind: KindOpenAI, APIKeyRef: "env:OPENAI_API_KEY"}},
	{"Ollama on this machine (no key)", ModelProvider{Name: "ollama", Kind: KindOpenAI, BaseURL: "http://127.0.0.1:11434/v1"}},
	{"Another OpenAI-compatible server", ModelProvider{Kind: KindOpenAI}},
}

// runSetup asks for the deployment's choices and writes them to the two
// files, keeping what is already there as each question's default. It never
// asks for a key: it asks where one is kept.
func runSetup(r *bufio.Reader, out io.Writer, startPath, providersPath string) (StartFile, error) {
	sf, _, err := ReadStartFile(startPath)
	if err != nil {
		return sf, err
	}
	pf, err := ReadProvidersFile(providersPath, true)
	if err != nil {
		return sf, err
	}
	q := asker{r: r, out: out}
	fmt.Fprintln(out, "Setting up warp. Press Enter to take the default in brackets. Nothing here asks for a key; you name where each one is kept.")

	sf.Auth = q.choose("Who may sign in? dev (this machine only, no sign-in), tailscale or oidc", or(sf.Auth, "dev"), "dev", "tailscale", "oidc")

	have := map[string]bool{}
	for _, m := range pf.Models {
		have[m.Name] = true
	}
	if len(pf.Models) > 0 {
		names := make([]string, 0, len(pf.Models))
		for _, m := range pf.Models {
			names = append(names, m.Name)
		}
		fmt.Fprintf(out, "Model providers already declared: %s\n", strings.Join(names, ", "))
	}
	for q.yes("Add a model provider?", len(pf.Models) == 0) {
		for i, p := range modelPresets {
			fmt.Fprintf(out, "  %d. %s\n", i+1, p.label)
		}
		pick := q.number("Which one", 1, len(modelPresets))
		m := modelPresets[pick-1].entry
		m.Name = q.text("Name agents will use for it", or(m.Name, "custom"), func(v string) error {
			if have[v] {
				return fmt.Errorf("%q is already declared", v)
			}
			if !providerName.MatchString(v) {
				return fmt.Errorf("letters, digits, dots, dashes and underscores")
			}
			return nil
		})
		if m.BaseURL != "" || pick == len(modelPresets) {
			m.BaseURL = q.text("Base URL (blank for the vendor's own)", m.BaseURL, nil)
		}
		m.APIKeyRef = q.text("Where is its key? env:NAME, keychain:SERVICE[/ACCOUNT], op://VAULT/ITEM/FIELD, or blank for none", m.APIKeyRef, func(v string) error {
			if v == "" {
				return nil
			}
			_, err := credref.Parse(v)
			return err
		})
		if err := (ProvidersFile{Models: []ModelProvider{m}}).Check(); err != nil {
			fmt.Fprintf(out, "  not added: %v\n", err)
			continue
		}
		pf.Models = append(pf.Models, m)
		have[m.Name] = true
	}

	for q.yes("Add an MCP tool provider?", false) {
		tp := ToolProvider{}
		tp.Name = q.text("Name", "", nil)
		tp.Transport = q.choose("Transport", "http", "http", "stdio")
		if tp.Transport == "http" {
			tp.Endpoint = q.text("URL", "", nil)
		} else {
			tp.Endpoint = q.text("Command", "", nil)
			if a := q.text("Arguments, space separated", "", nil); a != "" {
				tp.Args = strings.Fields(a)
			}
		}
		tp.CredentialRef = q.text("Where is its credential? (blank for none)", "", func(v string) error {
			if v == "" {
				return nil
			}
			_, err := credref.Parse(v)
			return err
		})
		if tp.CredentialRef != "" {
			def := ""
			if tp.Transport == "stdio" {
				def = "API_KEY"
			}
			tp.CredentialEnv = q.text("Sent as which variable or header (blank for a bearer token)", def, nil)
		}
		tp.Tools = []AcceptedTool{}
		if err := (ProvidersFile{ToolProviders: []ToolProvider{tp}}).Check(); err != nil {
			fmt.Fprintf(out, "  not added: %v\n", err)
			continue
		}
		pf.ToolProviders = append(pf.ToolProviders, tp)
		fmt.Fprintln(out, "  Its tools start pending. warp logs each one with its schema hash; add the ones you accept to providers.json.")
	}

	sf.Slack.Enabled = q.yes("Answer in Slack too?", sf.Slack.Enabled)
	if sf.Slack.Enabled {
		fmt.Fprintln(out, "  Create the app from `warp slack manifest`, install it, and make an app-level token with connections:write.")
		sf.Slack.Team = q.text("Slack team id (T...)", sf.Slack.Team, nonEmpty)
		sf.Slack.BotTokenRef = q.text("Where is the bot token (xoxb-)?", or(sf.Slack.BotTokenRef, "env:SLACK_BOT_TOKEN"), parseRef)
		sf.Slack.AppTokenRef = q.text("Where is the app token (xapp-)?", or(sf.Slack.AppTokenRef, "env:SLACK_APP_TOKEN"), parseRef)
	}
	if q.err != nil {
		return sf, q.err
	}
	if err := sf.Check(); err != nil {
		return sf, err
	}
	if err := writeJSON(startPath, sf); err != nil {
		return sf, err
	}
	if err := WriteProvidersFile(providersPath, pf); err != nil {
		return sf, err
	}
	fmt.Fprintf(out, "Wrote %s and %s. Edit them any time; `warp setup` asks again.\n", startPath, providersPath)
	return sf, nil
}

func parseRef(v string) error { _, err := credref.Parse(v); return err }

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
