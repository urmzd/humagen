package github

import (
	"context"
	"fmt"
	"sort"
	"strings"

	transport "github.com/urmzd/humagen/fiber/pkg/connectors/github"
	"github.com/urmzd/humagen/fiber/pkg/event"
	presentation "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/internal/wire"
)

func (c *Connector) guideLogin(agent string) string {
	var names []string
	for login, name := range c.cfg.Bots {
		if name == agent {
			names = append(names, strings.TrimSuffix(login, "[bot]"))
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func (c *Connector) prepareGuide(ctx context.Context, tag *event.Envelope) error {
	c.guideMu.Lock()
	defer c.guideMu.Unlock()
	agent := wire.AgentOf(*tag)
	login := c.guideLogin(agent)
	if login == "" {
		return fmt.Errorf("github: no guide bot login for %s", agent)
	}
	source, err := sourceOf(*tag)
	if err != nil {
		return err
	}
	ref, err := c.Reference(source)
	if err != nil {
		return err
	}
	evidence, err := c.cfg.API.(ContextAPI).ReadContext(ctx, ref, 1)
	if err != nil {
		return err
	}
	head := evidence.Subject.Head.SHA
	if !transport.ValidSHA(head) {
		return fmt.Errorf("github: guide needs a PR head SHA")
	}
	if pinned := source.Params["head_sha"]; pinned != "" && pinned != head {
		return fmt.Errorf("github: stale PR guide trigger")
	}
	setSource(tag, source.WithParam("head_sha", head))
	wire.SetGitHubHeadSHA(tag, head)
	return c.cfg.API.(GuideAPI).UpdateGuide(ctx, ref, Guide{Agent: agent, BotLogin: login, HeadSHA: head, Body: presentation.GuideBody(evidence, agent, login, c.cfg.Mutations)})
}
