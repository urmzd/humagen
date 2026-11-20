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
}

// IsZero reports whether the look is the default in every field.
func (l Look) IsZero() bool { return l == Look{} }

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
	t *state.Table[map[string]Look]
}

// Open returns a store on st, which may be shared with other processes. A
// nil st keeps looks in this process's memory.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	return &Store{t: state.NewTable[map[string]Look](st, "agent-looks")}, nil
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
		if all[name] == look {
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
