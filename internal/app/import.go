package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/sessionimport"
	"go.yaml.in/yaml/v3"
)

const importUsage = `usage: humagen import sessions [flags]
       humagen import skills [DIR] [flags]
       humagen import agents [DIR] --provider NAME --model NAME [flags]

Bring what you already have in other assistants into a running server,
through its API.

sessions  adds your Claude Code and Codex sessions, and ChatGPT or claude.ai
          exports, to knowledge sources as one document each, so agents can
          search and cite them. A session already added is skipped.
skills    drafts each skill folder under DIR (default ~/.agents/skills) in
          the skill library. A skill the library already has is skipped.
agents    creates an agent from each Markdown agent file in DIR (default
          ~/.agents/agents), on the model you name and with no tools. An
          agent the workspace already has is skipped.

Flags: --server URL (HUMAGEN_URL), --token REF (env:HUMAGEN_TOKEN), --dry-run, --json
Run humagen import VERB --help for a verb's flags.`

// importOptions are the flags both verbs take, and each verb's own.
type importOptions struct {
	verb, server, token string
	explicitToken       bool
	dryRun, json        bool

	// sessions
	from     string
	exports  multiFlag
	since    string
	match    string
	prefix   string
	maxBytes int
	children bool
	home     string

	// skills and agents
	dir   string
	scope string
	only  string

	// agents
	provider, model string
}

// multiFlag is a flag given more than once.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func parseImport(args []string, out io.Writer) (importOptions, error) {
	var c importOptions
	if len(args) == 0 {
		if _, err := fmt.Fprintln(out, importUsage); err != nil {
			return c, err
		}
		return c, errors.New("humagen import needs a verb: sessions, skills or agents")
	}
	switch args[0] {
	case "help", "-h", "-help", "--help":
		if _, err := fmt.Fprintln(out, importUsage); err != nil {
			return c, err
		}
		return c, flag.ErrHelp
	case "sessions", "skills", "agents":
		c.verb = args[0]
	default:
		return c, errors.New("unknown import verb; choose sessions, skills or agents")
	}
	home, _ := os.UserHomeDir()
	fs := flag.NewFlagSet("import "+c.verb, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&c.server, "server", or(os.Getenv("HUMAGEN_URL"), "http://localhost:8080"), "Humagen API URL (HUMAGEN_URL)")
	fs.StringVar(&c.token, "token", "env:HUMAGEN_TOKEN", "API bearer token reference; empty disables authentication")
	fs.BoolVar(&c.dryRun, "dry-run", false, "say what would be sent, and send nothing")
	fs.BoolVar(&c.json, "json", false, "print the result as JSON")
	switch c.verb {
	case "sessions":
		fs.StringVar(&c.from, "from", "claude,codex", "which tools' session files to read: claude, codex, both, or none")
		fs.Var(&c.exports, "export", "a conversations.json from a ChatGPT or claude.ai data export; may repeat")
		fs.StringVar(&c.since, "since", "", "only sessions started since then: a date (2026-10-01) or an age (720h)")
		fs.StringVar(&c.match, "match", "", "only sessions whose directory or title contains this")
		fs.StringVar(&c.prefix, "source", "sessions", "what the knowledge sources' names start with; each is PREFIX-TOOL-YYYY-MM")
		fs.IntVar(&c.maxBytes, "max-bytes", 48<<10, "the most one session's document may be; a longer session keeps what was asked and cuts answers")
		fs.BoolVar(&c.children, "children", false, "also add the sessions other sessions started, such as sub-agents'")
		fs.StringVar(&c.home, "home", home, "the home directory the tools keep their sessions under")
	case "skills":
		fs.StringVar(&c.scope, "scope", "personal", "the library scope the skills are drafted in: personal or workspace")
		fs.StringVar(&c.only, "only", "", "only these skills, by folder name, separated by commas")
	default:
		fs.StringVar(&c.provider, "provider", "", "the model provider every imported agent runs on, such as anthropic, or policy")
		fs.StringVar(&c.model, "model", "", "the model, or with --provider policy the model policy, every imported agent runs on")
		fs.StringVar(&c.only, "only", "", "only these agents, by name, separated by commas")
	}
	fs.Usage = func() {
		fs.SetOutput(out)
		switch c.verb {
		case "skills":
			fmt.Fprintln(out, "usage: humagen import skills [DIR] [flags]")
		case "agents":
			fmt.Fprintln(out, "usage: humagen import agents [DIR] --provider NAME --model NAME [flags]")
		default:
			fmt.Fprintln(out, "usage: humagen import sessions [flags]")
		}
		fs.PrintDefaults()
	}
	// Flags are read on either side of the directory.
	var rest []string
	for left := args[1:]; ; {
		if err := fs.Parse(left); err != nil {
			return c, err
		}
		if fs.NArg() == 0 {
			break
		}
		rest = append(rest, fs.Arg(0))
		left = fs.Args()[1:]
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "token" {
			c.explicitToken = true
		}
	})
	switch {
	case c.verb == "sessions" && len(rest) != 0:
		return c, errors.New("unexpected arguments; use humagen import sessions --help")
	case len(rest) > 1:
		return c, fmt.Errorf("humagen import %s takes one directory", c.verb)
	case c.verb != "sessions" && len(rest) == 1:
		c.dir = rest[0]
	case c.verb != "sessions":
		c.dir = filepath.Join(home, ".agents", c.verb)
	}
	if c.verb == "sessions" {
		if !knowledge.ValidName(c.prefix) || len(c.prefix) > 32 {
			return c, errors.New("--source must be lower case letters, digits and hyphens, at most 32 characters")
		}
		if c.maxBytes < sessionimport.MinMarkdown || c.maxBytes > 512<<10 {
			return c, fmt.Errorf("--max-bytes must be between %d and %d", sessionimport.MinMarkdown, 512<<10)
		}
	} else if c.verb == "skills" && c.scope != "personal" && c.scope != "workspace" {
		return c, errors.New("--scope must be personal or workspace")
	} else if c.verb == "agents" && (strings.TrimSpace(c.provider) == "" || strings.TrimSpace(c.model) == "") {
		// An agent file names a model as its own tool calls it, which is not
		// a name this server's providers take.
		return c, errors.New("humagen import agents needs --provider NAME and --model NAME: the model every imported agent runs on")
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

// importAPI sends the import's requests. The token is only ever sent.
type importAPI struct {
	server, token string
	client        *http.Client
}

// apiError is a refusal: its status and the server's own words.
type apiError struct {
	method, path string
	status       int
	body         string
}

func (e *apiError) Error() string {
	hint := ""
	switch e.status {
	case http.StatusUnauthorized:
		hint = "; set an API token with --token REF or HUMAGEN_TOKEN"
	case http.StatusForbidden:
		hint = "; this token may not do that, use one that may with --token REF or HUMAGEN_TOKEN"
	}
	return fmt.Sprintf("%s %s: HTTP %d: %s%s", e.method, e.path, e.status, strings.Join(strings.Fields(e.body), " "), hint)
}

func (a importAPI) do(ctx context.Context, method, path string, body, into any) error {
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.server+path, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w; is the server running? Set its address with --server URL or HUMAGEN_URL", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &apiError{method: method, path: path, status: resp.StatusCode, body: string(data)}
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(data, into); err != nil {
		return errors.New("the API returned invalid JSON; check that --server or HUMAGEN_URL is a Humagen server")
	}
	return nil
}

func status(err error) int {
	var e *apiError
	if errors.As(err, &e) {
		return e.status
	}
	return 0
}

// importCmd reads what is on this machine and sends it to the running
// server. It adds and never changes or removes: what the server already has
// is left as it is.
func importCmd(args []string, out io.Writer) error {
	c, err := parseImport(args, out)
	if err != nil {
		return err
	}
	ctx := context.Background()
	api := importAPI{server: c.server, client: &http.Client{
		Timeout:       60 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	if c.token != "" && !c.dryRun {
		// An unset default token is the dev-mode path. An explicit reference
		// must resolve, so a typo cannot silently become an anonymous request.
		_, present := os.LookupEnv("HUMAGEN_TOKEN")
		if c.explicitToken || c.token != "env:HUMAGEN_TOKEN" || (present && strings.TrimSpace(os.Getenv("HUMAGEN_TOKEN")) != "") {
			rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			api.token, err = credref.Default.Resolve(rctx, c.token)
			cancel()
			if err != nil {
				return fmt.Errorf("resolve --token: %w", err)
			}
		}
	}
	switch c.verb {
	case "skills":
		return importSkills(ctx, c, api, out)
	case "agents":
		return importAgents(ctx, c, api, out)
	}
	return importSessions(ctx, c, api, out)
}

// sessionsResult is what an import of sessions did.
type sessionsResult struct {
	Added   int            `json:"added"`
	Present int            `json:"present"`
	Skipped int            `json:"skipped"`
	Failed  []importFailed `json:"failed"`
	Sources []string       `json:"sources"`
	DryRun  bool           `json:"dry_run,omitempty"`
	ByTool  map[string]int `json:"by_tool"`
}

type importFailed struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// sinceTime reads --since: a date, or an age before now.
func sinceTime(v string, now time.Time) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return t, nil
	}
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return now.Add(-d), nil
	}
	return time.Time{}, errors.New("--since must be a date such as 2026-10-01 or an age such as 720h")
}

// sessionSink adds sessions to knowledge sources, a month of one tool's
// sessions to a source, and a further source when one is full.
type sessionSink struct {
	ctx    context.Context
	c      importOptions
	api    importAPI
	have   map[string]bool // session URIs the server has
	exists map[string]bool // sources the server has
	count  map[string]int  // documents in each
	full   map[string]bool
	res    *sessionsResult
}

// load reads what the server already has under the prefix.
func (k *sessionSink) load() error {
	var listed struct {
		Sources []struct {
			Name string `json:"name"`
		} `json:"sources"`
	}
	if err := k.api.do(k.ctx, http.MethodGet, "/api/knowledge", nil, &listed); err != nil {
		return err
	}
	for _, src := range listed.Sources {
		if !strings.HasPrefix(src.Name, k.c.prefix+"-") {
			continue
		}
		var docs struct {
			Documents []struct {
				URI string `json:"uri"`
			} `json:"documents"`
		}
		if err := k.api.do(k.ctx, http.MethodGet, "/api/knowledge/"+url.PathEscape(src.Name)+"/documents", nil, &docs); err != nil {
			return err
		}
		k.exists[src.Name] = true
		k.count[src.Name] = len(docs.Documents)
		for _, d := range docs.Documents {
			if d.URI != "" {
				k.have[d.URI] = true
			}
		}
	}
	return nil
}

// add sends one session, to the first source of its month with room.
func (k *sessionSink) add(s sessionimport.Session) {
	if k.have[s.URI()] {
		k.res.Present++
		return
	}
	month := "undated"
	if !s.Started.IsZero() {
		month = s.Started.UTC().Format("2006-01")
	}
	base := k.c.prefix + "-" + s.Tool + "-" + month
	title := s.Title
	if s.Dir != "" {
		title += " (" + filepath.Base(s.Dir) + ")"
	}
	for n := 1; n <= 50; n++ {
		name := base
		if n > 1 {
			name = fmt.Sprintf("%s-%d", base, n)
		}
		if k.full[name] || k.count[name] >= knowledge.MaxImportDocuments {
			continue
		}
		if k.c.dryRun {
			k.sent(name, s)
			return
		}
		if !k.exists[name] {
			err := k.api.do(k.ctx, http.MethodPost, "/api/knowledge", map[string]string{
				"name":        name,
				"description": fmt.Sprintf("Sessions imported from %s, started %s.", s.Tool, month),
			}, nil)
			if err != nil && status(err) != http.StatusConflict {
				k.res.Failed = append(k.res.Failed, importFailed{Name: s.URI(), Reason: err.Error()})
				return
			}
			k.exists[name] = true
		}
		err := k.api.do(k.ctx, http.MethodPost, "/api/knowledge/"+url.PathEscape(name)+"/documents", map[string]string{
			"title": title, "uri": s.URI(), "text": s.Markdown(k.c.maxBytes),
		}, nil)
		var refused *apiError
		if errors.As(err, &refused) && refused.status == http.StatusBadRequest && strings.Contains(refused.body, "limit reached") {
			k.full[name] = true
			continue
		}
		if err != nil {
			k.res.Failed = append(k.res.Failed, importFailed{Name: s.URI(), Reason: err.Error()})
			return
		}
		k.sent(name, s)
		return
	}
	k.res.Failed = append(k.res.Failed, importFailed{Name: s.URI(), Reason: "every source for " + base + " is full"})
}

func (k *sessionSink) sent(name string, s sessionimport.Session) {
	k.count[name]++
	k.have[s.URI()] = true
	k.res.Added++
	k.res.ByTool[s.Tool]++
	if i := sort.SearchStrings(k.res.Sources, name); i == len(k.res.Sources) || k.res.Sources[i] != name {
		k.res.Sources = append(k.res.Sources, name)
		sort.Strings(k.res.Sources)
	}
}

func importSessions(ctx context.Context, c importOptions, api importAPI, out io.Writer) error {
	since, err := sinceTime(c.since, time.Now())
	if err != nil {
		return err
	}
	var tools []string
	for _, t := range strings.Split(c.from, ",") {
		switch t = strings.TrimSpace(t); t {
		case "", "none":
		case sessionimport.Claude, sessionimport.Codex:
			tools = append(tools, t)
		default:
			return fmt.Errorf("--from reads claude and codex, not %q; give a ChatGPT or claude.ai export with --export FILE", t)
		}
	}
	if len(tools) == 0 && len(c.exports) == 0 {
		return errors.New("nothing to read; name a tool with --from or an export with --export FILE")
	}
	res := sessionsResult{Failed: []importFailed{}, Sources: []string{}, ByTool: map[string]int{}, DryRun: c.dryRun}
	sink := &sessionSink{ctx: ctx, c: c, api: api, res: &res, have: map[string]bool{}, exists: map[string]bool{}, count: map[string]int{}, full: map[string]bool{}}
	if !c.dryRun {
		if err := sink.load(); err != nil {
			return err
		}
	}
	take := func(s sessionimport.Session) {
		switch {
		case s.Child && !c.children,
			!since.IsZero() && s.Started.Before(since),
			c.match != "" && !strings.Contains(s.Dir, c.match) && !strings.Contains(s.Title, c.match):
			res.Skipped++
		default:
			sink.add(s)
		}
	}
	for _, tool := range tools {
		files, err := sessionimport.Discover(tool, c.home)
		if err != nil {
			return err
		}
		for _, f := range files {
			// A file last written before --since holds no session since.
			if !since.IsZero() && f.Modified.Before(since) {
				res.Skipped++
				continue
			}
			s, err := sessionimport.Open(tool, f.Path)
			switch {
			case errors.Is(err, sessionimport.ErrNotSession):
				res.Skipped++
			case err != nil:
				res.Failed = append(res.Failed, importFailed{Name: f.Path, Reason: err.Error()})
			default:
				take(s)
			}
		}
	}
	for _, path := range c.exports {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		err = sessionimport.ReadExport(f, func(s sessionimport.Session) error { take(s); return nil })
		f.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	if c.json {
		if err := json.NewEncoder(out).Encode(res); err != nil {
			return err
		}
		if len(res.Failed) > 0 {
			return fmt.Errorf("%d sessions were not added", len(res.Failed))
		}
		return nil
	}
	verb := "Added"
	if c.dryRun {
		verb = "Would add"
	}
	if _, err := fmt.Fprintf(out, "%s %d sessions (%s) to %d knowledge sources; %d already there, %d skipped, %d failed.\n",
		verb, res.Added, toolCounts(res.ByTool), len(res.Sources), res.Present, res.Skipped, len(res.Failed)); err != nil {
		return err
	}
	for _, f := range res.Failed {
		fmt.Fprintf(out, "  failed %s: %s\n", f.Name, f.Reason)
	}
	if res.Added > 0 && !c.dryRun {
		fmt.Fprintf(out, "Next: give an agent or a project the sources named %s-… under Knowledge, so it can search them.\n", c.prefix)
	}
	if len(res.Failed) > 0 {
		return fmt.Errorf("%d sessions were not added", len(res.Failed))
	}
	return nil
}

func toolCounts(by map[string]int) string {
	if len(by) == 0 {
		return "none"
	}
	names := make([]string, 0, len(by))
	for n := range by {
		names = append(names, n)
	}
	sort.Strings(names)
	for i, n := range names {
		names[i] = fmt.Sprintf("%s %d", n, by[n])
	}
	return strings.Join(names, ", ")
}

// skillsResult is what an import of skills did.
type skillsResult struct {
	Drafted []string `json:"drafted"`
	Present []string `json:"present"`
	// NoRoom are the skills left out because the scope holds as many as it
	// may.
	NoRoom []string `json:"no_room"`
	// Reduced are the skills drafted without part of their frontmatter,
	// each with the fields left out.
	Reduced map[string][]string `json:"reduced"`
	Failed  []importFailed      `json:"failed"`
	DryRun  bool                `json:"dry_run,omitempty"`
}

// skillFolder reads one skill folder as a library definition, checked the
// way the server checks it. Hidden files and links are left out.
//
// The library reads the frontmatter the skill standard names. A SKILL.md
// whose other fields it cannot read, such as the argument lists one assistant
// adds, is drafted with only the standard's fields, and dropped names them.
func skillFolder(dir, name string) (def library.Definition, dropped []string, err error) {
	def, err = readSkillFolder(dir, name)
	if err != nil {
		return def, nil, err
	}
	whole, err := def.Normalize()
	if err != nil {
		for i, f := range def.Files {
			if f.Path != "SKILL.md" {
				continue
			}
			reduced, left, ok := standardFrontmatter(f.Data)
			if !ok || len(left) == 0 {
				return def, nil, err
			}
			def.Files[i].Data, dropped = reduced, left
		}
		if dropped == nil {
			return def, nil, err
		}
		if whole, err = def.Normalize(); err != nil {
			return def, nil, err
		}
	}
	if !whole.Valid() {
		return whole, nil, errors.New("not a skill the library takes: check its name, description and instructions in SKILL.md")
	}
	return whole, dropped, nil
}

// skillStandard are the frontmatter fields of the skill standard.
var skillStandard = []string{"name", "description", "license", "compatibility", "metadata", "allowed-tools"}

// standardFrontmatter rewrites a SKILL.md with only the standard's
// frontmatter fields, and names the fields it left out.
func standardFrontmatter(md []byte) (out []byte, dropped []string, ok bool) {
	meta, body, ok := frontmatter(md)
	if !ok {
		return nil, nil, false
	}
	var head strings.Builder
	for _, key := range skillStandard {
		value, has := meta[key]
		if !has {
			continue
		}
		line, err := yaml.Marshal(map[string]any{key: value})
		if err != nil {
			return nil, nil, false
		}
		head.Write(line)
		delete(meta, key)
	}
	for key := range meta {
		dropped = append(dropped, key)
	}
	sort.Strings(dropped)
	return []byte("---\n" + head.String() + "---\n\n" + body + "\n"), dropped, true
}

func readSkillFolder(dir, name string) (library.Definition, error) {
	def := library.Definition{Tools: []string{}}
	def.Name = name
	root := filepath.Join(dir, name)
	// A store may keep a skill in a folder named otherwise than the skill.
	// The skill's own name, in SKILL.md, is the one the library keeps.
	if md, err := os.ReadFile(filepath.Join(root, "SKILL.md")); err == nil {
		if meta, _, ok := frontmatter(md); ok {
			if own, _ := meta["name"].(string); strings.TrimSpace(own) != "" {
				def.Name = strings.TrimSpace(own)
			}
		}
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") && path != root {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if len(def.Files) >= library.MaxPackageFiles {
			return fmt.Errorf("more than %d files", library.MaxPackageFiles)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > library.MaxPackageFileBytes {
			return fmt.Errorf("%s is over %d bytes", d.Name(), library.MaxPackageFileBytes)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		def.Files = append(def.Files, library.File{Path: filepath.ToSlash(rel), Data: data})
		return nil
	})
	return def, err
}

func importSkills(ctx context.Context, c importOptions, api importAPI, out io.Writer) error {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return err
	}
	only := map[string]bool{}
	for _, n := range strings.Split(c.only, ",") {
		if n = strings.TrimSpace(n); n != "" {
			only[n] = true
		}
	}
	asked := map[string]bool{}
	res := skillsResult{Drafted: []string{}, Present: []string{}, NoRoom: []string{}, Reduced: map[string][]string{}, Failed: []importFailed{}, DryRun: c.dryRun}
	have := map[string]bool{}
	if !c.dryRun {
		var listed struct {
			Skills []struct {
				Name string `json:"name"`
			} `json:"skills"`
		}
		if err := api.do(ctx, http.MethodGet, "/api/library/skills?scope="+c.scope, nil, &listed); err != nil {
			return err
		}
		for _, s := range listed.Skills {
			have[s.Name] = true
		}
	}
	for _, e := range entries {
		name := e.Name()
		// A store keeps skills as folders, or as links to them.
		if info, err := os.Stat(filepath.Join(c.dir, name)); err != nil || !info.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(c.dir, name, "SKILL.md")); err != nil {
			continue
		}
		if len(only) > 0 && !only[name] {
			continue
		}
		asked[name] = true
		if have[name] {
			res.Present = append(res.Present, name)
			continue
		}
		def, dropped, err := skillFolder(c.dir, name)
		if err != nil {
			res.Failed = append(res.Failed, importFailed{Name: name, Reason: err.Error()})
			continue
		}
		// Two folders may hold the same skill; the first is the one drafted.
		if def.Name != name && have[def.Name] {
			res.Present = append(res.Present, name)
			continue
		}
		// The server refuses a skill over the scope's limit without saying
		// why, so the count is kept here.
		if !c.dryRun && len(have) >= library.MaxSkills {
			res.NoRoom = append(res.NoRoom, name)
			continue
		}
		if !c.dryRun {
			err := api.do(ctx, http.MethodPost, "/api/library/skills", map[string]any{
				"scope": map[string]string{"kind": c.scope}, "definition": def, "version": 0,
			}, nil)
			if err != nil {
				res.Failed = append(res.Failed, importFailed{Name: name, Reason: err.Error()})
				continue
			}
		}
		have[def.Name] = true
		res.Drafted = append(res.Drafted, def.Name)
		if len(dropped) > 0 {
			res.Reduced[def.Name] = dropped
		}
	}
	for n := range only {
		found := false
		found = asked[n]
		if !found {
			res.Failed = append(res.Failed, importFailed{Name: n, Reason: "no skill folder of that name in " + c.dir})
		}
	}
	sort.Slice(res.Failed, func(i, j int) bool { return res.Failed[i].Name < res.Failed[j].Name })
	if c.json {
		if err := json.NewEncoder(out).Encode(res); err != nil {
			return err
		}
		if n := len(res.Failed) + len(res.NoRoom); n > 0 {
			return fmt.Errorf("%d skills were not drafted", n)
		}
		return nil
	}
	verb := "Drafted"
	if c.dryRun {
		verb = "Would draft"
	}
	if _, err := fmt.Fprintf(out, "%s %d skills in the %s library; %d already there, %d failed.\n", verb, len(res.Drafted), c.scope, len(res.Present), len(res.Failed)); err != nil {
		return err
	}
	for _, f := range res.Failed {
		fmt.Fprintf(out, "  failed %s: %s\n", f.Name, f.Reason)
	}
	reduced := make([]string, 0, len(res.Reduced))
	for n := range res.Reduced {
		reduced = append(reduced, n)
	}
	sort.Strings(reduced)
	for _, n := range reduced {
		fmt.Fprintf(out, "  %s: drafted without frontmatter the library does not read: %s\n", n, strings.Join(res.Reduced[n], ", "))
	}
	if len(res.NoRoom) > 0 {
		fmt.Fprintf(out, "  no room for %d more: the %s library holds at most %d skills. Choose which with --only, or draft the rest in another scope: %s\n", len(res.NoRoom), c.scope, library.MaxSkills, strings.Join(res.NoRoom, ", "))
	}
	if len(res.Drafted) > 0 && !c.dryRun {
		fmt.Fprintln(out, "Next: test and publish each draft under Library, Skills.")
	}
	if n := len(res.Failed) + len(res.NoRoom); n > 0 {
		return fmt.Errorf("%d skills were not drafted", n)
	}
	return nil
}

// frontmatter splits a Markdown file into the YAML between its opening ---
// lines and the text after. A file without one, or with one that is not a
// YAML mapping, is not ok.
func frontmatter(data []byte) (meta map[string]any, body string, ok bool) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return nil, text, false
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return nil, text, false
	}
	if err := yaml.Unmarshal([]byte(text[4:4+end]), &meta); err != nil || meta == nil {
		return nil, text, false
	}
	body = text[4+end+4:]
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		body = body[i+1:]
	} else {
		body = ""
	}
	return meta, strings.TrimSpace(body), true
}

// agentsResult is what an import of agents did.
type agentsResult struct {
	Created []string       `json:"created"`
	Present []string       `json:"present"`
	Failed  []importFailed `json:"failed"`
	DryRun  bool           `json:"dry_run,omitempty"`
}

// importAgents creates an agent from each Markdown agent file: its name and
// description from the frontmatter, its text as the system prompt. The tools
// a file lists are another assistant's and name nothing here, so an agent
// is created with none.
func importAgents(ctx context.Context, c importOptions, api importAPI, out io.Writer) error {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return err
	}
	only := map[string]bool{}
	for _, n := range strings.Split(c.only, ",") {
		if n = strings.TrimSpace(n); n != "" {
			only[n] = true
		}
	}
	res := agentsResult{Created: []string{}, Present: []string{}, Failed: []importFailed{}, DryRun: c.dryRun}
	have := map[string]bool{}
	if !c.dryRun {
		// Each agent is listed as its latest revision, which holds its spec.
		var listed struct {
			Agents []struct {
				Spec struct {
					Name string `json:"name"`
				} `json:"spec"`
			} `json:"agents"`
		}
		if err := api.do(ctx, http.MethodGet, "/api/agents", nil, &listed); err != nil {
			return err
		}
		for _, a := range listed.Agents {
			have[a.Spec.Name] = true
		}
	}
	seen := map[string]bool{}
	for _, e := range entries {
		file := e.Name()
		if !strings.HasSuffix(file, ".md") || strings.HasPrefix(file, ".") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(c.dir, file))
		if err != nil {
			// A folder named like a file, or a link to nothing.
			continue
		}
		name := strings.TrimSuffix(strings.TrimSuffix(file, ".md"), ".agent")
		meta, body, ok := frontmatter(data)
		if own, _ := meta["name"].(string); strings.TrimSpace(own) != "" {
			name = strings.TrimSpace(own)
		}
		if len(only) > 0 && !only[name] {
			continue
		}
		seen[name] = true
		description, _ := meta["description"].(string)
		switch {
		case !ok:
			res.Failed = append(res.Failed, importFailed{Name: name, Reason: file + " does not start with YAML frontmatter"})
			continue
		case body == "":
			res.Failed = append(res.Failed, importFailed{Name: name, Reason: file + " has no text to be the system prompt"})
			continue
		case have[name]:
			res.Present = append(res.Present, name)
			continue
		}
		if !c.dryRun {
			err := api.do(ctx, http.MethodPost, "/api/agents", map[string]any{
				"name": name, "description": oneLineText(description), "provider": c.provider, "model": c.model,
				"system_prompt": body, "tools": []string{},
			}, nil)
			if err != nil {
				res.Failed = append(res.Failed, importFailed{Name: name, Reason: err.Error()})
				continue
			}
		}
		have[name] = true
		res.Created = append(res.Created, name)
	}
	for n := range only {
		if !seen[n] {
			res.Failed = append(res.Failed, importFailed{Name: n, Reason: "no agent file of that name in " + c.dir})
		}
	}
	sort.Slice(res.Failed, func(i, j int) bool { return res.Failed[i].Name < res.Failed[j].Name })
	failed := func() error {
		if len(res.Failed) > 0 {
			return fmt.Errorf("%d agents were not created", len(res.Failed))
		}
		return nil
	}
	if c.json {
		if err := json.NewEncoder(out).Encode(res); err != nil {
			return err
		}
		return failed()
	}
	verb := "Created"
	if c.dryRun {
		verb = "Would create"
	}
	if _, err := fmt.Fprintf(out, "%s %d agents on %s %s; %d already there, %d failed.\n", verb, len(res.Created), c.provider, c.model, len(res.Present), len(res.Failed)); err != nil {
		return err
	}
	for _, f := range res.Failed {
		fmt.Fprintf(out, "  failed %s: %s\n", f.Name, f.Reason)
	}
	if len(res.Created) > 0 && !c.dryRun {
		fmt.Fprintln(out, "Next: each agent has no tools. Grant what it needs under Agents.")
	}
	return failed()
}

func oneLineText(s string) string { return strings.Join(strings.Fields(s), " ") }
