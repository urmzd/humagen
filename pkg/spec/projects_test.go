package spec

import (
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/threads"
)

// platformWith is a Platform document whose workspace is body, indented
// under it.
func platformWith(body string) string {
	return "api_version: humagen/v1\nkind: Platform\nmetadata: {name: default}\nspec:\n  workspace:\n" + body
}

// tree is a workspace.projects list, one flow mapping per entry.
func tree(entries ...string) string {
	var b strings.Builder
	b.WriteString("    projects:\n")
	for _, e := range entries {
		b.WriteString("      - " + e + "\n")
	}
	return b.String()
}

func loadBody(t *testing.T, body string) (Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "humagen.yaml")
	writeFile(t, path, body)
	return Load(path)
}

// Each of the tree's checks refuses the file at start and in humagen config
// check, naming the document and the path of the entry that breaks it.
func TestEachProjectCheckNamesItsPath(t *testing.T) {
	companies := make([]string, 11)
	for i := range companies {
		companies[i] = fmt.Sprintf("{name: C%d, kind: company}", i)
	}
	many := make([]string, 501)
	for i := range many {
		many[i] = fmt.Sprintf("{name: P%d}", i)
	}
	for name, tc := range map[string]struct{ body, want string }{
		// 1. Names match the project regex and are unique ignoring case.
		"a name that starts with a dash": {tree(`{name: "-x"}`), `workspace.projects[0].name: "-x" is not a project name`},
		"a name with a slash":            {tree(`{name: Field}`, `{name: "a/b"}`), `workspace.projects[1].name: "a/b" is not a project name`},
		"a name that starts with ~":      {tree(`{name: "~x"}`), `workspace.projects[0].name: "~x" is not a project name`},
		"no name":                        {tree(`{description: Nameless}`), `workspace.projects[0].name: "" is not a project name`},
		"a name too long":                {tree(`{name: ` + strings.Repeat("a", 64) + `}`), `workspace.projects[0].name:`},
		"one name twice, ignoring case":  {tree(`{name: Field}`, `{name: field}`), `workspace.projects[1].name: "field" is workspace.projects[0]'s name again`},
		// 2. A parent names a project in the same file.
		"a parent only the console has": {tree(`{name: Globex, parent: Field}`), `workspace.projects[0].parent: "Field" is not a project in this file`},
		"a parent in another case":      {tree(`{name: Field}`, `{name: Globex, parent: field}`), `workspace.projects[1].parent: "field" is not a project in this file; names are exact, so write "Field"`},
		// 3. No cycle; depth; company and account placement; the company cap;
		// an account only on an account project.
		"its own parent": {tree(`{name: A, parent: A}`), `workspace.projects[0].parent: "A" cannot be its own parent`},
		"a cycle":        {tree(`{name: Top}`, `{name: A, parent: B}`, `{name: B, parent: A}`), `workspace.projects[1].parent: "A" is its own ancestor`},
		"a cycle below a project": {
			tree(`{name: Hangs, parent: A}`, `{name: A, parent: B}`, `{name: B, parent: A}`), `workspace.projects[1].parent: "A" is its own ancestor`,
		},
		"depth 5": {
			tree(`{name: L1}`, `{name: L2, parent: L1}`, `{name: L3, parent: L2}`, `{name: L4, parent: L3}`, `{name: L5, parent: L4}`),
			`workspace.projects[4].parent: "L5" would sit 5 levels below the workspace; at most 4`,
		},
		"company under a team": {
			tree(`{name: Field}`, `{name: News, kind: company, parent: Field}`),
			`workspace.projects[1].kind: a company project sits under the workspace or another company project, and "Field" is a team`,
		},
		"company under an account": {
			tree(`{name: Globex, kind: account}`, `{name: News, kind: company, parent: Globex}`),
			`workspace.projects[1].kind: a company project sits under the workspace or another company project, and "Globex" is an account`,
		},
		"an account under an account": {
			tree(`{name: Globex, kind: account}`, `{name: Renewals, kind: account, parent: Globex}`),
			`workspace.projects[1].kind: an account cannot sit under another account, and "Globex" is one`,
		},
		"eleven company projects": {tree(companies...), `workspace.projects[10].kind: a workspace has at most 10 company projects`},
		"an account on a team": {
			tree(`{name: Field, account: {customer: Globex Inc}}`),
			`workspace.projects[0].account: only an account project carries an account, and "Field" is a team`,
		},
		// 4. Kinds are built in or the workspace's own, which behave as team
		// or account, and the kind rules follow what a kind behaves as.
		"an unknown kind": {tree(`{name: Partners, kind: partner}`), `workspace.projects[0].kind: "partner" is not a kind: company, team, account or one of workspace.project_kinds`},
		"a kind like company": {
			"    project_kinds: [{name: division, like: company}]\n", `workspace.project_kinds[0].like: "company" is not team or account`,
		},
		"a built-in kind defined again": {"    project_kinds: [{name: team, like: team}]\n", `workspace.project_kinds[0].name: "team" is a kind Humagen defines`},
		"a kind defined twice": {
			"    project_kinds: [{name: partner, like: account}, {name: partner, like: team}]\n", `workspace.project_kinds[1].name: "partner" is defined twice`,
		},
		"a kind's name in capitals": {"    project_kinds: [{name: Partner, like: account}]\n", `workspace.project_kinds[0].name: "Partner" is not a kind's name`},
		"an account-like kind under an account": {
			"    project_kinds: [{name: partner, like: account}]\n" + tree(`{name: Globex, kind: account}`, `{name: Reseller, kind: partner, parent: Globex}`),
			`workspace.projects[1].kind: an account cannot sit under another account, and "Globex" is one`,
		},
		// 5. Members: their forms, roles and caps, and an entry for everyone.
		"a member with no role": {tree(`{name: Field, members: [{who: "*", role: member}, {who: ana, role: boss}]}`), `workspace.projects[0].members[1].role: "boss" is not a role`},
		"a role with a space":   {tree(`{name: Field, members: [{who: "*", role: member}, {who: "role:field eng", role: owner}]}`), `workspace.projects[0].members[1].who: "role:field eng" names no role`},
		"one person twice":      {tree(`{name: Field, members: [{who: "*", role: member}, {who: ana, role: owner}, {who: ana, role: member}]}`), `workspace.projects[0].members[2].who: "ana" is listed twice`},
		"members only under a project everyone owns": {
			tree(`{name: Field, members: [{who: "*", role: owner}]}`, `{name: Globex, parent: Field, members: [{who: ana, role: owner}]}`),
			`workspace.projects[1].members: everyone owns "Field", which owns everything below it, so "Globex" cannot be members only`,
		},
		"a company project for viewers": {tree(`{name: News, kind: company, members: [{who: "*", role: viewer}]}`), `workspace.projects[0].members: a company project is open to everyone ("*") as a member at least`},
		"the workspace for viewers":     {"    members: [{who: \"*\", role: viewer}]\n", `workspace.members: a company project is open to everyone ("*") as a member at least`},
		"a child more open than its parent": {
			tree(`{name: Field, members: [{who: "*", role: viewer}]}`, `{name: Globex, parent: Field, members: [{who: "*", role: member}]}`),
			`workspace.projects[1].members: "Globex" would be more open than "Field"`,
		},
		// The fields' bounds, which the store would refuse at start.
		"501 projects":                 {tree(many...), `workspace.projects: a workspace has at most 500 projects, not 501`},
		"a long title":                 {tree(`{name: Field, title: ` + strings.Repeat("t", 81) + `}`), `workspace.projects[0].title: at most 80 characters, not 81`},
		"a title on two lines":         {tree(`{name: Field, title: "Field\nengineering"}`), `workspace.projects[0].title: one line of plain text`},
		"a long description":           {tree(`{name: Field, description: ` + strings.Repeat("d", 501) + `}`), `workspace.projects[0].description: at most 500 characters`},
		"long instructions":            {tree(`{name: Field, instructions: ` + strings.Repeat("i", 4001) + `}`), `workspace.projects[0].instructions: at most 4000 characters`},
		"long workspace instructions":  {"    instructions: " + strings.Repeat("i", 4001) + "\n", `workspace.instructions: at most 4000 characters, not 4001`},
		"a control character":          {tree(`{name: Field, description: "a\u0007b"}`), `workspace.projects[0].description: plain text`},
		"an account link that is http": {tree(`{name: Globex, kind: account, account: {url: "http://crm.example.com/1"}}`), `workspace.projects[0].account.url: an https URL`},
		"a long customer":              {tree(`{name: Globex, kind: account, account: {customer: ` + strings.Repeat("c", 121) + `}}`), `workspace.projects[0].account.customer: at most 120 characters`},
		// Channel slugs, each once, at most 20 with general, and their fields.
		"a channel name with capitals": {tree(`{name: Field, channels: [{name: Renewals}]}`), `workspace.projects[0].channels[0].name: "Renewals" is not a channel name`},
		"one channel twice":            {tree(`{name: Field, channels: [{name: ops}, {name: ops}]}`), `workspace.projects[0].channels[1].name: "ops" is channels[0]'s name again`},
		"21 channels":                  {tree(`{name: Field, channels: [` + channelList(20) + `]}`), `workspace.projects[0].channels: a project has at most 20 channels, general among them, not 21`},
		"a long purpose":               {tree(`{name: Field, channels: [{name: ops, purpose: ` + strings.Repeat("p", 201) + `}]}`), `workspace.projects[0].channels[0].purpose: at most 200 characters, not 201`},
		"a default that is no name":    {tree(`{name: Field, channels: [{name: ops, default: "@ignite"}]}`), `workspace.projects[0].channels[0].default: "@ignite" is not an agent's name or auto`},
		"a workspace channel misnamed": {"    channels: [{name: \"news room\"}]\n", `workspace.channels[0].name: "news room" is not a channel name`},
		// A Slack binding names a client and a Slack channel, bound once.
		"a binding to a direct message": {tree(`{name: Field, channels: [{name: ops, slack: {client: slack-acme, channel: D0123}}]}`), `workspace.projects[0].channels[0].slack.channel: "D0123" is not a Slack channel's id`},
		"a binding to a client's title": {tree(`{name: Field, channels: [{name: ops, slack: {client: Slack Acme, channel: C0123}}]}`), `workspace.projects[0].channels[0].slack.client: "Slack Acme" is not a client's id`},
		"one Slack channel bound twice": {
			tree(`{name: Field, channels: [{name: ops, slack: {client: slack-acme, channel: C0123}}]}`, `{name: Launch, channels: [{name: general, slack: {client: slack-acme, channel: C0123}}]}`),
			`workspace.projects[1].channels[0].slack.channel: C0123 of slack-acme is bound at workspace.projects[0].channels[0].slack.channel already`,
		},
		// Experts: who, 1 to 8 topics, at most 50.
		"an expert with no topics":    {tree(`{name: Field, experts: [{who: "okta|00u1ana", topics: []}]}`), `workspace.projects[0].experts[0].topics: from 1 to 8 topics, not 0`},
		"an expert topic in capitals": {tree(`{name: Field, experts: [{who: "okta|00u1ana", topics: [Billing]}]}`), `workspace.projects[0].experts[0].topics[0]: "Billing" is not a topic`},
		"everyone as an expert":       {tree(`{name: Field, experts: [{who: "*", topics: [billing]}]}`), `workspace.projects[0].experts[0].who: a person's subject or role:<name>`},
		"a role that is no role":      {"    experts: [{who: \"role:Field Leads\", topics: [security]}]\n", `workspace.experts[0].who: "role:Field Leads" is not a role`},
		// Knowledge sources: a source's name, each once, at most 8.
		"a source in capitals":        {tree(`{name: Field, knowledge: [Handbook]}`), `workspace.projects[0].knowledge[0]: "Handbook" is not a knowledge source's name`},
		"a source twice":              {tree(`{name: Field, knowledge: [handbook, playbook, handbook]}`), `workspace.projects[0].knowledge[2]: handbook is knowledge[0] again`},
		"nine sources":                {tree(`{name: Field, knowledge: [a, b, c, d, e, f, g, h, i]}`), `workspace.projects[0].knowledge: a project names at most 8 knowledge sources, not 9`},
		"a workspace source misnamed": {"    knowledge: [\"the handbook\"]\n", `workspace.knowledge[0]: "the handbook" is not a knowledge source's name`},
		// Skills: a skill's name, each once, at most 32, with a
		// description and instructions.
		"a skill in capitals":          {tree(`{name: Field, skills: [{name: Review, description: d, instructions: i}]}`), `workspace.projects[0].skills[0].name: "Review" is not a skill's name`},
		"a skill named like a price":   {"    skills: [{name: 5x, description: d, instructions: i}]\n", `workspace.skills[0].name: "5x" is not a skill's name`},
		"a skill twice":                {tree(`{name: Field, skills: [{name: review, description: d, instructions: i}, {name: review, description: d, instructions: i}]}`), `workspace.projects[0].skills[1].name: review is skills[0] again`},
		"a skill with no description":  {tree(`{name: Field, skills: [{name: review, instructions: i}]}`), `workspace.projects[0].skills[0].description: say what the skill is for`},
		"a skill with no instructions": {tree(`{name: Field, skills: [{name: review, description: d}]}`), `workspace.projects[0].skills[0].instructions: a skill has instructions`},
		"long skill instructions":      {tree(`{name: Field, skills: [{name: review, description: d, instructions: ` + strings.Repeat("i", 8001) + `}]}`), `workspace.projects[0].skills[0].instructions: at most 8000 characters, not 8001`},
		"a long argument hint":         {tree(`{name: Field, skills: [{name: review, description: d, argument_hint: ` + strings.Repeat("h", 101) + `, instructions: i}]}`), `workspace.projects[0].skills[0].argument_hint: at most 100 characters, not 101`},
		"33 skills":                    {tree(`{name: Field, skills: [` + skillList(33) + `]}`), `workspace.projects[0].skills: a project declares at most 32 skills, not 33`},
		// Mention kinds: a kind's name, not a built-in one, each once, with
		// a title and what it resolves to.
		"a built-in kind again":    {"    mention_kinds: [{kind: sme, title: Experts, resolves: experts}]\n", `workspace.mention_kinds[0].kind: sme is reserved`},
		"a kind in capitals":       {"    mention_kinds: [{kind: Team, title: Teams, resolves: role}]\n", `workspace.mention_kinds[0].kind: "Team" is not a kind's name`},
		"a kind with no title":     {"    mention_kinds: [{kind: team, resolves: role}]\n", `workspace.mention_kinds[0].title: name the kind`},
		"a kind resolving nothing": {"    mention_kinds: [{kind: team, title: Teams, resolves: teams}]\n", `workspace.mention_kinds[0].resolves: "teams" is experts, skills, role or people`},
		"a kind twice":             {"    mention_kinds: [{kind: team, title: Teams, resolves: role}, {kind: team, title: T, resolves: people}]\n", `workspace.mention_kinds[1].kind: team is mention_kinds[0] again`},
	} {
		_, err := loadBody(t, platformWith(tc.body))
		if want := `platform "default": ` + tc.want; err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", name, err, want)
		}
	}
	// A field the language lacks is refused as it is read, before the checks.
	_, err := loadBody(t, platformWith(tree(`{name: Field, parnet: Top}`)))
	if err == nil || !strings.Contains(err.Error(), `(Platform default): spec: json: unknown field "parnet"`) {
		t.Errorf("a field the language lacks: err = %v", err)
	}
}

// channelList is n channels besides general, as a flow list.
func channelList(n int) string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("{name: c%d}", i)
	}
	return strings.Join(names, ", ")
}

func skillList(n int) string {
	var out []string
	for i := range n {
		out = append(out, fmt.Sprintf("{name: s%d, description: d, instructions: i}", i))
	}
	return strings.Join(out, ", ")
}

// Skills and mention kinds the store and the grammar accept pass, on the
// workspace and on a project.
func TestSkillsAndMentionKindsTheStoreTakesPass(t *testing.T) {
	got, err := loadBody(t, platformWith("    skills: [{name: summarize, description: Summarize a thread., instructions: \"Summarize in five bullets.\"}]\n"+
		"    mention_kinds: [{kind: team, title: Teams, resolves: role}, {kind: dev, title: Developers, resolves: people}]\n"+
		tree(`{name: Field, skills: [`+skillList(32)+`]}`, `{name: Globex, parent: Field, skills: []}`)))
	if err != nil {
		t.Fatal(err)
	}
	w := got.Platforms[0].Spec.Workspace
	if len(w.Skills) != 1 || len(w.MentionKinds) != 2 || w.MentionKinds[0].Resolves != "role" || len(w.Projects[0].Skills) != 32 || w.Projects[1].Skills == nil {
		t.Fatalf("read %+v", w)
	}
}

// Knowledge sources the store accepts pass, on the workspace and on a
// project, whether or not the workspace has them yet.
func TestKnowledgeTheStoreTakesPasses(t *testing.T) {
	got, err := loadBody(t, platformWith("    knowledge: [handbook]\n"+tree(`{name: Field, knowledge: [playbook, 0-day, handbook, a, b, c, d, e]}`, `{name: Globex, parent: Field, knowledge: []}`)))
	if err != nil {
		t.Fatal(err)
	}
	w := got.Platforms[0].Spec.Workspace
	if len(w.Knowledge) != 1 || len(w.Projects[0].Knowledge) != 8 || w.Projects[0].Knowledge[0] != "playbook" || w.Projects[1].Knowledge == nil || len(w.Projects[1].Knowledge) != 0 {
		t.Fatalf("read %+v", w)
	}
}

// Channels and experts the store accepts pass: general declared to set its
// purpose, a channel with a default, experts by subject and by role, on the
// workspace and on a project.
func TestChannelsAndExpertsTheStoreTakesPass(t *testing.T) {
	body := "    channels: [{name: announcements, purpose: Company news}]\n" +
		"    experts: [{who: \"role:security\", topics: [security]}]\n" +
		tree(`{name: Globex, kind: account, channels: [{name: general, purpose: The account}, {name: renewals, title: Renewals, purpose: Renewal work, default: globex-helper}, `+channelList(17)+`], experts: [{who: "okta|00u1ana", topics: [billing, renewals]}]}`)
	got, err := loadBody(t, platformWith(body))
	if err != nil {
		t.Fatal(err)
	}
	w := got.Platforms[0].Spec.Workspace
	if len(w.Channels) != 1 || len(w.Experts) != 1 || len(w.Projects[0].Channels) != 19 || w.Projects[0].Experts[0].Topics[1] != "renewals" {
		t.Fatalf("read %+v", w)
	}
}

// A tree the store accepts passes: four levels, children listed before their
// parents, company under company, an account under a team with its customer,
// "~" for the workspace, and ten company projects.
func TestATreeTheStoreTakesPasses(t *testing.T) {
	entries := []string{
		`{name: Renewals, parent: Globex, instructions: "Quote the contract.\nNever promise dates."}`,
		`{name: Globex, parent: Field, kind: account, account: {customer: Globex Inc, external_id: "sf:0015g00000ABC", url: "https://crm.example.com/globex"}}`,
		`{name: Field, title: Field engineering, kind: team, instructions: Customer accounts.}`,
		`{name: Q4, parent: Renewals}`,
		`{name: News, kind: company, parent: "~"}`,
		`{name: Releases, kind: company, parent: News}`,
	}
	for i := range 8 {
		entries = append(entries, fmt.Sprintf("{name: C%d, kind: company}", i))
	}
	cfg, err := loadBody(t, platformWith("    instructions: Acme sells warehouse robots.\n"+tree(entries...)))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Check(Kinds{}); err != nil {
		t.Fatalf("strict check: %v", err)
	}
	w := cfg.Platforms[0].Spec.Workspace
	if *w.Instructions != "Acme sells warehouse robots." || len(w.Projects) != 14 {
		t.Fatalf("workspace = %+v", w)
	}
	g := w.Projects[1]
	if g.Kind != ProjectAccount || g.Parent != "Field" || g.Account.Customer != "Globex Inc" || g.Account.ExternalID != "sf:0015g00000ABC" {
		t.Fatalf("Globex = %+v", g)
	}
	if ParentOf(w.Projects[4]) != "" || KindOf(w.Projects[3]) != ProjectTeam {
		t.Fatalf(`"~" is the workspace and team the default kind: %+v %+v`, w.Projects[4], w.Projects[3])
	}
}

// The tree a file declares round-trips through what humagen writes.
func TestWhatHumagenWritesOfATreeItReads(t *testing.T) {
	instr := "Answer plainly."
	cfg := Config{Platforms: []Platform{{Metadata: Metadata{Name: "default"}, Spec: PlatformSpec{Workspace: Workspace{
		Instructions: &instr,
		Projects: []Project{
			{Name: "Field", Title: "Field engineering", Kind: ProjectTeam, Instructions: "Never promise dates.\nBe brief.",
				Channels: []Channel{{Name: "renewals", Purpose: "Renewal work", Default: "auto"}}, Experts: []Expert{{Who: "role:billing", Topics: []string{"billing"}}}},
			{Name: "Globex", Parent: "Field", Kind: "partner", Account: &Account{Customer: "Globex Inc", ExternalID: "sf:1", URL: "https://crm.example.com/g"},
				Members: []Member{{Who: "*", Role: RoleViewer}, {Who: "okta|00u1ana", Role: RoleOwner}}},
		},
		Members:      []Member{{Who: "*", Role: RoleMember}, {Who: "role:comms", Role: RoleOwner}},
		ProjectKinds: []ProjectKind{{Name: "partner", Title: "Partner", Like: ProjectAccount}},
	}}}}}
	cfg.System.Defaults()
	b, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := loadBody(t, string(b))
	if err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	w := got.Platforms[0].Spec.Workspace
	if *w.Instructions != instr || len(w.Projects) != 2 || !reflect.DeepEqual(w.Projects, cfg.Platforms[0].Spec.Workspace.Projects) {
		t.Fatalf("round trip lost something:\n%s", b)
	}
}

// The language's bounds are the store's, so a file humagen config check passes
// is one the store takes, and one it refuses the store would refuse too.
func TestTheLanguagesBoundsAreTheProjectStores(t *testing.T) {
	caps := projects.DefaultCaps()
	for name, pair := range map[string][2]int{
		"projects":      {maxProjects, caps.Projects},
		"depth":         {maxProjectDepth, caps.Depth},
		"company":       {maxCompany, caps.Company},
		"title":         {maxProjectTitle, projects.MaxTitle},
		"description":   {maxProjectDescription, projects.MaxDescription},
		"instructions":  {maxProjectInstructions, projects.MaxInstructions},
		"customer":      {maxAccountCustomer, projects.MaxCustomer},
		"external id":   {maxAccountExternalID, projects.MaxExternalID},
		"url":           {maxAccountURL, projects.MaxURL},
		"channels":      {maxChannels, projects.MaxChannels},
		"channel title": {maxChannelTitle, projects.MaxTitle},
		"purpose":       {maxPurpose, projects.MaxPurpose},
		"experts":       {maxExperts, projects.MaxExperts},
		"knowledge":     {maxKnowledge, projects.MaxKnowledge},
		"skills":        {maxSkills, projects.MaxSkills},
		"skill name":    {maxSkillName, projects.MaxSkillName},
		"skill text":    {maxSkillDescription, projects.MaxSkillDescription},
		"argument hint": {maxArgumentHint, projects.MaxArgumentHint},
		"skill body":    {maxSkillInstructions, projects.MaxSkillInstructions},
		"topics":        {maxExpertTopics, projects.MaxTopics},
		"expert who":    {maxExpertWho, projects.MaxWho},
		"members":       {maxMembers, projects.MaxMembers},
		"kinds":         {maxKinds, projects.MaxKinds},
		"kind title":    {maxKindTitle, projects.MaxKindTitle},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s: the spec allows %d, the store %d", name, pair[0], pair[1])
		}
	}
	for _, name := range []string{
		"Field", "a", "Globex Inc", "q4_launch-2", strings.Repeat("a", 63), strings.Repeat("a", 64),
		"", " lead", "-x", "_x", "~", "~x", "a/b", "a.b", "Ünïcode", "tab\tname", "a:b",
	} {
		if projectName.MatchString(name) != projects.ValidName(name) {
			t.Errorf("%q: the spec says %v, the store %v", name, projectName.MatchString(name), projects.ValidName(name))
		}
	}
	for _, name := range []string{"general", "renewals", "q4-launch", "a", strings.Repeat("a", 40), strings.Repeat("a", 41), "", "-x", "Renewals", "a_b", "a b", "ünï"} {
		if channelName.MatchString(name) != projects.ValidChannel(name) {
			t.Errorf("channel %q: the spec says %v, the store %v", name, channelName.MatchString(name), projects.ValidChannel(name))
		}
	}
	for _, topic := range []string{"billing", "a", "q4-renewals", strings.Repeat("a", 32), strings.Repeat("a", 33), "", "4q", "Billing", "a_b"} {
		if topicName.MatchString(topic) != projects.ValidTopic(topic) {
			t.Errorf("topic %q: the spec says %v, the store %v", topic, topicName.MatchString(topic), projects.ValidTopic(topic))
		}
	}
	for _, skill := range []string{"review", "a", "q4-report", "a1-b2", strings.Repeat("a", 64), strings.Repeat("a", 65), "", "5x", "-a", "a-", "a--b", "Review", "a_b", "a.b"} {
		if (len(skill) <= maxSkillName && skillName.MatchString(skill)) != projects.ValidSkill(skill) {
			t.Errorf("skill %q: the spec says %v, the store %v", skill, skillName.MatchString(skill), projects.ValidSkill(skill))
		}
	}
	for _, r := range append(slices.Clone(mentionResolves), "teams", "") {
		if slices.Contains(mentionResolves, r) != threads.ValidResolves(r) {
			t.Errorf("resolves %q: the spec says %v, the grammar %v", r, slices.Contains(mentionResolves, r), threads.ValidResolves(r))
		}
	}
	for kind := range threads.BuiltInKinds {
		if !slices.Contains(reservedKinds, kind) {
			t.Errorf("the built-in kind %s is not reserved", kind)
		}
	}
	for _, source := range []string{"handbook", "a", "0-day", "q4-notes", strings.Repeat("a", 63), strings.Repeat("a", 64), "", "-x", "Handbook", "a_b", "a b", "a,b", "ünï"} {
		if sourceName.MatchString(source) != projects.ValidSource(source) {
			t.Errorf("source %q: the spec says %v, the store %v", source, sourceName.MatchString(source), projects.ValidSource(source))
		}
	}
	for _, agent := range []string{"auto", "globex-helper", "humagen_writer", "Ignite", "@x", "a.b", strings.Repeat("a", 64), strings.Repeat("a", 65)} {
		if defaultName.MatchString(agent) != projects.ValidDefault(agent) {
			t.Errorf("default %q: the spec says %v, the store %v", agent, defaultName.MatchString(agent), projects.ValidDefault(agent))
		}
	}
	if generalChannel != projects.General || autoDefault != projects.Auto || expertRolePrefix != projects.RolePrefix {
		t.Error("general, auto or role: is written differently in the spec and the store")
	}
	if RootParent != projects.RootAddress {
		t.Errorf("the workspace is written %q in the spec and %q in addresses", RootParent, projects.RootAddress)
	}
}
