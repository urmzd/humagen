package platform

import (
	"encoding/json"
	"github.com/urmzd/humagen/pkg/projects"
)

// ProjectTemplates returns a copy of the configured, bounded catalog.
func (p *Platform) ProjectTemplates() []projects.Template {
	raw, _ := json.Marshal(p.cfg.ProjectTemplates)
	out := []projects.Template{}
	_ = json.Unmarshal(raw, &out)
	return out
}
