package projects

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// KindSpec is a workspace's own kind: a name and a title over a built-in
// behavior. The behaviors stay humagen's and stay testable; the names are the
// workspace's. Only the root carries them.
type KindSpec struct {
	// Name matches ^[a-z][a-z0-9-]{0,31}$ and is not a built-in kind.
	Name Kind `json:"name"`
	// Title is what people see, at most MaxKindTitle characters.
	Title string `json:"title,omitempty"`
	// Like is the behavior: team or account.
	Like Kind `json:"like"`
}

// Bounds on the workspace's kinds.
const (
	MaxKinds     = 16
	MaxKindTitle = 40
)

var kindName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

func validKindName(k Kind) bool { return kindName.MatchString(string(k)) }

// validKinds checks the root's kinds on their own.
func validKinds(ks []KindSpec) error {
	if len(ks) > MaxKinds {
		return fmt.Errorf("%w: a workspace defines at most %d kinds", ErrInvalid, MaxKinds)
	}
	seen := map[Kind]bool{}
	for _, k := range ks {
		switch {
		case builtIn(k.Name):
			return fmt.Errorf("%w: %s is a kind Humagen defines", ErrInvalid, k.Name)
		case !validKindName(k.Name):
			return fmt.Errorf("%w: a kind's name is lower case letters, digits and dashes, starting with a letter, at most 32", ErrInvalid)
		case utf8.RuneCountInString(k.Title) > MaxKindTitle || !oneLine(k.Title):
			return fmt.Errorf("%w: a kind's title is one line of at most %d characters", ErrInvalid, MaxKindTitle)
		case k.Like != Team && k.Like != Account:
			return fmt.Errorf("%w: the kind %s behaves like team or account", ErrInvalid, k.Name)
		case seen[k.Name]:
			return fmt.Errorf("%w: the kind %s is defined twice", ErrInvalid, k.Name)
		}
		seen[k.Name] = true
	}
	return nil
}

// likeIn is the built-in behavior of a kind among a set of the workspace's
// kinds: the kind itself when it is built in, its Like when it is one of ks,
// and "" when it is neither.
func likeIn(ks []KindSpec, k Kind) Kind {
	if builtIn(k) {
		return k
	}
	if i := slices.IndexFunc(ks, func(s KindSpec) bool { return s.Name == k }); i >= 0 {
		return ks[i].Like
	}
	return ""
}

// Like returns the built-in behavior of a kind: company, team or account. It
// is "" for a kind the workspace does not define.
func (t *Tree) Like(k Kind) Kind { return likeIn(t.nodes[Root].p.Kinds, k) }

// Kinds returns the workspace's own kinds.
func (t *Tree) Kinds() []KindSpec { return slices.Clone(t.nodes[Root].p.Kinds) }

// kindRules checks a project's kind against the tree: the workspace defines
// it, only an account-like kind carries an account, a company project sits
// under the root or another company project, and an account-like project
// neither sits under one nor has one under it.
func (t *Tree) kindRules(p, parent Project, children []Project) error {
	like := t.Like(p.Kind)
	switch {
	case like == "":
		return fmt.Errorf("%w: the workspace has no kind %q", ErrInvalid, p.Kind)
	case p.Account != nil && like != Account:
		return fmt.Errorf("%w: only an account project carries an account", ErrInvalid)
	case p.Kind == Company && parent.Kind != Company:
		return fmt.Errorf("%w: a company project sits under the workspace or another company project", ErrInvalid)
	case like == Account && t.Like(parent.Kind) == Account:
		return fmt.Errorf("%w: an account cannot sit under another account", ErrInvalid)
	}
	for _, c := range children {
		switch {
		case c.Kind == Company && p.Kind != Company:
			return fmt.Errorf("%w: the company project %s sits under %s, so it stays company", ErrInvalid, c.Name, display(p.Name))
		case like == Account && t.Like(c.Kind) == Account:
			return fmt.Errorf("%w: the account %s sits under %s, and an account cannot sit under another", ErrInvalid, c.Name, display(p.Name))
		}
	}
	return nil
}

// kindsHeld checks a change to the root's kinds against the projects that use
// them: a kind in use cannot be removed, nor its behavior changed.
func kindsHeld(projects map[string]Project, before, after []KindSpec) error {
	for _, was := range before {
		i := slices.IndexFunc(after, func(k KindSpec) bool { return k.Name == was.Name })
		if i >= 0 && after[i].Like == was.Like {
			continue
		}
		var users []string
		for name, p := range projects {
			if name != Root && p.Kind == was.Name {
				users = append(users, name)
			}
		}
		if len(users) == 0 {
			continue
		}
		slices.SortFunc(users, compareNames)
		if i < 0 {
			return fmt.Errorf("%w: the kind %s is used by %s", ErrInUse, was.Name, strings.Join(users, ", "))
		}
		return fmt.Errorf("%w: the kind %s is used by %s, so it keeps behaving like %s", ErrInUse, was.Name, strings.Join(users, ", "), was.Like)
	}
	return nil
}
