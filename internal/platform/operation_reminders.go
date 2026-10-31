package platform

import (
	"context"
	"strconv"
	"time"

	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/operations"
)

func reminderSetting(value *int) string {
	if value == nil {
		return "inherit"
	}
	return strconv.Itoa(*value)
}

func (p *Platform) operationReminderDelay(tenant, agent string, blocked bool) time.Duration {
	value := 4 * 60 * 60
	if blocked {
		value = 2 * 60 * 60
	}
	for _, name := range []string{"", agent} {
		control, err := p.AgentControl(tenant, name)
		if err != nil {
			// The defaults stand in until the control can be read.
			p.log.Error("platform: agent control could not be read", "error", err)
		}
		configured := control.ReminderAfterSeconds
		if blocked {
			configured = control.BlockedReminderAfterSeconds
		}
		if configured != nil {
			value = *configured
		}
	}
	return time.Duration(value) * time.Second
}

// reconcileOperationReminders uses persisted inbox causes as its durable
// timers. It neither invents a question nor approves or restarts any work.
func (p *Platform) reconcileOperationReminders(ctx context.Context) {
	at := p.now()
	tenants, err := p.Inbox.Tenants()
	if err != nil {
		p.log.Warn("platform: operation reminders could not list workspaces; the next pass tries again", "error", err)
	}
	for _, tenant := range tenants {
		after := ""
		if cursor, ok := p.operationReminderCursor.Load(tenant); ok {
			after = cursor.(string)
		}
		messages, next, err := p.Inbox.OpenPage(tenant, after, operations.MaxResults)
		if err != nil {
			p.log.Warn("platform: operation reminders could not read the inbox; the next pass tries again", "tenant", tenant, "error", err)
			continue
		}
		p.operationReminderCursor.Store(tenant, next)
		for _, message := range messages {
			for _, part := range message.Parts {
				if part.State != inbox.PartOpen {
					continue
				}
				var agent string
				base := message.At
				blocked := false
				switch {
				case part.Question != nil:
					q := part.Question
					pending, ok, err := p.rt.PendingQuestion(ctx, tenant, q.Run)
					if err != nil || !ok || pending.ID != q.ID || (!pending.ExpiresAt.IsZero() && !at.Before(pending.ExpiresAt)) {
						continue
					}
					agent = q.Agent
					if agent == "" {
						agent = message.Source.Agent
					}
					if !pending.At.IsZero() {
						base = pending.At
					} else if !q.AskedAt.IsZero() {
						base = q.AskedAt
					}
				case part.Operation != nil:
					blocked = true
					op := part.Operation
					version := 0
					if op.Kind == "assignment" {
						a, ok, err := p.Operations.Assignment(tenant, op.ID)
						if err != nil || !ok || (a.State != operations.Blocked && a.State != operations.NeedsPerson) {
							continue
						}
						version = a.Version
						agent = a.Agent
					} else if op.Kind == "handoff" {
						h, ok, err := p.Operations.Handoff(tenant, op.ID)
						if err != nil || !ok || (h.State != operations.Blocked && h.State != operations.NeedsPerson) {
							continue
						}
						version = h.Version
						agent = h.From
					} else {
						continue
					}
					if message.Key != "operation:"+op.Kind+":"+op.ID+":"+strconv.Itoa(version) {
						continue
					}
				default:
					continue
				}
				delay := p.operationReminderDelay(tenant, agent, blocked)
				if delay <= 0 || at.Before(base.Add(delay)) {
					continue
				}
				if _, _, err := p.Inbox.Remind(tenant, message.ID, at); err != nil {
					p.log.Error("platform: operation reminder could not be kept", "error", err)
				}
				break
			}
		}
	}
}
