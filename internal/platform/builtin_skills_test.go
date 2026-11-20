package platform

import (
	"slices"
	"testing"

	"github.com/urmzd/humagen/pkg/library"
)

// Every workspace starts with skill-creator and agent-creator, published, so
// the chat bar's / menu has commands before anyone writes a skill. It follows the shipped
// text until a person revises it, and a platform that ships nothing adds none.
func TestBuiltinSkillsShipOnceAndYieldToAPersonsRevision(t *testing.T) {
	p, _ := configured(t, Config{ShipBuiltins: true})
	ctx := t.Context()
	scope := library.Scope{Kind: "workspace"}
	for range 2 {
		if err := p.EnsureBuiltins(ctx, "acme"); err != nil {
			t.Fatal(err)
		}
	}
	history, err := p.SkillLibrary.History("acme", scope, "skill-creator")
	if err != nil || len(history) != 1 {
		t.Fatalf("history = %d revisions, %v; want one, written once", len(history), err)
	}
	got := history[0]
	if got.By != builtinSkillOwner || got.PublishedAt.IsZero() || got.Instructions == "" {
		t.Fatalf("revision = by %q, published %v; want the platform's, published", got.By, got.PublishedAt)
	}
	published, _, err := p.SkillLibrary.ListPublished("acme", scope)
	var names []string
	for _, d := range published {
		names = append(names, d.Name)
	}
	slices.Sort(names)
	if err != nil || !slices.Equal(names, []string{"agent-creator", "skill-creator"}) {
		t.Fatalf("published = %v, %v; want agent-creator and skill-creator", names, err)
	}

	// A person's revision is theirs: the next start leaves it as they wrote it.
	mine := got.Definition
	mine.Instructions = "Ask what the task is, then write the skill."
	if _, err := p.SkillLibrary.Draft("acme", "ana", scope, mine, got.Version, nil, p.now()); err != nil {
		t.Fatal(err)
	}
	if err := p.EnsureBuiltins(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	history, err = p.SkillLibrary.History("acme", scope, "skill-creator")
	if err != nil || len(history) != 2 || history[1].By != "ana" {
		t.Fatalf("after a person's revision: %d revisions, %v; want theirs last and nothing added", len(history), err)
	}

	bare, _ := configured(t, Config{})
	if err := bare.EnsureBuiltins(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	if history, err := bare.SkillLibrary.History("acme", scope, "skill-creator"); err != nil || len(history) != 0 {
		t.Fatalf("a platform that ships nothing has %d revisions, %v; want none", len(history), err)
	}
}
