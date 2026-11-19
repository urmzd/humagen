package library

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// generatedSchema is a generated stand-in for a large schema file a skill
// ships: XML text of exactly n bytes, its content varied by seed.
func generatedSchema(n int, seed string) []byte {
	head := "<?xml version=\"1.0\"?>\n<!-- generated test fixture " + seed + " -->\n<schema>\n"
	tail := "</schema>\n"
	var b bytes.Buffer
	b.WriteString(head)
	for i := 0; b.Len() < n-len(tail); i++ {
		fmt.Fprintf(&b, "  <element name=\"e%d\" type=\"t%d\"/>\n", i, i)
	}
	b.Truncate(n - len(tail))
	b.WriteString(tail)
	return b.Bytes()
}

// schemaSkill is a generated skill named name whose supporting files are
// sized as given.
func schemaSkill(name string, sizes ...int) Definition {
	d := Definition{Skill: projects.Skill{Name: name}, Tools: []string{}, Files: []File{
		{Path: "SKILL.md", Data: []byte("---\nname: " + name + "\ndescription: Validates documents against a generated schema\n---\n\nRead schemas/ on demand.\n")},
	}}
	for i, size := range sizes {
		d.Files = append(d.Files, File{Path: fmt.Sprintf("schemas/part%d.xsd", i), Data: generatedSchema(size, fmt.Sprintf("%s-%d", name, i))})
	}
	return d
}

// A supporting file may be as large as MaxPackageFileBytes and no larger,
// and is named when it is over. SKILL.md, whose body is what a model is
// asked, keeps its own smaller bound, and the package its total.
func TestPackageBoundsForSupportingFilesSkillFileAndTotal(t *testing.T) {
	if _, err := schemaSkill("schemas", MaxPackageFileBytes).Normalize(); err != nil {
		t.Fatalf("a supporting file of exactly %d bytes: %v", MaxPackageFileBytes, err)
	}
	_, err := schemaSkill("schemas", MaxPackageFileBytes+1).Normalize()
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), fmt.Sprintf("schemas/part0.xsd is over %d bytes", MaxPackageFileBytes)) {
		t.Fatalf("a supporting file one byte over: %v", err)
	}
	if MaxPackageFileBytes != 1<<20 || MaxPackageSkillFileBytes != 128<<10 || MaxPackageBytes != 2<<20 || MaxSkillBytes != 16<<20 {
		t.Fatal("the bounds the docs and api/openapi.yaml state changed")
	}

	skillFile := func(n int) Definition {
		d := schemaSkill("schemas")
		head := "---\nname: schemas\ndescription: Validates documents against a generated schema\n---\n\n"
		d.Files[0].Data = []byte(head + strings.Repeat("x", n-len(head)))
		return d
	}
	if _, err := skillFile(MaxPackageSkillFileBytes).Package(); err != nil {
		t.Fatalf("a SKILL.md of exactly %d bytes: %v", MaxPackageSkillFileBytes, err)
	}
	if _, err := skillFile(MaxPackageSkillFileBytes + 1).Package(); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), fmt.Sprintf("SKILL.md is over %d bytes", MaxPackageSkillFileBytes)) {
		t.Fatalf("a SKILL.md one byte over: %v", err)
	}

	skillMD := len(schemaSkill("schemas").Files[0].Data)
	rest := MaxPackageBytes - skillMD - MaxPackageFileBytes
	if _, err := schemaSkill("schemas", MaxPackageFileBytes, rest).Normalize(); err != nil {
		t.Fatalf("a package of exactly %d bytes: %v", MaxPackageBytes, err)
	}
	if _, err := schemaSkill("schemas", MaxPackageFileBytes, rest+1).Normalize(); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), fmt.Sprintf("package exceeds %d bytes", MaxPackageBytes)) {
		t.Fatalf("a package one byte over: %v", err)
	}
}

// A revision with a supporting file of the largest size is kept whole on
// every store, and a skill's revisions hold at most MaxSkillBytes of files
// together, so one record and one history answer stay where they were.
func TestLargeSupportingFilesAreKeptWholeAndBoundedPerSkill(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		scope := Scope{Kind: "personal", Subject: "ada"}
		skillMD := len(schemaSkill("schemas").Files[0].Data)
		// Each revision holds MaxPackageBytes: the most a package may.
		sizes := func(v int) Definition {
			d := schemaSkill("schemas", MaxPackageFileBytes, MaxPackageBytes-skillMD-MaxPackageFileBytes)
			d.Files[1].Data = generatedSchema(MaxPackageFileBytes, fmt.Sprintf("revision %d", v))
			return d
		}
		first := sizes(1)
		want := sha256.Sum256(first.Files[1].Data)
		fits := MaxSkillBytes / MaxPackageBytes
		for v := 1; v <= fits; v++ {
			if _, err := s.Draft("acme", "ada", scope, sizes(v), v-1, nil, time.Now()); err != nil {
				t.Fatalf("revision %d of %d bytes: %v", v, MaxPackageBytes, err)
			}
		}
		_, err = s.Draft("acme", "ada", scope, sizes(fits+1), fits, nil, time.Now())
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), fmt.Sprintf("more than %d bytes of files together", MaxSkillBytes)) {
			t.Fatalf("revision %d, past %d bytes in all: %v", fits+1, MaxSkillBytes, err)
		}
		reopened, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		history, err := reopened.History("acme", scope, "schemas")
		if err != nil || len(history) != fits {
			t.Fatalf("history = %d revisions, %v", len(history), err)
		}
		pkg, err := history[0].Package()
		if err != nil {
			t.Fatal(err)
		}
		data, _, err := pkg.ReadResource("schemas/part0.xsd", MaxPackageFileBytes)
		if err != nil || len(data) != MaxPackageFileBytes || sha256.Sum256(data) != want {
			t.Fatalf("the large file did not come back whole: %d bytes, %v", len(data), err)
		}
		held := 0
		for _, r := range history {
			held += PackageBytes(r.Files)
		}
		if held > MaxSkillBytes {
			t.Fatalf("the skill holds %d bytes of files", held)
		}
	})
}
