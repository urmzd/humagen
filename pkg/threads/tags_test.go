package threads_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/urmzd/warp/pkg/threads"
)

// directory is a Resolver over fixed names.
type directory struct {
	agents map[string]bool
	people map[string]string
	slack  map[string]string
}

func (d directory) Agent(name string) bool { return d.agents[name] }

func (d directory) Person(handle string) (string, bool) {
	s, ok := d.people[handle]
	return s, ok
}

func (d directory) Slack(user string) (string, bool) {
	s, ok := d.slack[user]
	return s, ok
}

var acme = directory{
	agents: map[string]bool{"ignite": true, "globex-helper": true, "Scribe": true},
	people: map[string]string{"dana": "okta|dana", "ana.b": "okta|ana"},
	slack:  map[string]string{"U0DANA": "okta|dana"},
}

func agent(name string) threads.Mention  { return threads.Mention{Kind: threads.Agent, Name: name} }
func person(name string) threads.Mention { return threads.Mention{Kind: threads.Person, Name: name} }
func sme(topic string) threads.Mention   { return threads.Mention{Kind: threads.SME, Name: topic} }

func TestTheTagGrammar(t *testing.T) {
	for _, c := range []struct {
		text string
		want []threads.Mention
	}{
		{"@ignite draft the renewal note", []threads.Mention{agent("ignite")}},
		{"@auto what changed?", []threads.Mention{agent("auto")}},
		{"@AUTO and @Auto are one tag", []threads.Mention{agent("auto")}},
		{"@dana can you check?", []threads.Mention{person("okta|dana")}},
		{"thanks @ana.b.", []threads.Mention{person("okta|ana")}},
		{"@sme who knows?", []threads.Mention{sme("")}},
		{"@sme:billing and @sme:Billing", []threads.Mention{sme("billing")}},
		{"<@U0DANA> can you check?", []threads.Mention{person("okta|dana")}},
		{"<@U0DANA|dana> again", []threads.Mention{person("okta|dana")}},
		{"@ignite: please, and @globex-helper, @Scribe!", []threads.Mention{agent("ignite"), agent("globex-helper"), agent("Scribe")}},
		{"@ignite @dana @ignite", []threads.Mention{agent("ignite"), person("okta|dana")}},
		{"(@dana)", []threads.Mention{person("okta|dana")}},
		{"nothing to see", []threads.Mention{}},
	} {
		got, err := threads.Parse(c.text, acme)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("Parse(%q) = %+v, %v; want %+v", c.text, got, err, c.want)
		}
	}
}

func TestATagThatNamesNobodyIsKeptAndRefused(t *testing.T) {
	got, err := threads.Parse("@nobody @sme:1st <@U0EVE> look", acme)
	if err != nil || len(got) != 3 {
		t.Fatalf("Parse = %+v, %v", got, err)
	}
	for i, kind := range []string{threads.Person, threads.SME, threads.Person} {
		if got[i].Kind != kind || got[i].Refused == "" {
			t.Fatalf("tag %d = %+v; want a refused %s", i, got[i], kind)
		}
	}
	if got[0].Refused != "no one goes by @nobody" {
		t.Fatalf("refusal = %q", got[0].Refused)
	}
	// With no directory at all, every name is nobody's.
	if got, _ := threads.Parse("@ignite", nil); len(got) != 1 || got[0].Refused == "" {
		t.Fatalf("Parse with no resolver = %+v", got)
	}
}

func TestATokenInCodeIsNotATag(t *testing.T) {
	for _, text := range []string{
		"run `@ignite deploy` by hand",
		"```\n@ignite in a block\n```",
		"``code with ` and @dana inside``",
		"````\n```\n@dana in a longer fence\n```\n````",
	} {
		if got, err := threads.Parse(text, acme); err != nil || len(got) != 0 {
			t.Errorf("Parse(%q) = %+v, %v; want no tags", text, got, err)
		}
	}
	// A backtick that opens nothing is a backtick.
	if got, _ := threads.Parse("it's ` broken @dana", acme); len(got) != 1 {
		t.Fatalf("an unmatched backtick hid a tag: %+v", got)
	}
	if got, _ := threads.Parse("`@ignite` but @dana", acme); !reflect.DeepEqual(got, []threads.Mention{person("okta|dana")}) {
		t.Fatalf("a tag after a code span = %+v", got)
	}
}

func TestAnAtInsideAWordIsNotATag(t *testing.T) {
	for _, text := range []string{"mail dana@acme.com", "see https://x.example/@dana", "a@ignite", "@@ignite", "@ alone", "@-ignite", "@.dana"} {
		if got, _ := threads.Parse(text, acme); len(got) != 0 {
			t.Errorf("Parse(%q) = %+v; want no tags", text, got)
		}
	}
}

func TestAMessageTagsAtMostTen(t *testing.T) {
	var ten, eleven []string
	for i := range threads.MaxMentions + 1 {
		eleven = append(eleven, fmt.Sprintf("@p%d", i))
	}
	ten = eleven[:threads.MaxMentions]
	if got, err := threads.Parse(strings.Join(ten, " "), acme); err != nil || len(got) != threads.MaxMentions {
		t.Fatalf("ten tags = %d, %v", len(got), err)
	}
	if _, err := threads.Parse(strings.Join(eleven, " "), acme); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("eleven tags = %v, want ErrInvalid", err)
	}
	// One name tagged many times is one tag.
	if got, err := threads.Parse(strings.Repeat("@dana ", 40), acme); err != nil || len(got) != 1 {
		t.Fatalf("one tag forty times = %+v, %v", got, err)
	}
}

// counting is a Resolver that counts its lookups.
type counting struct {
	directory
	agents, people, slack int
}

func (c *counting) Agent(name string) bool { c.agents++; return c.directory.Agent(name) }

func (c *counting) Person(handle string) (string, bool) {
	c.people++
	return c.directory.Person(handle)
}

func (c *counting) Slack(user string) (string, bool) { c.slack++; return c.directory.Slack(user) }

// A message costs at most MaxMentions lookups of each kind, however long it
// is and however often it names someone: each name is looked up once, and a
// message naming more than MaxMentions is refused before the next lookup.
func TestAMessageCostsAtMostTenLookupsOfEachKind(t *testing.T) {
	r := &counting{directory: acme}
	if got, err := threads.Parse(strings.Repeat("@a ", 1<<18)+strings.Repeat("<@U0EVE> ", 1<<14), r); err != nil || len(got) != 2 {
		t.Fatalf("two names written many times = %+v, %v", got, err)
	}
	if r.agents != 1 || r.people != 1 || r.slack != 1 {
		t.Fatalf("one name each was looked up %d, %d and %d times", r.agents, r.people, r.slack)
	}
	for _, text := range []string{
		func() string {
			var b strings.Builder
			for i := range 1 << 15 {
				fmt.Fprintf(&b, "@p%d ", i)
			}
			return b.String()
		}(),
		// A colon after a name is punctuation, not another name.
		func() string {
			var b strings.Builder
			for i := range 1 << 15 {
				fmt.Fprintf(&b, "@x:%d ", i)
			}
			return b.String() + strings.Repeat("@q ", 20)
		}(),
	} {
		r := &counting{directory: acme}
		_, err := threads.Parse(text, r)
		if r.agents > threads.MaxMentions || r.people > threads.MaxMentions {
			t.Fatalf("a message looked up %d agents and %d people", r.agents, r.people)
		}
		if strings.HasPrefix(text, "@p") && !errors.Is(err, threads.ErrInvalid) {
			t.Fatalf("a message naming thousands = %v, want ErrInvalid", err)
		}
	}
}

// Text that cannot be posted is refused before anything reads it.
func TestTextThatCannotBePostedIsRefusedBeforeItIsRead(t *testing.T) {
	for _, text := range []string{"", "  \n", strings.Repeat("a", threads.MaxText+1), strings.Repeat("é", threads.MaxText+1), "bell\a"} {
		if err := threads.CheckText(text); !errors.Is(err, threads.ErrInvalid) {
			t.Errorf("CheckText(%.20q) = %v, want ErrInvalid", text, err)
		}
	}
	for _, text := range []string{"hi", strings.Repeat("é", threads.MaxText), "two\nlines\tand a tab"} {
		if err := threads.CheckText(text); err != nil {
			t.Errorf("CheckText(%.20q) = %v", text, err)
		}
	}
}

// workspace is a directory that also knows skills and mention kinds.
type workspace struct {
	directory
	skills map[string]bool
	kinds  map[string]string
	// asked counts the lookups of skills and kinds.
	skillAsks, kindAsks int
}

func (w *workspace) Skill(name string) bool { w.skillAsks++; return w.skills[name] }

func (w *workspace) MentionKind(kind string) (string, bool) {
	w.kindAsks++
	r, ok := w.kinds[kind]
	return r, ok
}

func newWorkspace() *workspace {
	return &workspace{
		directory: acme,
		skills:    map[string]bool{"review": true, "q4-report": true, "triage": true, "draft": true, "plan": true},
		kinds: map[string]string{
			"team": threads.ResolvesRole, "dev": threads.ResolvesPeople, "expert": threads.ResolvesExperts, "howto": threads.ResolvesSkills,
			"ignite": threads.ResolvesRole, "odd": "nothing",
		},
	}
}

func skillOf(name string) threads.Mention { return threads.Mention{Kind: threads.Skill, Name: name} }
func role(name string) threads.Mention    { return threads.Mention{Kind: threads.Role, Name: name} }

func TestSkillsAndMentionKinds(t *testing.T) {
	for _, c := range []struct {
		text string
		want []threads.Mention
	}{
		{"@ignite $review this", []threads.Mention{agent("ignite"), skillOf("review")}},
		{"$review, then $q4-report.", []threads.Mention{skillOf("review"), skillOf("q4-report")}},
		{"$review and @skill:review and @Skill:Review and @howto:review are one", []threads.Mention{skillOf("review")}},
		{"($review)", []threads.Mention{skillOf("review")}},
		{"$review-", []threads.Mention{skillOf("review")}},
		// A price, a variable or a $ inside a word is text.
		{"it costs $5, or US$5, see $PATH and $my_var and a$review", []threads.Mention{}},
		{"`$review` is code", []threads.Mention{}},
		// A mention kind resolves as it says.
		{"@team:sre and @team:Field_Eng", []threads.Mention{role("sre"), role("Field_Eng")}},
		{"@dev:dana", []threads.Mention{person("okta|dana")}},
		{"@expert:billing @sme:billing", []threads.Mention{sme("billing")}},
		// A kind declared under an agent's name takes an item; the agent
		// alone is still the agent.
		{"@ignite: hi @ignite", []threads.Mention{agent("ignite")}},
		{"@ignite:sre", []threads.Mention{role("sre")}},
		// A kind nobody declared, or one that resolves to nothing known, is
		// the name before the colon, as before kinds.
		{"@globex-helper:x", []threads.Mention{agent("globex-helper")}},
		{"@dana:x", []threads.Mention{person("okta|dana")}},
		{"@odd:x", []threads.Mention{threads.NoOne("odd")}},
	} {
		got, err := threads.Parse(c.text, newWorkspace())
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("Parse(%q) = %+v, %v; want %+v", c.text, got, err, c.want)
		}
	}
}

// A skill, a role or a person a kind names that is not there is kept and
// refused, and a skill nobody declared refuses the message (UnknownSkill).
func TestAnUnknownItemIsRefused(t *testing.T) {
	got, err := threads.Parse("$ghost @skill:a_b @team:9x @dev:nobody @expert:9x", newWorkspace())
	if err != nil || len(got) != 5 {
		t.Fatalf("Parse = %+v, %v", got, err)
	}
	for i, want := range []threads.Mention{
		{Kind: threads.Skill, Name: "ghost", Refused: "there is no skill called ghost"},
		{Kind: threads.Skill, Name: "a_b", Refused: "there is no skill called a_b"},
		{Kind: threads.Role, Name: "9x"},
		threads.NoOne("nobody"),
		{Kind: threads.SME, Name: "9x"},
	} {
		if got[i].Kind != want.Kind || got[i].Name != want.Name || got[i].Refused == "" || want.Refused != "" && got[i].Refused != want.Refused {
			t.Errorf("tag %d = %+v; want a refused %+v", i, got[i], want)
		}
	}
	if err := threads.UnknownSkill(got); !errors.Is(err, threads.ErrInvalid) || !strings.Contains(err.Error(), "there is no skill called ghost") {
		t.Fatalf("UnknownSkill = %v", err)
	}
	if err := threads.UnknownSkill([]threads.Mention{skillOf("review"), threads.NoOne("x")}); err != nil {
		t.Fatalf("UnknownSkill of a known skill = %v", err)
	}
	// With no lookups, no skill is known, and only the built-in kinds are.
	got, _ = threads.Parse("$review @team:sre", acme)
	if len(got) != 2 || got[0].Refused == "" || got[1] != threads.NoOne("team") {
		t.Fatalf("Parse with no lookups = %+v", got)
	}
}

func TestAMessageNamesAtMostFourSkills(t *testing.T) {
	w := newWorkspace()
	if got, err := threads.Parse("$review $q4-report $triage $draft", w); err != nil || len(got) != threads.MaxSkills {
		t.Fatalf("four skills = %+v, %v", got, err)
	}
	if _, err := threads.Parse("$review $q4-report $triage $draft $plan", w); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("five skills = %v, want ErrInvalid", err)
	}
	// A skill with no agent keeps room for the @auto it starts.
	var nine []string
	for i := range threads.MaxMentions - 1 {
		nine = append(nine, fmt.Sprintf("@p%d", i))
	}
	if got, err := threads.Parse(strings.Join(nine[1:], " ")+" $review", w); err != nil || len(threads.WithSkillAgent(got)) != threads.MaxMentions {
		t.Fatalf("eight tags and a skill = %v", err)
	}
	if _, err := threads.Parse(strings.Join(nine, " ")+" $review", w); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("nine tags, a skill and no agent = %v, want ErrInvalid", err)
	}
	if _, err := threads.Parse(strings.Join(nine[1:], " ")+" @ignite $review", w); err != nil {
		t.Fatalf("ten tags with an agent and a skill = %v", err)
	}
}

// Skills and kinds are looked up once each, and a message that names many
// costs no more than MaxMentions of them.
func TestSkillsAndKindsCostBoundedLookups(t *testing.T) {
	w := newWorkspace()
	if _, err := threads.Parse(strings.Repeat("$review @team:sre @skill:review ", 1<<14), w); err != nil {
		t.Fatal(err)
	}
	if w.skillAsks != 1 || w.kindAsks != 1 {
		t.Fatalf("one skill and one kind were looked up %d and %d times", w.skillAsks, w.kindAsks)
	}
	for _, text := range []string{
		func() string {
			var b strings.Builder
			for i := range 1 << 14 {
				fmt.Fprintf(&b, "$s%d ", i)
			}
			return b.String()
		}(),
		func() string {
			var b strings.Builder
			for i := range 1 << 14 {
				fmt.Fprintf(&b, "@k%d:x ", i)
			}
			return b.String()
		}(),
	} {
		w := newWorkspace()
		if _, err := threads.Parse(text, w); !errors.Is(err, threads.ErrInvalid) {
			t.Fatalf("a message naming thousands = %v", err)
		}
		if w.skillAsks > threads.MaxMentions || w.kindAsks > threads.MaxMentions+1 {
			t.Fatalf("a message looked up %d skills and %d kinds", w.skillAsks, w.kindAsks)
		}
	}
}

func TestAMessageThatNamesASkillAndNoAgentStartsAuto(t *testing.T) {
	refused := agent("ignite")
	refused.Refused = "not here"
	unknown := skillOf("ghost")
	unknown.Refused = "there is no skill called ghost"
	for _, c := range []struct {
		in, want []threads.Mention
	}{
		{[]threads.Mention{skillOf("review")}, []threads.Mention{skillOf("review"), agent("auto")}},
		{[]threads.Mention{person("okta|dana"), skillOf("review")}, []threads.Mention{person("okta|dana"), skillOf("review"), agent("auto")}},
		{[]threads.Mention{agent("ignite"), skillOf("review")}, []threads.Mention{agent("ignite"), skillOf("review")}},
		// An agent tagged and refused is still tagged: nobody answers in its
		// place.
		{[]threads.Mention{refused, skillOf("review")}, []threads.Mention{refused, skillOf("review")}},
		{[]threads.Mention{unknown}, []threads.Mention{unknown}},
		{[]threads.Mention{person("okta|dana")}, []threads.Mention{person("okta|dana")}},
	} {
		if got := threads.WithSkillAgent(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("WithSkillAgent(%+v) = %+v, want %+v", c.in, got, c.want)
		}
	}
	// It does not write into the tags it was given.
	in := make([]threads.Mention, 1, 4)
	in[0] = skillOf("review")
	_ = threads.WithSkillAgent(in)
	if in[:2][1] != (threads.Mention{}) {
		t.Fatal("WithSkillAgent wrote into its input's backing array")
	}
}
