package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/skillfiles"
	saigeskills "github.com/urmzd/saige/agent/skills"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/turn/native"
)

// maxPinnedSkills is how many skills one run's pin is read for.
const maxPinnedSkills = 64

// maxSkillFileRead is the largest supporting file skill_file_read (and
// file_read) answers with. What it returns enters the model's context, so
// this is the bound supporting files had before the library took larger ones
// (library.MaxPackageFileBytes). A larger file is listed with its size and
// placed whole on the host for a coding agent, which reads it itself; a
// native model is told it is over this bound and the read is refused.
const maxSkillFileRead = 128 << 10

// ToolSkillFileRead reads a supporting file of a skill the run is pinned
// with. It is not a grant: no agent spec or skill tool list names it. A turn
// is offered it exactly when its text lists skill: paths (withSkills), so
// the text never names a tool the turn does not have.
//
// It reads only the packages the run was admitted with (readPinnedSkillFile):
// no conversation file, no other skill, no path outside a package, at most
// maxSkillFileRead bytes a file and maxSkillFileReads files an attempt of a
// turn. What it can return is part of the skill the person chose for the
// turn, whose instructions are already in the turn's text, so neither a
// skill's tool list, a workspace's tools ceiling nor the action policy holds
// it and no person is asked. Each call is
// recorded on the run's stream as every tool call is, and a paused agent or
// workspace stops it as it stops every tool (operationAdmissionRequest).
const ToolSkillFileRead = wire.SkillFileRead

// maxSkillFileReads is how many supporting files one attempt of a turn
// reads with skill_file_read (a retried attempt counts again): one more than the files a turn's text lists for a skill
// (maxListedSkillFiles), so a model can read every listed file and one the
// instructions name.
const maxSkillFileReads = maxListedSkillFiles + 1

// skillFileTool is skill_file_read for one turn.
type skillFileTool struct {
	p   *Platform
	req runtime.Request

	mu    sync.Mutex
	reads int
}

func (p *Platform) skillFileTool(req runtime.Request) saigetypes.Tool {
	return &skillFileTool{p: p, req: req}
}

func (t *skillFileTool) Definition() saigetypes.ToolDef {
	return saigetypes.ToolDef{
		Name:        ToolSkillFileRead,
		Description: "Read a supporting file of a skill this turn uses, with the exact path the turn lists: skill:<name>/<path>. It reads only the files of those skills, at most 131072 bytes a file. Reading a script does not execute it.",
		Parameters:  schema([]string{"path"}, map[string]string{"path": "skill:<name>/<path>, as the turn lists it"}),
	}
}

func (t *skillFileTool) Execute(_ context.Context, args map[string]any) (string, error) {
	if err := t.p.operationAdmissionRequest(t.req); err != nil {
		return "", err
	}
	requested := arg(args, "path")
	if !strings.HasPrefix(requested, "skill:") {
		return "", fmt.Errorf("%w: skill_file_read reads only the supporting files of the skills this turn uses, as skill:<name>/<path>", ErrSandbox)
	}
	t.mu.Lock()
	if t.reads >= maxSkillFileReads {
		t.mu.Unlock()
		return "", fmt.Errorf("%w: this turn has read %d skill files, as many as one turn may", ErrSandbox, maxSkillFileReads)
	}
	t.reads++
	t.mu.Unlock()
	return t.p.readPinnedSkillFile(t.req, ToolSkillFileRead, requested)
}

// readPinnedSkillFile reads only a package version pinned by this run's
// admission. Through file_read, the ordinary file_read grant and action
// policy still apply; through skill_file_read, they do not (ToolSkillFileRead).
func (p *Platform) readPinnedSkillFile(req runtime.Request, tool, requested string) (string, error) {
	name, path, ok := strings.Cut(strings.TrimPrefix(requested, "skill:"), "/")
	if !ok || req.RunID == "" {
		return "", fmt.Errorf("%w: no pinned skill file", ErrSandbox)
	}
	// A turn may read before its run's record is written (runRecordOf).
	run, err := p.runRecordOf(req.Tenant, req.RunID)
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
		switch {
		case errors.Is(err, saigeskills.ErrResourceTooLarge):
			return "", fmt.Errorf("%w: that skill file is over the %d bytes %s returns", ErrSandbox, maxSkillFileRead, tool)
		case errors.Is(err, saigeskills.ErrOutsideSkillRoot):
			return "", fmt.Errorf("%w: %q is outside the package of skill %s", ErrSandbox, path, name)
		case errors.Is(err, saigeskills.ErrResourceNotFound):
			return "", fmt.Errorf("%w: skill %s has no file %q", ErrSandbox, name, path)
		}
		if err != nil || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
			return "", fmt.Errorf("%w: that skill file is not readable text", ErrSandbox)
		}
		return string(data), nil
	}
	return "", fmt.Errorf("%w: skill %s is not one this turn uses", ErrSandbox, name)
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
