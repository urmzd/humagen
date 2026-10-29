package platform

import (
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/projects"
)

// PostWelcome seeds one retained Company announcement. Its key survives hide,
// read, withdrawal and restart, as every PostOnce announcement does, so a
// welcome the workspace's branding changes later is not posted again. Its
// title and text are the branding's, else Humagen's under its name.
func (p *Platform) PostWelcome(tenant string) (inbox.Message, error) {
	w := p.wording(tenant)
	m, _, err := p.announce(tenant, projects.Root, "welcome:company:v1", inbox.From{Kind: "system", Name: SystemSender}, Announcement{Subject: w.WelcomeTitle, Text: w.WelcomeText, Severity: inbox.Info}, p.now())
	return m, err
}
