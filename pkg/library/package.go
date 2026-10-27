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

const (
	MaxPackageFiles        = 64
	MaxPackageFileBytes    = 128 << 10
	MaxPackageBytes        = 512 << 10
	MaxPackageInstructions = 32000
)

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
		if file.Data == nil || err != nil || path != file.Path || len(path) > 256 || seen[path] || len(file.Data) > MaxPackageFileBytes {
			return skills.Skill{}, fmt.Errorf("%w: invalid or duplicate package path %q", ErrInvalid, file.Path)
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
