package platform

import (
	"regexp"
	"strings"
)

// A message from Slack is written in Slack's own syntax: user mentions as
// <@U0123>, the app itself among them, links in angle brackets, and &, < and
// > as entities. Before the platform reads it with the one tagging grammar it
// is translated here, so every surface is parsed the same way afterwards:
//
//   - the app's own mention is how a channel message reaches it, not a tag,
//     and is dropped;
//   - a bot user the client maps to an agent is a tag of that agent, @agent;
//   - a Slack person the client maps to someone here is a tag of them, by
//     their handle;
//   - anyone else stays plain text, with no @, and is refused as not in this
//     workspace, so a Slack name can never be read as somebody's handle;
//   - nothing inside inline code or a code block is translated.
//
// At most maxSlackTags people and agents are tagged; the rest stay plain.

// maxSlackTags is the most tags one message carries, as the grammar allows.
const maxSlackTags = 10

// Why a Slack mention is not a tag.
const (
	refusedOutside = "not in this workspace"
	refusedTooMany = "a message tags at most 10 people and agents"
)

// slackContext is what translating a message needs to know about the client it
// came through.
type slackContext struct {
	// AppUser is the app's own bot user id.
	AppUser string
	// Bots maps a bot user id to the agent it stands for: the client's Bots.
	Bots map[string]string
	// People maps a Slack user id to the subject it is here: the client's
	// slack.people.
	People map[string]string
	// HandleOf is a subject's handle in the workspace's directory, if they
	// have one.
	HandleOf func(subject string) (string, bool)
}

// slackRefusal is a Slack mention that tagged nobody, and why.
type slackRefusal struct {
	User   string
	Reason string
}

// slackToken is anything Slack writes in angle brackets: <@U1>, <!here>,
// <#C1|general>, <https://x|label>, each with an optional label.
var slackToken = regexp.MustCompile(`<([^<>|]+)(?:\|([^<>]*))?>`)

var slackEntities = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&")

// slackText translates one Slack message into the platform's text, and says
// which mentions tagged nobody.
func slackText(text string, cx slackContext) (string, []slackRefusal) {
	var b strings.Builder
	var refused []slackRefusal
	tags := 0
	for _, seg := range codeSegments(text) {
		if seg.code {
			b.WriteString(slackEntities.Replace(seg.text))
			continue
		}
		s := seg.text
		last := 0
		for _, m := range slackToken.FindAllStringSubmatchIndex(s, -1) {
			b.WriteString(slackEntities.Replace(s[last:m[0]]))
			last = m[1]
			inner, label := s[m[2]:m[3]], ""
			if m[4] >= 0 {
				label = s[m[4]:m[5]]
			}
			out := slackWord(inner, label, cx, &tags, &refused)
			if out == "" {
				// A dropped mention takes one space with it.
				if last < len(s) && s[last] == ' ' {
					last++
				}
				continue
			}
			b.WriteString(out)
		}
		b.WriteString(slackEntities.Replace(s[last:]))
	}
	return strings.TrimSpace(b.String()), refused
}

// slackWord is what one token becomes.
func slackWord(inner, label string, cx slackContext, tags *int, refused *[]slackRefusal) string {
	plainLabel := slackEntities.Replace(label)
	switch {
	case strings.HasPrefix(inner, "@U") || strings.HasPrefix(inner, "@W"):
		user := inner[1:]
		if user == cx.AppUser && cx.AppUser != "" {
			return ""
		}
		tag := ""
		if agent, ok := cx.Bots[user]; ok && agent != "" {
			tag = "@" + agent
		} else if who, ok := cx.People[user]; ok && who != "" && cx.HandleOf != nil {
			if handle, ok := cx.HandleOf(who); ok && handle != "" {
				tag = "@" + handle
			}
		}
		if tag == "" {
			*refused = append(*refused, slackRefusal{User: user, Reason: refusedOutside})
			return bare(plainLabel, user)
		}
		if *tags == maxSlackTags {
			*refused = append(*refused, slackRefusal{User: user, Reason: refusedTooMany})
			return strings.TrimPrefix(tag, "@")
		}
		*tags++
		return tag
	case inner == "!here" || inner == "!channel" || inner == "!everyone":
		// A Slack-wide ping means nothing here, and is not a tag.
		return inner[1:]
	case strings.HasPrefix(inner, "!subteam^"):
		return bare(plainLabel, strings.TrimPrefix(inner, "!subteam^"))
	case strings.HasPrefix(inner, "!"):
		// A date or another special: its fallback text.
		return bare(plainLabel, inner[1:])
	case strings.HasPrefix(inner, "#C") || strings.HasPrefix(inner, "#G"):
		return "#" + bare(plainLabel, inner[1:])
	}
	url := slackEntities.Replace(inner)
	if plainLabel == "" || plainLabel == url || strings.HasPrefix(url, "mailto:") {
		return bare(plainLabel, url)
	}
	return plainLabel + " (" + url + ")"
}

// bare is a label, or else a fallback, with no leading @ to be read as a tag.
func bare(label, fallback string) string {
	if label = strings.TrimLeft(strings.TrimSpace(label), "@"); label != "" {
		return label
	}
	return strings.TrimLeft(fallback, "@")
}

// segment is a run of text, inside code or not.
type segment struct {
	text string
	code bool
}

// codeSegments splits text into what is inside code blocks and inline code
// and what is not. A fence or a backtick with no partner is plain text, as
// Slack shows it.
func codeSegments(text string) []segment {
	var out []segment
	split := func(s, fence string, inner func(string)) {
		parts := strings.Split(s, fence)
		if len(parts)%2 == 0 {
			// The last fence has no partner: it and what follows are text.
			parts[len(parts)-2] += fence + parts[len(parts)-1]
			parts = parts[:len(parts)-1]
		}
		for i, p := range parts {
			if i%2 == 1 {
				out = append(out, segment{text: fence + p + fence, code: true})
			} else {
				inner(p)
			}
		}
	}
	split(text, "```", func(s string) {
		split(s, "`", func(p string) {
			if p != "" {
				out = append(out, segment{text: p})
			}
		})
	})
	return out
}
