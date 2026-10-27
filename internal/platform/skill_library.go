package platform

import (
	"context"
	"crypto/rand"
	"fmt"
	"slices"
	"strconv"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
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
		// Promotion carries exactly the selected revision, never edited content.
		definition = source.Definition
	}
	action := "draft"
	if from != nil {
		action = "promote"
	}
	change := skillChange(action, scope, library.ID(scope, definition.Name), expected+1)
	change.Rule = string(auth.ProjectsWrite)
	attempt, err := p.BeginAudit(who, change)
	if err != nil {
		return library.Revision{}, err
	}
	r, err := p.SkillLibrary.Draft(who.Tenant, who.Subject, scope, definition, expected, from, p.now())
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
	passed := false
	for _, id := range r.Tests {
		if test, err := p.Runs.Get(who.Tenant, id); err == nil && test.Status == runs.Completed && p.Seer(who).Run(test) {
			passed = true
		}
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

// librarySkill resolves the nearest published declaration through the real
// project chain. A personal skill is considered only for the effective actor.
func (p *Platform) librarySkill(tenant, project, person, name string) (pinnedSkill, bool) {
	if person != "" {
		if r, ok := p.SkillLibrary.Published(tenant, library.Scope{Kind: "personal", Subject: person}, name); ok {
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
		if r, ok := p.SkillLibrary.Published(tenant, scope, name); ok {
			return pinRevision(r), true
		}
	}
	return pinnedSkill{}, false
}
func pinRevision(r library.Revision) pinnedSkill {
	tools := slices.Clone(r.Tools)
	if tools == nil {
		tools = []string{}
	}
	pinned := pinnedSkill{Private: r.Scope.Kind == "personal", Name: r.Name, From: r.Scope.Project, Instructions: r.Instructions, Description: r.Description, Version: r.Version, Tools: &tools}
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
		for _, r := range p.SkillLibrary.List(tenant, library.Scope{Kind: "personal", Subject: person}) {
			if _, ok := p.SkillLibrary.Published(tenant, r.Scope, r.Name); ok {
				add(r.Name)
			}
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
		for _, r := range p.SkillLibrary.List(tenant, scope) {
			if _, ok := p.SkillLibrary.Published(tenant, scope, r.Name); ok {
				add(r.Name)
			}
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
