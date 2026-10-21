package clients

import (
	"fmt"
	"regexp"

	"github.com/urmzd/warp/pkg/projects"
)

// HomeChannel is the channel a client's threads are filed in when its home
// names none: every project has it.
const HomeChannel = "general"

var channelSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// Home is where a client files the threads it starts outside a channel bound
// to one: a direct message, the assistant pane, an unbound channel, a GitHub
// issue. A client with none files them in the root's general channel.
type Home struct {
	// Project is a project's name, or "~" for the root.
	Project string `json:"project"`
	// Channel is a channel of that project. Empty is general.
	Channel string `json:"channel,omitempty"`
}

func (h Home) validate() error {
	if h.Project != projects.RootAddress && !projects.ValidName(h.Project) {
		return fmt.Errorf("%w: a client's home is a project's name, or ~ for the workspace", ErrInvalid)
	}
	if h.Channel != "" && !channelSlug.MatchString(h.Channel) {
		return fmt.Errorf("%w: a channel is 1 to 40 lowercase letters, digits and dashes", ErrInvalid)
	}
	return nil
}

// HomeOf is where a client files the threads it starts outside a bound
// channel: its home, with the root as "" and general filled in.
func (c Client) HomeOf() Home {
	if c.Home == nil {
		return Home{Project: projects.Root, Channel: HomeChannel}
	}
	h := Home{Project: projects.NameOf(c.Home.Project), Channel: c.Home.Channel}
	if h.Channel == "" {
		h.Channel = HomeChannel
	}
	return h
}
