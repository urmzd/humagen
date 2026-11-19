package library

import (
	"archive/zip"
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/saige/agent/skills"
)

// The bounds of one package and of one skill. A supporting file is read on
// demand: a native model reads one of at most 128 KiB with file_read, and a
// coding agent reads it on the host. Only SKILL.md's body is put into
// what a model is asked, so its bounds do not grow with the others.
const (
	// MaxPackageFiles is how many files a package holds, SKILL.md included:
	// room for skills that ship a tree of schema files. MaxPackageBytes, not
	// this count, bounds what they hold, and a turn's text lists at most 63
	// of them, as many as the earlier bound of 64 allowed.
	MaxPackageFiles = 256
	// MaxPackageFileBytes is the largest supporting file: room for the
	// schema and source files skills ship.
	MaxPackageFileBytes = 1 << 20
	// MaxPackageSkillFileBytes is the largest SKILL.md, frontmatter
	// included.
	MaxPackageSkillFileBytes = 128 << 10
	// MaxPackageBytes is the most one package's files hold together.
	MaxPackageBytes = 2 << 20
	// MaxPackageInstructions is how many characters SKILL.md's body holds.
	MaxPackageInstructions = 32000
	// MaxSkillBytes is the most the files of every revision of one skill
	// hold together: file bytes, what MaxVersions packages of the earlier
	// 512 KiB bound came to. A skill's revisions are one stored record and
	// one history answer, which also hold each file's path and each
	// revision's text, so they are larger. At worst, with every path 256
	// bytes the JSON encoder escapes to six and every text field at its
	// bound in such characters, over MaxVersions (32) revisions:
	//
	//	files     16 MiB in base64                 22,369,622 B
	//	paths     32 x 256 files x 256 B x 6       12,582,912 B
	//	          (with 64 files a revision         3,145,728 B)
	//	SKILL.md  32 x 32000 characters x 6         6,144,000 B
	//	body      inputs, outputs, tools, the rest  about 2.7 MB
	//
	// The largest record measures 43,783,718 B (41.8 MiB), and was
	// 34,479,661 B (32.9 MiB) with 64 files
	// (TestTheLargestSkillRecordStaysUnderItsStatedBound). A history answer
	// adds a package hash a revision. A record of real files is near its
	// file bytes in base64.
	MaxSkillBytes = 16 << 20
)

// PackageBytes is how many bytes the files of a package hold together.
func PackageBytes(files []File) int {
	total := 0
	for _, file := range files {
		total += len(file.Data)
	}
	return total
}

// File is a package-relative immutable file. JSON encodes Data as base64.
type File struct {
	Path string `json:"path"`
	Data []byte `json:"data"`
}

func cloneFiles(files []File) []File {
	if files == nil {
		return nil
	}
	out := make([]File, len(files))
	for i, file := range files {
		out[i] = File{Path: file.Path, Data: slices.Clone(file.Data)}
	}
	return out
}

// Package composes saige's standard parser, bounds and immutable snapshot.
// The archive is only an in-memory fs.FS adapter; no host paths are opened.
func (d Definition) Package() (skills.Skill, error) {
	if len(d.Files) == 0 || len(d.Files) > MaxPackageFiles {
		return skills.Skill{}, fmt.Errorf("%w: a package needs 1 to %d files", ErrInvalid, MaxPackageFiles)
	}
	var archive bytes.Buffer
	w := zip.NewWriter(&archive)
	seen, total := map[string]bool{}, 0
	for _, file := range d.Files {
		path, err := skills.CleanResourcePath(file.Path)
		if file.Data == nil || err != nil || path != file.Path || len(path) > 256 || seen[path] {
			return skills.Skill{}, fmt.Errorf("%w: invalid or duplicate package path %q", ErrInvalid, file.Path)
		}
		if limit := maxFileBytes(path); len(file.Data) > limit {
			return skills.Skill{}, fmt.Errorf("%w: %s is over %d bytes", ErrInvalid, path, limit)
		}
		if path == skills.SkillFile && bytes.ContainsRune(file.Data, 0) {
			return skills.Skill{}, fmt.Errorf("%w: SKILL.md must be text", ErrInvalid)
		}
		seen[path] = true
		total += len(file.Data)
		if total > MaxPackageBytes {
			return skills.Skill{}, fmt.Errorf("%w: package exceeds %d bytes", ErrInvalid, MaxPackageBytes)
		}
		entry, err := w.CreateHeader(&zip.FileHeader{Name: d.Name + "/" + path, Method: zip.Store})
		if err != nil {
			return skills.Skill{}, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		if _, err := entry.Write(file.Data); err != nil {
			return skills.Skill{}, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
	}
	if err := w.Close(); err != nil {
		return skills.Skill{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		return skills.Skill{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	pkg, err := skills.Snapshot(reader, d.Name, skills.SnapshotOptions{Source: "library", Bounds: skills.Bounds{MaxFiles: MaxPackageFiles, MaxFileBytes: MaxPackageFileBytes, MaxTotalBytes: MaxPackageBytes}})
	if err != nil {
		return skills.Skill{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return pkg, nil
}

// maxFileBytes is the bound of one package file: SKILL.md has its own.
func maxFileBytes(path string) int {
	if path == skills.SkillFile {
		return MaxPackageSkillFileBytes
	}
	return MaxPackageFileBytes
}

// Normalize makes SKILL.md authoritative for metadata and instructions.
// Frontmatter only narrows the explicit platform tool selection.
func (d Definition) Normalize() (Definition, error) {
	if d.Files == nil {
		return d, nil
	}
	pkg, err := d.Package()
	if err != nil {
		return Definition{}, err
	}
	d.Description, d.Instructions = pkg.Description, pkg.Body
	if pkg.AllowedTools != nil {
		d.Tools = slices.DeleteFunc(slices.Clone(d.Tools), func(tool string) bool { return !slices.Contains(pkg.AllowedTools, tool) })
	}
	return d, nil
}

// PackageFiles exposes old instruction-only revisions as a standard package.
// It does not change their saved revision or publication state.
func (d Definition) PackageFiles() []File {
	if d.Files != nil {
		return cloneFiles(d.Files)
	}
	var text strings.Builder
	fmt.Fprintf(&text, "---\nname: %s\ndescription: %s\n", d.Name, strconv.Quote(d.Description))
	if len(d.Tools) > 0 {
		fmt.Fprintf(&text, "allowed-tools: %s\n", strconv.Quote(strings.Join(d.Tools, " ")))
	}
	fmt.Fprintf(&text, "---\n\n%s\n", d.Instructions)
	return []File{{Path: skills.SkillFile, Data: []byte(text.String())}}
}

func (d Definition) validPackage() bool {
	if d.Files == nil {
		return utf8.RuneCountInString(d.Instructions) <= projects.MaxSkillInstructions
	}
	pkg, err := d.Package()
	if err != nil || pkg.Name != d.Name || pkg.Description != d.Description || pkg.Body != d.Instructions || utf8.RuneCountInString(d.Instructions) > MaxPackageInstructions {
		return false
	}
	for _, tool := range d.Tools {
		if pkg.AllowedTools != nil && !slices.Contains(pkg.AllowedTools, tool) {
			return false
		}
	}
	return true
}
