package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// agentsResult is what an import of agents did.
type agentsResult struct {
	Created []string `json:"created"`
	Present []string `json:"present"`
	// Skipped are the files that name an agent an earlier file of the folder
	// gives. Nothing is sent for them.
	Skipped []agentSkipped `json:"skipped"`
	Failed  []importFailed `json:"failed"`
	// Reduced are the fields each agent file holds that its agent, created
	// or already there, does not carry, by file.
	Reduced map[string][]string `json:"reduced"`
	// NotEnforced are the hooks and permission rules among them, by file,
	// sorted: each field with the known names under it, such as
	// hooks.PreToolUse or permissions.deny, or the field alone. Humagen runs
	// and applies none.
	NotEnforced map[string][]string `json:"not_enforced"`
	// NotEnforcedOther counts, by file and field, the entries under a hook
	// or permission field that are not named, since their keys may be rules
	// or commands.
	NotEnforcedOther map[string]map[string]int `json:"not_enforced_other"`
	DryRun           bool                      `json:"dry_run,omitempty"`
}

// agentSkipped is a file left out because an earlier file of the folder
// gives the agent it names.
type agentSkipped struct {
	File string `json:"file"`
	Name string `json:"name"`
	From string `json:"from"`
}

// maxAgentFields bounds an agent file's frontmatter: a file with more fields
// fails, so a report lists every field it leaves out.
const maxAgentFields = 1000

// agentFile is what an agent file's frontmatter gives its agent, and what
// it does not.
type agentFile struct {
	name, description string
	// notCarried are the top-level fields the agent does not carry, sorted.
	notCarried []string
	// hooks and permissions are the hook and permission fields among them,
	// each with the known names under it, sorted.
	hooks, permissions []string
	// other counts the entries under a hook or permission field that are not
	// known names, by field.
	other map[string]int
}

// agentPermissionFields are the fields, folded to lower case without - or
// _, with which another assistant allows or denies an agent's calls.
var agentPermissionFields = map[string]bool{"permissions": true, "permissionmode": true, "disallowedtools": true}

// hookEvents and permissionKeys are the names a report shows under hooks
// and permissions. Any other key is counted, not shown: a rule or a command
// may be written as a key.
var (
	hookEvents = map[string]bool{
		"PreToolUse": true, "PostToolUse": true, "PostToolUseFailure": true, "PermissionRequest": true,
		"Notification": true, "UserPromptSubmit": true, "Stop": true, "SubagentStart": true,
		"SubagentStop": true, "PreCompact": true, "SessionStart": true, "SessionEnd": true,
	}
	permissionKeys = map[string]bool{"allow": true, "deny": true, "ask": true, "defaultMode": true, "additionalDirectories": true}
)

// agentFrontmatter splits an agent file into its frontmatter and its text.
// The frontmatter opens with a first line of ---, and closes with the next
// line that is --- alone, trailing spaces aside; it must be one YAML mapping.
// When it is not, why says how, in a few words that never quote the file.
func agentFrontmatter(data []byte) (meta map[string]any, body, why string) {
	lines := strings.SplitAfter(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	fence := func(line string) bool { return strings.TrimRight(line, " \t\n") == "---" }
	if !fence(lines[0]) {
		return nil, "", "does not start with YAML frontmatter"
	}
	for i := 1; i < len(lines); i++ {
		if fence(lines[i]) {
			meta, why = agentYAML(strings.Join(lines[1:i], ""))
			return meta, strings.TrimSpace(strings.Join(lines[i+1:], "")), why
		}
	}
	return nil, "", "has frontmatter with no closing --- line"
}

// agentYAML reads a frontmatter block as one YAML mapping.
func agentYAML(src string) (map[string]any, string) {
	dec := yaml.NewDecoder(strings.NewReader(src))
	var doc yaml.Node
	if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
		return nil, "has empty frontmatter"
	} else if err != nil {
		return nil, yamlFault(err)
	}
	// A document end (...) or a second document would otherwise leave every
	// field after it unread, without a word.
	var more yaml.Node
	if err := dec.Decode(&more); !errors.Is(err, io.EOF) {
		return nil, "has more than one YAML document in its frontmatter"
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, "has frontmatter that is not a YAML mapping"
	}
	if len(doc.Content[0].Content)/2 > maxAgentFields {
		return nil, fmt.Sprintf("has more than %d frontmatter fields", maxAgentFields)
	}
	var meta map[string]any
	if err := doc.Decode(&meta); err != nil {
		return nil, yamlFault(err)
	}
	if meta == nil {
		meta = map[string]any{}
	}
	return meta, ""
}

// yamlFault is the kind of YAML a parser refused. Its message is not
// passed on: it may quote the file.
func yamlFault(err error) string {
	if strings.Contains(err.Error(), "already defined") {
		return "has frontmatter that repeats a key"
	}
	return "has frontmatter that is not valid YAML"
}

// agentFields reads what an agent file's frontmatter gives the agent: its
// name and description, when each is text. Every other field, and a name or
// description that is neither text nor empty, is not carried.
func agentFields(meta map[string]any) agentFile {
	f := agentFile{other: map[string]int{}}
	fold := strings.NewReplacer("-", "", "_", "")
	for key, value := range meta {
		switch key {
		case "name", "description":
			if value == nil {
				continue
			}
			text, ok := value.(string)
			switch {
			case !ok:
				f.notCarried = append(f.notCarried, key)
			case key == "name":
				f.name = strings.TrimSpace(text)
			default:
				f.description = text
			}
			continue
		}
		f.notCarried = append(f.notCarried, key)
		var into *[]string
		var known map[string]bool
		switch kind := fold.Replace(strings.ToLower(key)); {
		case kind == "hooks":
			into, known = &f.hooks, hookEvents
		case agentPermissionFields[kind]:
			into, known = &f.permissions, permissionKeys
		default:
			continue
		}
		names, other := entries(value, known)
		if len(names) == 0 {
			*into = append(*into, key)
		}
		for _, n := range names {
			*into = append(*into, key+"."+n)
		}
		if other > 0 {
			f.other[key] = other
		}
	}
	sort.Strings(f.notCarried)
	sort.Strings(f.hooks)
	sort.Strings(f.permissions)
	return f
}

// entries are the known keys of a mapping, and how many others it has.
func entries(value any, known map[string]bool) (names []string, other int) {
	var keys []any
	switch v := value.(type) {
	case map[string]any:
		for k := range v {
			keys = append(keys, k)
		}
	case map[any]any:
		for k := range v {
			keys = append(keys, k)
		}
	}
	for _, k := range keys {
		if s, ok := k.(string); ok && known[s] {
			names = append(names, s)
		} else {
			other++
		}
	}
	sort.Strings(names)
	return names, other
}

// What a person is told once, at the end of a run, when some file held a
// hook or a permission rule.
const (
	hooksNote = `Hooks are not enforced: Humagen does not run a file's hook commands. A check
  before a tool call is set with the agent's approvals, under Agents, and with
  Ask or Never under Operations, Action Policies. Nothing stands in for a hook
  that runs after a call or when a turn stops.`
	permissionsNote = `Permission rules are not enforced: no rule of a file is applied. An agent has
  only the tools granted to it under Agents, where its approvals are set, and
  Ask or Never for a kind of action is set under Operations, Action Policies.
  A rule on one command or path has no equivalent.`
)

// importAgents creates an agent from each Markdown agent file: its name and
// description from the frontmatter, its text as the system prompt. The tools
// a file lists are another assistant's and name nothing here, so an agent
// is created with none. Every other field is not carried, and the report
// names it for each file an agent is created from or already there for.
//
// Two files may name one agent; the first, by file name, is the one
// imported, as for skills, and the other is reported as skipped.
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
	res := agentsResult{Created: []string{}, Present: []string{}, Skipped: []agentSkipped{}, Failed: []importFailed{},
		Reduced: map[string][]string{}, NotEnforced: map[string][]string{}, NotEnforcedOther: map[string]map[string]int{}, DryRun: c.dryRun}
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
	// from is the file each agent of this run was created from or found
	// already there for; reported are those files, in file order.
	from := map[string]string{}
	var reported []string
	hooks, permissions := false, false
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
		meta, body, why := agentFrontmatter(data)
		f := agentFields(meta)
		if f.name != "" {
			name = f.name
		}
		if len(only) > 0 && !only[name] {
			continue
		}
		seen[name] = true
		switch {
		case why != "":
			res.Failed = append(res.Failed, importFailed{Name: name, Reason: file + " " + why})
			continue
		case body == "":
			res.Failed = append(res.Failed, importFailed{Name: name, Reason: file + " has no text to be the system prompt"})
			continue
		case from[name] != "":
			res.Skipped = append(res.Skipped, agentSkipped{File: file, Name: name, From: from[name]})
			continue
		case have[name]:
			res.Present = append(res.Present, name)
		default:
			if !c.dryRun {
				err := api.do(ctx, http.MethodPost, "/api/agents", map[string]any{
					"name": name, "description": oneLineText(f.description), "provider": c.provider, "model": c.model,
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
		// A present agent's file is reported too: whether the agent was
		// made from this file as it is now is not known, and its fields
		// are not carried either way.
		from[name] = file
		if len(f.notCarried) > 0 {
			res.Reduced[file] = f.notCarried
			reported = append(reported, file)
		}
		if len(f.hooks)+len(f.permissions) > 0 {
			res.NotEnforced[file] = append(append([]string{}, f.hooks...), f.permissions...)
			sort.Strings(res.NotEnforced[file])
			hooks, permissions = hooks || len(f.hooks) > 0, permissions || len(f.permissions) > 0
		}
		if len(f.other) > 0 {
			res.NotEnforcedOther[file] = f.other
		}
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
	skipped := ""
	if len(res.Skipped) > 0 {
		skipped = fmt.Sprintf(" %d skipped,", len(res.Skipped))
	}
	if _, err := fmt.Fprintf(out, "%s %d agents on %s %s; %d already there,%s %d failed.\n", verb, len(res.Created), c.provider, c.model, len(res.Present), skipped, len(res.Failed)); err != nil {
		return err
	}
	for _, f := range res.Failed {
		fmt.Fprintf(out, "  failed %s: %s\n", shown(f.Name), inline(f.Reason))
	}
	for _, s := range res.Skipped {
		fmt.Fprintf(out, "  skipped %s: it names the agent %s, which %s gives\n", shown(s.File), shown(s.Name), shown(s.From))
	}
	for _, file := range reported {
		fmt.Fprintf(out, "  %s: not carried: %s\n", shown(file), shownList(res.Reduced[file], nil))
		if names, other := res.NotEnforced[file], res.NotEnforcedOther[file]; len(names)+len(other) > 0 {
			fmt.Fprintf(out, "  %s: not enforced: %s\n", shown(file), shownList(names, other))
		}
	}
	if hooks {
		fmt.Fprintln(out, hooksNote)
	}
	if permissions {
		fmt.Fprintln(out, permissionsNote)
	}
	if len(res.Created) > 0 && !c.dryRun {
		fmt.Fprintln(out, "Next: each agent has no tools. Grant what it needs under Agents.")
	}
	return failed()
}

// A line names at most maxShownNames names, each cut at maxNameRunes.
const (
	maxShownNames = 40
	maxNameRunes  = 64
)

// shown is a name taken from a person's files as a report prints it: as it
// is when it is plain printable text, quoted as Go quotes a string when it
// holds anything else (a control character, an escape sequence, a line
// break, a leading or trailing space), and cut after maxNameRunes runes,
// with ... after it. An empty name is (empty).
func shown(name string) string {
	if name == "" {
		return "(empty)"
	}
	cut, n := len(name), 0
	for i := range name {
		if n == maxNameRunes {
			cut = i
			break
		}
		n++
	}
	s := name[:cut]
	plain := utf8.ValidString(s) && strings.TrimSpace(s) == s
	for _, r := range s {
		if !unicode.IsPrint(r) {
			plain = false
		}
	}
	if !plain {
		s = strconv.Quote(s)
	}
	if cut < len(name) {
		s += "..."
	}
	return s
}

// shownList is names as a line prints them, at most maxShownNames, then
// "and N more", and then the count of other entries under each field.
func shownList(names []string, other map[string]int) string {
	parts := []string{}
	for i, n := range names {
		if i == maxShownNames {
			parts = append(parts, fmt.Sprintf("and %d more", len(names)-maxShownNames))
			break
		}
		parts = append(parts, shown(n))
	}
	fields := make([]string, 0, len(other))
	for field := range other {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		entry := "entries"
		if other[field] == 1 {
			entry = "entry"
		}
		parts = append(parts, fmt.Sprintf("%d other %s %s", other[field], shown(field), entry))
	}
	return strings.Join(parts, ", ")
}

// shownAll is every name, each as shown prints it.
func shownAll(names []string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = shown(n)
	}
	return strings.Join(parts, ", ")
}

// inline is text that may hold a name or a path from a person's files, such
// as a reason, as a report prints it: every rune that is not printable,
// line breaks and escape sequences among them, is written as Go escapes it,
// and nothing is cut.
func inline(text string) string {
	var b strings.Builder
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			q := strconv.Quote(text[i : i+1])
			b.WriteString(q[1 : len(q)-1])
		case unicode.IsPrint(r):
			b.WriteString(text[i : i+size])
		default:
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
		}
		i += size
	}
	return b.String()
}
