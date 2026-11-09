package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/orchagent/flow/runtime"
	types "github.com/urmzd/saige/agent/types"
)

const (
	ToolPauseAgent  = "pause_agent"
	ToolRetireAgent = "retire_agent"
	ToolWriteEval   = "write_eval_suite"
	ToolRunEval     = "run_eval_suite"
	ToolReadEvals   = "read_agent_evals"
)

// builderApproval is the approval the builder's changing tools declare in
// the registry: a manager or an admin approves every call, and the person
// asking may be the one. A tool carries it itself, so granting the tool to
// another agent does not transfer the builder's authority.
func builderApproval() ToolApproval {
	return ToolApproval{Mode: ApprovalAlways, Approvers: []string{"role:admin", "role:manager"}, SelfApproval: true}
}

func (p *Platform) builderTarget(ctx context.Context, req runtime.Request, name string) (wire.Revision, error) {
	if !p.isHelper(req.Agent()) {
		return wire.Revision{}, fmt.Errorf("%w: only the builder changes agents", ErrBuilder)
	}
	if p.helperProtected(name) {
		return wire.Revision{}, fmt.Errorf("%w: privileged platform agents cannot be changed by the builder", ErrBuilder)
	}
	if home, ok := p.ProjectTree(req.Tenant).Home(name); ok && home != projects.Root {
		return wire.Revision{}, fmt.Errorf("%w: only the project's owners change this agent", ErrBuilder)
	}
	return p.Catalog().Get(ctx, req.Tenant, name, 0)
}

func (p *Platform) builderLifecycleTool(req runtime.Request, retire bool) types.Tool {
	name, description := ToolPauseAgent, "Pause an agent after approval. Existing work stays readable; future work is held. Set paused=false to resume."
	if retire {
		name, description = ToolRetireAgent, "Retire an agent after approval. Its saved versions and previous work remain readable."
	}
	return &types.ToolFunc{Def: types.ToolDef{Name: name, Description: description, Parameters: schema([]string{"name"}, map[string]string{"name": "Agent name", "paused": "true (default) or false to resume"})}, Fn: func(ctx context.Context, args map[string]any) (string, error) {
		name := strings.TrimSpace(arg(args, "name"))
		if IsBuiltin(name) {
			return "", fmt.Errorf("%w: built-in agents cannot be paused or retired by the builder", ErrBuilder)
		}
		rev, err := p.builderTarget(ctx, req, name)
		if err != nil {
			return "", err
		}
		if retire {
			if err := p.Agents.Delete(ctx, req.Tenant, name, rev.Rev); err != nil {
				return "", err
			}
			return "Retired " + name + ".", nil
		}
		control, err := p.AgentControl(req.Tenant, name)
		if err != nil {
			return "", err
		}
		paused := arg(args, "paused")
		if paused != "" && paused != "true" && paused != "false" {
			return "", fmt.Errorf("%w: paused is true or false", ErrBuilder)
		}
		control.Paused = paused != "false"
		if _, err = p.Operations.SetControl(req.Tenant, name, control, control.Version, "agent:"+p.cfg.EntryAgent, p.now()); err != nil {
			return "", err
		}
		if control.Paused {
			return "Paused " + name + ".", nil
		}
		return "Resumed " + name + ".", nil
	}}
}

func (p *Platform) builderWriteEvalTool(req runtime.Request) types.Tool {
	return &types.ToolFunc{Def: types.ToolDef{Name: ToolWriteEval, Description: "Save a deterministic eval suite after approval. A later agent revision must pass it without regressing. Provide a suite as JSON with name, target=agent, subject, and cases (id, input, checks).", Parameters: schema([]string{"suite"}, map[string]string{"suite": "Suite JSON"})}, Fn: func(ctx context.Context, args map[string]any) (string, error) {
		var suite evals.Suite
		raw := arg(args, "suite")
		if len(raw) > 128000 {
			return "", evals.ErrInvalid
		}
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&suite); err != nil {
			return "", err
		}
		if suite.Target != evals.Agent {
			return "", fmt.Errorf("%w: the builder writes agent suites", ErrBuilder)
		}
		if _, err := p.builderTarget(ctx, req, suite.Subject); err != nil {
			return "", err
		}
		old, err := p.Evals.Get(req.Tenant, suite.Name)
		if err != nil && !errors.Is(err, evals.ErrNotFound) {
			return "", err
		}
		replace := err == nil
		if replace && old.Subject != suite.Subject {
			return "", fmt.Errorf("%w: a suite cannot move to another agent", ErrBuilder)
		}
		if replace {
			if err := p.builderReplaySuiteAllowed(req, old); err != nil {
				return "", err
			}
		}
		who, personErr := p.builderPerson(req)
		if personErr != nil {
			who = auth.Principal{Tenant: req.Tenant}
		}
		suite, err = p.MergeEvalSuiteReplay(who, suite, replace)
		if err != nil {
			return "", err
		}
		suite.CreatedBy = who.Subject
		if suite.CreatedBy == "" {
			// Legacy plain suites can still be authored without a person. They
			// cannot supply authority for later unattended recorded replay.
			suite.CreatedBy = "agent:" + p.cfg.EntryAgent
		}
		suite.UpdatedAt = p.now()
		if err := p.Evals.Put(req.Tenant, suite, replace); err != nil {
			return "", err
		}
		return "Saved eval suite " + suite.Name + ".", nil
	}}
}

func (p *Platform) builderRunEvalTool(req runtime.Request) types.Tool {
	return &types.ToolFunc{Def: types.ToolDef{Name: ToolRunEval, Description: "Run an agent's saved eval suite and return its run reference. Read its results with read_agent_evals.", Parameters: schema([]string{"suite"}, map[string]string{"suite": "Suite name"})}, Fn: func(ctx context.Context, args map[string]any) (string, error) {
		suite, err := p.Evals.Get(req.Tenant, arg(args, "suite"))
		if err != nil {
			return "", err
		}
		if _, err := p.builderTarget(ctx, req, suite.Subject); err != nil {
			return "", err
		}
		if err := p.builderReplaySuiteAllowed(req, suite); err != nil {
			return "", err
		}
		by := "agent:" + p.cfg.EntryAgent
		if who, err := p.builderPerson(req); err == nil {
			by = who.Subject
		}
		run, err := p.StartEval(ctx, req.Tenant, suite.Name, by)
		if err != nil {
			return "", err
		}
		b, err := json.Marshal(run)
		return string(b), err
	}}
}

func (p *Platform) builderReadEvalsTool(req runtime.Request) types.Tool {
	return &types.ToolFunc{Def: types.ToolDef{Name: ToolReadEvals, Description: "Read an agent's deterministic eval suites, recent results and feedback candidate inputs. Add explicit checks before promoting a candidate into a suite.", Parameters: schema([]string{"name"}, map[string]string{"name": "Agent name"})}, Fn: func(ctx context.Context, args map[string]any) (string, error) {
		name := arg(args, "name")
		if _, err := p.builderTarget(ctx, req, name); err != nil {
			return "", err
		}
		suites, err := p.Evals.List(req.Tenant)
		if err != nil {
			return "", err
		}
		type suiteResult struct {
			Suite evals.Suite `json:"suite"`
			Runs  []evals.Run `json:"runs"`
		}
		out := struct {
			Suites     []suiteResult     `json:"suites"`
			Candidates []evals.Candidate `json:"candidates"`
		}{Suites: []suiteResult{}, Candidates: []evals.Candidate{}}
		for _, suite := range suites {
			if suite.Subject != name || suite.Target != evals.Agent {
				continue
			}
			if err := p.builderReplaySuiteAllowed(req, suite); err != nil {
				return "", err
			}
			runs, err := p.builderEvalRunsView(req, suite.Name)
			if err != nil {
				return "", err
			}
			out.Suites = append(out.Suites, suiteResult{p.EvalSuiteView(suite), runs})
		}
		candidates, err := p.Evals.Candidates(req.Tenant, name, 50)
		if err != nil {
			return "", err
		}
		// A builder's answer never discloses feedback from a private source run.
		visible := p.openWork(req.Tenant, p.ownProject(req.Tenant, req.RunID))
		for _, candidate := range candidates {
			if run, err := p.Runs.Get(req.Tenant, candidate.Run); err == nil && visible(run) {
				out.Candidates = append(out.Candidates, candidate)
			}
		}
		b, err := json.Marshal(out)
		return string(b), err
	}}
}

func (p *Platform) builderReplaySuiteAllowed(req runtime.Request, suite evals.Suite) error {
	visible := p.openWork(req.Tenant, p.ownProject(req.Tenant, req.RunID))
	for _, c := range suite.Cases {
		if c.Trace == nil {
			continue
		}
		who, err := p.builderPerson(req)
		if err != nil || !p.EvalSuiteSourcesVisible(who, suite) {
			return ErrForbidden
		}
		run, err := p.Runs.Get(req.Tenant, c.Trace.Run)
		if err != nil || run.UsesPrivateContext() || !visible(run) {
			return ErrForbidden
		}
		if c.Trace.Retrieval == nil {
			continue
		}
		if _, err = p.recordedSourceEvidence(req.Tenant, p.ownProject(req.Tenant, req.RunID), c.Trace.Run); err != nil {
			return err
		}
	}
	return nil
}

func (p *Platform) builderEvalRunsView(req runtime.Request, name string) ([]evals.Run, error) {
	records, err := p.Evals.History(req.Tenant, name)
	if err != nil {
		return nil, err
	}
	out := []evals.Run{}
	who, err := p.builderPerson(req)
	if err != nil {
		return out, nil
	}
	own := p.ownProject(req.Tenant, req.RunID)
	visible := p.openWork(req.Tenant, own)
	for _, record := range records {
		if !p.evalRunVisible(who, record) {
			continue
		}
		allowed := true
		for _, source := range record.Access.Replay {
			run, err := p.Runs.Get(req.Tenant, source.Run)
			if err != nil || run.UsesPrivateContext() || !visible(run) {
				allowed = false
				break
			}
			if source.Retrieval {
				if _, err := p.recordedSourceEvidence(req.Tenant, own, source.Run); err != nil {
					allowed = false
					break
				}
			}
		}
		if allowed {
			out = append(out, record.Run)
		}
	}
	return out, nil
}
