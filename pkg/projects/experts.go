package projects

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Expert bounds.
const (
	// MaxExperts is how many experts one project lists.
	MaxExperts = 50
	// MaxTopics is how many topics one expert answers for.
	MaxTopics = 8
	// MaxExpertTopics is how many topics the experts line names.
	MaxExpertTopics = 16
	// MaxWho bounds a subject.
	MaxWho = 200
)

// FieldExperts is the experts a project lists, when the platform file sets
// them.
const FieldExperts = "experts"

// ExpertsChanged is what a change to a project's experts records.
const ExpertsChanged = "experts"

// RolePrefix marks an entry that names a role rather than a person.
const RolePrefix = "role:"

// Expert is who answers for some topics in a project: a person by subject,
// or everyone holding a role, written "role:<name>".
type Expert struct {
	Who    string   `json:"who"`
	Topics []string `json:"topics"`
}

var (
	validTopic = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	validRole  = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
)

// ValidTopic reports whether a topic can be listed: lower case letters,
// digits and dashes, starting with a letter, at most 32.
func ValidTopic(topic string) bool { return validTopic.MatchString(topic) }

func cloneExperts(es []Expert) []Expert {
	out := make([]Expert, len(es))
	for i, e := range es {
		out[i] = Expert{Who: e.Who, Topics: slices.Clone(e.Topics)}
		if out[i].Topics == nil {
			out[i].Topics = []string{}
		}
	}
	return out
}

func sameExperts(a, b []Expert) bool {
	return slices.EqualFunc(a, b, func(x, y Expert) bool { return x.Who == y.Who && slices.Equal(x.Topics, y.Topics) })
}

// validExperts checks a project's experts: at most MaxExperts, each a subject
// or a role listed once, with 1 to MaxTopics topics listed once each.
func validExperts(es []Expert) error {
	if len(es) > MaxExperts {
		return fmt.Errorf("%w: a project lists at most %d experts", ErrInvalid, MaxExperts)
	}
	seen := map[string]bool{}
	for _, e := range es {
		if err := validWho(e.Who); err != nil {
			return err
		}
		if seen[e.Who] {
			return fmt.Errorf("%w: %s is listed as an expert twice", ErrInvalid, e.Who)
		}
		seen[e.Who] = true
		if len(e.Topics) == 0 || len(e.Topics) > MaxTopics {
			return fmt.Errorf("%w: an expert answers for 1 to %d topics", ErrInvalid, MaxTopics)
		}
		topics := map[string]bool{}
		for _, t := range e.Topics {
			if !ValidTopic(t) {
				return fmt.Errorf("%w: a topic is lower case letters, digits and dashes, starting with a letter, at most 32; not %q", ErrInvalid, t)
			}
			if topics[t] {
				return fmt.Errorf("%w: %s lists %s twice", ErrInvalid, e.Who, t)
			}
			topics[t] = true
		}
	}
	return nil
}

// validWho checks who an expert is: a subject, or "role:<name>". Everyone
// ("*") is no one's expert.
func validWho(who string) error {
	if name, ok := strings.CutPrefix(who, RolePrefix); ok {
		if !validRole.MatchString(name) {
			return fmt.Errorf("%w: a role is lower case letters, digits, dashes and underscores; not %q", ErrInvalid, who)
		}
		return nil
	}
	switch {
	case strings.TrimSpace(who) == "" || who == "*":
		return fmt.Errorf("%w: an expert is a person's subject or role:<name>", ErrInvalid)
	case utf8.RuneCountInString(who) > MaxWho || !oneLine(who) || strings.TrimSpace(who) != who:
		return fmt.Errorf("%w: a subject is one line of at most %d characters", ErrInvalid, MaxWho)
	}
	return nil
}

// ExpertsFor finds who answers for a topic in a project: the experts of the
// nearest project on its chain (the project, then its ancestors, then the
// root) that lists any for it, and that project's name. With topic "" it is
// the nearest project that lists any expert at all. who is empty when no
// project on the chain has one.
func (t *Tree) ExpertsFor(name, topic string) (who []string, from string) {
	for _, p := range t.Chain(name) {
		for _, e := range p.Experts {
			if topic == "" || slices.Contains(e.Topics, topic) {
				who = append(who, e.Who)
			}
		}
		if len(who) > 0 {
			return who, p.Name
		}
	}
	return nil, ""
}

// ExpertTopics returns the topics someone answers for in a project, along
// its chain, by name: at most MaxExpertTopics, the nearest projects' first
// when there are more.
func (t *Tree) ExpertTopics(name string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range t.Chain(name) {
		for _, e := range p.Experts {
			for _, topic := range e.Topics {
				if !seen[topic] && len(out) < MaxExpertTopics {
					seen[topic] = true
					out = append(out, topic)
				}
			}
		}
	}
	slices.Sort(out)
	return out
}

// ExpertsLine is what the project layer of a turn says about its experts,
// or "" when there are none.
func ExpertsLine(topics []string) string {
	if len(topics) == 0 {
		return ""
	}
	return "Experts here: " + strings.Join(topics, ", ") + ". Ask one with ask_person, asked_of sme:<topic>."
}

// SetExperts replaces the experts a project lists. version must be the
// current one (ErrVersion); experts the file sets are changed only by the
// file (ErrManaged); the root and company projects need projects:govern. A
// change that changes nothing writes nothing and keeps the version.
func (s *Store) SetExperts(tenantName, name string, version int, experts []Expert, w Writer, at time.Time) (Project, error) {
	experts = cloneExperts(experts)
	if err := validExperts(experts); err != nil {
		return Project{}, err
	}
	return s.edit(tenantName, name, version, w, at, ExpertsChanged, func(p *Project) error {
		p.Experts = experts
		return nil
	})
}
