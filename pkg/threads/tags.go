package threads

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/projects"
)

// Reserved names. No agent and no person goes by them.
const (
	// AutoName is @auto: the built-in router picks the agent.
	AutoName = "auto"
	// SMEName is @sme, and @sme:<topic>: the project's experts.
	SMEName = "sme"
	// SkillName is the built-in mention kind @skill:<name>, which is
	// $<name>.
	SkillName = "skill"
)

// What a mention kind resolves its items to.
const (
	// ResolvesExperts is the experts of a topic, as @sme:<topic>.
	ResolvesExperts = "experts"
	// ResolvesSkills is a skill, as $<name>.
	ResolvesSkills = "skills"
	// ResolvesRole is the people holding a policy role.
	ResolvesRole = "role"
	// ResolvesPeople is a person, by handle, as @<handle>.
	ResolvesPeople = "people"
)

// ValidResolves reports whether a mention kind can resolve to r.
func ValidResolves(r string) bool {
	return r == ResolvesExperts || r == ResolvesSkills || r == ResolvesRole || r == ResolvesPeople
}

// BuiltInKinds are the mention kinds every workspace has, and what each
// resolves to: @sme:<topic> and @skill:<name>.
var BuiltInKinds = map[string]string{SMEName: ResolvesExperts, SkillName: ResolvesSkills}

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

// Lookups is what a Resolver may look up besides: the skills a message
// posted where it is can name, and the mention kinds the workspace declares
// besides the built-in ones. A Resolver without it knows no skill and only
// the built-in kinds.
type Lookups interface {
	// Skill reports whether a skill goes by name where the message is
	// posted.
	Skill(name string) bool
	// MentionKind returns what a kind the workspace declares resolves to.
	MentionKind(kind string) (resolves string, ok bool)
}

// slackMention is Slack's own mention of a person: <@U123> or <@U123|dana>.
var slackMention = regexp.MustCompile(`^<@([A-Z][A-Z0-9]{2,31})(?:\|[^<>]{0,100})?>`)

// Parse finds the tags in a message: @<agent>, @auto, @<handle> for a
// person, @sme and @sme:<topic> for experts, $<name> and @skill:<name> for a
// skill, @<kind>:<name> for a mention kind the workspace declares, and
// Slack's <@U…> for a Slack person. Text inside inline code or a code block
// is not a tag, nor is an @ or a $ inside a word, such as in an email
// address. A $ is a skill only when a skill's name follows it whole, so a
// price ($5) or a variable ($PATH, $my_var) is not one. A tag that names
// nobody is kept, refused, so the person who wrote it is told. Each tag is
// kept once, in the order it first appears, and a message holds at most
// MaxMentions, of which at most MaxSkills skills (ErrInvalid). Parse is a
// pure function of the text and the lookups.
//
// Each name is looked up once, however often it is written, and a message
// naming more than MaxMentions is refused before the next one is looked up,
// so a message costs at most MaxMentions lookups of each kind.
func Parse(text string, r Resolver) ([]Mention, error) {
	var out []Mention
	seen := map[[2]string]bool{}
	asked := map[string]bool{}
	tooMany := fmt.Errorf("%w: a message tags at most %d people, agents and skills", ErrInvalid, MaxMentions)
	kinds := kindLookup{r: r, asked: map[string]kindAnswer{}}
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
			key, resolve := atTag(token, r, &kinds)
			if err := add(key, resolve); err != nil {
				return nil, err
			}
			i += 1 + len(token)
		case c == '$' && startsTag(text, i):
			name, n := readSkill(text[i+1:])
			if name == "" {
				i += 1 + n
				continue
			}
			if err := add("$"+name, func() Mention { return skillTag(name, r) }); err != nil {
				return nil, err
			}
			i += 1 + n
		default:
			i++
		}
	}
	skills := 0
	for _, m := range out {
		if m.Kind == Skill {
			skills++
		}
	}
	switch {
	case len(out) > MaxMentions:
		return nil, tooMany
	case skills > MaxSkills:
		return nil, fmt.Errorf("%w: a message names at most %d skills", ErrInvalid, MaxSkills)
	}
	if out == nil {
		out = []Mention{}
	}
	return out, nil
}

// WithSkillAgent is what a message that names a skill and tags no agent
// starts: @auto, which picks the agent and is told to use the skill. It
// returns the message's tags with @auto added after them when it does, and
// the tags as they are otherwise. An agent tagged and refused still counts
// as tagged, so no other agent answers in its place. Check first that the
// tag fits (RoomForSkillAgent).
func WithSkillAgent(ms []Mention) []Mention {
	skill := false
	for _, m := range ms {
		switch {
		case m.Kind == Agent:
			return ms
		case m.Kind == Skill && m.Refused == "":
			skill = true
		}
	}
	if !skill {
		return ms
	}
	return append(cloneMentions(ms), Mention{Kind: Agent, Name: AutoName})
}

// RoomForSkillAgent refuses a message whose tags leave no room for the @auto
// WithSkillAgent would add: it already holds MaxMentions (ErrInvalid).
func RoomForSkillAgent(ms []Mention) error {
	if len(WithSkillAgent(ms)) > MaxMentions {
		return fmt.Errorf("%w: a message tags at most %d people, agents and skills, and the @auto a skill starts is one more", ErrInvalid, MaxMentions)
	}
	return nil
}

// UnknownSkill is the error a message that names a skill nobody declared
// where it is posted is refused with, or nil. A message that names one is
// not posted at all: the person who wrote it meant something that is not
// there.
func UnknownSkill(ms []Mention) error {
	for _, m := range ms {
		if m.Kind == Skill && m.Refused != "" {
			return fmt.Errorf("%w: %s", ErrInvalid, m.Refused)
		}
	}
	return nil
}

// kindLookup asks a Resolver what a mention kind resolves to, once a kind.
type kindLookup struct {
	r     Resolver
	asked map[string]kindAnswer
}

type kindAnswer struct {
	resolves string
	ok       bool
}

// of is what a kind resolves to: a built-in kind's, or one the workspace
// declares.
func (k *kindLookup) of(kind string) (string, bool) {
	if r, ok := BuiltInKinds[kind]; ok {
		return r, true
	}
	if a, ok := k.asked[kind]; ok {
		return a.resolves, a.ok
	}
	var a kindAnswer
	if l, ok := k.r.(Lookups); ok && k.r != nil {
		a.resolves, a.ok = l.MentionKind(kind)
		a.ok = a.ok && ValidResolves(a.resolves)
	}
	k.asked[kind] = a
	return a.resolves, a.ok
}

// atTag is what a token after an @ tags, and the key it is looked up by:
// the reserved names whatever their case, <kind>:<name> for a mention kind,
// and otherwise the name before any colon, as tag reads it. A token that
// names a kind's item resolves as the kind says, so @skill:review is
// $review and a kind over experts is @sme:<topic>.
func atTag(token string, r Resolver, kinds *kindLookup) (string, func() Mention) {
	low := strings.ToLower(token)
	switch low {
	case AutoName:
		return "@" + low, func() Mention { return Mention{Kind: Agent, Name: AutoName} }
	case SMEName:
		return "@" + low, func() Mention { return Mention{Kind: SME} }
	}
	if kind, item, cut := strings.Cut(token, ":"); cut && item != "" {
		if resolves, ok := kinds.of(strings.ToLower(kind)); ok {
			lowItem := strings.ToLower(item)
			switch resolves {
			case ResolvesSkills:
				return "$" + lowItem, func() Mention { return skillTag(lowItem, r) }
			case ResolvesExperts:
				return "@sme:" + lowItem, func() Mention { return smeTag(lowItem) }
			case ResolvesRole:
				return "@role:" + item, func() Mention { return roleTag(item) }
			case ResolvesPeople:
				return "@person:" + lowItem, func() Mention { return personTag(lowItem, r) }
			}
		}
	}
	name, _, _ := strings.Cut(token, ":")
	return "@" + name, func() Mention { return tag(name, r) }
}

// maxSlackMention bounds how much of the text one Slack mention is read
// from: its user id and the name Slack may put after it.
const maxSlackMention = 512

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

// readSkill reads the skill a $ names, and how much of the text it read.
// The name is the run of letters, digits, dashes and underscores after the
// $, less the dashes a sentence puts after it, and it names a skill only
// when the whole run is a skill's name: "" otherwise, so $5, $PATH and $a_b
// are text.
func readSkill(s string) (string, int) {
	n := 0
	for n < len(s) && n < maxToken {
		c := s[n]
		if !isAlnum(c) && c != '_' && c != '-' {
			break
		}
		n++
	}
	name := strings.TrimRight(s[:n], "-")
	if !projects.ValidSkill(name) {
		return "", n
	}
	return name, len(name)
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// tag is what @<name> tags: an agent, else a person, else no one.
func tag(token string, r Resolver) Mention {
	if r != nil && r.Agent(token) {
		return Mention{Kind: Agent, Name: token}
	}
	return personTag(strings.ToLower(token), r)
}

// personTag is the person who goes by a handle, or no one.
func personTag(handle string, r Resolver) Mention {
	if r != nil {
		if subject, ok := r.Person(handle); ok {
			return Mention{Kind: Person, Name: subject}
		}
	}
	return NoOne(handle)
}

func smeTag(topic string) Mention {
	if !projects.ValidTopic(topic) {
		return Mention{Kind: SME, Name: topic, Refused: "a topic is lower case letters, digits and dashes, starting with a letter"}
	}
	return Mention{Kind: SME, Name: topic}
}

func roleTag(role string) Mention {
	if !projects.ValidRole(role) {
		return Mention{Kind: Role, Name: role, Refused: "a role is letters, digits, dashes and underscores, starting with a letter or an underscore"}
	}
	return Mention{Kind: Role, Name: role}
}

func skillTag(name string, r Resolver) Mention {
	if l, ok := r.(Lookups); ok && r != nil && projects.ValidSkill(name) && l.Skill(name) {
		return Mention{Kind: Skill, Name: name}
	}
	return Mention{Kind: Skill, Name: name, Refused: "there is no skill called " + name}
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
