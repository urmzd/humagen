package platform

import (
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/projects"
)

// PostWelcome seeds one retained Company announcement. Its key survives hide,
// read, withdrawal and restart, as every PostOnce announcement does.
func (p *Platform) PostWelcome(tenant string) (inbox.Message, error) {
	m, _, err := p.announce(tenant, projects.Root, "welcome:company:v1", inbox.From{Kind: "system", Name: SystemSender}, Announcement{Subject: welcomeTitle, Text: welcomeText, Severity: inbox.Info}, p.now())
	return m, err
}
