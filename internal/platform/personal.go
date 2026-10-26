package platform

import (
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
)

// PersonalProject provisions a private workspace only for authenticated humans.
func (p *Platform) PersonalProject(who auth.Principal) (projects.Project, error) {
	if who.Kind != auth.User || who.Subject == "" || who.Tenant == "" {
		return projects.Project{}, projects.ErrForbidden
	}
	return p.Projects.EnsurePersonal(who.Tenant, who.Subject, p.now())
}
