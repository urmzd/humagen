package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/runtime"
	saigetypes "github.com/urmzd/saige/agent/types"
)

const (
	ToolAlertCreate  = "alert_create"
	ToolAlertResolve = "alert_resolve"
)

// sessionAlertCaller confines tools to the initiating person's permissions and
// this run's session. Adding a tool to the native registry never grants it.
func (p *Platform) sessionAlertCaller(req runtime.Request) (auth.Principal, threads.Thread, error) {
	run, err := p.Runs.Get(req.Tenant, req.RunID)
	if err != nil {
		return auth.Principal{}, threads.Thread{}, err
	}
	if IsEvaluation(run) {
		return auth.Principal{}, threads.Thread{}, auth.ErrForbidden
	}
	who := p.byPolicy(req.Tenant, personOf(run.Actor))
	if who.Subject == "" || !who.Can(auth.AlertsWrite) {
		return who, threads.Thread{}, auth.ErrForbidden
	}
	th, err := p.SessionRecord(context.Background(), who, run.Thread)
	if err == nil {
		err = p.MayPost(who, Place{Project: th.Project, Thread: th.ID})
	}
	return who, th, err
}
func (p *Platform) sessionAlertCreateTool(req runtime.Request) saigetypes.Tool {
	params := schema([]string{"session", "title", "priority"}, map[string]string{"session": "This run's session ID", "title": "The action needed, at most 500 characters", "priority": "urgent, normal, or low", "body": "Details that help the person decide", "resolve_when": "What answer or action resolves this alert", "action": "answer, approve, retry, or reconnect"})
	params.Properties["who"] = saigetypes.PropertyDef{Type: "array", Description: "At most 20 existing session readers, as public handles or role names; defaults to the initiating person", Items: &saigetypes.PropertyDef{Type: "string"}}
	return &saigetypes.ToolFunc{Def: saigetypes.ToolDef{Name: ToolAlertCreate, Description: "Raise an alert on this session only when a person needs to act. Reading does not resolve an alert.", Parameters: params}, Fn: func(ctx context.Context, args map[string]any) (string, error) {
		who, th, err := p.sessionAlertCaller(req)
		if err != nil {
			return "", err
		}
		if id := arg(args, "session"); id == "" || id != th.ID {
			return "", threads.ErrNotFound
		}
		var recipients []string
		if raw, exists := args["who"]; exists {
			values, ok := raw.([]any)
			if !ok || len(values) > 20 {
				return "", alerts.ErrInvalid
			}
			for _, value := range values {
				name, ok := value.(string)
				if !ok {
					return "", alerts.ErrInvalid
				}
				recipients = append(recipients, name)
			}
		}
		recipients, err = p.SessionAlertAudience(who, th.ID, recipients)
		if err != nil {
			return "", err
		}
		a, err := p.CreateSessionAlert(req.Tenant, alerts.Alert{Session: th.ID, Project: th.Project, RunID: req.RunID, Agent: req.Agent(), Title: arg(args, "title"), Body: arg(args, "body"), Priority: alerts.Priority(arg(args, "priority")), Action: arg(args, "action"), ResolveWhen: arg(args, "resolve_when"), Who: recipients, At: p.now()})
		if err != nil {
			return "", err
		}
		b, err := json.Marshal(p.Names(req.Tenant).Alert(a))
		return string(b), err
	}}
}
func (p *Platform) sessionAlertResolveTool(req runtime.Request) saigetypes.Tool {
	return &saigetypes.ToolFunc{Def: saigetypes.ToolDef{Name: ToolAlertResolve, Description: "Resolve an alert in this session once its requested action is complete.", Parameters: schema([]string{"id"}, map[string]string{"id": "The alert id in this session"})}, Fn: func(ctx context.Context, args map[string]any) (string, error) {
		who, th, err := p.sessionAlertCaller(req)
		if err != nil {
			return "", err
		}
		id := strings.TrimSpace(arg(args, "id"))
		a, err := p.Alerts.Alert(req.Tenant, id)
		if err != nil || a.Session != th.ID {
			return "", fmt.Errorf("%w: alert", alerts.ErrNotFound)
		}
		a, err = p.AckAlert(req.Tenant, id, who.Subject)
		if err != nil {
			return "", err
		}
		b, err := json.Marshal(p.Names(req.Tenant).Alert(a.View(p.now())))
		return string(b), err
	}}
}
