package platform

import (
	"context"
	"fmt"
	"strings"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/threads"
)

// A command a person types in a session's composer as "!" and the command
// (MessageInput.Command). It runs at once, as the person, in the session's
// sandbox, through RunPersonCommand and under its rules: only where the
// deployment's sandbox is contained, only for someone who may post in the
// session, only while no work is under way there, and within its time and
// output limits. Their own command needs no approval.
//
// What it printed is posted in the session as the person's message, with
// the command record on it (threads.Command). That message starts no run.
// The agent that answers the next message reads it there: in the room's
// transcript, or ahead of the message's words for the session's own agent
// (commandsFor). The message is the record: it is kept with the session's
// other messages, so it is read again on reload, and the run that answers
// the next message carries it in what it was asked.

// commandOf is the command a "!" message runs: the text after the "!".
func commandOf(text string) (string, error) {
	rest, ok := strings.CutPrefix(strings.TrimLeft(text, " \t"), "!")
	command := strings.TrimSpace(rest)
	switch {
	case !ok:
		return "", fmt.Errorf("%w: a command's message starts with !", threads.ErrInvalid)
	case command == "":
		return "", fmt.Errorf("%w: type a command after !", threads.ErrInvalid)
	case len(command) > MaxPersonCommand:
		return "", fmt.Errorf("%w: a command is at most %d bytes", threads.ErrInvalid, MaxPersonCommand)
	}
	return command, nil
}

// checkCommandInput refuses what a command's message cannot carry: files,
// an answer, a client acting for someone, or no session to run in.
func (p *Platform) checkCommandInput(in MessageInput) (string, error) {
	switch {
	case len(in.Uploads) > 0:
		return "", fmt.Errorf("%w: a command's message carries no files", threads.ErrInvalid)
	case in.Answers != nil || in.ActAs != nil || in.Surface != nil || in.reply || in.heard:
		return "", fmt.Errorf("%w: a command runs only from the console, as yourself", threads.ErrInvalid)
	case !p.PersonCommands():
		return "", fmt.Errorf("%w: a person's command runs only in an isolated sandbox", ErrUnisolated)
	}
	return commandOf(in.Text)
}

// postCommand runs a "!" message's command in the session's sandbox and
// posts it, with how it came out, as the person's message. It starts no run.
func (p *Platform) postCommand(ctx context.Context, who auth.Principal, id string, in MessageInput) (Posted, error) {
	command, err := p.checkCommandInput(in)
	if err != nil {
		return Posted{}, err
	}
	th, err := p.workspaceThread(who, id)
	if err != nil {
		return Posted{}, err
	}
	if th.Session == nil {
		return Posted{}, fmt.Errorf("%w: a command runs only in a session", threads.ErrInvalid)
	}
	text := "!" + command
	if err := threads.CheckText(text); err != nil {
		return Posted{}, err
	}
	by, name, via, _, err := p.author(who, nil)
	if err != nil {
		return Posted{}, err
	}
	if err := p.MayPost(who, Place{Project: th.Project, Thread: th.ID}); err != nil {
		return Posted{}, err
	}
	prev, err := p.prevOf(who.Tenant, th, in.After)
	if err != nil {
		return Posted{}, err
	}
	if err := p.posts.take(who.Tenant, by, th.Project, p.now()); err != nil {
		return Posted{}, err
	}
	// Who may run it, where and when: RunPersonCommand's rules, as the
	// session terminal follows them.
	res, err := p.RunPersonCommand(ctx, who, id, PersonCommand{Command: command})
	if err != nil {
		return Posted{}, err
	}
	output, cut := commandOutput(res.Output)
	msg := threads.Message{
		By: by, Name: name, Text: text, Mentions: []threads.Mention{}, Via: via, Prev: prev,
		Command: &threads.Command{
			Command: command, ExitCode: res.ExitCode, TimedOut: res.TimedOut,
			Output: output, Truncated: res.Truncated || cut, DurationMS: max(res.DurationMS, 0),
		},
	}
	if th, err = p.recordOf(who.Tenant, id, p.claimant(who, th.Project)); err != nil {
		return Posted{}, err
	}
	m, _, err := p.Threads.Post(who.Tenant, th.ID, msg, p.now())
	if err != nil {
		return Posted{}, err
	}
	if th, err = p.Threads.Get(who.Tenant, th.ID); err != nil {
		return Posted{}, err
	}
	out := p.posted(who.Tenant, th, m)
	out.Rule = threads.RulePostOnly
	return out, nil
}

// commandOutput is a command's output as its message keeps it: line endings
// as \n, no control characters but tabs and line breaks, and at most
// threads.MaxCommandOutput bytes, its end. cut says it was shortened here.
func commandOutput(s string) (string, bool) {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, "�"))
	if len(s) <= threads.MaxCommandOutput {
		return s, false
	}
	s = s[len(s)-threads.MaxCommandOutput:]
	for len(s) > 0 && s[0]&0xC0 == 0x80 {
		s = s[1:]
	}
	return s, true
}

// commandsFor is what an agent sent only a message's words is sent: the
// commands people ran in the session just before it, then the words. A
// message with no command before it is its words alone.
func (p *Platform) commandsFor(tenant string, th threads.Thread, m threads.Message) string {
	if th.Session == nil || !strings.HasPrefix(m.Prev, "m-") {
		return m.Text
	}
	path, err := p.Threads.Path(tenant, th.ID, m.ID)
	if err != nil {
		return m.Text
	}
	return threads.Commands(threads.CommandsBefore(path), p.shower(tenant), m.Text)
}
