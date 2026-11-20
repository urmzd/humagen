package platform

import (
	"context"
	"errors"
	"strings"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

func (p *Platform) classifyFollowup(ctx context.Context, who auth.Principal, th threads.Thread, text string) (*threads.Followup, error) {
	rows, err := p.Runs.Conversation(who.Tenant, th.ID)
	if errors.Is(err, runs.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var waiting []string
	var current strings.Builder
	for _, r := range rows {
		if r.Parent != "" || r.Status.Terminal() {
			continue
		}
		// A person cannot interrupt work they cannot stop.
		if err := p.MayStop(who, r); err != nil {
			return nil, err
		}
		waiting = append(waiting, r.ID)
		if current.Len() < 8000 {
			current.WriteString(r.Text)
			current.WriteString("\n")
		}
	}
	if len(waiting) == 0 {
		return nil, nil
	}
	a, _ := p.asker(who.Tenant).(join.FollowupAsker)
	d := join.Classify(ctx, a, current.String(), text)
	return &threads.Followup{Outcome: d.Outcome, Reason: d.Reason, Waiting: waiting}, nil
}

func (p *Platform) stopForFollowup(ctx context.Context, who auth.Principal, m threads.Message) error {
	if m.Followup.Outcome == "queue" {
		return nil
	}
	for _, id := range m.Followup.Waiting {
		r, err := p.Runs.Get(who.Tenant, id)
		if err != nil {
			return err
		}
		if err := p.MayStop(who, r); err != nil {
			return err
		}
		if err := p.requestStop(ctx, who.Tenant, id, m.Followup.Outcome+": "+m.Followup.Reason, who.Subject); err != nil {
			return err
		}
	}
	return nil
}

// drainFollowups is also a durable completion step, retried after a worker restart.
// Message-derived run IDs make repeated dispatch safe across workers.
func (p *Platform) drainFollowups(ctx context.Context, tenant, thread string) error {
	if thread == "" {
		return nil
	}
	th, err := p.Threads.Get(tenant, thread)
	if errors.Is(err, threads.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	rows, err := p.Runs.Conversation(tenant, thread)
	if err != nil && !errors.Is(err, runs.ErrNotFound) {
		return err
	}
	for _, r := range rows {
		if r.Parent == "" && !r.Status.Terminal() {
			return nil
		}
	}
	cursor := ""
	for {
		msgs, next, err := p.Threads.Messages(tenant, thread, cursor, 100)
		if err != nil {
			return err
		}
		for _, m := range msgs {
			if m.Followup == nil || m.Followup.Handled {
				continue
			}
			if m.Followup.Outcome == "stop" {
				if err := p.Threads.HandleFollowup(tenant, thread, m.ID); err != nil {
					return err
				}
				continue
			}
			// Reconstruct current permissions, never retain a posting-time privilege.
			person, err := p.People.Get(tenant, m.By)
			if err != nil {
				return err
			}
			who := auth.Principal{Tenant: tenant, Subject: m.By, Name: m.Name, Kind: auth.User}.WithPolicy(p.cfg.Policy)
			for _, role := range person.Roles {
				who.Roles = append(who.Roles, auth.Role(role))
			}
			keeps, err := p.mayPost(who, Place{Project: th.Project, Thread: thread}, projects.Member)
			if err != nil {
				return err
			}
			target := th
			if len(m.Mentions) == 0 && len(target.Agents) == 0 {
				for _, id := range m.Followup.Waiting {
					prev, err := p.Runs.Get(tenant, id)
					if err != nil {
						return err
					}
					target.Agents = append(target.Agents, prev.Agent)
				}
			}
			choice, err := checkMessageChoice(m.Followup.Model, m.Followup.Effort)
			if err != nil {
				return err
			}
			out, err := p.carryOut(withMessageChoice(ctx, choice), who, tenant, p.ProjectTree(tenant), target, m, clientOfMessage(m), false, false, keeps, p.Seer(who).Agent)
			if err != nil {
				return err
			}
			if err := p.Threads.HandleFollowup(tenant, thread, m.ID); err != nil {
				return err
			}
			if len(out.Runs) > 0 {
				return nil
			}
		}
		if next == "" {
			return nil
		}
		cursor = next
	}
}

func clientOfMessage(m threads.Message) string {
	if m.Via == "console" {
		return ""
	}
	return m.Via
}
