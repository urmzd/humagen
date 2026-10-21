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
	maxProjects     = 500
	maxProjectDepth = 4
	maxCompany      = 10

	maxProjectTitle        = 80
	maxProjectDescription  = 500
	maxProjectInstructions = 4000
	maxAccountCustomer     = 120
	maxAccountExternalID   = 120
	maxAccountURL          = 500

	maxChannels      = 20
	maxChannelTitle  = 80
	maxPurpose       = 200
	maxExperts       = 50
	maxExpertTopics  = 8
	maxExpertWho     = 200
	generalChannel   = "general"
	autoDefault      = "auto"
	expertRolePrefix = "role:"
	maxMembers    = 100
	maxKinds      = 16
	maxKindTitle  = 40
	maxSubject    = 255
	everyoneEntry = "*"
	roleEntry     = "role:"
)

var (
	projectName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _-]{0,62}$`)
	channelName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	topicName   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	// defaultName is an agent's name, as a client's agent list holds it,
	// or auto.
	defaultName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	expertRole  = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	kindName    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	roleName    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)
)

// checkProjects holds the workspace's instructions and project tree to what
// the project store accepts, each mistake with its path:
//
//  1. names match the project regex and are unique ignoring case;
//  2. a parent names a project in this file;
//  3. no cycle, depth at most 4, at most 10 company projects, company only
//     under the workspace or company, account never under account, and an
//     account only on an account project;
//  4. a kind is company, team, account or one of project_kinds, and each of
//     those behaves as team or account;
//  5. members are "*", "role:<name>" or a subject, each with viewer, member
//     or owner, at most 100 and no one twice; until reads are confined to
//     members, every list the file sets has an entry for everyone of at
//     least viewer, a company project's (the workspace's too) of at least
//     member, and no project is more open than a parent the file also gives
//     members.
//
// The fields' bounds are checked as well. A project the console made cannot
// be seen from here, so the store checks again when the file is applied.
func checkProjects(w Workspace) error {
	if w.Instructions != nil {
		if err := checkText(*w.Instructions, maxProjectInstructions, true); err != nil {
			return fmt.Errorf("workspace.instructions: %w", err)
		}
	}
	if err := checkChannels(w.Channels); err != nil {
		return fmt.Errorf("workspace.%w", err)
	}
	if err := checkExperts(w.Experts); err != nil {
		return fmt.Errorf("workspace.%w", err)
	}
	likes, err := checkKinds(w.ProjectKinds)
	if err != nil {
		return err
	}
	if w.Members != nil {
		if err := checkMembers(w.Members, ProjectCompany); err != nil {
			return fmt.Errorf("workspace.members%w", err)
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
		if likes[KindOf(p)] == "" {
			return fmt.Errorf("%s.kind: %q is not a kind: company, team, account or one of workspace.project_kinds", at(i), p.Kind)
		}
		if err := checkProjectFields(p, likes[KindOf(p)]); err != nil {
			return fmt.Errorf("%s.%w", at(i), err)
		}
		if p.Members != nil {
			if err := checkMembers(p.Members, likes[KindOf(p)]); err != nil {
				return fmt.Errorf("%s.members%w", at(i), err)
			}
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
		kind, up := likes[KindOf(p)], ProjectCompany
		if parent[i] >= 0 {
			up = likes[KindOf(ps[parent[i]])]
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
		// A child is never more open than its parent, where the file sets
		// both lists; a list the console keeps is checked at start.
		above := w.Members
		if parent[i] >= 0 {
			above = ps[parent[i]].Members
		}
		if p.Members != nil && above != nil && openness(p.Members) > openness(above) {
			name := "the workspace"
			if parent[i] >= 0 {
				name = fmt.Sprintf("%q", ps[parent[i]].Name)
			}
			return fmt.Errorf("%s.members: %q would be more open than %s; a project is never more open than its parent", at(i), p.Name, name)
		}
	}
	return nil
}

// checkKinds checks the workspace's own kinds, and returns what every kind
// the file may name behaves as.
func checkKinds(ks []ProjectKind) (map[string]string, error) {
	likes := map[string]string{ProjectCompany: ProjectCompany, ProjectTeam: ProjectTeam, ProjectAccount: ProjectAccount}
	if len(ks) > maxKinds {
		return nil, fmt.Errorf("workspace.project_kinds: a workspace defines at most %d kinds, not %d", maxKinds, len(ks))
	}
	for i, k := range ks {
		at := fmt.Sprintf("workspace.project_kinds[%d]", i)
		switch {
		case !kindName.MatchString(k.Name):
			return nil, fmt.Errorf("%s.name: %q is not a kind's name: lower case letters, digits and dashes, starting with a letter, at most 32", at, k.Name)
		case k.Name == ProjectCompany || k.Name == ProjectTeam || k.Name == ProjectAccount:
			return nil, fmt.Errorf("%s.name: %q is a kind warp defines", at, k.Name)
		case likes[k.Name] != "":
			return nil, fmt.Errorf("%s.name: %q is defined twice", at, k.Name)
		case k.Like != ProjectTeam && k.Like != ProjectAccount:
			return nil, fmt.Errorf("%s.like: %q is not team or account", at, k.Like)
		}
		if err := checkText(k.Title, maxKindTitle, false); err != nil {
			return nil, fmt.Errorf("%s.title: %w", at, err)
		}
		likes[k.Name] = k.Like
	}
	return likes, nil
}

// checkMembers checks one list of members for a project that behaves as
// like. Its error starts with what follows the list's path.
func checkMembers(ms []Member, like string) error {
	if len(ms) > maxMembers {
		return fmt.Errorf(": a project has at most %d members, not %d", maxMembers, len(ms))
	}
	seen := map[string]bool{}
	star := ""
	for i, m := range ms {
		switch {
		case m.Who == everyoneEntry:
			star = m.Role
		case strings.HasPrefix(m.Who, roleEntry):
			if !roleName.MatchString(strings.TrimPrefix(m.Who, roleEntry)) {
				return fmt.Errorf("[%d].who: %q names no role: a role is letters, digits, dashes and underscores", i, m.Who)
			}
		case m.Who == "" || len(m.Who) > maxSubject || strings.TrimSpace(m.Who) != m.Who || checkText(m.Who, maxSubject, false) != nil:
			return fmt.Errorf("[%d].who: a member is *, role:<name> or a subject of at most %d bytes", i, maxSubject)
		}
		if m.Role != RoleViewer && m.Role != RoleMember && m.Role != RoleOwner {
			return fmt.Errorf("[%d].role: %q is not a role: viewer, member or owner", i, m.Role)
		}
		if seen[m.Who] {
			return fmt.Errorf("[%d].who: %q is listed twice", i, m.Who)
		}
		seen[m.Who] = true
	}
	switch {
	case star == "":
		return fmt.Errorf(`: an entry for everyone ("*") of at least viewer is needed: members-only projects need read confinement, which is not built`)
	case like == ProjectCompany && star == RoleViewer:
		return fmt.Errorf(`: a company project is open to everyone ("*") as a member at least`)
	}
	return nil
}

// openness ranks how widely a list opens a project: its entry for everyone,
// where an owner opens no wider than a member.
func openness(ms []Member) int {
	for _, m := range ms {
		if m.Who == everyoneEntry {
			if m.Role == RoleViewer {
				return 1
			}
			return 2
		}
	}
	return 0
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
func checkProjectFields(p Project, like string) error {
	if err := checkText(p.Title, maxProjectTitle, false); err != nil {
		return fmt.Errorf("title: %w", err)
	}
	if err := checkText(p.Description, maxProjectDescription, true); err != nil {
		return fmt.Errorf("description: %w", err)
	}
	if err := checkText(p.Instructions, maxProjectInstructions, true); err != nil {
		return fmt.Errorf("instructions: %w", err)
	}
	if err := checkChannels(p.Channels); err != nil {
		return err
	}
	if err := checkExperts(p.Experts); err != nil {
		return err
	}
	a := p.Account
	if a == nil {
		return nil
	}
	if like != ProjectAccount {
		return fmt.Errorf("account: only an account project carries an account, and %q is %s", p.Name, aKind(KindOf(p)))
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

// checkChannels checks a project's channels: names that are slugs, each
// once, at most 20 with general, and each field's bounds. A default names
// an agent or auto; whether it can answer where it is set is decided when
// a message is posted, and one that cannot answers nothing. Its error
// starts with "channels", for the caller to put after the project's path.
func checkChannels(cs []Channel) error {
	seen := map[string]int{}
	total := len(cs)
	for i, c := range cs {
		at := fmt.Sprintf("channels[%d]", i)
		if !channelName.MatchString(c.Name) {
			return fmt.Errorf("%s.name: %q is not a channel name: lower case letters, digits and dashes, starting with a letter or digit, at most 40", at, c.Name)
		}
		if j, dup := seen[c.Name]; dup {
			return fmt.Errorf("%s.name: %q is channels[%d]'s name again", at, c.Name, j)
		}
		seen[c.Name] = i
		if err := checkText(c.Title, maxChannelTitle, false); err != nil {
			return fmt.Errorf("%s.title: %w", at, err)
		}
		if err := checkText(c.Purpose, maxPurpose, false); err != nil {
			return fmt.Errorf("%s.purpose: %w", at, err)
		}
		if c.Default != "" && !defaultName.MatchString(c.Default) {
			return fmt.Errorf("%s.default: %q is not an agent's name or auto", at, c.Default)
		}
	}
	if _, declared := seen[generalChannel]; !declared {
		total++
	}
	if total > maxChannels {
		return fmt.Errorf("channels: a project has at most %d channels, general among them, not %d", maxChannels, total)
	}
	return nil
}

// checkExperts checks a project's experts: a subject or role:<name>, each
// with 1 to 8 topics of lower case letters, digits and dashes, at most 50.
// Its error starts with "experts".
func checkExperts(es []Expert) error {
	if len(es) > maxExperts {
		return fmt.Errorf("experts: a project names at most %d experts, not %d", maxExperts, len(es))
	}
	for i, e := range es {
		at := fmt.Sprintf("experts[%d]", i)
		who := strings.TrimSpace(e.Who)
		switch {
		case who == "" || who == "*" || who != e.Who || utf8.RuneCountInString(who) > maxExpertWho || checkText(who, maxExpertWho, false) != nil:
			return fmt.Errorf("%s.who: a person's subject or role:<name>, on one line of at most %d characters", at, maxExpertWho)
		case strings.HasPrefix(who, expertRolePrefix) && !expertRole.MatchString(strings.TrimPrefix(who, expertRolePrefix)):
			return fmt.Errorf("%s.who: %q is not a role: role:<name>, lower case letters, digits, dashes and underscores", at, who)
		case len(e.Topics) == 0 || len(e.Topics) > maxExpertTopics:
			return fmt.Errorf("%s.topics: from 1 to %d topics, not %d", at, maxExpertTopics, len(e.Topics))
		}
		for j, topic := range e.Topics {
			if !topicName.MatchString(topic) {
				return fmt.Errorf("%s.topics[%d]: %q is not a topic: lower case letters, digits and dashes, starting with a letter, at most 32", at, j, topic)
			}
		}
	}
	return nil
}
