package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/urmzd/saige/flow/address"
	"github.com/urmzd/saige/flow/connector"
	transport "github.com/urmzd/saige/flow/connectors/github"
)

// readActions are the transport's actions that only read and take an
// "address" naming another issue or pull request. A write never appears
// here: a write stays bound to the signed request that granted it.
var readActions = map[string]bool{
	"github_read_context":    true,
	"github_read_file":       true,
	"github_workflow_status": true,
}

// repositoryReads are the read actions that also read a whole repository,
// named as owner/repo. The grants below apply to it as to its issues.
var repositoryReads = map[string]bool{
	"github_read_context": true,
}

type invokeFunc = func(context.Context, address.Address, json.RawMessage) (connector.Result, error)

// grantedRead lets a read action reach a repository Config.MayRead grants
// that Config.ReadRepositories does not name. The transport reads its own
// run's repository and the repositories it was given; for any other, the
// action is invoked on the named issue or pull request itself, with no
// pinned head, which is what the transport does for a granted repository.
// A repository read (github_read_context with owner/repo) is invoked on the
// repository's own address the same way.
// Everything else (the owners the client serves, the reference's form) is
// still checked by the transport.
//
// A repository Config.ReadRefused names is read by no run but its own: a run
// raised on another repository, or on another surface, is refused whatever
// grants it, and a run on one of the repository's own issues or pull
// requests still reads it.
func (c *Connector) grantedRead(name string, next invokeFunc) invokeFunc {
	if c.cfg.MayRead == nil && c.cfg.ReadRefused == nil {
		return next
	}
	static := make(map[string]bool, len(c.cfg.ReadRepositories))
	for _, r := range c.cfg.ReadRepositories {
		static[strings.ToLower(r)] = true
	}
	return func(ctx context.Context, source address.Address, raw json.RawMessage) (connector.Result, error) {
		var args map[string]json.RawMessage
		var reference string
		if json.Unmarshal(raw, &args) != nil || json.Unmarshal(args["address"], &reference) != nil || reference == "" {
			return next(ctx, source, raw)
		}
		target, ok := referenced(reference, repositoryReads[name])
		if !ok {
			return next(ctx, source, raw)
		}
		owner, repo := target.Workspace, target.Path[0]
		if source.Connector == Name && strings.EqualFold(source.Workspace, owner) && len(source.Path) > 0 && strings.EqualFold(source.Path[0], repo) {
			return next(ctx, source, raw)
		}
		if c.cfg.ReadRefused != nil && c.cfg.ReadRefused(ctx, owner, repo) {
			return connector.Result{}, fmt.Errorf("github: %s/%s is not readable from other surfaces; its repository settings keep it to runs on its own issues and pull requests", owner, repo)
		}
		if static[strings.ToLower(owner+"/"+repo)] || c.cfg.MayRead == nil || !c.cfg.MayRead(ctx, owner, repo) {
			return next(ctx, source, raw)
		}
		delete(args, "address")
		rest, err := json.Marshal(args)
		if err != nil {
			return next(ctx, source, raw)
		}
		return next(ctx, target, rest)
	}
}

// referenced is the issue or pull request a reference names, with no
// parameters, or, when repository is true, the repository it names
// (owner/repo, github://owner/repo or https://github.com/owner/repo). It
// parses with the transport's own parser, so every form the transport reads
// passes these grants first; anything else is left to the transport to
// refuse.
func referenced(reference string, repository bool) (address.Address, bool) {
	parsed, err := transport.ParseReadReference(reference)
	if err != nil {
		return address.Address{}, false
	}
	target := address.Address{Connector: parsed.Connector, Workspace: parsed.Workspace, Path: parsed.Path}
	switch {
	case target.Connector != Name:
		return address.Address{}, false
	case len(target.Path) == 1 && !repository:
		return address.Address{}, false
	case len(target.Path) == 3 && target.Path[1] != "issues" && target.Path[1] != "pull":
		return address.Address{}, false
	}
	if _, ok := repoName(target.Workspace + "/" + target.Path[0]); !ok {
		return address.Address{}, false
	}
	return target, true
}
