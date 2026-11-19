package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/skillfiles"
	saigeskills "github.com/urmzd/saige/agent/skills"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/turn/native"
)

// maxPinnedSkills is how many skills one run's pin is read for.
const maxPinnedSkills = 64

// maxSkillFileRead is the largest supporting file file_read answers with.
// What file_read returns enters the model's context, so this is the bound
// supporting files had before the library took larger ones
// (library.MaxPackageFileBytes). A larger file is listed with its size and
// placed whole on the host for a coding agent, which reads it itself; a
// native model is told it is over this bound and file_read refuses it.
const maxSkillFileRead = 128 << 10

// readPinnedSkillFile reads only a package version pinned by this run's
// admission. The ordinary file_read grant and action policy still apply.
func (p *Platform) readPinnedSkillFile(req runtime.Request, requested string) (string, error) {
	name, path, ok := strings.Cut(strings.TrimPrefix(requested, "skill:"), "/")
	if !ok || req.RunID == "" {
		return "", fmt.Errorf("%w: no pinned skill file", ErrSandbox)
	}
	run, err := p.Runs.Get(req.Tenant, req.RunID)
	if err != nil {
		return "", fmt.Errorf("%w: no pinned skill file", ErrSandbox)
	}
	var pinned []pinnedSkill
	raw := wire.Meta(run.Tag())[runs.MetaSkills]
	if len(raw) > 2<<20 || json.Unmarshal([]byte(raw), &pinned) != nil || len(pinned) > maxPinnedSkills {
		return "", fmt.Errorf("%w: invalid pinned skills", ErrSandbox)
	}
	for _, skill := range pinned {
		if skill.Name != name || skill.PackageID == "" || skill.Version < 1 {
			continue
		}
		pkg, err := p.pinnedPackage(req.Tenant, skill)
		if err != nil {
			return "", err
		}
		data, _, err := pkg.ReadResource(path, maxSkillFileRead)
		if errors.Is(err, saigeskills.ErrResourceTooLarge) {
			return "", fmt.Errorf("%w: that skill file is over the %d bytes file_read returns", ErrSandbox, maxSkillFileRead)
		}
		if err != nil || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
			return "", fmt.Errorf("%w: that skill file is not readable text", ErrSandbox)
		}
		return string(data), nil
	}
	return "", fmt.Errorf("%w: skill is not pinned to this run", ErrSandbox)
}

// pinnedPackage is the package a run was pinned to: the revision the pin
// names, in the run's own tenant, and only while its snapshot still has the
// hash the run was admitted with. A later draft is another version, so it
// is never what this reads.
func (p *Platform) pinnedPackage(tenant string, skill pinnedSkill) (saigeskills.Skill, error) {
	latest, err := p.SkillLibrary.Find(tenant, skill.PackageID)
	if err != nil {
		return saigeskills.Skill{}, fmt.Errorf("%w: no pinned skill file", ErrSandbox)
	}
	revision, err := p.SkillLibrary.Get(tenant, latest.Scope, latest.Name, skill.Version)
	if err != nil || revision.Name != skill.Name {
		return saigeskills.Skill{}, fmt.Errorf("%w: no pinned skill file", ErrSandbox)
	}
	pkg, err := revision.Package()
	if err != nil || pkg.Hash != skill.PackageHash {
		return saigeskills.Skill{}, fmt.Errorf("%w: skill package does not match this run", ErrSandbox)
	}
	return pkg, nil
}

// A model whose program reads this host's files itself (a coding agent,
// skillfiles.HostReader) is offered no platform tool, so file_read cannot
// give it a pinned skill's supporting files. For a turn on one, the files
// of the revisions the run is pinned to are written to a directory of the
// turn's own (pkg/skillfiles), the turn's text names each by its path there,
// and the directory is removed when the turn returns, however it ends. The
// directory is placed for the run and tenant the runtime gave this turn,
// and the provider is granted it by those alone (skillfiles.Granted): the
// text names the paths, and names grant nothing.
//
// Only what the run was admitted with is written: the pinned packages, read
// in the run's tenant and checked against the pinned hash (pinnedPackage),
// which is what file_read reads for a native model. A file that cannot be
// written refuses the turn, so a program is never told a path that is not
// there.

// hostFiles is how one turn's models read its skills' supporting files.
type hostFiles struct {
	// dir is the directory they were placed in, or "" when no model of the
	// turn reads this host's files.
	dir string
	// tool reports that a model of the turn reads them with file_read.
	tool bool
}

// skillReaders reports how the models a turn may run on read supporting
// files: host when any of them, the one it starts on or one it may fall back
// to, reads this host's files itself, and tool when any of them is offered
// the platform's tools instead. A turn is composed once for every model of
// its chain, so each kind is told its own way when the chain holds both.
func (p *Platform) skillReaders(req runtime.Request) (host, tool bool) {
	spec := req.Definition.Spec
	reads := func(ref native.ModelRef) bool {
		// Only a kind a program adds can be such a model. A built-in kind
		// and a workspace's own provider are not built to find out, so
		// nothing more is read or made for them than before.
		build := p.cfg.Ext.Providers[ref.Provider]
		if build == nil || p.cfg.DeclaredKinds[ref.Provider] != "" {
			return false
		}
		s := spec
		s.Provider, s.Model = ref.Provider, ref.Model
		model, err := build(s)
		if err != nil {
			return false
		}
		reader, ok := model.(skillfiles.HostReader)
		return ok && reader.ReadsHostFiles()
	}
	for _, ref := range append([]native.ModelRef{{Provider: spec.Provider, Model: spec.Model}}, spec.Fallbacks...) {
		if reads(ref) {
			host = true
		} else {
			tool = true
		}
	}
	return host, tool
}

// hostSkillFiles places the supporting files of a turn's pinned skills for
// a model that reads this host's files, and reports where. It is withSkills'
// host: called only for a turn pinned with a skill that has such files.
func (p *Platform) hostSkillFiles(req runtime.Request, skills []pinnedSkill) (hostFiles, error) {
	host, tool := p.skillReaders(req)
	if !host {
		return hostFiles{tool: true}, nil
	}
	if req.RunID == "" || len(skills) > maxPinnedSkills {
		return hostFiles{}, fmt.Errorf("%w: this turn's skill files cannot be placed", ErrSandbox)
	}
	var files []skillfiles.File
	packages := 0
	for _, skill := range skills {
		if len(skill.Resources) == 0 {
			continue
		}
		if skill.PackageID == "" || skill.Version < 1 || !projects.ValidSkill(skill.Name) {
			return hostFiles{}, fmt.Errorf("%w: skill %q is not a pinned package", ErrSandbox, skill.Name)
		}
		pkg, err := p.pinnedPackage(req.Tenant, skill)
		if err != nil {
			return hostFiles{}, err
		}
		packages++
		for _, resource := range skill.Resources {
			clean, err := saigeskills.CleanResourcePath(resource.Path)
			if err != nil || clean != resource.Path {
				return hostFiles{}, fmt.Errorf("%w: skill %q names a file outside its package", ErrSandbox, skill.Name)
			}
			data, _, err := pkg.ReadResource(clean, library.MaxPackageFileBytes)
			if err != nil {
				return hostFiles{}, fmt.Errorf("%w: a file of skill %q is not in the version this run is pinned to", ErrSandbox, skill.Name)
			}
			files = append(files, skillfiles.File{Path: path.Join(skill.Name, clean), Data: data})
		}
	}
	dir, err := skillfiles.Place(req.Tenant, req.RunID, files, skillfiles.Limits{Files: packages * library.MaxPackageFiles, Bytes: packages * library.MaxPackageBytes})
	if err != nil {
		return hostFiles{}, fmt.Errorf("%w: %v", ErrSandbox, err)
	}
	return hostFiles{dir: dir, tool: tool}, nil
}

// releaseSkillFiles removes the directory a turn's skill files were placed
// in. It runs when the turn returns: answered, failed, stopped or parked.
func (p *Platform) releaseSkillFiles(req runtime.Request) {
	if err := skillfiles.Release(req.Tenant, req.RunID); err != nil {
		p.log.Warn("platform: a turn's skill files were not removed", "run", req.RunID, "error", err)
	}
}

// releaseTurn gives back what resolveRevision took for one turn. It runs
// when the outer executor returns, a failure and a stop included.
func (p *Platform) releaseTurn(req runtime.Request) {
	p.releaseSourceGrounding(req)
	p.releaseSkillFiles(req)
}
