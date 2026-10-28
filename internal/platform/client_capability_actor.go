package platform

import (
	"context"
	"encoding/json"

	"github.com/urmzd/humagen/fiber/pkg/address"
	"github.com/urmzd/humagen/fiber/pkg/connector"
	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/operations"
)

type capabilityActor struct {
	p   *Platform
	st  *seat
	req runtime.Request
}

func (a capabilityActor) Name() string { return a.st.peer.Name() }
func (a capabilityActor) Actions() []connector.Action {
	out := []connector.Action{}
	current, ok := a.p.currentInstallation(a.st.client)
	if !ok {
		return out
	}
	for _, action := range a.st.peer.Actions() {
		if !current.AllowsAction(action.Name) {
			continue
		}
		next := action
		name := action.Name
		next.Invoke = func(ctx context.Context, target address.Address, raw json.RawMessage) (connector.Result, error) {
			current, ok := a.p.currentInstallation(a.st.client)
			if !ok || !current.Routes(a.req.Agent()) || !current.AllowsAction(name) {
				return connector.Result{}, operations.ErrDenied
			}
			if current.Surface == clients.Slack {
				channel := ""
				if len(target.Path) > 0 {
					channel = target.Path[0]
				}
				var args struct {
					Channel string `json:"channel"`
				}
				if json.Unmarshal(raw, &args) == nil && args.Channel != "" {
					channel = args.Channel
				}
				project, _ := a.p.operationPlace(a.req)
				if direction, bound := a.p.bridgeDirection(current, channel); bound {
					ch, ok := a.p.Projects.BoundTo(current.Tenant, current.ID, channel)
					if !direction.AllowsOutcomes() || !ok || ch.Project != project {
						return connector.Result{}, operations.ErrDenied
					}
				} else if !directMessage(channel) && project != current.HomeOf().Project {
					return connector.Result{}, operations.ErrDenied
				}

			}
			st := a.p.surfaces[string(current.Surface)].seat(current.Tenant, current.ID)
			if st == nil {
				return connector.Result{}, operations.ErrDenied
			}
			var invoke func(context.Context, address.Address, json.RawMessage) (connector.Result, error)
			for _, actual := range st.peer.Actions() {
				if actual.Name == name {
					invoke = actual.Invoke
					break
				}
			}
			if invoke == nil {
				return connector.Result{}, operations.ErrDenied
			}

			if categoryOf(name) != operations.ReadContext {
				if run, err := a.p.Runs.Get(a.req.Tenant, a.req.RunID); err == nil && (a.p.privateThread(a.req.Tenant, run.Thread) || a.p.RunRequiresReviewedSummary(ctx, run, a.p.RunPlace(a.req.Tenant, run).Project)) {
					return connector.Result{}, operations.ErrDenied
				}
			}
			project, person := a.p.operationPlace(a.req)
			actor := person
			if actor == "" {
				actor = "agent:" + a.req.Agent()
			}
			change := audit.Change{Action: "client.action", Resource: "client", ID: current.ID, Scope: "project", Project: project, Agent: a.req.Agent(), After: map[string]string{"action": name}}
			if run, err := a.p.Runs.Get(a.req.Tenant, a.req.RunID); err == nil {
				change.Thread = run.Thread
			}
			attempt, err := a.p.BeginAudit(auth.Principal{Tenant: a.req.Tenant, Subject: actor}, change)
			if err != nil {
				return connector.Result{}, err
			}
			result, err := invoke(ctx, target, raw)
			if done := attempt.Finish(err); done != nil {
				a.p.LogAuditCompletionFailure(done)
			}
			return result, err
		}
		if len(out) >= 32 {
			break
		}
		out = append(out, next)
	}
	return out
}

var _ connector.Actor = capabilityActor{}
