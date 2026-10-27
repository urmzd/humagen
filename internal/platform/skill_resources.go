package platform

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/runs"
)

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
	raw := run.Tag().Meta[runs.MetaSkills]
	if len(raw) > 2<<20 || json.Unmarshal([]byte(raw), &pinned) != nil || len(pinned) > 64 {
		return "", fmt.Errorf("%w: invalid pinned skills", ErrSandbox)
	}
	for _, skill := range pinned {
		if skill.Name != name || skill.PackageID == "" || skill.Version < 1 {
			continue
		}
		latest, err := p.SkillLibrary.Find(req.Tenant, skill.PackageID)
		if err != nil {
			return "", fmt.Errorf("%w: no pinned skill file", ErrSandbox)
		}
		revision, err := p.SkillLibrary.Get(req.Tenant, latest.Scope, latest.Name, skill.Version)
		if err != nil || revision.Name != name {
			return "", fmt.Errorf("%w: no pinned skill file", ErrSandbox)
		}
		pkg, err := revision.Package()
		if err != nil || pkg.Hash != skill.PackageHash {
			return "", fmt.Errorf("%w: skill package does not match this run", ErrSandbox)
		}
		data, _, err := pkg.ReadResource(path, library.MaxPackageFileBytes)
		if err != nil || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
			return "", fmt.Errorf("%w: that skill file is not readable text", ErrSandbox)
		}
		return string(data), nil
	}
	return "", fmt.Errorf("%w: skill is not pinned to this run", ErrSandbox)
}
