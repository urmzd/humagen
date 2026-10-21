package spec

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The project tree's bounds, as the project store (pkg/projects) enforces
// them at start. They are checked here too, so warp config check finds a
// mistake before a start does; a test holds the two to the same values.
const (
	maxProjects     = 100
	maxProjectDepth = 4
	maxCompany      = 10

	maxProjectTitle        = 80
	maxProjectDescription  = 500
	maxProjectInstructions = 4000
	maxAccountCustomer     = 120
	maxAccountExternalID   = 120
	maxAccountURL          = 500
)

var projectName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _-]{0,62}$`)

// checkProjects holds the workspace's instructions and project tree to what
// the project store accepts, each mistake with its path:
//
//  1. names match the project regex and are unique ignoring case;
//  2. a parent names a project in this file;
//  3. no cycle, depth at most 4, at most 10 company projects, company only
//     under the workspace or company, account never under account, and an
//     account only on an account project;
//  4. a kind is company, team or account.
//
// The fields' bounds are checked as well. A project the console made cannot
// be seen from here, so the store checks again when the file is applied.
func checkProjects(w Workspace) error {
	if w.Instructions != nil {
		if err := checkText(*w.Instructions, maxProjectInstructions, true); err != nil {
			return fmt.Errorf("workspace.instructions: %w", err)
		}
	}
	ps := w.Projects
	if len(ps) > maxProjects {
		return fmt.Errorf("workspace.projects: a workspace has at most %d projects, not %d", maxProjects, len(ps))
	}
	at := func(i int) string { return fmt.Sprintf("workspace.projects[%d]", i) }
	exact := map[string]int{}
	folded := map[string]int{}
	for i, p := range ps {
		if !projectName.MatchString(p.Name) {
			return fmt.Errorf("%s.name: %q is not a project name: letters, digits, spaces, dashes and underscores, starting with a letter or digit, at most 63", at(i), p.Name)
		}
		if j, dup := folded[strings.ToLower(p.Name)]; dup {
			return fmt.Errorf("%s.name: %q is %s's name again; names are unique ignoring case", at(i), p.Name, at(j))
		}
		exact[p.Name], folded[strings.ToLower(p.Name)] = i, i
		if p.Kind != "" && p.Kind != ProjectCompany && p.Kind != ProjectTeam && p.Kind != ProjectAccount {
			return fmt.Errorf("%s.kind: %q is not a kind: company, team or account", at(i), p.Kind)
		}
		if err := checkProjectFields(p); err != nil {
			return fmt.Errorf("%s.%w", at(i), err)
		}
	}
	// parent[i] is the index of i's parent, or -1 for the workspace.
	parent := make([]int, len(ps))
	for i, p := range ps {
		name := ParentOf(p)
		if name == "" {
			parent[i] = -1
			continue
		}
		j, ok := exact[name]
		if !ok {
			if k, near := folded[strings.ToLower(name)]; near {
				return fmt.Errorf("%s.parent: %q is not a project in this file; names are exact, so write %q", at(i), name, ps[k].Name)
			}
			return fmt.Errorf("%s.parent: %q is not a project in this file. A parent made only in the console cannot be checked before start, so declare it here too, or leave parent out for the top level", at(i), name)
		}
		parent[i] = j
	}
	// A project whose walk up comes back to it is on a cycle. One whose walk
	// meets a cycle above it is not, and is left to that cycle's own report.
	for i := range ps {
		j := parent[i]
		for steps := 0; j >= 0 && j != i && steps < len(ps); steps++ {
			j = parent[j]
		}
		if j == i {
			if parent[i] == i {
				return fmt.Errorf("%s.parent: %q cannot be its own parent", at(i), ps[i].Name)
			}
			return fmt.Errorf("%s.parent: %q is its own ancestor", at(i), ps[i].Name)
		}
	}
	companies := 0
	for i, p := range ps {
		depth := 1
		for j := parent[i]; j >= 0; j = parent[j] {
			depth++
		}
		if depth > maxProjectDepth {
			return fmt.Errorf("%s.parent: %q would sit %d levels below the workspace; at most %d", at(i), p.Name, depth, maxProjectDepth)
		}
		kind, up := KindOf(p), ProjectCompany
		if parent[i] >= 0 {
			up = KindOf(ps[parent[i]])
		}
		switch {
		case kind == ProjectCompany && up != ProjectCompany:
			return fmt.Errorf("%s.kind: a company project sits under the workspace or another company project, and %q is %s", at(i), ps[parent[i]].Name, aKind(up))
		case kind == ProjectAccount && up == ProjectAccount:
			return fmt.Errorf("%s.kind: an account cannot sit under another account, and %q is one", at(i), ps[parent[i]].Name)
		}
		if kind == ProjectCompany {
			if companies++; companies > maxCompany {
				return fmt.Errorf("%s.kind: a workspace has at most %d company projects", at(i), maxCompany)
			}
		}
	}
	return nil
}

// aKind is a kind in words, with its article.
func aKind(k string) string {
	if k == ProjectAccount {
		return "an account"
	}
	return "a " + k
}

// checkProjectFields checks one project's fields on their own. Its error
// starts with the field's name, for the caller to put after the project's
// path.
func checkProjectFields(p Project) error {
	if err := checkText(p.Title, maxProjectTitle, false); err != nil {
		return fmt.Errorf("title: %w", err)
	}
	if err := checkText(p.Description, maxProjectDescription, true); err != nil {
		return fmt.Errorf("description: %w", err)
	}
	if err := checkText(p.Instructions, maxProjectInstructions, true); err != nil {
		return fmt.Errorf("instructions: %w", err)
	}
	a := p.Account
	if a == nil {
		return nil
	}
	if k := KindOf(p); k != ProjectAccount {
		return fmt.Errorf("account: only an account project carries an account, and %q is %s", p.Name, aKind(k))
	}
	if err := checkText(a.Customer, maxAccountCustomer, false); err != nil {
		return fmt.Errorf("account.customer: %w", err)
	}
	if err := checkText(a.ExternalID, maxAccountExternalID, false); err != nil {
		return fmt.Errorf("account.external_id: %w", err)
	}
	if a.URL != "" {
		u, err := url.Parse(a.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(a.URL) > maxAccountURL {
			return fmt.Errorf("account.url: an https URL of at most %d characters, with no credentials", maxAccountURL)
		}
	}
	return nil
}

// checkText checks a field's length in characters and that it holds no
// control characters, other than line breaks and tabs where lines are allowed.
func checkText(s string, limit int, lines bool) error {
	if n := utf8.RuneCountInString(s); n > limit {
		return fmt.Errorf("at most %d characters, not %d", limit, n)
	}
	if strings.ContainsFunc(s, func(r rune) bool {
		return unicode.IsControl(r) && (!lines || (r != '\n' && r != '\t' && r != '\r'))
	}) {
		if lines {
			return fmt.Errorf("plain text, with no control characters but line breaks and tabs")
		}
		return fmt.Errorf("one line of plain text")
	}
	return nil
}
