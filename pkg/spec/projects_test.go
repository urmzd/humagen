package spec

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urmzd/warp/pkg/projects"
)

// platformWith is a Platform document whose workspace is body, indented
// under it.
func platformWith(body string) string {
	return "api_version: warp/v1\nkind: Platform\nmetadata: {name: default}\nspec:\n  workspace:\n" + body
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
	path := filepath.Join(t.TempDir(), "warp.yaml")
	writeFile(t, path, body)
	return Load(path)
}

// Each of the tree's checks refuses the file at start and in warp config
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
		// 4. Kinds are built in.
		"an unknown kind": {tree(`{name: Partners, kind: partner}`), `workspace.projects[0].kind: "partner" is not a kind: company, team or account`},
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
		// Experts: who, 1 to 8 topics, at most 50.
		"an expert with no topics":    {tree(`{name: Field, experts: [{who: "okta|00u1ana", topics: []}]}`), `workspace.projects[0].experts[0].topics: from 1 to 8 topics, not 0`},
		"an expert topic in capitals": {tree(`{name: Field, experts: [{who: "okta|00u1ana", topics: [Billing]}]}`), `workspace.projects[0].experts[0].topics[0]: "Billing" is not a topic`},
		"everyone as an expert":       {tree(`{name: Field, experts: [{who: "*", topics: [billing]}]}`), `workspace.projects[0].experts[0].who: a person's subject or role:<name>`},
		"a role that is no role":      {"    experts: [{who: \"role:Field Leads\", topics: [security]}]\n", `workspace.experts[0].who: "role:Field Leads" is not a role`},
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

// The tree a file declares round-trips through what warp writes.
func TestWhatWarpWritesOfATreeItReads(t *testing.T) {
	instr := "Answer plainly."
	cfg := Config{Platforms: []Platform{{Metadata: Metadata{Name: "default"}, Spec: PlatformSpec{Workspace: Workspace{
		Instructions: &instr,
		Projects: []Project{
			{Name: "Field", Title: "Field engineering", Kind: ProjectTeam, Instructions: "Never promise dates.\nBe brief.",
				Channels: []Channel{{Name: "renewals", Purpose: "Renewal work", Default: "auto"}}, Experts: []Expert{{Who: "role:billing", Topics: []string{"billing"}}}},
			{Name: "Globex", Parent: "Field", Kind: ProjectAccount, Account: &Account{Customer: "Globex Inc", ExternalID: "sf:1", URL: "https://crm.example.com/g"}},
		},
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
	if *w.Instructions != instr || len(w.Projects) != 2 || !reflect.DeepEqual(w.Projects[0], cfg.Platforms[0].Spec.Workspace.Projects[0]) || *w.Projects[1].Account != *cfg.Platforms[0].Spec.Workspace.Projects[1].Account {
		t.Fatalf("round trip lost something:\n%s", b)
	}
}

// The language's bounds are the store's, so a file warp config check passes
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
		"topics":        {maxExpertTopics, projects.MaxTopics},
		"expert who":    {maxExpertWho, projects.MaxWho},
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
	for _, agent := range []string{"auto", "globex-helper", "warp_writer", "Ignite", "@x", "a.b", strings.Repeat("a", 64), strings.Repeat("a", 65)} {
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
