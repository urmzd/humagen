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
