package runs

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxDerivedTitle bounds a title the system derives, in runes.
const maxDerivedTitle = 60

// MaxSessionTitle bounds a session's title the platform gives it, the
// placeholder and the generated one, in runes: short enough that a sidebar
// row shows it whole.
const MaxSessionTitle = 40

// DeriveTitle is the name the system gives a conversation from its first
// message until someone, or a title agent, names it: the first line, without
// the agents it tags at the start, capitalized, cut at a word under 60
// characters. It is a pure function of the text, so every surface shows the
// same title at once.
func DeriveTitle(first string) string {
	return deriveTitle(first, maxDerivedTitle)
}

func deriveTitle(first string, limit int) string {
	line, _, _ := strings.Cut(strings.TrimSpace(first), "\n")
	words := strings.Fields(line)
	for len(words) > 1 && strings.HasPrefix(words[0], "@") {
		words = words[1:]
	}
	if len(words) == 0 {
		return ""
	}
	if len(words) == 1 && strings.HasPrefix(words[0], "@") {
		return words[0]
	}
	title := strings.TrimRight(strings.Join(words, " "), " ,;:-")
	if utf8.RuneCountInString(title) > limit {
		cut := []rune(title)[:limit]
		if i := strings.LastIndexByte(string(cut), ' '); i > limit/2 {
			cut = []rune(string(cut)[:i])
		}
		title = strings.TrimRight(string(cut), " ,;:-.") + "…"
	}
	r, size := utf8.DecodeRuneInString(title)
	return string(unicode.ToUpper(r)) + title[size:]
}

// slackMention is a Slack user reference, <@U123ABC> or <@U123ABC|name>.
var slackMention = regexp.MustCompile(`^\s*<@[A-Za-z0-9]+(\|[^>]*)?>`)

// SessionTitle is the placeholder title a session gets from the message a
// person starts it with, until a generated one replaces it: DeriveTitle of
// the text without the people and agents it tags at the start, Slack's
// <@U123ABC> markup included, cut at a word under MaxSessionTitle runes.
func SessionTitle(text string) string {
	return deriveTitle(dropSlackMentions(text), MaxSessionTitle-1)
}

// CommandTitle is the placeholder title of a session started with a "!"
// command. A command can hold a secret, so it is never shown as the title
// or sent to a model to name the session; the first message that asks for
// something names it, as after a greeting.
const CommandTitle = "Shell command"

func dropSlackMentions(text string) string {
	for {
		loc := slackMention.FindStringIndex(text)
		if loc == nil {
			return text
		}
		text = text[loc[1]:]
	}
}

var (
	greetingWords = map[string]bool{
		"hi": true, "hello": true, "hey": true, "heya": true, "hiya": true, "howdy": true,
		"yo": true, "sup": true, "hola": true, "greetings": true, "wassup": true, "whatsup": true,
		"there": true, "mate": true, "all": true, "everyone": true, "team": true, "folks": true,
		"friend": true, "buddy": true, "again": true, "guys": true,
	}
	greetingPhrases = strings.NewReplacer(
		"good morning", " ", "good afternoon", " ", "good evening", " ", "good day", " ",
		"gday", " ", "whats up", " ", "morning", " ", "afternoon", " ", "evening", " ",
	)
)

// Greeting reports whether text, after the agents and people it tags, is only
// a greeting: "hi", "hey there", "howdy mate", "good morning", "hello @lead",
// with any punctuation or emoji. A message that asks for anything is not.
func Greeting(text string) bool {
	text = dropSlackMentions(text)
	var b strings.Builder
	for _, f := range strings.Fields(strings.ToLower(text)) {
		if strings.HasPrefix(f, "@") {
			continue
		}
		b.WriteString(f)
		b.WriteByte(' ')
	}
	var clean strings.Builder
	for _, r := range b.String() {
		switch {
		case r == '\'' || r == '\u2019':
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			clean.WriteRune(r)
		default:
			clean.WriteByte(' ')
		}
	}
	rest := greetingPhrases.Replace(" " + strings.Join(strings.Fields(clean.String()), " ") + " ")
	words := strings.Fields(rest)
	if len(words) == 0 {
		return strings.TrimSpace(clean.String()) != ""
	}
	hasGreeting := false
	for _, w := range words {
		if !greetingWords[w] {
			return false
		}
		switch w {
		case "there", "mate", "all", "everyone", "team", "folks", "friend", "buddy", "again", "guys":
		default:
			hasGreeting = true
		}
	}
	return hasGreeting
}
