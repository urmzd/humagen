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
