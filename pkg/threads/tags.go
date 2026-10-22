package threads

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/urmzd/warp/pkg/projects"
)

// Reserved names. No agent and no person goes by them.
const (
	// AutoName is @auto: the built-in router picks the agent.
	AutoName = "auto"
	// SMEName is @sme, and @sme:<topic>: the project's experts.
	SMEName = "sme"
)

// maxToken bounds what Parse reads as one name.
const maxToken = 100

// Resolver looks up the names a message tags. Every lookup is within the
// tenant the message is posted in.
type Resolver interface {
	// Agent reports whether an agent goes by name. Whether it is available
	// where the message is posted is decided after.
	Agent(name string) bool
	// Person returns the subject of the person who goes by a handle.
	Person(handle string) (subject string, ok bool)
	// Slack returns the subject a Slack user id is mapped to, through the
	// people of the client the message came through.
	Slack(user string) (subject string, ok bool)
}

// slackMention is Slack's own mention of a person: <@U123> or <@U123|dana>.
var slackMention = regexp.MustCompile(`^<@([A-Z][A-Z0-9]{2,31})(?:\|[^<>]{0,100})?>`)

// Parse finds the tags in a message: @<agent>, @auto, @<handle> for a
// person, @sme and @sme:<topic> for experts, and Slack's <@U…> for a Slack
// person. Text inside inline code or a code block is not a tag, nor is an @
// inside a word, such as in an email address. A tag that names nobody is
// kept, refused, so the person who wrote it is told. Each tag is kept once,
// in the order it first appears, and a message holds at most MaxMentions
// (ErrInvalid). Parse is a pure function of the text and the lookups.
//
// Each name is looked up once, however often it is written, and a message
// naming more than MaxMentions is refused before the next one is looked up,
// so a message costs at most MaxMentions lookups of each kind.
func Parse(text string, r Resolver) ([]Mention, error) {
	var out []Mention
	seen := map[[2]string]bool{}
	asked := map[string]bool{}
	tooMany := fmt.Errorf("%w: a message tags at most %d people and agents", ErrInvalid, MaxMentions)
	add := func(key string, resolve func() Mention) error {
		if asked[key] {
			return nil
		}
		if len(asked) == MaxMentions {
			return tooMany
		}
		asked[key] = true
		m := resolve()
		k := [2]string{m.Kind, m.Name}
		if !seen[k] {
			seen[k] = true
			out = append(out, m)
		}
		return nil
	}
	for i := 0; i < len(text); {
		switch c := text[i]; {
		case c == '`':
			i = skipCode(text, i)
		case c == '<' && strings.HasPrefix(text[i:], "<@"):
			if m := slackMention.FindStringSubmatch(text[i:min(len(text), i+maxSlackMention)]); m != nil {
				if err := add("<"+m[1], func() Mention { return slackTag(m[1], r) }); err != nil {
					return nil, err
				}
				i += len(m[0])
				continue
			}
			i++
		case c == '@' && startsTag(text, i):
			token := readToken(text[i+1:])
			if token == "" {
				i++
				continue
			}
			if err := add("@"+tagKey(token), func() Mention { return tag(token, r) }); err != nil {
				return nil, err
			}
			i += 1 + len(token)
		default:
			i++
		}
	}
	if len(out) > MaxMentions {
		return nil, tooMany
	}
	if out == nil {
		out = []Mention{}
	}
	return out, nil
}

// maxSlackMention bounds how much of the text one Slack mention is read
// from: its user id and the name Slack may put after it.
const maxSlackMention = 512

// tagKey is what decides what a token tags: the reserved names whatever
// their case, and otherwise the name before any colon, as tag reads it.
func tagKey(token string) string {
	low := strings.ToLower(token)
	if low == AutoName || low == SMEName || strings.HasPrefix(low, SMEName+":") {
		return low
	}
	name, _, _ := strings.Cut(token, ":")
	return name
}

// skipCode moves past a code span or block that starts at a run of
// backticks: to just after the next run of exactly as many. A run with no
// match is literal, and only it is skipped.
func skipCode(text string, i int) int {
	n := 0
	for i+n < len(text) && text[i+n] == '`' {
		n++
	}
	fence := strings.Repeat("`", n)
	for j := i + n; j < len(text); {
		k := strings.Index(text[j:], fence)
		if k < 0 {
			break
		}
		at := j + k
		end := at + n
		if end < len(text) && text[end] == '`' {
			// A longer run does not close this one.
			for end < len(text) && text[end] == '`' {
				end++
			}
			j = end
			continue
		}
		return end
	}
	return i + n
}

// startsTag reports whether the @ at i starts a tag: at the start of the
// text, or after something that is not part of a word, an address or a path.
func startsTag(text string, i int) bool {
	if i == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(text[:i])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("_.@/\\-+", r)
}

// readToken reads the name after an @, leaving out the punctuation a
// sentence puts after it.
func readToken(s string) string {
	n := 0
	for n < len(s) && n < maxToken {
		c := s[n]
		if !isAlnum(c) && c != '.' && c != '_' && c != '-' && c != ':' {
			break
		}
		n++
	}
	token := strings.TrimRight(s[:n], ".:-")
	if token == "" || !isAlnum(token[0]) {
		return ""
	}
	return token
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func tag(token string, r Resolver) Mention {
	low := strings.ToLower(token)
	switch {
	case low == AutoName:
		return Mention{Kind: Agent, Name: AutoName}
	case low == SMEName:
		return Mention{Kind: SME}
	case strings.HasPrefix(low, SMEName+":"):
		topic := strings.TrimPrefix(low, SMEName+":")
		if !projects.ValidTopic(topic) {
			return Mention{Kind: SME, Name: topic, Refused: "a topic is lower case letters, digits and dashes, starting with a letter"}
		}
		return Mention{Kind: SME, Name: topic}
	}
	// Only @sme takes a topic: "@ignite:" is ignite.
	if name, _, cut := strings.Cut(token, ":"); cut {
		token, low = name, strings.ToLower(name)
	}
	if r != nil && r.Agent(token) {
		return Mention{Kind: Agent, Name: token}
	}
	if r != nil {
		if subject, ok := r.Person(low); ok {
			return Mention{Kind: Person, Name: subject}
		}
	}
	return NoOne(token)
}

// NoOne is what a tag that names nobody comes to: a person nobody goes by,
// kept as written.
func NoOne(token string) Mention {
	return Mention{Kind: Person, Name: token, Refused: "no one goes by @" + token}
}

func slackTag(user string, r Resolver) Mention {
	if r != nil {
		if subject, ok := r.Slack(user); ok {
			return Mention{Kind: Person, Name: subject}
		}
	}
	return Mention{Kind: Person, Name: user, Refused: "that Slack person is not mapped to anyone here"}
}
