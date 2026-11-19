package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"regexp"
	"slices"
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
          search and cite them. A session already added is skipped, unless
          its document would now read differently: then it is replaced in
          place.
skills    drafts each skill folder under DIR (default ~/.agents/skills) in
          the skill library. A skill the library holds as the folder holds
          it is skipped. A folder that changed since it was imported is
          drafted again as a new revision, which is not published: agents
          keep the revision they use until the draft is tested and
          published. A skill edited in the library since is a conflict, and
          is left alone unless --force.
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
	corpus   string
	byProj   bool
	project  string

	// skills and agents
	dir   string
	scope string
	only  string
	// force drafts a changed skill folder after a newest revision no import
	// wrote.
	force bool

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
		fs.BoolVar(&c.byProj, "by-project", false, "put each session in a source named for the directory it ran in (PREFIX-in-NAME), and attach that source to the project of the same name when the workspace has one")
		fs.StringVar(&c.project, "project", "", "with --export: put the exports' sessions in the source --by-project names for a directory called NAME (PREFIX-in-NAME), and attach it to the project NAME when the workspace has one; sessions read with --from are filed as without it")
		fs.StringVar(&c.corpus, "corpus", "", "the corpus the sources are put in, so the console's Knowledge page shows them; left out it is named as --source is, and none puts them in no corpus")
	case "skills":
		fs.StringVar(&c.scope, "scope", "personal", "the library scope the skills are drafted in: personal or workspace")
		fs.StringVar(&c.only, "only", "", "only these skills, by folder name, separated by commas")
		fs.BoolVar(&c.force, "force", false, "draft a changed folder even after a newest revision that was edited in the library, not imported")
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
	named := false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "token":
			c.explicitToken = true
		case "project":
			named = true
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
		if c.corpus == "" {
			c.corpus = c.prefix
		}
		if c.corpus != "none" && !knowledge.ValidName(c.corpus) {
			return c, errors.New("--corpus must be lower case letters, digits and hyphens, or none")
		}
		if c.maxBytes < sessionimport.MinMarkdown || c.maxBytes > 512<<10 {
			return c, fmt.Errorf("--max-bytes must be between %d and %d", sessionimport.MinMarkdown, 512<<10)
		}
		// An export carries no directory, so it is the only thing --project
		// files: a session read from this machine has one, and --by-project.
		c.project = strings.TrimSpace(c.project)
		switch {
		case named && len(c.exports) == 0:
			return c, errors.New("--project files the sessions of an export, so it needs --export FILE; file the sessions on this machine by their directories with --by-project")
		case named && nameSlug(c.project) == "":
			// As --by-project gives a directory with no letter or digit in
			// its name (the root) no source.
			return c, errors.New("--project needs a project's name with a letter or a digit in it")
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
	// The largest answer is every revision of one skill with its files:
	// library.MaxVersions packages of library.MaxPackageBytes, in base64.
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
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
// is left as it is. A session's document that would read differently is
// replaced in place, and a skill folder that changed is drafted as a new
// revision beside the ones there.
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
	// A dry run of skills reads the library to say what it would do, so it
	// needs the token; a dry run of anything else sends nothing.
	if c.token != "" && (!c.dryRun || c.verb == "skills") {
		// An unset default token is the dev-mode path. An explicit reference
		// must resolve, so a typo cannot silently become an anonymous request.
		_, present := os.LookupEnv("HUMAGEN_TOKEN")
		if c.explicitToken || c.token != "env:HUMAGEN_TOKEN" || (present && strings.TrimSpace(os.Getenv("HUMAGEN_TOKEN")) != "") {
			rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			api.token, err = credref.Default.Resolve(rctx, c.token)
			cancel()
			if err != nil && c.dryRun {
				// A dry run that cannot read the library says so and still
				// checks the folders.
				api.token, err = "", nil
			}
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
	Added int `json:"added"`
	// Updated are the sessions whose documents were replaced in place: ones
	// that changed since they were added, or that an earlier version wrote.
	Updated int `json:"updated"`
	Present int `json:"present"`
	// Conflicts are the sessions whose documents another writer changed
	// while this run was replacing them, to something this run would not
	// have written. They are left as that writer wrote them.
	Conflicts []string       `json:"conflicts"`
	Skipped   int            `json:"skipped"`
	Failed    []importFailed `json:"failed"`
	Sources   []string       `json:"sources"`
	// Corpora are the corpora the sources are in after the import.
	Corpora []string `json:"corpora"`
	// Attached are the sources now attached to a project, as SOURCE=PROJECT,
	// and Unfiled the directories, and the --project, no project is named for.
	Attached []string `json:"attached,omitempty"`
	Unfiled  []string `json:"unfiled,omitempty"`
	// Project is the --project the exports' sessions are filed in, and
	// ProjectSource the source they go to. Elsewhere are the exports'
	// sessions an earlier run put in another source, as URI=SOURCE: they are
	// left there, since no route moves a document between sources.
	Project       string   `json:"project,omitempty"`
	ProjectSource string   `json:"project_source,omitempty"`
	Elsewhere     []string `json:"elsewhere,omitempty"`
	// ElsewhereChanged are those of Elsewhere whose export reads differently
	// now than the document kept: it was not updated.
	ElsewhereChanged []string       `json:"elsewhere_changed,omitempty"`
	DryRun           bool           `json:"dry_run,omitempty"`
	ByTool           map[string]int `json:"by_tool"`
	// StructuresNotKept says why this run wrote its sessions' documents
	// without their structures: a server older than this command, with no
	// import route, or a token that may not import. Empty when every
	// document this run wrote went with its structure.
	StructuresNotKept string `json:"structures_not_kept,omitempty"`
	// Restored are the sessions whose documents read as this run would
	// write them and had no trusted structure (one an earlier run could not
	// keep, or one an edit ended): this run sent them again through the
	// import route, with their structure, leaving their text as it was.
	Restored int `json:"restored"`
	// WithoutStructure counts the documents of this run's sessions left
	// without a trusted structure: written without one, or found without one
	// and not restored. A later run that may import restores them.
	WithoutStructure int `json:"without_structure"`
	// StructuresFull are the sources a structure was not restored in
	// because their structures are at their bound (8 MiB a source): their
	// documents are present, unchanged, and counted in WithoutStructure.
	StructuresFull []string `json:"structures_full,omitempty"`
	// NotInCorpus are the sources left out of any corpus, and
	// NotInCorpusReason why: every corpus name this run may use is another
	// person's or full.
	NotInCorpus       []string `json:"not_in_corpus,omitempty"`
	NotInCorpusReason string   `json:"not_in_corpus_reason,omitempty"`
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
	have   map[string]held // the sessions the server has, by URI
	exists map[string]bool // sources the server has
	count  map[string]int  // documents in each
	full   map[string]bool
	dirs   map[string]string // by project: a directory's slug to its name
	owner  map[string]string // by project: what each source says it holds
	// ran and filed are the slugs this run read a directory's sessions for,
	// and the ones it filed an export under: a source is one kind's only.
	ran, filed map[string]bool
	// foreign are the sources this run was refused: not its own to add to,
	// to attach or to put in a corpus.
	foreign map[string]bool
	// project is the --project the sessions now being read are filed in: set
	// while an export is read, and empty for the sessions of this machine.
	project string
	res     *sessionsResult
	// denied is why this run writes no more documents: the token may not
	// write knowledge.
	denied string
}

// structureLimits are the bounds the server keeps a structure within.
var structureLimits = sessionimport.Limits{Bytes: knowledge.MaxImportedBytes, Turns: knowledge.MaxImportedTurns, TurnBytes: knowledge.MaxImportedTurnBytes, ID: knowledge.MaxImportedID}

// noImport reads a refusal of an import route. A reason is returned when
// this run should go on writing documents without structures: a server that
// has no such route (older than this command), or a token that may not
// import. fatal is set instead when the token may not write knowledge at
// all, which an ordinary write would be refused for too: the run does not
// fall back, and every session it has left fails with the reason. The
// server names the missing permission as its routes do ("you need
// PERMISSION"), which is what is matched. Any other error is the write's
// own, and "" is returned.
func noImport(err error) (reason string, fatal bool) {
	var e *apiError
	if !errors.As(err, &e) {
		return "", false
	}
	var said struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal([]byte(e.body), &said)
	switch {
	case e.status == http.StatusMethodNotAllowed, e.status == http.StatusNotFound && strings.Contains(e.body, "no such route"):
		return "the server has no import route: it is older than this command", false
	case e.status == http.StatusForbidden && said.Error == "you need knowledge:write":
		return "this token may not write knowledge (knowledge:write), which importing a session needs", true
	case e.status == http.StatusForbidden && said.Error == "you need knowledge:import":
		return "this token may not import sessions (knowledge:import)", false
	case e.status == http.StatusForbidden:
		return "the server refused the import route (HTTP 403)", false
	}
	return "", false
}

// structuresFull reports whether an import was refused because the
// source's structures are at their bound (knowledge.MaxImportedSourceBytes).
func structuresFull(err error) bool {
	var e *apiError
	return errors.As(err, &e) && e.status == http.StatusBadRequest && strings.Contains(e.body, "limit reached for imported session structures")
}

// write sends a session's document: through the import route with the
// session's structure, which the server keeps with that revision and never
// reads out of its text, or else through the ordinary route without one.
// importPath and plainPath are the two routes, and body the document's
// fields. A server that has no import route, or a token that may not
// import, is found on the first write; this run then writes every document
// without its structure and says so (StructuresNotKept). A token that may
// not write knowledge stops the run's writes (denied). A session no
// structure can name (an id too long, or with control characters) is
// written without one.
func (k *sessionSink) write(method, importPath, plainPath string, body map[string]any, s sessionimport.Session) error {
	if k.denied != "" {
		return errors.New(k.denied)
	}
	st, ok := s.Structure(structureLimits)
	if ok && k.res.StructuresNotKept == "" {
		body["session"] = st
		err := k.api.do(k.ctx, method, importPath, body, nil)
		reason, fatal := noImport(err)
		switch {
		case fatal:
			k.denied = reason
			return errors.New(reason)
		case reason == "":
			return err
		}
		k.res.StructuresNotKept = reason
		delete(body, "session")
	}
	err := k.api.do(k.ctx, method, plainPath, body, nil)
	if err == nil {
		k.res.WithoutStructure++
	}
	return err
}

// restore sends a session's document again through the import route, with
// its structure and the title and text it already has, when the server
// keeps no trusted structure with it: the server writes the structure alone.
// It reports whether it did. A run that cannot import, or a source whose
// structures are at their bound, leaves the document as it is, present and
// counted as without a structure, and is not a failure.
func (k *sessionSink) restore(was held, title, text string, s sessionimport.Session) (restored bool, err error) {
	if k.denied != "" {
		return false, errors.New(k.denied)
	}
	st, ok := s.Structure(structureLimits)
	if !ok || k.res.StructuresNotKept != "" {
		k.res.WithoutStructure++
		return false, nil
	}
	err = k.api.do(k.ctx, http.MethodPut, "/api/knowledge/"+url.PathEscape(was.source)+"/imports/"+url.PathEscape(was.id), map[string]any{
		"title": title, "text": text, "digest": was.digest, "session": st,
	}, nil)
	reason, fatal := noImport(err)
	switch {
	case fatal:
		k.denied = reason
		return false, errors.New(reason)
	case reason != "":
		k.res.StructuresNotKept = reason
		k.res.WithoutStructure++
		return false, nil
	case structuresFull(err):
		if !slices.Contains(k.res.StructuresFull, was.source) {
			k.res.StructuresFull = append(k.res.StructuresFull, was.source)
		}
		k.res.WithoutStructure++
		return false, nil
	}
	return err == nil, err
}

// held is a session's document as the server has it. A session is known by
// its URI alone, so a document an earlier version wrote is the same
// session's. Its digest, of its title and text, says whether it reads as
// the session now would: a document kept before digests were has none, and
// so reads differently.
type held struct {
	source, id string
	digest     string
	// structured is whether the server keeps a trusted structure with the
	// document: its listing gives the structure's size, which is zero for
	// one no import wrote and for one edited since.
	structured bool
}

// load reads what the server already has under the prefix.
func (k *sessionSink) load() error {
	var listed struct {
		Sources []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
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
				ID             string `json:"id"`
				URI            string `json:"uri"`
				Digest         string `json:"digest"`
				StructureBytes int    `json:"structure_bytes"`
			} `json:"documents"`
		}
		if err := k.api.do(k.ctx, http.MethodGet, "/api/knowledge/"+url.PathEscape(src.Name)+"/documents", nil, &docs); err != nil {
			return err
		}
		k.exists[src.Name] = true
		k.owner[src.Name] = src.Description
		k.count[src.Name] = len(docs.Documents)
		for _, d := range docs.Documents {
			if _, ok := k.have[d.URI]; d.URI != "" && !ok {
				k.have[d.URI] = held{source: src.Name, id: d.ID, digest: d.Digest, structured: d.StructureBytes > 0}
			}
		}
	}
	return nil
}

// add sends one session, to the first source of its month with room.
func (k *sessionSink) add(s sessionimport.Session) {
	// A directory is noted even when its sessions are all there, so a later
	// run attaches a source whose project was made since.
	// dir is the name the session is filed by: the project an export was
	// given, or else by project the last name of the directory it ran in.
	slug, dir := "", ""
	switch {
	case k.project != "":
		dir = k.project
	case k.c.byProj && s.Dir != "":
		dir = filepath.Base(s.Dir)
	}
	if slug = nameSlug(dir); slug != "" {
		// Two directories are never kept in one source. A slug is made
		// not to be shared, and one that is all the same is refused
		// here, before a session of the second is sent.
		if held, ok := k.dirs[slug]; ok && !strings.EqualFold(held, dir) {
			k.res.Failed = append(k.res.Failed, importFailed{Name: s.URI(), Reason: fmt.Sprintf("the directories %s and %s would share the source %s-in-%s; import one of them by month, without --by-project", held, dir, k.c.prefix, slug)})
			return
		}
		k.dirs[slug] = dir
		if k.project != "" {
			k.filed[slug] = true
		} else {
			k.ran[slug] = true
		}
	}
	title := s.Title
	if s.Dir != "" {
		title += " (" + filepath.Base(s.Dir) + ")"
	}
	month := "undated"
	if !s.Started.IsZero() {
		month = s.Started.UTC().Format("2006-01")
	}
	base := k.c.prefix + "-" + s.Tool + "-" + month
	what := fmt.Sprintf("Sessions imported from %s, started %s.", s.Tool, month)
	// By project, a session is kept with the others of its directory.
	if slug != "" {
		base = k.c.prefix + "-in-" + slug
		what = ranIn(dir)
		if k.project != "" {
			what = filedIn(dir)
		}
	}
	// refuse reports a session not added to a source that is not this
	// run's to write to. Such a source is not attached or put in a corpus
	// by this run either.
	refuse := func(name, held string) {
		k.foreign[name] = true
		var reason string
		switch {
		case k.project != "" && held == "":
			reason = fmt.Sprintf("the source %s is not the exports' of the project %s: it does not say what it holds; file the export in sources of another name with --source PREFIX, or by month without --project", name, dir)
		case k.project != "":
			reason = fmt.Sprintf("the source %s is not the exports' of the project %s: it says it holds %q; file the export in sources of another name with --source PREFIX, or by month without --project", name, dir, held)
		case held == "":
			reason = fmt.Sprintf("the source %s does not say whose sessions it holds, so it is not %s's to add to; import %s by month, without --by-project", name, dir, dir)
		default:
			reason = fmt.Sprintf("the source %s holds another directory's sessions (%q); import %s by month, without --by-project", name, held, dir)
		}
		k.res.Failed = append(k.res.Failed, importFailed{Name: s.URI(), Reason: reason})
	}
	if was, ok := k.have[s.URI()]; ok {
		// An export filed in a project is replaced only in that project's
		// own source of exports. One an earlier run kept in any other
		// source is left wholly as it is, even when it reads differently
		// now: nothing moves a document between sources, this command
		// removes none, and another source's documents are not this run's
		// to write. The run says where it is, and whether it differs.
		if k.project != "" && was.id != "" && (!continues(was.source, base) || !strings.EqualFold(k.owner[was.source], what)) {
			at := s.URI() + "=" + was.source
			k.res.Present++
			k.res.Elsewhere = append(k.res.Elsewhere, at)
			if was.digest != knowledge.Digest(title, s.Markdown(k.c.maxBytes)) {
				k.res.ElsewhereChanged = append(k.res.ElsewhereChanged, at)
			}
			return
		}
		// A source named for a directory or a project is replaced in only
		// when it says it is this session's: the rule for adding to one and
		// for attaching it, applied before the one write that could not be
		// taken back. A source by month is not checked, and a session kept
		// in one is replaced there.
		if k.project == "" && was.id != "" && strings.HasPrefix(was.source, k.c.prefix+"-in-") && !k.holds(was.source, s) {
			k.foreign[was.source] = true
			held := "it does not say what it holds"
			if said := k.owner[was.source]; said != "" {
				held = fmt.Sprintf("it says it holds %q", said)
			}
			k.res.Failed = append(k.res.Failed, importFailed{Name: s.URI(), Reason: fmt.Sprintf("not updated, and left as it was: its document is in the source %s, which is not this session's: %s", was.source, held)})
			return
		}
		if !k.replace(was, title, s) {
			return
		}
	}
	for n := 1; n <= 50; n++ {
		name := base
		if n > 1 {
			name = fmt.Sprintf("%s-%d", base, n)
		}
		// A source is added to only when it says it is this directory's,
		// or this project's exports': not one an earlier run made for
		// another directory, not a directory's for an export or an
		// export's for a directory, though both are named for one project,
		// and not one that does not say. Nothing is written before that is
		// known.
		if slug != "" && k.exists[name] && !strings.EqualFold(k.owner[name], what) {
			refuse(name, k.owner[name])
			return
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
				"description": what,
			}, nil)
			switch {
			case err == nil:
				k.exists[name] = true
				k.owner[name] = what
			case status(err) == http.StatusConflict:
				// Another writer made it first. It is read, and is this
				// run's only if it says so: this run's description was not
				// written and is not taken for the source's.
				held, rerr := k.said(name)
				if rerr != nil {
					k.foreign[name] = true
					k.res.Failed = append(k.res.Failed, importFailed{Name: s.URI(), Reason: fmt.Sprintf("the source %s is already there and what it holds could not be read, so nothing was added to it: %s", name, rerr)})
					return
				}
				k.exists[name] = true
				k.owner[name] = held
				if slug != "" && !strings.EqualFold(held, what) {
					refuse(name, held)
					return
				}
			default:
				k.res.Failed = append(k.res.Failed, importFailed{Name: s.URI(), Reason: err.Error()})
				return
			}
		}
		src := "/api/knowledge/" + url.PathEscape(name)
		err := k.write(http.MethodPost, src+"/imports", src+"/documents", map[string]any{
			"title": title, "uri": s.URI(), "text": s.Markdown(k.c.maxBytes),
		}, s)
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

// holds reports whether a source named for a directory or a project says it
// is the one a session not filed with --project belongs in: the directory
// the session ran in, or for an export's, with no directory, the exports of
// the project the source is named for.
func (k *sessionSink) holds(source string, s sessionimport.Session) bool {
	said := k.owner[source]
	if s.Dir != "" {
		dir := filepath.Base(s.Dir)
		return strings.EqualFold(said, ranIn(dir)) && continues(source, k.c.prefix+"-in-"+nameSlug(dir))
	}
	before, after, _ := strings.Cut(filedIn("\x00"), "\x00")
	if !strings.HasPrefix(said, before) || !strings.HasSuffix(said, after) || len(said) <= len(before)+len(after) {
		return false
	}
	project := said[len(before) : len(said)-len(after)]
	return nameSlug(project) != "" && continues(source, k.c.prefix+"-in-"+nameSlug(project))
}

// said reads what a source the server has says it holds.
func (k *sessionSink) said(name string) (string, error) {
	var listed struct {
		Sources []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"sources"`
	}
	if err := k.api.do(k.ctx, http.MethodGet, "/api/knowledge", nil, &listed); err != nil {
		return "", err
	}
	for _, src := range listed.Sources {
		if src.Name == name {
			return src.Description, nil
		}
	}
	return "", errors.New("it is not among the sources this token reads")
}

// replace brings the document the server has of a session up to what the
// session now reads as, and reports whether the session is still to be
// added because its document is gone. A document that already reads the
// same is left alone: nothing is written. Any other is replaced in place,
// in one call the server makes only if the document's title and text are
// still the ones this run read, so it keeps its id, its source and whether
// it is left out of search, and a refused or failed call leaves it as it
// was.
//
// The session's structure goes in the same call (write), so the document
// and its structure are replaced together or neither is.
//
// The call is made once. A run the server refuses lost to another writer,
// and never writes over what that writer left: it reads the document again,
// counts it present if it reads as this run would have written it, and
// otherwise leaves it and names the session as a conflict. Which of the two
// is the later state of the session this run cannot know.
func (k *sessionSink) replace(was held, title string, s sessionimport.Session) (add bool) {
	text := s.Markdown(k.c.maxBytes)
	digest := knowledge.Digest(title, text)
	// A document this run sent has no id here, and is as it was sent.
	if was.id == "" || was.digest == digest && was.structured {
		k.res.Present++
		return false
	}
	src := "/api/knowledge/" + url.PathEscape(was.source)
	docs := src + "/documents"
	var err error
	restoring := was.digest == digest
	if restoring {
		// It reads as this run would write it, and the server keeps no
		// trusted structure with it: the structure is sent again.
		var restored bool
		if restored, err = k.restore(was, title, text, s); err == nil {
			if restored {
				k.res.Restored++
			} else {
				k.res.Present++
			}
			return false
		}
	} else {
		err = k.write(http.MethodPut, src+"/imports/"+url.PathEscape(was.id), docs+"/"+url.PathEscape(was.id)+"/text", map[string]any{
			"title": title, "text": text, "digest": was.digest,
		}, s)
	}
	switch {
	case err == nil:
		k.have[s.URI()] = held{source: was.source}
		k.res.Updated++
		return false
	case status(err) != http.StatusConflict:
		k.res.Failed = append(k.res.Failed, importFailed{Name: s.URI(), Reason: "not updated, and left as it was: " + err.Error()})
		return false
	}
	var listed struct {
		Documents []struct {
			ID             string `json:"id"`
			Digest         string `json:"digest"`
			StructureBytes int    `json:"structure_bytes"`
		} `json:"documents"`
	}
	if err := k.api.do(k.ctx, http.MethodGet, docs, nil, &listed); err != nil {
		k.res.Failed = append(k.res.Failed, importFailed{Name: s.URI(), Reason: "not updated, and left as it was: " + err.Error()})
		return false
	}
	for _, d := range listed.Documents {
		switch {
		case d.ID != was.id:
		case d.Digest == digest:
			// Another writer left it reading as this run would: present,
			// and counted as without a structure when none is kept.
			k.res.Present++
			if d.StructureBytes == 0 {
				k.res.WithoutStructure++
			}
			return false
		default:
			k.res.Conflicts = append(k.res.Conflicts, s.URI())
			return false
		}
	}
	// Removed since this run read it: the session is added again.
	delete(k.have, s.URI())
	k.count[was.source]--
	return true
}

// dirSlug is a directory's last name as a source's name may hold it: lower
// case letters, digits and hyphens, at most 24. "" is a directory with no
// name to go by. Two directories share a slug only when their names differ
// in case alone: a name the slug does not spell exactly (foo_bar, My App, a
// long one) or one that ends as a continued source does (app-2) carries
// eight digits of its hash, so foo_bar is not kept with foo-bar and app-2 is
// not taken for the second source of app.
func dirSlug(dir string) string {
	if dir == "" {
		return ""
	}
	return nameSlug(filepath.Base(dir))
}

// nameSlug is dirSlug of a directory's last name, which is also how a
// project named with --project is spelled in its source's name.
func nameSlug(name string) string {
	base := strings.ToLower(name)
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return ""
	}
	if slug == base && len(slug) <= 24 && !continued.MatchString(slug) {
		return slug
	}
	if len(slug) > 14 {
		slug = strings.Trim(slug[:14], "-")
	}
	return slug + "-x" + slugSum(base)
}

// slugSum is the eight digits a slug carries of its directory's name. They
// make a shared slug unlikely, not impossible: add refuses the second
// directory of one.
var slugSum = func(base string) string {
	sum := sha256.Sum256([]byte(base))
	return hex.EncodeToString(sum[:4])
}

// ranIn is what a by-project source says it holds: the directory's name,
// which is how a later run knows whose the source is.
func ranIn(dir string) string {
	return "Sessions imported from other assistants that ran in " + dir + "."
}

// filedIn is what the source of an export filed with --project says it
// holds. It is not what ranIn says, so a directory's source and an export's
// are told apart by what they say, as two directories' are.
func filedIn(project string) string {
	return "Conversations imported from an export of another assistant, filed in the project " + project + "."
}

// continued matches the name of a source that continues a full one.
var continued = regexp.MustCompile(`^(.+)-\d+$`)

// continues reports whether source is base or one that continues it.
func continues(source, base string) bool {
	if m := continued.FindStringSubmatch(source); m != nil && m[1] == base {
		return true
	}
	return source == base
}

// attach attaches each by-project source to the workspace's project of the
// same name, so runs filed there are grounded in the sessions that ran in
// that directory. A directory no project is named for is reported, and its
// sessions stay in the corpus. The project named with --project is one more
// such name: the exports' source is attached to it the same way.
func (k *sessionSink) attach() {
	// Every page: a workspace holds more projects than one page does.
	projects := map[string]string{} // lower case name to name
	for before := ""; ; {
		var listed struct {
			Projects []struct {
				Name string `json:"name"`
			} `json:"projects"`
			Next *string `json:"next"`
		}
		path := "/api/projects?limit=200"
		if before != "" {
			path += "&before=" + url.QueryEscape(before)
		}
		if err := k.api.do(k.ctx, http.MethodGet, path, nil, &listed); err != nil {
			k.res.Failed = append(k.res.Failed, importFailed{Name: "projects", Reason: err.Error()})
			return
		}
		for _, p := range listed.Projects {
			projects[strings.ToLower(p.Name)] = p.Name
		}
		if listed.Next == nil || *listed.Next == "" || *listed.Next == before {
			break
		}
		before = *listed.Next
	}
	var sources []string
	for name := range k.exists {
		sources = append(sources, name)
	}
	sort.Strings(sources)
	in := k.c.prefix + "-in-"
	unfiled := map[string]bool{}
	for _, source := range sources {
		if !strings.HasPrefix(source, in) {
			continue
		}
		slug := strings.TrimPrefix(source, in)
		// NAME-2 continues NAME, and is its directory's too. No directory's
		// own slug ends that way (dirSlug).
		if m := continued.FindStringSubmatch(slug); m != nil {
			slug = m[1]
		}
		// A source of a directory this run did not read is left as it is.
		dir, ok := k.dirs[slug]
		if !ok {
			continue
		}
		// A source that says it holds another directory is not this one's
		// to attach, whatever the slug: its sessions were refused above. So
		// with a directory's source when this run only filed an export
		// under its name, and the other way about.
		mine := k.ran[slug] && strings.EqualFold(k.owner[source], ranIn(dir)) || k.filed[slug] && strings.EqualFold(k.owner[source], filedIn(dir))
		// Nor is one that does not say what it holds.
		if held := k.owner[source]; !mine {
			reason := fmt.Sprintf("not attached for %s: the source holds another directory's sessions (%q)", dir, held)
			switch {
			case held == "":
				reason = fmt.Sprintf("not attached for %s: the source does not say what it holds", dir)
			case !k.ran[slug]:
				reason = fmt.Sprintf("not attached for %s: the source is not the exports' of that project, it says it holds %q", dir, held)
			}
			k.res.Failed = append(k.res.Failed, importFailed{Name: source, Reason: reason})
			continue
		}
		// The project is the one named as the directory is, ignoring case
		// and nothing else: foo-bar is not foo_bar's.
		project, ok := projects[strings.ToLower(dir)]
		if !ok {
			unfiled[dir] = true
			continue
		}
		if err := k.api.do(k.ctx, http.MethodPut, "/api/knowledge/"+url.PathEscape(source)+"/attachment", map[string]string{"project": project}, nil); err != nil {
			k.res.Failed = append(k.res.Failed, importFailed{Name: source, Reason: err.Error()})
			continue
		}
		k.res.Attached = append(k.res.Attached, source+"="+project)
	}
	for dir := range unfiled {
		k.res.Unfiled = append(k.res.Unfiled, dir)
	}
	sort.Strings(k.res.Unfiled)
}

// maxCorpusMembers is how many sources one corpus holds.
const maxCorpusMembers = 50

// file puts every source of the import in a corpus, which is where the
// console's Knowledge page shows sources: one the importer made, and a
// further one when it is full. A source already in one is left there.
func (k *sessionSink) file() {
	var sources []string
	for name := range k.exists {
		if !k.foreign[name] {
			sources = append(sources, name)
		}
	}
	sort.Strings(sources)
	if len(sources) == 0 {
		return
	}
	type corpus struct {
		Members []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"members"`
	}
	fail := func(what string, err error) {
		k.res.Failed = append(k.res.Failed, importFailed{Name: "corpus " + what, Reason: err.Error()})
	}
	for n := 1; len(sources) > 0 && n <= 20; n++ {
		name := k.c.corpus
		if n > 1 {
			name = fmt.Sprintf("%s-%d", k.c.corpus, n)
		}
		var have corpus
		err := k.api.do(k.ctx, http.MethodGet, "/api/corpora/"+url.PathEscape(name), nil, &have)
		if status(err) == http.StatusNotFound {
			err = k.api.do(k.ctx, http.MethodPost, "/api/corpora", map[string]any{
				"name": name, "title": "Imported sessions", "description": "Sessions imported from other assistants with humagen import sessions.",
			}, &have)
			if status(err) == http.StatusConflict {
				// It is there and not this token's to read: another
				// person's. The sources go in the next one.
				continue
			}
		}
		if err != nil {
			fail(name, err)
			return
		}
		in := map[string]bool{}
		for _, m := range have.Members {
			if m.Kind == "source" {
				in[m.Name] = true
			}
		}
		room := maxCorpusMembers - len(have.Members)
		var left []string
		foreign := false
	members:
		for _, src := range sources {
			switch {
			case in[src]:
			case room <= 0:
				left = append(left, src)
			default:
				err := k.api.do(k.ctx, http.MethodPost, "/api/corpora/"+url.PathEscape(name)+"/members", map[string]string{"kind": "source", "name": src}, nil)
				if status(err) == http.StatusForbidden && room == maxCorpusMembers-len(have.Members) {
					// Readable and not this token's to change, and
					// nothing added to it yet: the sources go in the
					// next one.
					foreign = true
					break members
				}
				if err != nil {
					fail(name, err)
					return
				}
				room--
			}
		}
		if foreign {
			continue
		}
		k.res.Corpora = append(k.res.Corpora, name)
		sources = left
	}
	if len(sources) > 0 {
		k.res.NotInCorpus = sources
		k.res.NotInCorpusReason = fmt.Sprintf("every corpus name from %s to %s-20 is another person's or full", k.c.corpus, k.c.corpus)
	}
}

func (k *sessionSink) sent(name string, s sessionimport.Session) {
	k.count[name]++
	k.have[s.URI()] = held{source: name}
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
	res := sessionsResult{Conflicts: []string{}, Failed: []importFailed{}, Sources: []string{}, Corpora: []string{}, ByTool: map[string]int{}, DryRun: c.dryRun}
	if c.project != "" {
		res.Project, res.ProjectSource = c.project, c.prefix+"-in-"+nameSlug(c.project)
	}
	sink := &sessionSink{ctx: ctx, c: c, api: api, res: &res, have: map[string]held{}, exists: map[string]bool{}, count: map[string]int{}, full: map[string]bool{}, dirs: map[string]string{}, owner: map[string]string{}, ran: map[string]bool{}, filed: map[string]bool{}, foreign: map[string]bool{}}
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
	// Only what an export holds is filed in --project: the sessions read
	// above were filed as they are without it.
	sink.project = c.project
	for _, path := range c.exports {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		var empty int
		empty, err = sessionimport.ReadExport(f, func(s sessionimport.Session) error { take(s); return nil })
		res.Skipped += empty
		f.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	if !c.dryRun && c.corpus != "none" {
		sink.file()
	}
	if !c.dryRun && (c.byProj || c.project != "") {
		sink.attach()
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
	if res.Updated > 0 {
		fmt.Fprintf(out, "Updated %d sessions whose documents read differently now, in place.\n", res.Updated)
	}
	if res.Restored > 0 {
		fmt.Fprintf(out, "Restored the structure of %d sessions whose documents read the same but had none kept with them.\n", res.Restored)
	}
	if res.StructuresNotKept != "" {
		fmt.Fprintf(out, "Structures were not kept: %s. %d documents remain without a structure; a run that may import restores them.\n", res.StructuresNotKept, res.WithoutStructure)
	}
	if len(res.StructuresFull) > 0 {
		fmt.Fprintf(out, "Structures were not restored in %s: each holds as many session structures as a source may (8 MiB). Their documents are present and unchanged, without a structure.\n", strings.Join(res.StructuresFull, ", "))
	}
	if len(res.NotInCorpus) > 0 {
		fmt.Fprintf(out, "Not put in a corpus: %s: %s.\n", strings.Join(res.NotInCorpus, ", "), res.NotInCorpusReason)
	}
	if len(res.Conflicts) > 0 {
		fmt.Fprintf(out, "Left %d sessions as another import wrote them while this one ran; nothing of this run's was written over them: %s.\n", len(res.Conflicts), strings.Join(res.Conflicts, ", "))
	}
	for _, f := range res.Failed {
		fmt.Fprintf(out, "  failed %s: %s\n", f.Name, f.Reason)
	}
	if len(res.Attached) > 0 {
		fmt.Fprintf(out, "Attached to projects: %s.\n", strings.Join(res.Attached, ", "))
	}
	if len(res.Unfiled) > 0 {
		fmt.Fprintf(out, "No project is named for: %s. Their sessions are in the corpus; make the project and run this again to attach them.\n", strings.Join(res.Unfiled, ", "))
	}
	if c.dryRun && res.Project != "" {
		fmt.Fprintf(out, "Would file the exports' sessions in %s and attach it to the project %s, if the workspace has one of that name; a dry run does not ask the server, so it does not know which are already imported.\n", res.ProjectSource, res.Project)
	}
	if len(res.Elsewhere) > 0 {
		in := map[string]bool{}
		for _, e := range res.Elsewhere {
			in[e[strings.LastIndexByte(e, '=')+1:]] = true
		}
		sources := make([]string, 0, len(in))
		for name := range in {
			sources = append(sources, name)
		}
		sort.Strings(sources)
		fmt.Fprintf(out, "%d sessions are already imported in another source and were left there, not filed in %s: %s. To refile them, remove their documents from that source and run this again.\n", len(res.Elsewhere), res.Project, strings.Join(sources, ", "))
		if n := len(res.ElsewhereChanged); n > 0 {
			fmt.Fprintf(out, "%d of them read differently in the export now and were not updated: a document in another source is left as it is. To bring them up to date, import the export again as it was first filed, or refile them.\n", n)
		}
	}
	switch {
	case c.dryRun:
	case len(res.Corpora) > 0:
		fmt.Fprintf(out, "They are in the corpus %s under Knowledge. Next: give an agent that corpus, so it can search them.\n", strings.Join(res.Corpora, ", "))
	case res.Added > 0:
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
	// Added are the skills the library did not have, drafted as their first
	// revision.
	Added []string `json:"added"`
	// Drafted is Added under the name it had before updates were reported:
	// the skills the library lacked, each drafted as its first revision. It
	// never holds an updated skill. In a dry run that could not read the
	// library it is Unknown, every valid folder, as a dry run then printed.
	// Kept for programs that read it.
	Drafted []string `json:"drafted"`
	// Updated are the skills whose folder changed since it was imported,
	// each drafted as a new revision. Nothing is published.
	Updated []skillUpdate `json:"updated"`
	// Present are the skills whose newest revision holds what the folder
	// holds. Nothing is written for them.
	Present []string `json:"present"`
	// Conflicts are the skills whose folder differs from a newest revision
	// that no import wrote, or that changed while this import ran. Nothing
	// is written for them.
	Conflicts []importFailed `json:"conflicts"`
	// NoRoom are the skills left out because the scope holds as many as it
	// may.
	NoRoom []string `json:"no_room"`
	// Reduced are the skills drafted without part of their frontmatter,
	// each with the fields left out.
	Reduced map[string][]string `json:"reduced"`
	Failed  []importFailed      `json:"failed"`
	DryRun  bool                `json:"dry_run,omitempty"`
	// Unknown are the skill folders a dry run could not compare with the
	// library, because the library could not be read, and Unread is why.
	// Each is a skill the library would take; whether it would be added,
	// updated, present or in conflict is not known.
	Unknown []string `json:"unknown"`
	Unread  string   `json:"library_unread,omitempty"`
}

// skillUpdate is one skill drafted again from a changed folder.
type skillUpdate struct {
	Name string `json:"name"`
	// Version is the new draft, and Published the revision that stays
	// published until the draft is tested and published, 0 when none is.
	Version   int `json:"version"`
	Published int `json:"published"`
	// Changed, Added and Removed are the files that differ from the
	// revision before, by path. A renamed file is one removed and one added.
	Changed []string `json:"changed"`
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
}

// libraryRevision is what an import reads of a skill's revision.
type libraryRevision struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Version     int            `json:"version"`
	Via         string         `json:"via"`
	PackageHash string         `json:"package_hash"`
	PublishedAt *time.Time     `json:"published_at"`
	Files       []library.File `json:"files"`
}

// fileChanges is how a folder's files differ from a revision's, by path.
func fileChanges(was, now []library.File) (changed, added, removed []string) {
	changed, added, removed = []string{}, []string{}, []string{}
	old := map[string][]byte{}
	for _, f := range was {
		old[f.Path] = f.Data
	}
	for _, f := range now {
		data, had := old[f.Path]
		switch {
		case !had:
			added = append(added, f.Path)
		case !bytes.Equal(data, f.Data):
			changed = append(changed, f.Path)
		}
		delete(old, f.Path)
	}
	for path := range old {
		removed = append(removed, path)
	}
	sort.Strings(changed)
	sort.Strings(added)
	sort.Strings(removed)
	return changed, added, removed
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

// skillImport is one run of import skills against one scope of the library.
type skillImport struct {
	ctx   context.Context
	c     importOptions
	api   importAPI
	have  map[string]libraryRevision
	res   *skillsResult
	force bool
	// unread is why a dry run could not read the library, when it could
	// not: its folders are then checked here and reported as unknown.
	unread string
}

// list reads the newest revision of every skill the scope holds: its id,
// its version, how it was written and the hash of its package.
func (m *skillImport) list() error {
	var listed struct {
		Skills []libraryRevision `json:"skills"`
	}
	if err := m.api.do(m.ctx, http.MethodGet, "/api/library/skills?scope="+m.c.scope, nil, &listed); err != nil {
		return err
	}
	m.have = map[string]libraryRevision{}
	for _, s := range listed.Skills {
		m.have[s.Name] = s
	}
	return nil
}

// history reads every revision of a skill, oldest first, with its files.
func (m *skillImport) history(id string) ([]libraryRevision, error) {
	var got struct {
		Revisions []libraryRevision `json:"revisions"`
	}
	if err := m.api.do(m.ctx, http.MethodGet, "/api/library/skills/"+id, nil, &got); err != nil {
		return nil, err
	}
	if len(got.Revisions) == 0 {
		return nil, errors.New("the library returned no revision of it")
	}
	return got.Revisions, nil
}

// draft sends the folder as the skill's next revision, only if the skill
// still has expected revisions: one added since answers 409.
func (m *skillImport) draft(def library.Definition, expected int) error {
	return m.api.do(m.ctx, http.MethodPost, "/api/library/skills", map[string]any{
		"scope": map[string]string{"kind": m.c.scope}, "definition": def, "version": expected, "via": library.ViaImport,
	}, nil)
}

// lost settles a draft the library refused because the skill gained a
// revision while this import ran. Another import of the same folder has
// then written what this one would have, and the skill is present. Anything
// else is someone's newer work, which is reported and not written over.
func (m *skillImport) lost(name string, def library.Definition, hash string) {
	if err := m.list(); err != nil {
		m.res.Failed = append(m.res.Failed, importFailed{Name: name, Reason: err.Error()})
		return
	}
	if cur, ok := m.have[def.Name]; ok && cur.PackageHash == hash {
		m.res.Present = append(m.res.Present, name)
		return
	}
	m.res.Conflicts = append(m.res.Conflicts, importFailed{Name: name, Reason: raced})
}

// raced is the conflict of a skill someone else changed between this
// import's read of the library and its draft.
const raced = "the library's copy changed while this import ran, to something this folder does not hold; nothing was written. Run the import again"

// one imports one skill folder, against one snapshot of the skill: what the
// list read at the start says of it (m.have). The comparison, the decision
// and the condition of the write all use that snapshot, so a revision added
// since is never drafted after: the library refuses the draft, or the
// revisions read to describe the change show a newer head, and either way
// the skill is settled as raced (present when the newer head holds this
// folder, a conflict otherwise).
//
// A skill the library lacks is added. One
// whose newest revision holds the folder's files is present. One whose
// folder differs is drafted again, as a new revision after the newest, only
// when an import wrote the newest: a revision someone saved in the editor
// or promoted may hold work the folder lacks, so it is a conflict unless
// --force. A draft never publishes and never changes a revision, so what
// agents and running work are pinned to stays as it is.
func (m *skillImport) one(name string, def library.Definition, dropped []string) {
	fail := func(err error) {
		m.res.Failed = append(m.res.Failed, importFailed{Name: name, Reason: err.Error()})
	}
	pkg, err := def.Package()
	if err != nil {
		fail(err)
		return
	}
	reduced := func() {
		if len(dropped) > 0 {
			m.res.Reduced[def.Name] = dropped
		}
	}
	if m.unread != "" {
		m.res.Unknown = append(m.res.Unknown, def.Name)
		reduced()
		return
	}
	cur, held := m.have[def.Name]
	if !held {
		// The server refuses a skill over the scope's limit without saying
		// why, so the count is kept here.
		if len(m.have) >= library.MaxSkills {
			m.res.NoRoom = append(m.res.NoRoom, name)
			return
		}
		if !m.c.dryRun {
			if err := m.draft(def, 0); status(err) == http.StatusConflict {
				m.lost(name, def, pkg.Hash)
				return
			} else if err != nil {
				fail(err)
				return
			}
		}
		m.have[def.Name] = libraryRevision{Name: def.Name, Version: 1, Via: library.ViaImport, PackageHash: pkg.Hash}
		m.res.Added = append(m.res.Added, def.Name)
		reduced()
		return
	}
	if cur.PackageHash == pkg.Hash {
		m.res.Present = append(m.res.Present, name)
		return
	}
	revisions, err := m.history(cur.ID)
	if err != nil {
		fail(err)
		return
	}
	newest := revisions[len(revisions)-1]
	// The head this import decided on is the one the list gave: cur. The
	// revisions are read only to say what differs, and they must be of that
	// same head. A skill that gained a revision since the list was read
	// was changed by someone else while this import ran: that is a race,
	// settled as one, and never a newer head to draft after.
	if len(revisions) != cur.Version || newest.Version != cur.Version || newest.PackageHash != cur.PackageHash {
		if c, a, d := fileChanges(newest.Files, def.Files); len(c)+len(a)+len(d) == 0 {
			m.res.Present = append(m.res.Present, name)
			return
		}
		m.res.Conflicts = append(m.res.Conflicts, importFailed{Name: name, Reason: raced})
		return
	}
	update := skillUpdate{Name: def.Name, Version: cur.Version + 1}
	update.Changed, update.Added, update.Removed = fileChanges(newest.Files, def.Files)
	if len(update.Changed)+len(update.Added)+len(update.Removed) == 0 {
		// The same files under a hash this program computes differently.
		m.res.Present = append(m.res.Present, name)
		return
	}
	for _, r := range revisions {
		if r.PublishedAt != nil {
			update.Published = r.Version
		}
	}
	if cur.Via != library.ViaImport && !m.force {
		reason := fmt.Sprintf("its newest revision in the library, v%d, was not written by an import, so it may hold edits this folder lacks", newest.Version)
		for _, r := range revisions[:len(revisions)-1] {
			if c, a, d := fileChanges(r.Files, def.Files); len(c)+len(a)+len(d) == 0 {
				reason = fmt.Sprintf("this folder holds v%d, and the library has since moved on to v%d, which no import wrote", r.Version, newest.Version)
			}
		}
		m.res.Conflicts = append(m.res.Conflicts, importFailed{Name: name, Reason: reason + "; nothing was written. Bring those edits into the folder, or draft the folder after them with --force"})
		return
	}
	if cur.Version >= library.MaxVersions {
		fail(fmt.Errorf("the library holds as many revisions of it as it may (%d)", library.MaxVersions))
		return
	}
	if !m.c.dryRun {
		if err := m.draft(def, cur.Version); status(err) == http.StatusConflict {
			m.lost(name, def, pkg.Hash)
			return
		} else if err != nil {
			fail(err)
			return
		}
	}
	m.have[def.Name] = libraryRevision{ID: cur.ID, Name: def.Name, Version: update.Version, Via: library.ViaImport, PackageHash: pkg.Hash}
	m.res.Updated = append(m.res.Updated, update)
	reduced()
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
	res := skillsResult{Added: []string{}, Updated: []skillUpdate{}, Present: []string{}, Conflicts: []importFailed{}, NoRoom: []string{}, Reduced: map[string][]string{}, Failed: []importFailed{}, Unknown: []string{}, DryRun: c.dryRun}
	m := &skillImport{ctx: ctx, c: c, api: api, res: &res, force: c.force}
	// A dry run reads the library too, and writes nothing: what it would do
	// depends on what the library holds.
	if err := m.list(); err != nil {
		// A dry run with no library to read still checks the folders, as
		// it did before it read the library, and says the rest is unknown.
		if !c.dryRun {
			return err
		}
		m.unread, res.Unread = err.Error(), err.Error()
	}
	// Two folders may hold the same skill; the first is the one imported.
	seen := map[string]bool{}
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
		def, dropped, err := skillFolder(c.dir, name)
		if err != nil {
			res.Failed = append(res.Failed, importFailed{Name: name, Reason: err.Error()})
			continue
		}
		if seen[def.Name] {
			res.Present = append(res.Present, name)
			continue
		}
		seen[def.Name] = true
		m.one(name, def, dropped)
	}
	for n := range only {
		if !asked[n] {
			res.Failed = append(res.Failed, importFailed{Name: n, Reason: "no skill folder of that name in " + c.dir})
		}
	}
	sort.Slice(res.Failed, func(i, j int) bool { return res.Failed[i].Name < res.Failed[j].Name })
	// drafted is what it was before updates were reported: the skills first
	// drafted. A dry run that could not read the library drafted nothing it
	// can name, and reports every valid folder, as a dry run then did.
	res.Drafted = res.Added
	if m.unread != "" {
		res.Drafted = res.Unknown
	}
	left := len(res.Failed) + len(res.NoRoom) + len(res.Conflicts)
	leftErr := func() error {
		if left > 0 {
			return fmt.Errorf("%d skills were not drafted", left)
		}
		return nil
	}
	if c.json {
		if err := json.NewEncoder(out).Encode(res); err != nil {
			return err
		}
		return leftErr()
	}
	if m.unread != "" {
		if _, err := fmt.Fprintf(out, "Would import %d skills into the %s library; %d failed.\nThe library could not be read, so which of them would be added, updated, already there or in conflict is unknown: %s\n",
			len(res.Unknown), c.scope, len(res.Failed), m.unread); err != nil {
			return err
		}
		if len(res.Unknown) > 0 {
			fmt.Fprintf(out, "  unknown: %s\n", strings.Join(res.Unknown, ", "))
		}
		for _, f := range res.Failed {
			fmt.Fprintf(out, "  failed %s: %s\n", f.Name, f.Reason)
		}
		return leftErr()
	}
	added, updated := "Added", "updated"
	if c.dryRun {
		added, updated = "Would add", "update"
	}
	if _, err := fmt.Fprintf(out, "%s %d skills and %s %d as drafts in the %s library; %d already there, %d in conflict, %d failed.\n",
		added, len(res.Added), updated, len(res.Updated), c.scope, len(res.Present), len(res.Conflicts), len(res.Failed)); err != nil {
		return err
	}
	for _, u := range res.Updated {
		var parts []string
		for _, part := range []struct {
			what  string
			paths []string
		}{{"changed", u.Changed}, {"added", u.Added}, {"removed", u.Removed}} {
			if len(part.paths) > 0 {
				parts = append(parts, part.what+" "+strings.Join(part.paths, ", "))
			}
		}
		stays := "no revision is published yet"
		if u.Published > 0 {
			stays = fmt.Sprintf("v%d stays published", u.Published)
		}
		fmt.Fprintf(out, "  updated %s: draft v%d (%s); %s\n", u.Name, u.Version, strings.Join(parts, "; "), stays)
	}
	for _, f := range res.Conflicts {
		fmt.Fprintf(out, "  conflict %s: %s\n", f.Name, f.Reason)
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
	if len(res.Added)+len(res.Updated) > 0 && !c.dryRun {
		fmt.Fprintln(out, "Next: test and publish each draft under Library, Skills. Until then agents keep the revision they use now.")
	}
	return leftErr()
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
