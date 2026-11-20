package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestSkillRevisionsPublishExactTestedContentAndSurviveReplay(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		scope := Scope{Kind: "personal", Subject: "ada"}
		definition := Definition{Skill: projects.Skill{Name: "summarize", Description: "Summarize notes", Instructions: "Use three sentences"}, Tools: []string{}}
		first, err := s.Draft("acme", "ada", scope, definition, 0, nil, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Publish("acme", "ada", scope, first.Name, first.Version, time.Now()); !errors.Is(err, ErrConflict) {
			t.Fatalf("untested published: %v", err)
		}
		if err := s.Tested("acme", scope, first.Name, first.Version, "test-run"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Publish("acme", "ada", scope, first.Name, first.Version, time.Now()); err != nil {
			t.Fatal(err)
		}
		definition.Instructions = "Changed draft"
		if _, err := s.Draft("acme", "ada", scope, definition, 1, nil, time.Now()); err != nil {
			t.Fatal(err)
		}
		s, err = Open(st)
		if err != nil {
			t.Fatal(err)
		}
		published, ok, err := s.Published("acme", scope, first.Name)
		if err != nil || !ok || published.Version != 1 || published.Instructions != "Use three sentences" {
			t.Fatalf("draft changed live revision: %+v", published)
		}
		if other, _, err := s.List("other", scope); err != nil || len(other) != 0 {
			t.Fatal("cross-tenant skill", err)
		}
		if bob, _, err := s.List("acme", Scope{Kind: "personal", Subject: "bob"}); err != nil || len(bob) != 0 {
			t.Fatal("cross-scope skill")
		}
		if _, err := s.Draft("acme", "ada", scope, definition, 1, nil, time.Now()); !errors.Is(err, ErrConflict) {
			t.Fatal("stale draft accepted")
		}
	})
}

func TestFailedSkillWritesAndReturnedSlicesDoNotChangePublishedRevision(t *testing.T) {
	st := statetest.NewFaulty(state.NewMemory())
	s, _ := Open(st)
	scope := Scope{Kind: "workspace"}
	d := Definition{Skill: projects.Skill{Name: "review", Description: "Review", Instructions: "Read carefully"}, Tools: []string{"file_read"}}
	r, err := s.Draft("acme", "ada", scope, d, 0, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	d.Tools[0] = "file_write"
	r.Tools[0] = "file_write"
	st.Fail(statetest.ErrInjected)
	if err := s.Tested("acme", scope, "review", 1, "run"); err == nil {
		t.Fatal("failed write accepted")
	}
	held, _ := s.Get("acme", scope, "review", 1)
	if held.Tools[0] != "file_read" || len(held.Tests) != 0 {
		t.Fatalf("mutated immutable revision: %+v", held)
	}
}

// An import drafts as any draft does, unpublished and after the revisions
// read, and the revision records that an import wrote it.
func TestImportDraftsARevisionMarkedAsImported(t *testing.T) {
	statetest.Each(t, importDraftsARevisionMarkedAsImported)
}

func importDraftsARevisionMarkedAsImported(t *testing.T, open func(*testing.T) state.Store) {
	s, err := Open(open(t))
	if err != nil {
		t.Fatal(err)
	}
	scope, now := Scope{Kind: "workspace"}, time.Now()
	d := Definition{Skill: projects.Skill{Name: "review", Description: "Review changes", Instructions: "Read the proposal"}, Tools: []string{}}
	first, err := s.Import("acme", "ada", scope, d, 0, now)
	if err != nil || first.Via != ViaImport || first.Version != 1 || !first.PublishedAt.IsZero() || len(first.Tests) != 0 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	if _, err := s.Import("acme", "ada", scope, d, 0, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("an import after a revision it did not read = %v", err)
	}
	d.Instructions = "Read the proposal twice"
	if second, err := s.Draft("acme", "ada", scope, d, 1, nil, now); err != nil || second.Via != "" {
		t.Fatalf("an edit = %+v, %v", second, err)
	}
	rows, err := s.History("acme", scope, "review")
	if err != nil || len(rows) != 2 || rows[0].Via != ViaImport || rows[1].Via != "" {
		t.Fatalf("history = %+v, %v", rows, err)
	}
}

// A tool list that is absent stays absent, and an empty one stays empty,
// through what is stored and read back: absent does not limit a run's tools
// and empty allows none. An imported revision is limited only as its
// SKILL.md declares, the empty list an earlier import wrote included; an
// edit saved in the editor is limited as stored.
func TestAToolListKeepsAbsentApartFromEmpty(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		s, err := Open(open(t))
		if err != nil {
			t.Fatal(err)
		}
		scope, now := Scope{Kind: "workspace"}, time.Now()
		pkg := func(name, allowed string) Definition {
			return Definition{Skill: projects.Skill{Name: name}, Files: []File{
				{Path: "SKILL.md", Data: []byte("---\nname: " + name + "\ndescription: Generated fixture\n" + allowed + "---\nRead references/guide.md.\n")},
				{Path: "references/guide.md", Data: []byte("Generated fixture guide")},
			}}
		}
		cases := []struct {
			name, allowed string
			tools         []string
			imported      bool
			stored, limit []string
		}{
			{"now", "", nil, true, nil, nil},
			{"before-cli", "", []string{}, true, nil, nil},
			{"declared", "allowed-tools: file_read file_read\n", nil, true, []string{"file_read"}, []string{"file_read"}},
			{"declared-none", "allowed-tools: \"\"\n", nil, true, []string{}, []string{}},
			{"edited", "", []string{}, false, []string{}, []string{}},
			{"edited-open", "", nil, false, nil, nil},
		}
		for _, c := range cases {
			d := pkg(c.name, c.allowed)
			d.Tools = c.tools
			if c.imported {
				_, err = s.Import("acme", "ada", scope, d, 0, now)
			} else {
				_, err = s.Draft("acme", "ada", scope, d, 0, nil, now)
			}
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			rows, err := s.History("acme", scope, c.name)
			if err != nil || len(rows) != 1 {
				t.Fatalf("%s: %v", c.name, err)
			}
			r := rows[0]
			if (r.Tools == nil) != (c.stored == nil) || !slices.Equal(r.Tools, c.stored) {
				t.Errorf("%s stored %#v, want %#v", c.name, r.Tools, c.stored)
			}
			if limit := r.ToolLimit(); (limit == nil) != (c.limit == nil) || !slices.Equal(limit, c.limit) {
				t.Errorf("%s limits to %#v, want %#v", c.name, limit, c.limit)
			}
		}
	})
}

// A revision an import wrote before this release holds the empty list that
// import sent whatever the folder said. It is read with the limit its
// SKILL.md declares, a list past the bounds read as allowing none, and the
// next write of the record stores what is read, so the store, what it
// answers and what limits a run agree.
func TestAnEarlierImportIsReadWithItsDeclaredLimit(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		s, err := Open(open(t))
		if err != nil {
			t.Fatal(err)
		}
		scope := Scope{Kind: "workspace"}
		var many []string
		for i := range MaxSkillTools + 1 {
			many = append(many, fmt.Sprintf("t%d", i))
		}
		for _, c := range []struct {
			name, allowed string
			limit         []string
		}{
			{"legacy-open", "", nil},
			{"legacy-declared", "allowed-tools: file_read\n", []string{"file_read"}},
			{"legacy-over", "allowed-tools: " + strings.Join(many, " ") + "\n", []string{}},
		} {
			d := Definition{Skill: projects.Skill{Name: c.name, Description: "Generated fixture", Instructions: "Read references/guide.md."}, Tools: []string{}, Files: []File{
				{Path: "SKILL.md", Data: []byte("---\nname: " + c.name + "\ndescription: Generated fixture\n" + c.allowed + "---\nRead references/guide.md.\n")},
				{Path: "references/guide.md", Data: []byte("Generated fixture guide")},
			}}
			if d, err = d.Normalize(); err != nil {
				t.Fatal(err)
			}
			d.Tools = []string{}
			row := Revision{ID: ID(scope, c.name), Via: ViaImport, Definition: d, Scope: scope, Version: 1, By: "ada", At: time.Now(), Tests: []string{}}
			raw, err := json.Marshal([]Revision{row})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.raw.Put("acme", skillKey(scope, c.name), raw); err != nil {
				t.Fatal(err)
			}
			read := func(when string) {
				t.Helper()
				rows, err := s.History("acme", scope, c.name)
				if err != nil || len(rows) != 1 {
					t.Fatalf("%s %s: %v", c.name, when, err)
				}
				if got := rows[0].Tools; (got == nil) != (c.limit == nil) || !slices.Equal(got, c.limit) || !slices.Equal(rows[0].ToolLimit(), c.limit) {
					t.Fatalf("%s %s: read %#v, want %#v", c.name, when, got, c.limit)
				}
			}
			read("as stored")
			if err := s.Tested("acme", scope, c.name, 1, "generated-test"); err != nil {
				t.Fatal(err)
			}
			read("after a write")
			item, err := s.raw.Lookup("acme", skillKey(scope, c.name))
			if err != nil {
				t.Fatal(err)
			}
			var stored []Revision
			if err := json.Unmarshal(item.Value, &stored); err != nil {
				t.Fatal(err)
			}
			if got := stored[0].Tools; (got == nil) != (c.limit == nil) || !slices.Equal(got, c.limit) {
				t.Fatalf("%s: stored %#v after a write, want %#v", c.name, got, c.limit)
			}
		}
	})
}

// A record this release cannot read, such as one a later release wrote past
// its bounds or in another shape, fails only itself: listing its scope gives
// every other skill and names it as unreadable, and every read of it, and a
// draft after it, is still refused.
func TestARecordThisReleaseCannotReadFailsOnlyItself(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		s, err := Open(open(t))
		if err != nil {
			t.Fatal(err)
		}
		scope := Scope{Kind: "personal", Subject: "ada"}
		small := Definition{Skill: projects.Skill{Name: "small", Description: "Small", Instructions: "Answer briefly."}, Tools: []string{}}
		if _, err := s.Draft("acme", "ada", scope, small, 0, nil, time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := s.Tested("acme", scope, "small", 1, "test-run"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Publish("acme", "ada", scope, "small", 1, time.Now()); err != nil {
			t.Fatal(err)
		}
		// A published revision whose supporting file is past this release's
		// bound, as a later release could write it, and a record in a shape
		// this release does not decode.
		later := schemaSkill("later", MaxPackageFileBytes+1)
		later.Description, later.Instructions = "Validates documents against a generated schema", "Read schemas/ on demand."
		row := Revision{ID: ID(scope, "later"), Definition: later, Scope: scope, Version: 1, By: "ada", At: time.Now(), PublishedAt: time.Now(), PublishedBy: "ada", Tests: []string{"test-run"}}
		raw, err := json.Marshal([]Revision{row})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.raw.Put("acme", skillKey(scope, "later"), raw); err != nil {
			t.Fatal(err)
		}
		if err := s.raw.Put("acme", skillKey(scope, "garbled"), json.RawMessage(`{"revisions":"not a list"}`)); err != nil {
			t.Fatal(err)
		}
		o := &ops{}
		o.set("acme", idKey(row.ID), skillKey(scope, "later"))
		if err := o.commit(s.kv); err != nil {
			t.Fatal(err)
		}

		listed, unreadable, err := s.List("acme", scope)
		if err != nil || len(listed) != 1 || listed[0].Name != "small" || strings.Join(unreadable, ",") != "garbled,later" {
			t.Fatalf("list = %d skills, unreadable %v, %v", len(listed), unreadable, err)
		}
		published, unreadable, err := s.ListPublished("acme", scope)
		if err != nil || len(published) != 1 || published[0].Name != "small" || strings.Join(unreadable, ",") != "garbled,later" {
			t.Fatalf("published = %d skills, unreadable %v, %v", len(published), unreadable, err)
		}
		if _, err := s.Get("acme", scope, "small", 0); err != nil {
			t.Fatalf("a readable skill beside them: %v", err)
		}
		if _, err := s.Get("acme", scope, "later", 0); !errors.Is(err, ErrInvalid) {
			t.Fatalf("get of the unreadable record = %v", err)
		}
		if _, ok, err := s.Published("acme", scope, "later"); ok || !errors.Is(err, ErrInvalid) {
			t.Fatalf("published of the unreadable record = %v, %v", ok, err)
		}
		if _, err := s.Find("acme", row.ID); !errors.Is(err, ErrInvalid) {
			t.Fatalf("find of the unreadable record = %v", err)
		}
		if _, err := s.Draft("acme", "ada", scope, schemaSkill("later"), 1, nil, time.Now()); err == nil {
			t.Fatal("a draft after the unreadable record was written")
		}
	})
}
