package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/scratchpads"
	"github.com/urmzd/humagen/pkg/threads"
)

// A conversation's scratchpads (pkg/scratchpads) are people's named notes
// beside it. They are kept in the database, not in the session's scratch
// directory, so they outlive its seven-day retention and never race a run's
// copyback. Agents in the conversation read them with file_read and the
// path scratch:<name>, and file_list names them; an agent cannot change one.

// ScratchpadPrefix is how an agent's file tools name a scratchpad.
const ScratchpadPrefix = "scratch:"

// ScratchpadList is a conversation's scratchpads, without their content.
type ScratchpadList struct {
	Scratchpads []scratchpads.Pad `json:"scratchpads"`
	Writable    bool              `json:"writable"`
	Reason      string            `json:"reason,omitempty"`
	Limit       int               `json:"limit"`
}

// ScratchpadInput creates or changes a scratchpad. Version is the one read,
// required for a change.
type ScratchpadInput struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	Version int64  `json:"version,omitempty"`
}

func scratchpadErr(err error) error {
	switch {
	case errors.Is(err, scratchpads.ErrNotFound):
		return threads.ErrNotFound
	case errors.Is(err, scratchpads.ErrConflict):
		return fmt.Errorf("%w: %v", ErrWorkspaceConflict, err)
	case errors.Is(err, scratchpads.ErrInvalid):
		return fmt.Errorf("%w: %v", ErrSandbox, err)
	}
	return err
}

// scratchpadMayWrite is what a person needs to change a conversation's
// scratchpads: to write runs and to post in it, while it is open. Unlike its
// files, a run in flight does not hold them.
func (p *Platform) scratchpadMayWrite(who auth.Principal, th threads.Thread) error {
	if !who.Can(auth.RunsWrite) {
		return auth.ErrForbidden
	}
	if th.Archive != nil || th.ReadOnly {
		return fmt.Errorf("%w: this conversation is archived or read only", ErrForbidden)
	}
	return p.MayPost(who, Place{Project: th.Project, Thread: th.ID})
}

// ConversationScratchpads lists a conversation's scratchpads.
func (p *Platform) ConversationScratchpads(ctx context.Context, who auth.Principal, id string) (ScratchpadList, error) {
	if !who.Can(auth.RunsRead) {
		return ScratchpadList{}, auth.ErrForbidden
	}
	th, err := p.workspaceThread(who, id)
	if err != nil {
		return ScratchpadList{}, err
	}
	pads, err := p.Scratchpads.List(ctx, who.Tenant, th.ID)
	if err != nil {
		return ScratchpadList{}, scratchpadErr(err)
	}
	out := ScratchpadList{Scratchpads: pads, Writable: true, Limit: scratchpads.MaxPerThread}
	if err := p.scratchpadMayWrite(who, th); err != nil {
		out.Writable, out.Reason = false, err.Error()
	}
	return out, nil
}

// ConversationScratchpad reads one scratchpad with its content.
func (p *Platform) ConversationScratchpad(ctx context.Context, who auth.Principal, id, pad string) (scratchpads.Pad, error) {
	if !who.Can(auth.RunsRead) {
		return scratchpads.Pad{}, auth.ErrForbidden
	}
	th, err := p.workspaceThread(who, id)
	if err != nil {
		return scratchpads.Pad{}, err
	}
	out, err := p.Scratchpads.Get(ctx, who.Tenant, th.ID, pad)
	return out, scratchpadErr(err)
}

// CreateScratchpad adds a scratchpad to a conversation.
func (p *Platform) CreateScratchpad(ctx context.Context, who auth.Principal, id string, in ScratchpadInput) (scratchpads.Pad, error) {
	th, err := p.workspaceThread(who, id)
	if err != nil {
		return scratchpads.Pad{}, err
	}
	if err := p.scratchpadMayWrite(who, th); err != nil {
		return scratchpads.Pad{}, err
	}
	out, err := p.Scratchpads.Create(ctx, who.Tenant, th.ID, in.Name, in.Content, who.Subject, p.now())
	return out, scratchpadErr(err)
}

// UpdateScratchpad renames a scratchpad or replaces its content, from the
// version read.
func (p *Platform) UpdateScratchpad(ctx context.Context, who auth.Principal, id, pad string, in ScratchpadInput) (scratchpads.Pad, error) {
	th, err := p.workspaceThread(who, id)
	if err != nil {
		return scratchpads.Pad{}, err
	}
	if err := p.scratchpadMayWrite(who, th); err != nil {
		return scratchpads.Pad{}, err
	}
	if in.Version < 1 {
		return scratchpads.Pad{}, fmt.Errorf("%w: the version read is required", ErrWorkspaceConflict)
	}
	out, err := p.Scratchpads.Update(ctx, who.Tenant, th.ID, pad, in.Name, in.Content, in.Version, who.Subject, p.now())
	return out, scratchpadErr(err)
}

// DeleteScratchpad removes a scratchpad at the version read.
func (p *Platform) DeleteScratchpad(ctx context.Context, who auth.Principal, id, pad string, version int64) error {
	th, err := p.workspaceThread(who, id)
	if err != nil {
		return err
	}
	if err := p.scratchpadMayWrite(who, th); err != nil {
		return err
	}
	if version < 1 {
		return fmt.Errorf("%w: the version read is required", ErrWorkspaceConflict)
	}
	return scratchpadErr(p.Scratchpads.Delete(ctx, who.Tenant, th.ID, pad, version))
}

// readScratchpad is file_read's scratch:<name>: a scratchpad of the run's
// own conversation, by name.
func (p *Platform) readScratchpad(ctx context.Context, tenant, thread, path string) (string, error) {
	name := strings.TrimPrefix(path, ScratchpadPrefix)
	pad, err := p.Scratchpads.ByName(ctx, tenant, thread, name)
	if err != nil {
		return "", fmt.Errorf("%w: this conversation has no scratchpad named %q", ErrSandbox, name)
	}
	return pad.Content, nil
}

// scratchpadLines names a conversation's scratchpads for file_list.
func (p *Platform) scratchpadLines(ctx context.Context, tenant, thread string) string {
	pads, err := p.Scratchpads.List(ctx, tenant, thread)
	if err != nil || len(pads) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nScratchpads people keep in this conversation (read only; read one with file_read and path scratch:<name>):\n")
	for _, pad := range pads {
		fmt.Fprintf(&b, "scratch:%s (%d bytes)\n", pad.Name, pad.Size)
	}
	return b.String()
}
