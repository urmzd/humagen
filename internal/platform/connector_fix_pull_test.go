package platform

import (
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/orchagent/flow/connector"
	transport "github.com/urmzd/orchagent/flow/connectors/github"
	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/turn/native"
	saigetypes "github.com/urmzd/saige/agent/types"
	"testing"
	"time"
)

func TestFixPRRequiresActualRequesterRepositoryPermissionAndApproval(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	c, err := p.Clients.Create("t", clients.Client{ID: "gh", Name: "GitHub", Surface: clients.GitHub, Enabled: true, Identity: clients.Identity{Owners: []string{"acme"}}, Agents: []string{"a"}, GitHub: &clients.GitHubOptions{FixPullRequests: true, People: map[string]string{"ada": "person"}}}, clients.DefaultCaps(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tag := wire.NewEvent(wire.Raise{ID: "request", Tenant: "t", Origin: "api", Agent: "a", Actor: event.Actor{ID: "person"}, At: time.Now(), Context: map[string]string{runs.MetaClient: "console"}})
	if err = p.Runs.Record("run", tag, 1); err != nil {
		t.Fatal(err)
	}
	api := transport.NewFake()
	ref := transport.Ref{Owner: "acme", Repo: "repo", Number: 1}
	authorize := p.authorizeFixPull(c, api)
	ctx := connector.WithRunID(t.Context(), "run")
	if err = authorize(ctx, ref); err == nil {
		t.Fatal("ungranted repository write accepted")
	}
	api.SetWritePermission(ref, "ada", true)
	if err = authorize(ctx, ref); err != nil {
		t.Fatal(err)
	}
	api.SetWritePermission(ref, "ada", false)
	if err = authorize(ctx, ref); err == nil {
		t.Fatal("revoked repository write accepted")
	}
	req := runtime.Request{Tenant: "t", RunID: "run", Definition: native.Revision{Spec: native.Spec{Name: "a"}}}
	tool := p.operationTool(req, &saigetypes.ToolFunc{Def: saigetypes.ToolDef{Name: "github_create_fix_pr"}}, operations.ExternalMessages).(*operationTool)
	if tool.decision() != operations.Ask {
		t.Fatal("fix PR default did not ask for approval")
	}
	if err = tool.check(t.Context()); err == nil {
		t.Fatal("fix PR executed without approval")
	}
	policy, err := p.Operations.SetPolicy("t", operations.Policy{Scope: operations.Scope{Kind: "workspace"}, Rules: map[operations.Category]operations.Level{operations.ExternalMessages: operations.Allow}}, 0, "owner", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if tool.decision() != operations.Ask {
		t.Fatal("one allow skipped required approval")
	}
	policy.Rules[operations.WriteFiles] = operations.Allow
	policy, err = p.Operations.SetPolicy("t", policy, policy.Version, "owner", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if tool.decision() != operations.Allow {
		t.Fatal("explicit allows not honored")
	}
	policy.Rules[operations.WriteFiles] = operations.Never
	if _, err = p.Operations.SetPolicy("t", policy, policy.Version, "owner", time.Now()); err != nil {
		t.Fatal(err)
	}
	if tool.decision() != operations.Never {
		t.Fatal("file denial ignored")
	}

}
