package platform

import (
	"context"
	"net/url"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/memory"
)

// outcomeSourceDependencies projects only server-recorded learning origins.
// Retrieval evidence grants no access to a derived object or its private run.
type outcomeSourceDependencies struct{ p *Platform }

func (reader outcomeSourceDependencies) ReadSourceDependencies(ctx context.Context, who auth.Principal, runIDs []string) (SourceExtraDependencies, error) {
	out := SourceExtraDependencies{Skills: []SourceDependency{}, Memory: []SourceDependency{}, Supported: []string{"skills", "memory"}}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if who.Kind != auth.User || !who.Can(auth.KnowledgeRead) || !who.Can(auth.RunsRead) {
		return out, nil
	}
	p := reader.p
	ordinary := p.Seer(who)
	ordinary.govern = false
	ordinary.roles = ordinary.own
	allowed := map[string]bool{}
	ids := []string{}
	for _, id := range runIDs[:min(len(runIDs), 200)] {
		if !allowed[id] && ordinary.RunID(id) {
			allowed[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	scopes := []library.Scope{}
	owners := map[string]bool{memory.PersonalOwner(who.Subject): true}
	if who.Can(auth.ProjectsRead) {
		scopes = append(scopes, library.Scope{Kind: "personal", Subject: who.Subject})
		if ordinary.Project("") {
			scopes = append(scopes, library.Scope{Kind: "workspace"})
			owners[memory.ProjectOwner("")] = true
		}
		for _, project := range ordinary.tree.Projects() {
			if !ordinary.Project(project.Name) {
				continue
			}
			scopes = append(scopes, library.Scope{Kind: "project", Project: project.Name})
			owners[memory.ProjectOwner(project.Name)] = true
		}
	}
	for _, scope := range scopes {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		for _, skill := range p.SkillLibrary.List(who.Tenant, scope) {
			if !allowed[skill.OriginRun] || !ordinary.RunID(skill.OriginRun) {
				continue
			}
			query := url.Values{"scope": {scope.Kind}, "skill": {skill.ID}}
			if scope.Kind == "project" {
				query.Set("project", scope.Project)
			}
			out.Skills = append(out.Skills, SourceDependency{ID: skill.ID, Title: skill.Name, Relation: "recorded_origin_run", Path: "/library/skills?" + query.Encode(), OriginRun: skill.OriginRun})
			if len(out.Skills) == 50 {
				break
			}
		}
		if len(out.Skills) == 50 {
			break
		}
	}
	origins, err := p.Memory.ReviewedOrigins(who.Tenant, ids, owners)
	if err != nil {
		return out, err
	}
	for _, origin := range origins {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		note := origin.Note
		if !ordinary.MemoryOwner(origin.Owner) || !ordinary.RunID(origin.Run) || note.Status != memory.Approved || !note.ExpiresAt.IsZero() && !note.ExpiresAt.After(p.now()) {
			continue
		}
		query := url.Values{"scope": {"personal"}}
		if project, ok := memory.OwnerProject(origin.Owner); ok {
			query.Set("scope", "workspace")
			if project != "" {
				query.Set("scope", "project")
				query.Set("owner", project)
			}
		}
		out.Memory = append(out.Memory, SourceDependency{ID: note.ID, Title: note.Text, Relation: "recorded_origin_run", Path: "/library/memory?" + query.Encode(), OriginRun: origin.Run})
	}
	return out, nil
}
