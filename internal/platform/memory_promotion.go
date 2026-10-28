package platform

import (
	"context"
	"fmt"
	"strings"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/projects"
)

// MemoryPromotion selects an existing reviewed note. It cannot replace its
// text or disclose its original private provenance to the new audience.
type MemoryPromotion struct {
	Scope       string `json:"scope"`
	Owner       string `json:"owner,omitempty"`
	ID          string `json:"id"`
	Destination string `json:"destination"`
}

func (p *Platform) PromoteMemory(who auth.Principal, in MemoryPromotion) (memory.Note, error) {
	if !who.Can(auth.ProjectsWrite) {
		return memory.Note{}, projects.ErrForbidden
	}
	if len(in.Owner) > 255 || len(in.ID) > 128 || len(in.Destination) > 255 {
		return memory.Note{}, memory.ErrInvalid
	}
	var owner string
	switch in.Scope {
	case "personal":
		if who.Kind != auth.User || !who.Can(auth.RunsRead) || in.Owner != "" {
			return memory.Note{}, memory.ErrNotFound
		}
		owner = memory.PersonalOwner(who.Subject)
	case "workspace", "project":
		if !who.Can(auth.ProjectsRead) || in.Scope == "workspace" && in.Owner != "" {
			return memory.Note{}, memory.ErrNotFound
		}
		owner = memory.ProjectOwner(projects.NameOf(in.Owner))
	case "agent":
		if !who.Can(auth.AgentsRead) || in.Owner == "" {
			return memory.Note{}, memory.ErrNotFound
		}
		if _, err := p.Catalog().Get(context.Background(), who.Tenant, in.Owner, 0); err != nil {
			return memory.Note{}, memory.ErrNotFound
		}
		owner = in.Owner
	default:
		return memory.Note{}, memory.ErrInvalid
	}
	if !p.Seer(who).MemoryOwner(owner) {
		return memory.Note{}, memory.ErrNotFound
	}
	var selected *memory.Note
	for _, note := range p.Memory.List(who.Tenant, owner) {
		if note.ID == in.ID {
			n := note
			selected = &n
			break
		}
	}
	if selected == nil {
		return memory.Note{}, memory.ErrNotFound
	}
	if selected.Status != memory.Approved || !selected.ExpiresAt.IsZero() && !selected.ExpiresAt.After(p.now()) {
		return memory.Note{}, memory.ErrInvalid
	}
	destination := projects.NameOf(in.Destination)
	target, _, err := p.ProjectRole(who, destination, projects.Member)
	if err != nil {
		return memory.Note{}, err
	}
	if target.PersonalOwner != "" || memory.ProjectOwner(destination) == owner {
		return memory.Note{}, memory.ErrInvalid
	}
	for _, note := range p.Memory.List(who.Tenant, memory.ProjectOwner(destination)) {
		if strings.EqualFold(note.Text, selected.Text) {
			return memory.Note{}, memory.ErrInvalid
		}
	}
	// Promotion audits are private to the copier: the source and target may
	// have unrelated audiences. Shared notes contain only copier attribution.
	attempt, err := p.BeginAudit(who, audit.Change{Action: "memory.promote", Resource: "memory", ID: selected.ID, Scope: "personal", Subject: who.Subject, Route: "POST /api/memory/promote", Rule: string(auth.ProjectsWrite), After: map[string]string{"project": destination, "kind": string(selected.Kind)}})
	if err != nil {
		return memory.Note{}, err
	}
	note, err := p.Memory.AddDetailed(who.Tenant, memory.ProjectOwner(destination), selected.Text, memory.Person, memory.Pending, who.Subject, p.now(), selected.Details)
	if completion := attempt.Finish(err); completion != nil {
		p.LogAuditCompletionFailure(completion)
	}
	if err != nil {
		return memory.Note{}, err
	}
	p.raiseInbox(who.Tenant, "memory:note:"+memory.ProjectOwner(destination)+":"+note.ID, inbox.Message{Kind: inbox.KindMemory, Subject: fmt.Sprintf("%s proposed a project memory", p.NameOf(who.Tenant, who.Subject)), From: inbox.From{Kind: "person", Name: who.Subject}, Severity: inbox.Info, Audience: []string{inbox.OwnersAudience(destination)}, Source: inbox.Source{Project: destination}, Parts: []inbox.Part{{Type: inbox.PartMemory, Memory: &inbox.MemoryPart{Owner: memory.ProjectOwner(destination), Project: destination, Notes: []inbox.NoteRef{{ID: note.ID, Text: note.Text}}}}}}, p.now())
	return note, nil
}
