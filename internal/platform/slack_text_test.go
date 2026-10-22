package platform

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func slackFixture() slackContext {
	return slackContext{
		AppUser: "UAPP",
		Bots:    map[string]string{"UBOT": "ignite"},
		People:  map[string]string{"UDANA": "okta|dana", "UNOHANDLE": "okta|new"},
		HandleOf: func(subject string) (string, bool) {
			if subject == "okta|dana" {
				return "dana", true
			}
			return "", false
		},
	}
}

// A Slack message reads in the one grammar: the app's own mention is gone, a
// mapped bot or person is their tag, and nobody else can become one.
func TestSlackTextTagsOnlyWhatTheClientMaps(t *testing.T) {
	for _, c := range []struct {
		name, text, want string
		refused          []slackRefusal
	}{
		{"the app is not a tag", "<@UAPP> @ignite draft the renewal note", "@ignite draft the renewal note", nil},
		{"the app at the end", "what changed? <@UAPP>", "what changed?", nil},
		{"a bot is its agent", "<@UAPP> <@UBOT> look at this", "@ignite look at this", nil},
		{"a mapped person is their handle", "<@UAPP> <@UDANA> can you check?", "@dana can you check?", nil},
		{"anyone else is plain", "<@UAPP> ask <@UEVE|eve>", "ask eve", []slackRefusal{{"UEVE", refusedOutside}}},
		{"no label, no handle", "<@UNOHANDLE> hi", "UNOHANDLE hi", []slackRefusal{{"UNOHANDLE", refusedOutside}}},
		{"a ping is a word", "<!here> and <!channel|channel>", "here and channel", nil},
		{"a group is its name", "ask <!subteam^S01|@oncall>", "ask oncall", nil},
		{"a channel link", "see <#C0123|renewals>", "see #renewals", nil},
		{"a link keeps its address", "read <https://x.test/a?b=1&amp;c=2|the plan>", "read the plan (https://x.test/a?b=1&c=2)", nil},
		{"a bare link", "read <https://x.test/a>", "read https://x.test/a", nil},
		{"an email", "mail <mailto:a@x.test|a@x.test>", "mail a@x.test", nil},
		{"entities are text", "1 &lt; 2 &amp;&amp; &lt;@UDANA&gt;", "1 < 2 && <@UDANA>", nil},
		{"inline code is not a tag", "<@UAPP> run `<@UDANA> &lt;x&gt;` now", "run `<@UDANA> <x>` now", nil},
		{"a code block is not a tag", "```\n<@UBOT>\n``` <@UBOT>", "```\n<@UBOT>\n``` @ignite", nil},
		{"an open backtick is text", "a ` <@UDANA>", "a ` @dana", nil},
	} {
		got, refused := slackText(c.text, slackFixture())
		if got != c.want || !reflect.DeepEqual(refused, c.refused) {
			t.Errorf("%s: %q, %v; want %q, %v", c.name, got, refused, c.want, c.refused)
		}
	}
}

// Nothing a Slack name says becomes a tag: a refused mention never starts
// with @, so it cannot be read as someone's handle.
func TestASlackNameNeverBecomesATag(t *testing.T) {
	got, _ := slackText("<@UEVE|@dana> <@UEVE|dana> <!subteam^S1|@dana> <#C1|@dana>", slackFixture())
	if strings.Contains(got, "@") {
		t.Fatalf("a name became a tag: %q", got)
	}
}

func TestASlackMessageTagsAtMostTen(t *testing.T) {
	cx := slackFixture()
	cx.Bots = map[string]string{}
	var b strings.Builder
	for i := range maxSlackTags + 2 {
		id := fmt.Sprintf("UB%02d", i)
		cx.Bots[id] = fmt.Sprintf("agent%d", i)
		fmt.Fprintf(&b, "<@%s> ", id)
	}
	got, refused := slackText(b.String(), cx)
	if n := strings.Count(got, "@"); n != maxSlackTags {
		t.Fatalf("%d tags in %q", n, got)
	}
	if len(refused) != 2 || refused[0].Reason != refusedTooMany {
		t.Fatalf("refused = %v", refused)
	}
}
