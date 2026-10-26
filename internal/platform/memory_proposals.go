package platform

import (
	"fmt"
	"strings"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/projects"
)

// ProposeProjectMemory suggests a reviewed note from an already-authorized
// exact artifact version. The caller validates artifact acceptance and shared
// placement first. No caller, including an owner, auto-approves this path.
func (p *Platform) ProposeProjectMemory(who auth.Principal, project, text, provenance string) (memory.Note, error) {
	project = projects.NameOf(project)
	pr, _, err := p.ProjectRole(who, project, projects.Member)
	if err != nil {
		return memory.Note{}, err
	}
	if pr.PersonalOwner != "" || provenance == "" || len(provenance) > 512 || strings.ContainsAny(provenance, "\x00\r\n") {
		return memory.Note{}, memory.ErrInvalid
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "memory.suggest", Resource: "memory", ID: provenance, Project: project, Scope: "project", Rule: "project:member"})
	if err != nil {
		return memory.Note{}, err
	}
	note, err := p.Memory.Add(who.Tenant, memory.ProjectOwner(project), text, memory.Artifact, memory.Pending, provenance, p.now())
	if completion := attempt.Finish(err); completion != nil {
		p.LogAuditCompletionFailure(completion)
	}
	if err != nil {
		return memory.Note{}, err
	}
	p.raiseInbox(who.Tenant, "memory:note:"+memory.ProjectOwner(project)+":"+note.ID, inbox.Message{
		Kind: inbox.KindMemory, Subject: fmt.Sprintf("%s proposed a memory from an accepted artifact", p.NameOf(who.Tenant, who.Subject)), From: inbox.From{Kind: "person", Name: who.Subject}, Severity: inbox.Info, Audience: []string{inbox.OwnersAudience(project)}, Source: inbox.Source{Project: project}, Parts: []inbox.Part{{Type: inbox.PartMemory, Memory: &inbox.MemoryPart{Owner: memory.ProjectOwner(project), Project: project, Notes: []inbox.NoteRef{{ID: note.ID, Text: note.Text}}}}},
	}, p.now())
	return note, nil
}
