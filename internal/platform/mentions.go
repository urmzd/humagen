package platform

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/people"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/threads"
)

// Mention kinds: @<kind>:<name>, each completing from its own list and
// resolving on the server. The built-in kinds, sme (experts) and skill
// (skills), always exist; a workspace's Platform document declares more
// (Config.MentionKinds), each resolving to experts, skills, a role or a
// person. The tag grammar reads them (threads.Parse) and a mention of one
// notifies as the kind it resolves to does.

// MaxMentionItems is how many items one completion of a mention kind lists.
const MaxMentionItems = 20

// maxPeoplePages is how many pages of the directory one completion reads.
const maxPeoplePages = 4

// MaxMentionQuery bounds what a completion is asked for, in characters.
const MaxMentionQuery = 100

// ErrNoMentionKind reports a mention kind the workspace does not have.
var ErrNoMentionKind = errors.New("platform: no such mention kind")

// MentionKind is one kind of @<kind>:<name>: its name, what the console
// calls it, and what its items resolve to (threads.ResolvesExperts,
// ResolvesSkills, ResolvesRole or ResolvesPeople).
type MentionKind struct {
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Resolves string `json:"resolves"`
}

// builtInKinds are the mention kinds every workspace has, first.
var builtInKinds = []MentionKind{
	{Kind: threads.SkillName, Title: "Skills", Resolves: threads.ResolvesSkills},
}

// MentionKinds is the mention kinds a tenant has: the built-in ones, then
// the ones its Platform document declares, in its order.
func (p *Platform) MentionKinds(tenant string) []MentionKind {
	return slices.DeleteFunc(append(slices.Clone(builtInKinds), p.cfg.MentionKinds[tenant]...), func(k MentionKind) bool { return k.Resolves == threads.ResolvesExperts || k.Kind == threads.SMEName })
}

// mentionKind is what a kind the tenant declares resolves to. The built-in
// kinds are the grammar's own.
func (p *Platform) mentionKind(tenant, kind string) (string, bool) {
	for _, k := range p.cfg.MentionKinds[tenant] {
		if k.Kind == kind {
			return k.Resolves, true
		}
	}
	return "", false
}

// MentionItem is one item a mention kind completes to: what follows the
// colon, and a line about it.
type MentionItem struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// MentionItems completes a mention kind's items where a message is posted,
// "~" or "" for the workspace: the ones whose name starts with prefix,
// ignoring case, by name, at most MaxMentionItems. The caller may read the
// project. The items are what the kind resolves to there: the topics the
// project's chain has experts for, the skills it offers, the roles it names
// (in its member entries and its experts), or the people in the workspace's
// directory, which takes people:read, as the directory does: without it the
// list is empty.
func (p *Platform) MentionItems(who auth.Principal, project, kind, prefix string) ([]MentionItem, error) {
	name := projects.NameOf(project)
	tree := p.Projects.Tree(who.Tenant)
	if !tree.Has(name) {
		return nil, fmt.Errorf("%w: %q", projects.ErrNotFound, project)
	}
	if utf8.RuneCountInString(prefix) > MaxMentionQuery || len(prefix) > 4*MaxMentionQuery {
		return nil, fmt.Errorf("%w: a completion is asked for at most %d characters", projects.ErrInvalid, MaxMentionQuery)
	}
	var resolves string
	for _, k := range p.MentionKinds(who.Tenant) {
		if k.Kind == kind {
			resolves = k.Resolves
		}
	}
	if resolves == "" {
		return nil, fmt.Errorf("%w: there is no mention kind called %q", ErrNoMentionKind, kind)
	}
	low := strings.ToLower(prefix)
	out := []MentionItem{}
	// add keeps an item whose name starts with the prefix, or for a
	// person, whose name does.
	add := func(item MentionItem, called ...string) {
		match := strings.HasPrefix(strings.ToLower(item.Name), low)
		for _, c := range called {
			match = match || strings.HasPrefix(strings.ToLower(c), low)
		}
		if match && !slices.ContainsFunc(out, func(o MentionItem) bool { return o.Name == item.Name }) {
			out = append(out, item)
		}
	}
	switch resolves {
	case threads.ResolvesExperts:
		for _, topic := range tree.ExpertTopics(name) {
			add(MentionItem{Name: topic})
		}
	case threads.ResolvesSkills:
		for _, s := range p.AvailableSkills(who.Tenant, name, humanSubject(who)) {
			add(MentionItem{Name: s.Name, Description: s.Description})
		}
	case threads.ResolvesRole:
		for _, pr := range tree.Chain(name) {
			for _, m := range pr.Members {
				if role, ok := strings.CutPrefix(m.Who, projects.RolePrefix); ok {
					add(MentionItem{Name: role})
				}
			}
		}
	case threads.ResolvesPeople:
		if p.People == nil || !who.Can(auth.PeopleRead) {
			return out, nil
		}
		// The directory finds people whose handle or name holds the
		// prefix, by handle; a few pages are read for the ones it starts.
		q := people.Query{Q: prefix, Limit: people.MaxPage}
		for range maxPeoplePages {
			page, next, err := p.People.List(who.Tenant, q)
			if err != nil {
				return nil, err
			}
			for _, person := range page {
				add(MentionItem{Name: person.Handle, Description: person.Name}, person.Name)
			}
			if next == "" || len(out) >= MaxMentionItems {
				break
			}
			q.Before = next
		}
	}
	slices.SortFunc(out, func(a, b MentionItem) int { return strings.Compare(a.Name, b.Name) })
	return out[:min(len(out), MaxMentionItems)], nil
}
