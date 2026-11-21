// Package agentlooks keeps how each agent in a workspace looks and what it is
// called: the bot it is drawn as, and the name people see beside its handle.
//
// A look is presentation. It is not part of the agent's definition, so
// changing it makes no revision, pins nothing on a run and never changes how
// the agent behaves. Every field is optional, and an empty field means the
// default the platform has always drawn: the bot shape its handle hashes to,
// its eyes, a fabric finish, nothing worn, and its handle as its name. The
// agent's color is not here: it is a hue reserved in pkg/agentcolors, which
// keeps two agents from sharing one.
//
// It reads and writes the database per request, so every process on one
// database agrees on every look.
package agentlooks

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/pkg/state"
)

// ErrInvalid reports a look that will not be kept. Callers match with errors.Is.
var ErrInvalid = errors.New("agentlooks: invalid")

// ErrNameTaken reports a display name another agent in the workspace has.
var ErrNameTaken = errors.New("agentlooks: display name taken")

// MaxDisplayName bounds a display name, in characters. Slack shows a
// message's author in at most 80; a short name also fits a mark's label.
const MaxDisplayName = 40

// Limit bounds the looks one workspace keeps.
const Limit = 2000

// Shapes are the bots an agent may be drawn as, as bot-avatars names them.
var Shapes = []string{
	"clover", "flower", "triangle", "square", "blob", "ghost", "circle", "drop", "star",
	"droid", "mech", "alien", "hexagon", "cat", "cloud", "pill", "pebble", "puddle",
}

// Faces, Finishes, Hats and Glasses are the other choices, as bot-avatars
// names them. The first of each is the default and is stored as empty.
var (
	Faces    = []string{"eyes", "mouth"}
	Finishes = []string{"fabric", "plastic", "smooth", "flat"}
	Hats     = []string{"none", "beret", "beanie", "party", "crown"}
	Glasses  = []string{"none", "round", "square", "shades"}
)

// Look is how an agent is drawn and named. The zero Look is the default.
type Look struct {
	// DisplayName is what people call the agent: "Barbara". The handle,
	// which people tag and which never changes here, stays the agent's name.
	DisplayName string `json:"display_name,omitempty"`
	// Shape is the bot's body. Empty is the shape the handle hashes to.
	Shape      string `json:"shape,omitempty"`
	Face       string `json:"face,omitempty"`
	Finish     string `json:"finish,omitempty"`
	Hat        string `json:"hat,omitempty"`
	Glasses    string `json:"glasses,omitempty"`
	Headphones bool   `json:"headphones,omitempty"`
	BowTie     bool   `json:"bow_tie,omitempty"`
	// Greeting is a sentence or two in the agent's voice, shown where people
	// meet it: its Look tab, and the Slack app's Home.
	Greeting string `json:"greeting,omitempty"`
	// Starters are up to MaxStarters jobs people can hand it in one click.
	Starters []Starter `json:"starters,omitempty"`
}

// Starter is one job an agent offers: a short title and the message it sends.
type Starter struct {
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
}

// Bounds on what a look says. Slack shows a suggested prompt's title in at
// most 75 characters, so a title fits there.
const (
	MaxGreeting    = 280
	MaxStarters    = 3
	MaxStarterName = 60
	MaxStarterText = 500
)

// IsZero reports whether the look is the default in every field.
func (l Look) IsZero() bool { return l.Equal(Look{}) }

// Equal reports whether two looks are the same.
func (l Look) Equal(o Look) bool {
	return l.DisplayName == o.DisplayName && l.Shape == o.Shape && l.Face == o.Face &&
		l.Finish == o.Finish && l.Hat == o.Hat && l.Glasses == o.Glasses &&
		l.Headphones == o.Headphones && l.BowTie == o.BowTie && l.Greeting == o.Greeting &&
		slices.Equal(l.Starters, o.Starters)
}

// Normalize trims the display name and stores each default as empty, so the
// same look written two ways is one look. It does not validate.
func (l Look) Normalize() Look {
	l.DisplayName = strings.Join(strings.Fields(l.DisplayName), " ")
	l.Shape = strings.ToLower(strings.TrimSpace(l.Shape))
	clear := func(v string, choices []string) string {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == choices[0] {
			return ""
		}
		return v
	}
	l.Face = clear(l.Face, Faces)
	l.Finish = clear(l.Finish, Finishes)
	l.Hat = clear(l.Hat, Hats)
	l.Glasses = clear(l.Glasses, Glasses)
	l.Greeting = strings.TrimSpace(l.Greeting)
	var starters []Starter
	for _, st := range l.Starters {
		st.Title = strings.Join(strings.Fields(st.Title), " ")
		st.Prompt = strings.TrimSpace(st.Prompt)
		if st.Title != "" || st.Prompt != "" {
			starters = append(starters, st)
		}
	}
	l.Starters = starters
	return l
}

// Validate reports every problem with a normalized look at once.
func (l Look) Validate() error {
	var problems []string
	if n := utf8.RuneCountInString(l.DisplayName); n > MaxDisplayName {
		problems = append(problems, fmt.Sprintf("a display name is at most %d characters", MaxDisplayName))
	}
	if strings.ContainsFunc(l.DisplayName, func(r rune) bool { return unicode.IsControl(r) || r == '@' || r == '<' || r == '>' }) {
		problems = append(problems, "a display name may not hold control characters, @, < or >")
	}
	check := func(field, v string, choices []string) {
		if v != "" && !slices.Contains(choices, v) {
			problems = append(problems, fmt.Sprintf("%s must be one of %s", field, strings.Join(choices, ", ")))
		}
	}
	check("shape", l.Shape, Shapes)
	check("face", l.Face, Faces)
	check("finish", l.Finish, Finishes)
	check("hat", l.Hat, Hats)
	check("glasses", l.Glasses, Glasses)
	if utf8.RuneCountInString(l.Greeting) > MaxGreeting {
		problems = append(problems, fmt.Sprintf("a greeting is at most %d characters", MaxGreeting))
	}
	if len(l.Starters) > MaxStarters {
		problems = append(problems, fmt.Sprintf("an agent offers at most %d starters", MaxStarters))
	}
	for i, st := range l.Starters {
		switch {
		case st.Title == "" || st.Prompt == "":
			problems = append(problems, fmt.Sprintf("starter %d needs a title and a message", i+1))
		case utf8.RuneCountInString(st.Title) > MaxStarterName:
			problems = append(problems, fmt.Sprintf("a starter's title is at most %d characters", MaxStarterName))
		case utf8.RuneCountInString(st.Prompt) > MaxStarterText:
			problems = append(problems, fmt.Sprintf("a starter's message is at most %d characters", MaxStarterText))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalid, strings.Join(problems, "; "))
	}
	return nil
}

// key is the one record that holds a workspace's looks, by agent, so a
// display name is checked against every other in the same write.
const key = "#looks"

// Store keeps each workspace's looks.
type Store struct {
	t     *state.Table[map[string]Look]
	marks *state.Table[map[string]string]
}

// Open returns a store on st, which may be shared with other processes. A
// nil st keeps looks in this process's memory.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	return &Store{
		t:     state.NewTable[map[string]Look](st, "agent-looks"),
		marks: state.NewTable[map[string]string](st, "agent-looks"),
	}, nil
}

// Get returns the looks of the named agents that have one. An agent with
// the default look is left out.
func (s *Store) Get(tenant string, names []string) (map[string]Look, error) {
	all, err := s.t.Lookup(tenant, key)
	if errors.Is(err, state.ErrNotFound) {
		return map[string]Look{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]Look{}
	for _, requested := range names {
		if l, ok := all.Value[legacy.BuiltinAgentKey(requested)]; ok && !l.IsZero() {
			out[requested] = l
		}
	}
	return out, nil
}

// Set keeps an agent's look and returns it as kept. A zero look returns the
// agent to the default. A display name is unique in a workspace, ignoring
// case, and may not be another agent's handle: others lists the workspace's
// agents, so a name never reads as someone else's.
func (s *Store) Set(tenant, name string, look Look, others []string) (Look, error) {
	name = legacy.BuiltinAgentKey(name)
	if name == "" || strings.HasPrefix(name, "#") {
		return Look{}, fmt.Errorf("%w: %q is not an agent's name", ErrInvalid, name)
	}
	look = look.Normalize()
	if err := look.Validate(); err != nil {
		return Look{}, err
	}
	shown := strings.ToLower(look.DisplayName)
	if shown != "" {
		for _, other := range others {
			if other = legacy.BuiltinAgentKey(other); other != name && strings.ToLower(other) == shown {
				return Look{}, fmt.Errorf("%w: %q is another agent's handle", ErrNameTaken, look.DisplayName)
			}
		}
	}
	_, err := s.t.Update(tenant, key, func(all map[string]Look, _ bool) (map[string]Look, error) {
		next := make(map[string]Look, len(all)+1)
		for k, v := range all {
			if k != name {
				next[k] = v
			}
			if k != name && shown != "" && strings.ToLower(v.DisplayName) == shown {
				return nil, fmt.Errorf("%w: %s is called %q", ErrNameTaken, k, v.DisplayName)
			}
		}
		if all[name].Equal(look) {
			return nil, state.ErrUnchanged
		}
		if !look.IsZero() {
			if len(next) >= Limit {
				return nil, fmt.Errorf("%w: a workspace keeps at most %d looks", ErrInvalid, Limit)
			}
			next[name] = look
		}
		return next, nil
	})
	if err != nil {
		return Look{}, err
	}
	return look, nil
}

// marksKey is the record of each agent's drawn mark, by agent: the token
// its picture is kept and served under (MarkPath). It is apart from the
// looks so drawing a picture never moves a look's version.
const marksKey = "#marks"

// ErrNoMark reports a token no agent's picture is kept under.
var ErrNoMark = errors.New("agentlooks: no such mark")

// ValidToken reports whether s is the shape of a mark's token: 32 hex
// digits, so it can stand in a path and a blob key as it is.
func ValidToken(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// MarkKey is the blob key an agent's picture is kept under in its tenant.
func MarkKey(token string) string { return "agent-marks/" + token + ".png" }

// Marks returns the token of each named agent that has a drawn mark.
func (s *Store) Marks(tenant string, names []string) (map[string]string, error) {
	all, err := s.marks.Lookup(tenant, marksKey)
	if errors.Is(err, state.ErrNotFound) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, requested := range names {
		if tok, ok := all.Value[legacy.BuiltinAgentKey(requested)]; ok {
			out[requested] = tok
		}
	}
	return out, nil
}

// SetMark records the token of an agent's newly drawn mark and returns the
// one it replaces, empty when it had none, so the caller can delete the old
// picture.
func (s *Store) SetMark(tenant, name, token string) (string, error) {
	name = legacy.BuiltinAgentKey(name)
	if name == "" || strings.HasPrefix(name, "#") || !ValidToken(token) {
		return "", fmt.Errorf("%w: a mark needs an agent and a token", ErrInvalid)
	}
	var old string
	_, err := s.marks.Update(tenant, marksKey, func(all map[string]string, _ bool) (map[string]string, error) {
		next := make(map[string]string, len(all)+1)
		for k, v := range all {
			next[k] = v
		}
		old = next[name]
		if _, had := next[name]; !had && len(next) >= Limit {
			return nil, fmt.Errorf("%w: a workspace keeps at most %d marks", ErrInvalid, Limit)
		}
		next[name] = token
		return next, nil
	})
	return old, err
}

// IsMark reports whether token is an agent's current mark in tenant, so an
// old picture is never served once it is replaced.
func (s *Store) IsMark(tenant, token string) (bool, error) {
	if !ValidToken(token) {
		return false, nil
	}
	all, err := s.marks.Lookup(tenant, marksKey)
	if errors.Is(err, state.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, t := range all.Value {
		if t == token {
			return true, nil
		}
	}
	return false, nil
}
