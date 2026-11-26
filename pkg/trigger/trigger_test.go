package trigger_test

import (
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/trigger"
)

// agents is a workspace with three agents; lookup is case-insensitive and
// answers in the agent's own spelling, as a client's agent lookup does.
func agents(word string) (string, bool) {
	for _, a := range []string{"github-reviewer", "research-bot", "docs"} {
		if strings.EqualFold(word, a) {
			return a, true
		}
	}
	return "", false
}

var handles = []string{trigger.Name}

func TestTheTriggerIsAWholeWordInAnyCase(t *testing.T) {
	for text, want := range map[string]bool{
		"@humagen what changed?":                          true,
		"@Humagen what changed?":                          true,
		"@HUMAGEN: what changed?":                         true,
		"could @humagen take a look?":                     true,
		"thanks (@humagen)":                               true,
		"done.\n\n@humagen review this":                   true,
		"@humagen[bot] review":                            true,
		"@humagen":                                        true,
		"@humagen-bot what changed?":                      false,
		"@humagens what changed?":                         false,
		"mail ops@humagen.dev":                            false,
		"ask @humagen/reviewers":                          false,
		"humagen, what changed?":                          false,
		"@github-reviewer what changed?":                  false,
		"no mention at all":                               false,
		"":                                                false,
		"`@humagen help` prints the usage":                false,
		"run ``@humagen `x` `` here":                      false,
		"```\n@humagen help\n```":                         false,
		"~~~sh\n@humagen help\n~~~":                       false,
		"```\n@humagen help\n```\n@humagen what is this?": true,
		"> @humagen what changed?":                        false,
		"> @humagen what changed?\n\nI agree with this":   false,
		"  > > @humagen old question":                     false,
		"> earlier\n\n@humagen and now?":                  true,
		// What is shown or quoted rather than said.
		"<!-- @humagen review -->":                        false,
		"<!--\nTemplate: @humagen review\n-->\nmy change": false,
		"<!-- note -->\n@humagen review":                  true,
		"intro\n\n    @humagen help\n\nafter":             false,
		"\t@humagen help":                                 false,
		"intro\n    @humagen run-on line":                 true,
		"see <code>@humagen help</code>":                  false,
		"We use <pre-commit> hooks. @humagen review":      true,
		"see <codebase>. @humagen review":                 true,
		"see <CODE class=\"x\">@humagen help</CODE>":      false,
		"- Fixed X\n\n    @humagen please review":         true,
		"1. Fixed X\n\n    @humagen please review":        true,
		"- Fixed X\n\nDone.\n\n    @humagen help":         false,
		"<pre>\n@humagen help\n</pre>":                    false,
		"run `the\n@humagen help` here":                   false,
		"one ` tick\n\n@humagen and now?":                 true,
		"> quoted\nlazily @humagen continued":             false,
		"> quoted\n\n@humagen fresh":                      true,
		// A whole word, with nothing that makes it an address or a path.
		"@humagen.dev is a site":      false,
		"https://github.com/@humagen": false,
		"see a/@humagen":              false,
		"thanks @humagen.":            true,
		"thanks @humagen. More":       true,
		"@humagen, please":            true,
		"@humagen's take?":            true,
		"@humagen@example.com":        false,
	} {
		if _, ok := trigger.Read(text, handles, agents); ok != want {
			t.Errorf("%q: triggered = %v, want %v", text, ok, want)
		}
	}
}

func TestTheAgentAfterTheTriggerIsWhoIsAsked(t *testing.T) {
	for text, want := range map[string]trigger.Ask{
		"@humagen what changed?":                         {Text: "what changed?"},
		"@humagen: what changed?":                        {Text: "what changed?"},
		"@humagen github-reviewer review this":           {Text: "github-reviewer review this"},
		"@humagen github-reviewer, review this":          {Agent: "github-reviewer", Text: "review this"},
		"@humagen docs are stale":                        {Text: "docs are stale"},
		"@humagen GitHub-Reviewer: review this":          {Agent: "github-reviewer", Text: "review this"},
		"@humagen @docs summarize":                       {Agent: "docs", Text: "summarize"},
		"@humagen ask research-bot about the cache":      {Agent: "research-bot", Text: "about the cache"},
		"@humagen, ask research-bot: why?":               {Agent: "research-bot", Text: "why?"},
		"@humagen ask what the cache does":               {Text: "ask what the cache does"},
		"@humagen docs":                                  {Text: "docs"},
		"@humagen @docs":                                 {Agent: "docs", Text: ""},
		"@humagen docsbot help":                          {Text: "docsbot help"},
		"@humagen docs's index is stale":                 {Text: "docs's index is stale"},
		"@humagen please ask github-reviewer":            {Text: "please ask github-reviewer"},
		"could @humagen take a look?":                    {Text: "could @humagen take a look?"},
		"Thanks. @humagen research-bot: can you dig in?": {Agent: "research-bot", Text: "Thanks. @humagen research-bot: can you dig in?"},
		"Thanks. @humagen research-bot can you dig in?":  {Text: "Thanks. @humagen research-bot can you dig in?"},
		"@humagen\n\nwhy did CI fail?":                   {Text: "why did CI fail?"},
	} {
		got, ok := trigger.Read(text, handles, agents)
		if !ok || got != want {
			t.Errorf("%q: %+v, %v; want %+v", text, got, ok, want)
		}
	}
}

func TestWithoutALookupNoWordNamesAnAgent(t *testing.T) {
	got, ok := trigger.Read("@humagen docs summarize", handles, nil)
	if !ok || got.Agent != "" || got.Text != "docs summarize" {
		t.Fatalf("%+v, %v", got, ok)
	}
}

func TestEveryHandleIsATrigger(t *testing.T) {
	both := []string{trigger.Name, "humagen-acme"}
	for _, text := range []string{"@humagen hi", "@humagen-acme hi", "@Humagen-Acme[bot] hi"} {
		if got, ok := trigger.Read(text, both, nil); !ok || got.Text != "hi" {
			t.Errorf("%q: %+v, %v", text, got, ok)
		}
	}
	if _, ok := trigger.Read("@humagen hi", nil, nil); ok {
		t.Error("no handle is a trigger")
	}
}

func TestLeadingReadsOnlyTheFirstWords(t *testing.T) {
	for s, want := range map[string]string{
		"docs help":          "",
		"docs: help":         "docs",
		" @docs help":        "docs",
		"ask docs":           "docs",
		"help docs":          "",
		"":                   "",
		"ask":                "",
		"docs-bot":           "",
		"research-bot, why?": "research-bot",
		"research-bot why?":  "",
		"ask research-bot":   "research-bot",
	} {
		if got, _ := trigger.Leading(s, agents); got != want {
			t.Errorf("Leading(%q) = %q, want %q", s, got, want)
		}
	}
}

// Said takes quoted lines out of a message and says whether the rest is
// all prose: anything shown rather than said makes it not plain.
func TestSaidIsTheMessageWithoutItsQuotes(t *testing.T) {
	for text, want := range map[string]struct {
		said  string
		plain bool
	}{
		"review":                            {"review", true},
		"":                                  {"", true},
		"> earlier\n\nreview":               {"\nreview", true},
		"> one\n> two\n\n@humagen review\n": {"\n@humagen review\n", true},
		"review\n\n> earlier":               {"review\n\n", true},
		"> earlier":                         {"", true},
		"  review  ":                        {"  review  ", true},
		"> earlier\nreview":                 {"", false},
		"> earlier\n/review":                {"", false},
		"> earlier\n@humagen review":        {"", false},
		"    review":                        {"", false},
		"\treview":                          {"", false},
		"```\nreview\n```":                  {"", false},
		"`review`":                          {"", false},
		"`x` review":                        {"", false},
		"<!-- review -->":                   {"", false},
		"<!-- note -->\nreview":             {"review", false},
		"review\n\n    indented code":       {"review\n\n", false},
		"- item\n    review":                {"- item\n    review", true},
		"review <code>x</code>":             {"", false},
	} {
		said, plain := trigger.Said(text)
		if plain != want.plain || plain && said != want.said {
			t.Errorf("Said(%q) = %q, %v, want %q, %v", text, said, plain, want.said, want.plain)
		}
	}
}

// Runes whose lower case is longer or shorter than they are: the Kelvin
// sign, the dotted capital I, the capital sharp s and the stroked capital A.
const (
	kelvin   = "K"
	dottedI  = "İ"
	sharpS   = "ẞ"
	strokedA = "Ⱥ"
)

func TestOffsetsAreTheSameInAnyCase(t *testing.T) {
	long := []string{trigger.Name, "humagen-acme"}
	for _, tc := range []struct {
		name    string
		text    string
		handles []string
		asked   bool
		agent   string
		want    string
	}{
		{name: "kelvin signs before an at sign", text: strings.Repeat(kelvin, 4) + "@", handles: handles},
		{name: "dotted capitals before an open comment", text: strings.Repeat(dottedI, 3) + "<!-- x", handles: handles},
		{name: "kelvin signs after the trigger, longer handle", text: "@humagen" + kelvin + kelvin, handles: long, asked: true, want: kelvin + kelvin},
		{name: "kelvin signs then the trigger", text: kelvin + kelvin + " @humagen go", handles: handles, asked: true, want: kelvin + kelvin + " @humagen go"},
		{name: "dotted capitals then the trigger and an agent", text: dottedI + dottedI + "\n\n@humagen ask docs about " + dottedI, handles: handles, asked: true, agent: "docs", want: dottedI + dottedI + "\n\n@humagen ask docs about " + dottedI},
		{name: "trigger opens, capital sharp s after", text: "@HUMAGEN " + sharpS + " stra" + sharpS + "e", handles: handles, asked: true, want: sharpS + " stra" + sharpS + "e"},
		{name: "stroked capitals then the trigger", text: strokedA + strokedA + strokedA + " @humagen", handles: handles, asked: true, want: strokedA + strokedA + strokedA + " @humagen"},
		{name: "trigger in a comment after dotted capitals", text: dottedI + dottedI + dottedI + dottedI + " <!-- @humagen -->", handles: handles},
		{name: "trigger after a comment after kelvin signs", text: kelvin + "<!-- " + kelvin + " -->@humagen go", handles: handles, asked: true, want: kelvin + "<!-- " + kelvin + " -->@humagen go"},
		{name: "trigger in a code element after kelvin signs", text: kelvin + kelvin + "<CODE>@humagen</CODE> " + kelvin, handles: handles},
		{name: "trigger in a pre element after capital sharp s", text: sharpS + sharpS + "<pre>\n@humagen\n</pre>", handles: handles},
		{name: "trigger in a fence after kelvin signs", text: kelvin + kelvin + "\n```\n@humagen\n```\n", handles: handles},
		{name: "trigger after a fence of dotted capitals", text: "```\n" + dottedI + dottedI + "\n```\n@humagen: go " + dottedI, handles: handles, asked: true, want: "```\n" + dottedI + dottedI + "\n```\n@humagen: go " + dottedI},
		{name: "trigger in a code span after kelvin signs", text: kelvin + kelvin + kelvin + " `@humagen`", handles: handles},
		{name: "trigger in a quote of kelvin signs", text: "> " + kelvin + kelvin + " @humagen\n", handles: handles},
		{name: "trigger under a quote of dotted capitals", text: "> " + dottedI + dottedI + dottedI + "\n\n@humagen thanks", handles: handles, asked: true, want: "> " + dottedI + dottedI + dottedI + "\n\n@humagen thanks"},
		{name: "kelvin sign ends the trigger's word", text: kelvin + " @humagen" + kelvin + " go", handles: handles, asked: true, want: kelvin + " @humagen" + kelvin + " go"},
		{name: "bytes that are not text before the trigger", text: "\xff\xfe\xff @humagen go", handles: handles, asked: true, want: "\xff\xfe\xff @humagen go"},
		{name: "bytes that are not text before an open comment", text: "\xff\xff\xff\xff<!--", handles: handles},
		{name: "accented capitals before the trigger", text: "ÉÉ @Humagen go", handles: handles, asked: true, want: "ÉÉ @Humagen go"},
		{name: "a handle with an accented capital", text: "@École go", handles: []string{"école"}, asked: true, want: "go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ask, ok := trigger.Read(tc.text, tc.handles, agents)
			if ok != tc.asked || ask.Agent != tc.agent || ask.Text != tc.want {
				t.Fatalf("Read(%q) = %+v, %v; want agent %q, text %q, %v", tc.text, ask, ok, tc.agent, tc.want, tc.asked)
			}
			start, end, found := trigger.Find(tc.text, tc.handles...)
			if found != tc.asked {
				t.Fatalf("Find(%q) found = %v, want %v", tc.text, found, tc.asked)
			}
			if found && (start < 0 || end > len(tc.text) || start >= end || tc.text[start] != '@') {
				t.Fatalf("Find(%q) = %d, %d", tc.text, start, end)
			}
		})
	}
}

func TestSaidKeepsEveryRuneAsWritten(t *testing.T) {
	for _, tc := range []struct {
		name  string
		text  string
		said  string
		plain bool
	}{
		{name: "kelvin signs", text: kelvin + kelvin + " review", said: kelvin + kelvin + " review", plain: true},
		{name: "a quote of dotted capitals", text: "> " + dottedI + dottedI + "\n\nreview " + dottedI, said: "\nreview " + dottedI, plain: true},
		{name: "a comment after dotted capitals", text: dottedI + dottedI + dottedI + "<!-- x", said: "", plain: false},
		{name: "a comment between kelvin signs", text: kelvin + "\n<!-- " + kelvin + " -->\n" + kelvin, said: kelvin + "\n" + kelvin, plain: false},
		{name: "a code element after capital sharp s", text: sharpS + sharpS + "\n<Code>x</Code>\nreview", said: sharpS + sharpS + "\nreview", plain: false},
		{name: "a fence after stroked capitals", text: strokedA + strokedA + "\n```\nreview\n```\n", said: strokedA + strokedA + "\n", plain: false},
		{name: "bytes that are not text", text: "\xff\xff<pre>x</pre>\nreview", said: "review", plain: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			said, plain := trigger.Said(tc.text)
			if said != tc.said || plain != tc.plain {
				t.Fatalf("Said(%q) = %q, %v; want %q, %v", tc.text, said, plain, tc.said, tc.plain)
			}
		})
	}
}

// seeds are texts the fuzz tests start from.
var seeds = []string{
	"@humagen what changed?",
	"@HUMAGEN[bot]: ask docs about this",
	"could @humagen take a look?",
	"> quoted @humagen\n\n@humagen docs: go",
	"```\n@humagen\n```\n@humagen-acme go",
	"<!-- @humagen --> <code>@humagen</code> <PRE>x</PRE>",
	"- item\n\n    @humagen\n\n    code `@humagen`",
	strings.Repeat(kelvin, 4) + "@",
	strings.Repeat(dottedI, 3) + "<!-- x",
	"@humagen" + kelvin + kelvin,
	sharpS + "<code " + strokedA + ">@humagen</code>" + sharpS,
	"\xff\xfe @humagen \xc3",
}

func FuzzRead(f *testing.F) {
	for _, s := range seeds {
		f.Add(s, "humagen-acme")
	}
	f.Fuzz(func(t *testing.T, text, handle string) {
		hs := []string{trigger.Name, handle}
		start, end, found := trigger.Find(text, hs...)
		if found && (start < 0 || start >= end || end > len(text) || text[start] != '@') {
			t.Fatalf("Find(%q, %q) = %d, %d", text, hs, start, end)
		}
		if !found && (start != 0 || end != 0) {
			t.Fatalf("Find(%q, %q) = %d, %d with nothing found", text, hs, start, end)
		}
		ask, ok := trigger.Read(text, hs, agents)
		if ok != found {
			t.Fatalf("Read(%q, %q) found = %v, Find found = %v", text, hs, ok, found)
		}
		if len(ask.Text) > len(text) || !strings.Contains(text, ask.Text) {
			t.Fatalf("Read(%q, %q) text = %q, which the text does not hold", text, hs, ask.Text)
		}
		if _, n := trigger.Leading(text, agents); n < 0 || n > len(text) {
			t.Fatalf("Leading(%q) n = %d", text, n)
		}
	})
}

func FuzzSaid(f *testing.F) {
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		said, plain := trigger.Said(text)
		if len(said) > len(text) {
			t.Fatalf("Said(%q) = %q, longer than the text", text, said)
		}
		// What was said is whole lines of the text, in order.
		rest := text
		for _, line := range strings.SplitAfter(said, "\n") {
			at := strings.Index(rest, line)
			if at < 0 {
				t.Fatalf("Said(%q) = %q, with a line the text does not hold", text, said)
			}
			rest = rest[at+len(line):]
		}
		if plain && !strings.Contains(text, ">") && said != text {
			t.Fatalf("Said(%q) = %q, plain with no quote", text, said)
		}
	})
}
