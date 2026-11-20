package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/urmzd/humagen/pkg/credref"
)

const storeUsage = `usage: humagen store list [flags]
       humagen store install NAME [flags]
       humagen store remove NAME [--force] [flags]

The agent store of a running server, through its API. NAME is a package's
name (humagen/core) or its last part (core).

Flags: --server URL (HUMAGEN_URL), --token REF (env:HUMAGEN_TOKEN), --json
Run humagen store VERB --help for a verb's flags.`

type storeOptions struct {
	verb, name, server, token  string
	force, json, explicitToken bool
}

// storePackage is the part of the API's StorePackage the text output shows.
// Every value is printed as the server answered it: nothing about a package
// is worked out here.
type storePackage struct {
	Name      string `json:"name"`
	Title     string `json:"title"`
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
	Agents    []struct {
		Name            string `json:"name"`
		UpdateAvailable bool   `json:"update_available"`
	} `json:"agents"`
	Missing []string `json:"missing"`
}

func parseStore(args []string, out io.Writer) (storeOptions, error) {
	var c storeOptions
	if len(args) == 0 {
		if _, err := fmt.Fprintln(out, storeUsage); err != nil {
			return c, err
		}
		return c, errors.New("humagen store needs a verb: list, install or remove")
	}
	switch args[0] {
	case "help", "-h", "-help", "--help":
		if _, err := fmt.Fprintln(out, storeUsage); err != nil {
			return c, err
		}
		return c, flag.ErrHelp
	case "list", "install", "remove":
		c.verb = args[0]
	case "update":
		return c, errors.New("the API has no call that takes a package update; humagen store list shows where one is offered")
	default:
		return c, errors.New("unknown store verb; choose list, install or remove")
	}
	fs := flag.NewFlagSet("store "+c.verb, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&c.server, "server", or(os.Getenv("HUMAGEN_URL"), "http://localhost:8080"), "Humagen API URL (HUMAGEN_URL)")
	fs.StringVar(&c.token, "token", "env:HUMAGEN_TOKEN", "API bearer token reference; empty disables authentication")
	fs.BoolVar(&c.json, "json", false, "print the API response as JSON")
	if c.verb == "remove" {
		fs.BoolVar(&c.force, "force", false, "retire agents changed in the workspace too")
	}
	named := c.verb != "list"
	fs.Usage = func() {
		fs.SetOutput(out)
		if named {
			fmt.Fprintf(out, "usage: humagen store %s NAME [flags]\n", c.verb)
		} else {
			fmt.Fprintf(out, "usage: humagen store %s [flags]\n", c.verb)
		}
		fs.PrintDefaults()
	}
	// Flags are read on either side of the package name.
	var names []string
	for rest := args[1:]; ; {
		if err := fs.Parse(rest); err != nil {
			return c, err
		}
		if fs.NArg() == 0 {
			break
		}
		names = append(names, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "token" {
			c.explicitToken = true
		}
	})
	switch {
	case !named && len(names) != 0:
		return c, errors.New("unexpected arguments; use humagen store list --help")
	case named && (len(names) != 1 || strings.TrimSpace(names[0]) == ""):
		return c, fmt.Errorf("humagen store %s needs one package name; humagen store list shows them", c.verb)
	case named:
		c.name = names[0]
	}
	if c.token != "" {
		ref, err := credref.Parse(c.token)
		if err != nil {
			return c, errors.New("--token must be a credential reference; use env:NAME, keychain:SERVICE/ACCOUNT or op://VAULT/ITEM/FIELD")
		}
		c.token = ref.Raw
	}
	if strings.TrimSpace(c.server) == "" {
		return c, errors.New("no server address; set --server URL or HUMAGEN_URL")
	}
	u, err := url.Parse(c.server)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return c, errors.New("--server (or HUMAGEN_URL) must be an http or https URL without credentials, query or fragment")
	}
	c.server = strings.TrimRight(c.server, "/")
	return c, nil
}

// storeCmd sends one store request to the running server and prints its
// answer. The server's store decides everything: what is installed, what an
// install adds and what a removal retires.
func storeCmd(args []string, out io.Writer) error {
	c, err := parseStore(args, out)
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
	method, path := http.MethodGet, "/api/store/packages"
	switch c.verb {
	case "install":
		method, path = http.MethodPost, path+"/"+url.PathEscape(c.name)+"/install"
	case "remove":
		method, path = http.MethodDelete, path+"/"+url.PathEscape(c.name)
		if c.force {
			path += "?force=true"
		}
	}
	raw, err := c.request(ctx, token, method, path)
	if err != nil {
		return err
	}
	if c.json {
		_, err = fmt.Fprintln(out, string(raw))
		return err
	}
	switch c.verb {
	case "install":
		var res struct {
			Package storePackage `json:"package"`
			Added   []string     `json:"added"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			return fmt.Errorf("decode install: %w", err)
		}
		_, err = fmt.Fprintf(out, "Installed %s %s, added: %s\n", res.Package.Name, res.Package.Version, storeNames(res.Added))
	case "remove":
		var res struct {
			Package    storePackage `json:"package"`
			Retired    []string     `json:"retired"`
			Customized []string     `json:"customized"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			return fmt.Errorf("decode removal: %w", err)
		}
		_, err = fmt.Fprintf(out, "Removed %s, retired: %s\n", res.Package.Name, storeNames(res.Retired))
	default:
		var res struct {
			Packages []storePackage `json:"packages"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			return fmt.Errorf("decode packages: %w", err)
		}
		if len(res.Packages) == 0 {
			_, err = fmt.Fprintln(out, "The store has no packages.")
			return err
		}
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tVERSION\tINSTALLED\tUPDATE AVAILABLE\tMISSING\tTITLE")
		for _, p := range res.Packages {
			installed := "no"
			if p.Installed {
				installed = "yes"
			}
			var updates []string
			for _, a := range p.Agents {
				if a.UpdateAvailable {
					updates = append(updates, a.Name)
				}
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", p.Name, p.Version, installed, storeNames(updates), storeNames(p.Missing), p.Title)
		}
		err = w.Flush()
	}
	return err
}

// storeNames is a list of agent names as one cell or clause; none is "-".
func storeNames(names []string) string {
	if len(names) == 0 {
		return "-"
	}
	return strings.Join(names, ", ")
}

// request sends one call and returns the server's JSON unchanged. A refusal
// is returned with its status, the server's own words and what to do next.
// The token is only ever sent, never printed.
func (c storeOptions) request(ctx context.Context, token, method, path string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.server+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		// The transport's error repeats the method and the address.
		return nil, fmt.Errorf("%w; is the server running? Set its address with --server URL or HUMAGEN_URL", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, errors.New("store API response exceeds 1 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		hint := ""
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			hint = "; set an API token with --token REF or HUMAGEN_TOKEN"
		case http.StatusForbidden:
			need := "agents:write"
			if c.verb == "list" {
				need = "agents:read"
			}
			hint = "; this token lacks " + need + ", use one that has it with --token REF or HUMAGEN_TOKEN"
		case http.StatusNotFound:
			if c.name != "" {
				hint = "; no package named " + c.name + " on this server, see humagen store list"
			} else {
				hint = "; this server has no agent store, check --server or HUMAGEN_URL"
			}
		case http.StatusConflict:
			if c.verb == "remove" && !c.force {
				hint = "; pass --force to retire agents changed in the workspace too"
			}
		}
		return nil, fmt.Errorf("%s %s: HTTP %d: %s%s", method, path, resp.StatusCode, strings.Join(strings.Fields(string(data)), " "), hint)
	}
	if !json.Valid(data) {
		return nil, errors.New("store API returned invalid JSON; check that --server or HUMAGEN_URL is a Humagen server")
	}
	return json.RawMessage(data), nil
}
