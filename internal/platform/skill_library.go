package platform

import (
	"context"
	"crypto/rand"
	"fmt"
	"slices"
	"strconv"
	"sync"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/signals"
)

func (p *Platform) LibraryScope(who auth.Principal, scope library.Scope, write bool) error {
	if !scope.Valid() {
		return library.ErrInvalid
	}
	if scope.Kind == "personal" {
		if who.Kind != auth.User || scope.Subject != who.Subject {
			return library.ErrNotFound
		}
		if write && !who.Can(auth.ProjectsWrite) {
			return projects.ErrForbidden
		}
		return nil
	}
	project := scope.Project
	if !p.Projects.Tree(who.Tenant).Has(project) {
		return library.ErrNotFound
	}
	if !p.Seer(who).Project(project) {
		return library.ErrNotFound
	}
	if write {
		return p.MayChangeNotes(who, project)
	}
	return nil
}
func (p *Platform) LibrarySkill(who auth.Principal, id string) (library.Revision, error) {
	r, err := p.SkillLibrary.Find(who.Tenant, id)
	if err != nil || p.LibraryScope(who, r.Scope, false) != nil {
		return library.Revision{}, library.ErrNotFound
	}
	return r, nil
}
func skillChange(action string, scope library.Scope, id string, version int) audit.Change {
	change := audit.Change{Action: "skill." + action, Resource: "skill", ID: id, Scope: scope.Kind, Project: scope.Project, Subject: scope.Subject, After: map[string]string{"version": strconv.Itoa(version)}}
	if scope.Kind == "workspace" {
		change.Scope = "workspace"
	}
	return change
}
func (p *Platform) DraftSkill(who auth.Principal, scope library.Scope, definition library.Definition, expected int, from *library.Provenance) (library.Revision, error) {
	return p.draftSkill(who, scope, definition, expected, from, "")
}

// ImportSkill is DraftSkill for a revision read from a skill folder on
// someone's machine: the same checks and the same unpublished draft, which
// records that it was imported (library.ViaImport).
func (p *Platform) ImportSkill(who auth.Principal, scope library.Scope, definition library.Definition, expected int) (library.Revision, error) {
	return p.draftSkill(who, scope, definition, expected, nil, library.ViaImport)
}

func (p *Platform) draftSkill(who auth.Principal, scope library.Scope, definition library.Definition, expected int, from *library.Provenance, via string) (library.Revision, error) {
	if err := p.LibraryScope(who, scope, true); err != nil {
		return library.Revision{}, err
	}
	var normalizeErr error
	if definition, normalizeErr = definition.Normalize(); normalizeErr != nil {
		return library.Revision{}, normalizeErr
	}
	if !definition.Valid() {
		return library.Revision{}, library.ErrInvalid
	}
	if from != nil {
		source, err := p.SkillLibrary.Get(who.Tenant, from.Scope, from.Name, from.Version)
		if err != nil || p.LibraryScope(who, from.Scope, false) != nil {
			return library.Revision{}, library.ErrNotFound
		}
		if source.PublishedAt.IsZero() {
			return library.Revision{}, fmt.Errorf("%w: publish a revision before promoting it", library.ErrConflict)
		}
		// Promotion carries exactly the selected revision, never edited
		// content, with the tool limit it is used with.
		definition = source.Definition
		definition.Tools = source.ToolLimit()
	}
	action := "draft"
	if from != nil {
		action = "promote"
	}
	change := skillChange(action, scope, library.ID(scope, definition.Name), expected+1)
	change.Rule = string(auth.ProjectsWrite)
	if via != "" {
		change.After["via"] = via
	}
	attempt, err := p.BeginAudit(who, change)
	if err != nil {
		return library.Revision{}, err
	}
	var r library.Revision
	if via == library.ViaImport {
		r, err = p.SkillLibrary.Import(who.Tenant, who.Subject, scope, definition, expected, p.now())
	} else {
		r, err = p.SkillLibrary.Draft(who.Tenant, who.Subject, scope, definition, expected, from, p.now())
	}
	if completion := attempt.Finish(err); completion != nil {
		p.LogAuditCompletionFailure(completion)
	}
	return r, err
}
func (p *Platform) TestSkill(ctx context.Context, who auth.Principal, id string, version int, agent, project, text string) (runs.Run, error) {
	latest, err := p.LibrarySkill(who, id)
	if err != nil {
		return runs.Run{}, err
	}
	if err := p.LibraryScope(who, latest.Scope, true); err != nil {
		return runs.Run{}, err
	}
	if !who.Can(auth.RunsWrite) {
		return runs.Run{}, projects.ErrForbidden
	}
	r, err := p.SkillLibrary.Get(who.Tenant, latest.Scope, latest.Name, version)
	if err != nil {
		return runs.Run{}, err
	}
	if len(r.Tests) >= 10 {
		return runs.Run{}, library.ErrInvalid
	}
	project = projects.NameOf(project)
	if !p.Seer(who).Project(project) || !p.Seer(who).Agent(agent) {
		return runs.Run{}, library.ErrNotFound
	}
	if err := p.MayFileIn(who, project); err != nil {
		return runs.Run{}, err
	}
	// A draft is tested only in its same scope, or its owner's private project.
	destination, _ := p.Projects.Tree(who.Tenant).Get(project)
	if r.Scope.Kind == "personal" && destination.PersonalOwner != who.Subject {
		return runs.Run{}, projects.ErrForbidden
	}
	if r.Scope.Kind == "project" && project != r.Scope.Project {
		return runs.Run{}, projects.ErrForbidden
	}
	change := skillChange("test", r.Scope, id, version)
	change.Rule = string(auth.RunsWrite)
	attempt, err := p.BeginAudit(who, change)
	if err != nil {
		return runs.Run{}, err
	}
	run, err := p.Converse(ctx, who.Tenant, Turn{ID: fmt.Sprintf("skill-test-%s", rand.Text()), Agent: agent, Text: text, Project: project, Actor: ActorOf(who), ForceAsk: true, TestSkill: &r, Sees: p.Seer(who).Agent})
	if err == nil {
		err = p.SkillLibrary.Tested(who.Tenant, r.Scope, r.Name, r.Version, run.ID)
	}
	if completion := attempt.Finish(err); completion != nil {
		p.LogAuditCompletionFailure(completion)
	}
	return run, err
}
func (p *Platform) PublishSkill(who auth.Principal, id string, version int) (library.Revision, error) {
	latest, err := p.LibrarySkill(who, id)
	if err != nil {
		return library.Revision{}, err
	}
	if err := p.LibraryScope(who, latest.Scope, true); err != nil {
		return library.Revision{}, err
	}
	r, err := p.SkillLibrary.Get(who.Tenant, latest.Scope, latest.Name, version)
	if err != nil {
		return library.Revision{}, err
	}
	passed, failedTools := false, false
	for _, id := range r.Tests {
		test, err := p.Runs.Get(who.Tenant, id)
		if err != nil || test.Status != runs.Completed || !p.Seer(who).Run(test) {
			continue
		}
		failed, err := p.skillTestToolFailures(who.Tenant, test.ID)
		if err != nil {
			return library.Revision{}, err
		}
		if failed == 0 {
			passed = true
		} else {
			failedTools = true
		}
	}
	if !passed && failedTools {
		return library.Revision{}, fmt.Errorf("%w: a tool call failed in every finished test of this revision; fix the skill and test it again before publishing", library.ErrConflict)
	}
	if !passed {
		return library.Revision{}, fmt.Errorf("%w: a test must finish successfully before publishing", library.ErrConflict)
	}
	change := skillChange("publish", r.Scope, id, version)
	change.Rule = string(auth.ProjectsWrite)
	attempt, err := p.BeginAudit(who, change)
	if err != nil {
		return library.Revision{}, err
	}
	saved, err := p.SkillLibrary.Publish(who.Tenant, who.Subject, r.Scope, r.Name, r.Version, p.now())
	if completion := attempt.Finish(err); completion != nil {
		p.LogAuditCompletionFailure(completion)
	}
	return saved, err
}

// skillTestToolFailures is how many tool calls of a skill's test run failed:
// a tool the model was told of and not offered, a read refused, a tool that
// erred. A test passes only when its run completed and this is zero, the two
// checks every evaluation makes of a run (evals.CheckSignals). A run whose
// tool call failed may still complete, with an answer that only says it
// could not do what the skill asks, which is no evidence the skill works.
func (p *Platform) skillTestToolFailures(tenant, run string) (int, error) {
	events, err := p.Journal.Events(tenant, []string{run})
	if err != nil {
		return 0, err
	}
	return signals.Fold(run, events, nil).ToolErrors, nil
}

// FailedSkillTests is the tests of r its reader may see that completed with
// a failed tool call (skillTestToolFailures), so do not count as passing.
// It is read for an unpublished revision only: a published one passed.
func (p *Platform) FailedSkillTests(who auth.Principal, r library.Revision) ([]string, error) {
	if !r.PublishedAt.IsZero() {
		return nil, nil
	}
	var out []string
	for _, id := range r.Tests {
		test, err := p.Runs.Get(who.Tenant, id)
		if err != nil || test.Status != runs.Completed || !p.Seer(who).Run(test) {
			continue
		}
		failed, err := p.skillTestToolFailures(who.Tenant, test.ID)
		if err != nil {
			return nil, err
		}
		if failed > 0 {
			out = append(out, test.ID)
		}
	}
	return out, nil
}

// librarySkill resolves the nearest published declaration through the real
// project chain. A personal skill is considered only for the effective actor.
func (p *Platform) librarySkill(tenant, project, person, name string) (pinnedSkill, bool) {
	if person != "" {
		r, ok, err := p.SkillLibrary.Published(tenant, library.Scope{Kind: "personal", Subject: person}, name)
		if err != nil {
			p.log.Warn("platform: skill library could not be read", "tenant", tenant, "skill", name, "error", err)
			return pinnedSkill{}, false
		}
		if ok {
			return pinRevision(r), true
		}
	}
	for _, pr := range p.Projects.Tree(tenant).Chain(project) {
		for _, legacy := range pr.Skills {
			if legacy.Name == name {
				return pinnedSkill{Name: name, From: pr.Name, Instructions: legacy.Instructions, Description: legacy.Description}, true
			}
		}
		scope := library.Scope{Kind: "project", Project: pr.Name}
		if pr.IsRoot() {
			scope = library.Scope{Kind: "workspace"}
		}
		r, ok, err := p.SkillLibrary.Published(tenant, scope, name)
		if err != nil {
			p.log.Warn("platform: skill library could not be read", "tenant", tenant, "skill", name, "error", err)
			return pinnedSkill{}, false
		}
		if ok {
			return pinRevision(r), true
		}
	}
	return pinnedSkill{}, false
}
func pinRevision(r library.Revision) pinnedSkill {
	pinned := pinnedSkill{Private: r.Scope.Kind == "personal", Name: r.Name, From: r.Scope.Project, Instructions: r.Instructions, Description: r.Description, Version: r.Version}
	// A revision that does not limit the run's tools pins no limit
	// (library.Revision.ToolLimit), so the agent's own grants apply.
	if tools := r.ToolLimit(); tools != nil {
		pinned.Tools = &tools
	}
	if r.Files != nil {
		pinned.PackageID = r.ID
		if pkg, err := r.Package(); err == nil {
			pinned.PackageHash, pinned.Resources = pkg.Hash, pkg.Resources
		}
	}
	return pinned
}

func humanSubject(who auth.Principal) string {
	if who.Kind == auth.User {
		return who.Subject
	}
	return ""
}

// LibraryView removes private source and test references before returning a
// revision. A public promotion never reveals the private source's owner.
func (p *Platform) LibraryView(who auth.Principal, r library.Revision) library.Revision {
	// Answered as it is used: an imported revision's stored empty list
	// from before allowed-tools was read is not its limit.
	r.Tools = r.ToolLimit()
	if r.OriginRun != "" && !p.Seer(who).RunID(r.OriginRun) {
		r.OriginRun = ""
	}
	if r.From != nil && p.LibraryScope(who, r.From.Scope, false) != nil {
		r.From = nil
	}
	tests := []string{}
	for _, id := range r.Tests {
		if p.Seer(who).RunID(id) {
			tests = append(tests, id)
		}
	}
	r.Tests = tests
	return r
}

// maxUnreadSkills is how many skill records this process remembers having
// logged as unreadable: four full scopes of library.MaxSkills.
const maxUnreadSkills = 4 * library.MaxSkills

// unreadSkills is the skill records this process has logged as unreadable,
// by tenant, scope and name, so each is logged once rather than at every
// completion. Past maxUnreadSkills a record is logged each time, never
// hidden.
type unreadSkills struct {
	mu     sync.Mutex
	logged map[string]bool
}

// news is those of names in a scope not logged before, and remembers them.
func (u *unreadSkills) news(tenant string, scope library.Scope, names []string) []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	var out []string
	for _, name := range names {
		key := tenant + "\x00" + scope.Key() + "\x00" + name
		if u.logged[key] {
			continue
		}
		out = append(out, name)
		if len(u.logged) < maxUnreadSkills {
			if u.logged == nil {
				u.logged = map[string]bool{}
			}
			u.logged[key] = true
		}
	}
	return out
}

// warnUnreadSkills logs a scope of the library that could not be read, and
// once each skill in it this release cannot read and so leaves out.
func (p *Platform) warnUnreadSkills(tenant string, scope library.Scope, unreadable []string, err error) {
	if err != nil {
		p.log.Warn("platform: skill library could not be read", "tenant", tenant, "error", err)
	}
	if news := p.unreadSkills.news(tenant, scope, unreadable); len(news) > 0 {
		p.log.Warn("platform: skills this release cannot read are left out", "tenant", tenant, "scope", scope.Key(), "skills", news)
	}
}

// AvailableSkills is the bounded completion list for the same resolver that
// pins a skill. Only published library versions participate in real turns.
func (p *Platform) AvailableSkills(tenant, project, person string) []projects.SkillRef {
	names := []string{}
	add := func(name string) {
		if len(names) < projects.MaxChainSkills && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	if person != "" {
		own := library.Scope{Kind: "personal", Subject: person}
		published, unreadable, err := p.SkillLibrary.ListPublished(tenant, own)
		p.warnUnreadSkills(tenant, own, unreadable, err)
		for _, r := range published {
			add(r.Name)
		}
	}
	for _, pr := range p.Projects.Tree(tenant).Chain(project) {
		for _, legacy := range pr.Skills {
			add(legacy.Name)
		}
		scope := library.Scope{Kind: "project", Project: pr.Name}
		if pr.IsRoot() {
			scope = library.Scope{Kind: "workspace"}
		}
		published, unreadable, err := p.SkillLibrary.ListPublished(tenant, scope)
		p.warnUnreadSkills(tenant, scope, unreadable, err)
		for _, r := range published {
			add(r.Name)
		}
	}
	out := []projects.SkillRef{}
	for _, name := range names {
		if r, ok := p.librarySkill(tenant, project, person, name); ok {
			out = append(out, projects.SkillRef{Name: r.Name, Description: r.Description, From: r.From})
		}
	}
	return out
}
