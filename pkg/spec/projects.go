package spec

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The project tree's bounds, as the project store (pkg/projects) enforces
// them at start. They are checked here too, so humagen config check finds a
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
	openDefault      = "*"
	expertRolePrefix = "role:"

	maxKnowledge = 8

	maxSkills            = 32
	maxSkillName         = 64
	maxSkillDescription  = 1024
	maxArgumentHint      = 100
	maxSkillInstructions = 8000

	maxMentionKinds     = 16
	maxMentionKindTitle = 40

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
	// expertRole is the role an expert entry names, as the project store
	// takes it.
	expertRole = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	kindName   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	// roleName is the role a member entry names, as the project store
	// takes it.
	roleName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)
	// clientID is a client's id, and slackChannel a Slack channel's,
	// public (C) or private (G): a direct message is never bound.
	clientID     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	slackChannel = regexp.MustCompile(`^[CG][A-Z0-9]{2,31}$`)
	// sourceName is a knowledge source's name, as the knowledge store
	// takes it.
	sourceName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	// skillName is a skill's name, as the project store takes it.
	skillName = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	// mentionKindName is a mention kind's name, as the tag grammar reads it
	// before the colon.
	mentionKindName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
)

// mentionResolves is what a mention kind can resolve its items to, as the
// tag grammar (pkg/threads) takes it.
var mentionResolves = []string{"experts", "skills", "role", "people"}

// reservedKinds are the names no declared mention kind takes: the built-in
// kinds and @auto, which once picked an agent.
var reservedKinds = []string{"sme", "skill", "auto"}

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
//     or owner, at most 100 and no one twice; a list with no entry for
//     everyone makes the project members only, a company project's (the
//     workspace's too) has one of at least member, and no project is more
//     open than a parent the file also gives members.
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
	if err := checkBindings(w); err != nil {
		return err
	}
	if err := checkExperts(w.Experts); err != nil {
		return fmt.Errorf("workspace.%w", err)
	}
	if err := checkKnowledge(w.Knowledge); err != nil {
		return fmt.Errorf("workspace.%w", err)
	}
	if w.Run != nil {
		if err := w.Run.Check(); err != nil {
			return fmt.Errorf("workspace.run: %w", err)
		}
	}
	if err := checkSkills(w.Skills); err != nil {
		return fmt.Errorf("workspace.%w", err)
	}
	if err := checkMentionKinds(w.MentionKinds); err != nil {
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
		// A members-only project is read by its members alone, and owners
		// above own everything below: none sits under one everyone owns.
		if p.Members != nil && openness(p.Members) == 0 {
			for j := parent[i]; j >= 0; j = parent[j] {
				if everyoneOwns(ps[j].Members) {
					return fmt.Errorf("%s.members: everyone owns %q, which owns everything below it, so %q cannot be members only", at(i), ps[j].Name, p.Name)
				}
			}
			if everyoneOwns(w.Members) {
				return fmt.Errorf("%s.members: everyone owns the workspace, which owns everything below it, so %q cannot be members only", at(i), p.Name)
			}
		}
	}
	return nil
}

// everyoneOwns reports a list whose entry for everyone is owner.
func everyoneOwns(ms []Member) bool {
	for _, m := range ms {
		if m.Who == everyoneEntry {
			return m.Role == RoleOwner
		}
	}
	return false
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
			return nil, fmt.Errorf("%s.name: %q is a kind Humagen defines", at, k.Name)
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
	case like == ProjectCompany && (star == "" || star == RoleViewer):
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
	if err := checkKnowledge(p.Knowledge); err != nil {
		return err
	}
	if p.Run != nil {
		if err := p.Run.Check(); err != nil {
			return fmt.Errorf("run: %w", err)
		}
	}
	if err := checkSkills(p.Skills); err != nil {
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
		if c.Default != "" && c.Default != openDefault && !defaultName.MatchString(c.Default) {
			return fmt.Errorf("%s.default: %q is not an agent's name or auto", at, c.Default)
		}
		if b := c.Slack; b != nil {
			if !clientID.MatchString(b.Client) {
				return fmt.Errorf("%s.slack.client: %q is not a client's id", at, b.Client)
			}
			if !slackChannel.MatchString(b.Channel) {
				return fmt.Errorf("%s.slack.channel: %q is not a Slack channel's id, such as C0123ABCD; a direct message is never bound", at, b.Channel)
			}
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

// checkBindings checks that the file binds each Slack channel of a client
// to one channel.
func checkBindings(w Workspace) error {
	seen := map[ChannelSlack]string{}
	check := func(at string, cs []Channel) error {
		for i, c := range cs {
			if c.Slack == nil {
				continue
			}
			path := fmt.Sprintf("%s.channels[%d].slack.channel", at, i)
			if first, dup := seen[*c.Slack]; dup {
				return fmt.Errorf("%s: %s of %s is bound at %s already", path, c.Slack.Channel, c.Slack.Client, first)
			}
			seen[*c.Slack] = path
		}
		return nil
	}
	if err := check("workspace", w.Channels); err != nil {
		return err
	}
	for i, p := range w.Projects {
		if err := check(fmt.Sprintf("workspace.projects[%d]", i), p.Channels); err != nil {
			return err
		}
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

// checkKnowledge checks the knowledge sources a project names: at most 8,
// each a source's name, each once. Whether the workspace has them cannot be
// checked here, as sources are filled through the console: one it does not
// have grounds nothing. Its error starts with "knowledge".
func checkKnowledge(names []string) error {
	if len(names) > maxKnowledge {
		return fmt.Errorf("knowledge: a project names at most %d knowledge sources, not %d", maxKnowledge, len(names))
	}
	for i, n := range names {
		if !sourceName.MatchString(n) {
			return fmt.Errorf("knowledge[%d]: %q is not a knowledge source's name: lower case letters, digits and dashes, starting with a letter or digit, at most 63", i, n)
		}
		for j := range i {
			if names[j] == n {
				return fmt.Errorf("knowledge[%d]: %s is knowledge[%d] again", i, n, j)
			}
		}
	}
	return nil
}

// checkSkills checks the skills a project declares: at most 32, each named
// once with a skill's name, a description on one line of at most 1,024
// characters, an argument hint of at most 100, and instructions of at most
// 8,000. Its error starts with "skills".
func checkSkills(ss []Skill) error {
	if len(ss) > maxSkills {
		return fmt.Errorf("skills: a project declares at most %d skills, not %d", maxSkills, len(ss))
	}
	for i, s := range ss {
		at := fmt.Sprintf("skills[%d]", i)
		if len(s.Name) > maxSkillName || !skillName.MatchString(s.Name) {
			return fmt.Errorf("%s.name: %q is not a skill's name: lower case letters and digits in words joined by single dashes, starting with a letter, at most %d", at, s.Name, maxSkillName)
		}
		for j := range i {
			if ss[j].Name == s.Name {
				return fmt.Errorf("%s.name: %s is skills[%d] again", at, s.Name, j)
			}
		}
		if strings.TrimSpace(s.Description) == "" {
			return fmt.Errorf("%s.description: say what the skill is for", at)
		}
		if err := checkText(s.Description, maxSkillDescription, false); err != nil {
			return fmt.Errorf("%s.description: %w", at, err)
		}
		if err := checkText(s.ArgumentHint, maxArgumentHint, false); err != nil {
			return fmt.Errorf("%s.argument_hint: %w", at, err)
		}
		if strings.TrimSpace(s.Instructions) == "" {
			return fmt.Errorf("%s.instructions: a skill has instructions", at)
		}
		if err := checkText(s.Instructions, maxSkillInstructions, true); err != nil {
			return fmt.Errorf("%s.instructions: %w", at, err)
		}
	}
	return nil
}

// checkMentionKinds checks the mention kinds a workspace declares: at most
// 16, each a name the tag grammar reads before a colon, none a built-in
// kind's or another reserved name, each once, with a title of at most 40 and
// what it resolves to. Its error starts with "mention_kinds".
func checkMentionKinds(ks []MentionKind) error {
	if len(ks) > maxMentionKinds {
		return fmt.Errorf("mention_kinds: a workspace declares at most %d mention kinds, not %d", maxMentionKinds, len(ks))
	}
	for i, k := range ks {
		at := fmt.Sprintf("mention_kinds[%d]", i)
		switch {
		case !mentionKindName.MatchString(k.Kind):
			return fmt.Errorf("%s.kind: %q is not a kind's name: lower case letters, digits and dashes, starting with a letter, at most 32", at, k.Kind)
		case slices.Contains(reservedKinds, k.Kind):
			return fmt.Errorf("%s.kind: %s is reserved", at, k.Kind)
		case strings.TrimSpace(k.Title) == "":
			return fmt.Errorf("%s.title: name the kind as the console shows it", at)
		case !slices.Contains(mentionResolves, k.Resolves):
			return fmt.Errorf("%s.resolves: %q is experts, skills, role or people", at, k.Resolves)
		}
		if err := checkText(k.Title, maxMentionKindTitle, false); err != nil {
			return fmt.Errorf("%s.title: %w", at, err)
		}
		for j := range i {
			if ks[j].Kind == k.Kind {
				return fmt.Errorf("%s.kind: %s is mention_kinds[%d] again", at, k.Kind, j)
			}
		}
	}
	return nil
}
