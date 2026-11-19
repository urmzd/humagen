package library

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
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
	if MaxPackageFiles != 256 || MaxPackageFileBytes != 1<<20 || MaxPackageSkillFileBytes != 128<<10 || MaxPackageBytes != 2<<20 || MaxSkillBytes != 16<<20 {
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

// A package may hold MaxPackageFiles files, SKILL.md included, and one more
// is refused with the bound named.
func TestPackageFileCountBound(t *testing.T) {
	many := func(n int) Definition {
		d := schemaSkill("schemas")
		for i := range n - 1 {
			d.Files = append(d.Files, File{Path: fmt.Sprintf("schemas/part%03d.xsd", i), Data: generatedSchema(256, fmt.Sprint(i))})
		}
		return d
	}
	if d, err := many(MaxPackageFiles).Normalize(); err != nil || len(d.Files) != MaxPackageFiles {
		t.Fatalf("a package of exactly %d files: %v", MaxPackageFiles, err)
	}
	if _, err := many(MaxPackageFiles + 1).Normalize(); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), fmt.Sprintf("a package needs 1 to %d files", MaxPackageFiles)) {
		t.Fatalf("a package of %d files: %v", MaxPackageFiles+1, err)
	}
}

// worstRecord is the largest stored record of one skill the bounds allow:
// maxVersions revisions of files files each, whose paths are 256 bytes the
// JSON encoder escapes to six bytes (&), holding MaxSkillBytes of file bytes
// in all, with every text field at its bound in characters that escape.
// Generated data; it is measured, never validated.
func worstRecord(maxVersions, files int) []Revision {
	escaped := func(n int) string { return strings.Repeat("<", n) }
	path := func(i int) string {
		head := fmt.Sprintf("d%03d/", i)
		return head + strings.Repeat("&", 256-len(head)-4) + ".xsd"
	}
	scope := Scope{Kind: "project", Project: escaped(512)}
	var rows []Revision
	for v := 1; v <= maxVersions; v++ {
		d := Definition{
			Skill:   projects.Skill{Name: strings.Repeat("a", 64), Description: escaped(projects.MaxSkillDescription), Instructions: escaped(MaxPackageInstructions), ArgumentHint: escaped(projects.MaxArgumentHint)},
			Inputs:  escaped(4000),
			Outputs: escaped(4000),
		}
		for i := range 32 {
			d.Tools = append(d.Tools, fmt.Sprintf("%03d", i)+escaped(125))
		}
		for i := range files {
			d.Files = append(d.Files, File{Path: path(i), Data: make([]byte, MaxSkillBytes/maxVersions/files)})
		}
		rows = append(rows, Revision{OriginRun: escaped(128), ID: ID(scope, d.Name), Via: ViaImport, Definition: d, Scope: scope, Version: v, By: escaped(256),
			At: time.Now(), PublishedAt: time.Now(), PublishedBy: escaped(256), Tests: make([]string, 10), From: &Provenance{Scope: scope, Name: d.Name, Version: v}})
	}
	return rows
}

// The stored record of one skill, and the history answer that carries it,
// stay under the bound MaxSkillBytes states for them: file bytes are capped
// at 16 MiB, but 256 escaped paths a revision add to the record what 64 did
// not (the arithmetic is at MaxSkillBytes).
func TestTheLargestSkillRecordStaysUnderItsStatedBound(t *testing.T) {
	size := func(rows []Revision) int {
		raw, err := json.Marshal(rows)
		if err != nil {
			t.Fatal(err)
		}
		return len(raw)
	}
	before, after := size(worstRecord(MaxVersions, 64)), size(worstRecord(MaxVersions, MaxPackageFiles))
	t.Logf("largest record: %d bytes with 64 files a revision, %d with %d", before, after, MaxPackageFiles)
	if after > 44<<20 {
		t.Fatalf("the largest record is %d bytes, over the 44 MiB stated at MaxSkillBytes", after)
	}
}

// A skill that holds MaxVersions revisions takes no more, and the refusal
// names the bound and the way on, as the 16 MiB one does.
func TestAFullSkillsRefusalNamesItsBoundAndTheWayOn(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		s, err := Open(open(t))
		if err != nil {
			t.Fatal(err)
		}
		scope := Scope{Kind: "personal", Subject: "ada"}
		for v := 1; v <= MaxVersions; v++ {
			if _, err := s.Draft("acme", "ada", scope, schemaSkill("capped", 200+v), v-1, nil, time.Now()); err != nil {
				t.Fatal(v, err)
			}
		}
		_, err = s.Draft("acme", "ada", scope, schemaSkill("capped", 999), MaxVersions, nil, time.Now())
		want := fmt.Sprintf("capped holds as many revisions as a skill may (%d); %s", MaxVersions, fullSkill)
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), want) {
			t.Fatalf("revision %d = %v, want %q", MaxVersions+1, err, want)
		}
	})
}
