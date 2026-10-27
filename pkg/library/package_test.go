package library

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func packageDefinition() Definition {
	return Definition{Skill: projects.Skill{Name: "brief"}, Tools: []string{"file_read", "file_write"}, Files: []File{
		{Path: "SKILL.md", Data: []byte("---\nname: brief\ndescription: Summarize decisions\nallowed-tools: file_read\n---\n\nRead references/style.md before writing.\n")},
		{Path: "references/style.md", Data: []byte("Use clear owners and review dates.")},
		{Path: "assets/example.bin", Data: []byte{0, 255, 1}},
	}}
}

func TestPackageRevisionsAreImmutableAndReplayWithSupportingFiles(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		scope := Scope{Kind: "personal", Subject: "ada"}
		d := packageDefinition()
		first, err := s.Draft("acme", "ada", scope, d, 0, nil, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if first.Description != "Summarize decisions" || len(first.Tools) != 1 || first.Tools[0] != "file_read" {
			t.Fatalf("frontmatter did not narrow: %+v", first.Definition)
		}
		d.Files[1].Data[0] = 'X'
		first.Files[1].Data[0] = 'Y'
		held, _ := s.Get("acme", scope, "brief", 1)
		if string(held.Files[1].Data) != "Use clear owners and review dates." {
			t.Fatal("file alias mutated revision")
		}
		next := held.Definition
		next.Files[1].Data = []byte("New unpublished style")
		if _, err := s.Draft("acme", "ada", scope, next, 1, nil, time.Now()); err != nil {
			t.Fatal(err)
		}
		replayed, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		old, _ := replayed.Get("acme", scope, "brief", 1)
		pkg, err := old.Package()
		if err != nil {
			t.Fatal(err)
		}
		data, _, err := pkg.ReadResource("references/style.md", MaxPackageFileBytes)
		if err != nil || string(data) != "Use clear owners and review dates." {
			t.Fatalf("old package changed: %q, %v", data, err)
		}
		if _, err := replayed.Get("other", scope, "brief", 1); !errors.Is(err, ErrNotFound) {
			t.Fatal("package crossed tenant")
		}
	})
}

func TestMalformedPackagesFailWholeAndNeverGrantTools(t *testing.T) {
	for _, path := range []string{"../secret", "/secret", `refs\secret`, "refs/../secret", "SKILL.md"} {
		t.Run(path, func(t *testing.T) {
			d := packageDefinition()
			d.Files = append(d.Files, File{Path: path, Data: []byte("x")})
			if _, err := d.Normalize(); !errors.Is(err, ErrInvalid) {
				t.Fatal("accepted invalid path", err)
			}
		})
	}
	for _, mutate := range []func(*Definition){
		func(d *Definition) { d.Files = d.Files[1:] },
		func(d *Definition) { d.Name = "different" },
		func(d *Definition) { d.Files[0].Data = []byte("name: brief") },
		func(d *Definition) { d.Files[0].Data = []byte{255} },
		func(d *Definition) { d.Files[0].Data = append(d.Files[0].Data, 0) },
		func(d *Definition) { d.Files[1].Data = make([]byte, MaxPackageFileBytes+1) },
		func(d *Definition) { d.Files[1].Data = nil },
	} {
		d := packageDefinition()
		mutate(&d)
		if _, err := d.Normalize(); !errors.Is(err, ErrInvalid) {
			t.Fatal("accepted malformed package", err)
		}
	}
	d := packageDefinition()
	d.Tools = nil
	normal, err := d.Normalize()
	if err != nil || len(normal.Tools) != 0 {
		t.Fatal("frontmatter granted tools", err)
	}
	d = packageDefinition()
	d.Files[0].Data = []byte("---\nname: brief\ndescription: Brief\n---\n" + strings.Repeat("x", MaxPackageInstructions+1))
	if normal, err := d.Normalize(); err == nil && normal.Valid() {
		t.Fatal("unbounded instructions")
	}
}

func TestLegacyRevisionExposesAStandardSkillFileWithoutChangingItsDefinition(t *testing.T) {
	d := Definition{Skill: projects.Skill{Name: "brief", Description: "Quotes: \"safe\"", Instructions: "Read carefully"}, Tools: []string{}}
	files := d.PackageFiles()
	if d.Files != nil || len(files) != 1 || files[0].Path != "SKILL.md" {
		t.Fatal("legacy revision was rewritten")
	}
	d.Files = files
	normal, err := d.Normalize()
	if err != nil || normal.Description != d.Description || strings.TrimSpace(normal.Instructions) != d.Instructions {
		t.Fatal("legacy round trip", err)
	}
}
