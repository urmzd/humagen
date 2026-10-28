package platform

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/projects"
)

// MemoryFolder is metadata for one native memory owner. It contains no note
// text or private provenance. Limits apply separately to projects and agents.
type MemoryFolder struct {
	CanPromote   bool      `json:"can_promote"`
	SelectedOnly bool      `json:"selected_only,omitempty"`
	ID           string    `json:"id"`
	Scope        string    `json:"scope"`
	Owner        string    `json:"owner,omitempty"`
	Title        string    `json:"title"`
	Parent       string    `json:"parent,omitempty"`
	Notes        int       `json:"notes"`
	Pending      int       `json:"pending"`
	UpdatedAt    time.Time `json:"updated_at,omitzero"`
	CanWrite     bool      `json:"can_write"`
	CanPropose   bool      `json:"can_propose"`
	Management   string    `json:"management"`
	CanChat      bool      `json:"can_chat"`
	ChatProject  string    `json:"chat_project"`
	ChatAgent    string    `json:"chat_agent,omitempty"`
}

// MemoryFolders is a bounded union of native scopes. Each kind checks its
// own permission before reading owners, counts or management configuration.
func (p *Platform) MemoryFolders(ctx context.Context, who auth.Principal, q string, limit int, selected ...string) ([]MemoryFolder, error) {
	q = strings.TrimSpace(q)
	if utf8.RuneCountInString(q) > 200 || limit < 1 || limit > 50 || len(selected) > 1 || len(selected) == 1 && len(selected[0]) > 512 {
		return nil, fmt.Errorf("%w: invalid memory folder query", memory.ErrInvalid)
	}
	out := []MemoryFolder{}
	seen := map[string]bool{}
	seer := p.Seer(who)
	personal := ""
	if who.Kind == auth.User && who.Can(auth.RunsRead) {
		if pr, err := p.PersonalProject(who); err == nil {
			personal = pr.Name
		}
	}
	tree := p.Projects.Tree(who.Tenant)
	matches := func(values ...string) bool {
		return strings.Contains(strings.ToLower(strings.Join(values, " ")), strings.ToLower(q))
	}
	add := func(folder MemoryFolder, owner string) {
		if seen[folder.ID] {
			return
		}
		seen[folder.ID] = true
		folder.CanPromote = who.Can(auth.ProjectsWrite)
		for _, note := range p.Memory.List(who.Tenant, owner) {
			if note.Status == memory.Pending {
				folder.Pending++
			} else if note.ExpiresAt.IsZero() || note.ExpiresAt.After(p.now()) {
				folder.Notes++
			}
			if note.At.After(folder.UpdatedAt) {
				folder.UpdatedAt = note.At
			}
		}
		folder.CanChat = who.Kind == auth.User && who.Can(auth.RunsWrite) && who.Can(auth.RunsRead) && (folder.Scope != "personal" || personal != "") && (folder.ChatAgent == "" || tree.IsAvailable(folder.ChatProject, folder.ChatAgent))
		folder.Management = p.memoryManagement(ctx, who, folder)
		out = append(out, folder)
	}
	if who.Kind == auth.User && who.Can(auth.RunsRead) && matches("Personal") {
		add(MemoryFolder{ID: "personal", Scope: "personal", Title: "Personal", CanWrite: who.Can(auth.RunsWrite), CanPropose: who.Can(auth.RunsWrite), ChatProject: personal}, memory.PersonalOwner(who.Subject))
	}
	if who.Can(auth.ProjectsRead) {
		if seer.Project("") && matches("Company", p.Settings(who.Tenant).WorkspaceName) {
			write := p.MayChangeNotes(who, "") == nil
			_, _, member := p.projectRole(tree, who, "", projects.Member)
			add(MemoryFolder{ID: "workspace", Scope: "workspace", Title: cmp.Or(p.Settings(who.Tenant).WorkspaceName, "Company"), CanWrite: write, CanPropose: who.Can(auth.ProjectsWrite) && member == nil}, memory.ProjectOwner(""))
		}
		rows, _, err := tree.Page(projects.Query{Q: q, Limit: limit, Keep: func(pr projects.Project) bool {
			return pr.Name != "" && pr.PersonalOwner == "" && seer.Project(pr.Name)
		}})
		if err != nil {
			return nil, err
		}
		for _, pr := range rows {
			_, _, member := p.projectRole(tree, who, pr.Name, projects.Member)
			add(MemoryFolder{ID: "project:" + pr.Name, Scope: "project", Owner: pr.Name, Title: cmp.Or(pr.Title, pr.Name), Parent: pr.Parent, CanWrite: p.MayChangeNotes(who, pr.Name) == nil, CanPropose: who.Can(auth.ProjectsWrite) && member == nil, ChatProject: pr.Name}, memory.ProjectOwner(pr.Name))
		}
	}
	if who.Can(auth.AgentsRead) {
		page, added := wire.Page{Size: 100}, 0
		for scan := 0; scan < 1000 && added < limit; scan += 100 {
			rows, next, err := p.AgentDirectory(ctx, who.Tenant, page)
			if err != nil {
				return nil, err
			}
			for _, rev := range rows {
				name := rev.Spec.Name
				if legacy.IsBuiltinAgent(name) || !seer.Agent(name) || !matches(name, rev.Spec.Description) {
					continue
				}
				home, _ := tree.Home(name)
				write := who.Can(auth.AgentsWrite) && p.OwnsHome(who, name) == nil
				add(MemoryFolder{ID: "agent:" + name, Scope: "agent", Owner: name, Title: name, CanWrite: write, CanPropose: write, ChatProject: home, ChatAgent: name}, name)
				added++
				if added >= limit {
					break
				}
			}
			if next == "" {
				break
			}
			page.Token = next
		}
	}
	if len(selected) == 1 && selected[0] != "" && !seen[selected[0]] {
		key := selected[0]
		switch {
		case key == "personal" && who.Kind == auth.User && who.Can(auth.RunsRead):
			add(MemoryFolder{ID: key, Scope: "personal", Title: "Personal", SelectedOnly: true, CanWrite: who.Can(auth.RunsWrite), CanPropose: who.Can(auth.RunsWrite), ChatProject: personal}, memory.PersonalOwner(who.Subject))
		case key == "workspace" && who.Can(auth.ProjectsRead) && seer.Project(""):
			_, _, member := p.projectRole(tree, who, "", projects.Member)
			add(MemoryFolder{ID: key, Scope: "workspace", Title: cmp.Or(p.Settings(who.Tenant).WorkspaceName, "Company"), SelectedOnly: true, CanWrite: p.MayChangeNotes(who, "") == nil, CanPropose: who.Can(auth.ProjectsWrite) && member == nil}, memory.ProjectOwner(""))
		case strings.HasPrefix(key, "project:") && who.Can(auth.ProjectsRead):
			name := strings.TrimPrefix(key, "project:")
			if pr, ok := tree.Get(name); ok && seer.Project(name) {
				_, _, member := p.projectRole(tree, who, name, projects.Member)
				add(MemoryFolder{ID: key, Scope: "project", Owner: name, Title: cmp.Or(pr.Title, name), Parent: pr.Parent, SelectedOnly: true, CanWrite: p.MayChangeNotes(who, name) == nil, CanPropose: who.Can(auth.ProjectsWrite) && member == nil, ChatProject: name}, memory.ProjectOwner(name))
			}
		case strings.HasPrefix(key, "agent:") && who.Can(auth.AgentsRead):
			name := strings.TrimPrefix(key, "agent:")
			if rev, err := p.Catalog().Get(ctx, who.Tenant, name, 0); err == nil && seer.Agent(rev.Spec.Name) {
				name = rev.Spec.Name
				home, _ := tree.Home(name)
				write := who.Can(auth.AgentsWrite) && p.OwnsHome(who, name) == nil
				add(MemoryFolder{ID: "agent:" + name, Scope: "agent", Owner: name, Title: name, SelectedOnly: true, CanWrite: write, CanPropose: write, ChatProject: home, ChatAgent: name}, name)
			}
		}
	}
	return out, nil
}

func (p *Platform) memoryManagement(ctx context.Context, who auth.Principal, folder MemoryFolder) string {
	if folder.Scope == "workspace" {
		return "manual"
	}
	if folder.Scope == "personal" && !p.Prefs.Get(who.Tenant, who.Subject).Personal.Remember {
		return "opt_in_required"
	}
	cfg := p.Settings(who.Tenant)
	if cfg.MemoryAgent == "" {
		return "disabled"
	}
	rev, err := p.Catalog().Get(ctx, who.Tenant, cfg.MemoryAgent, 0)
	if err != nil || rev.Spec.Provider == "offline" && !p.cfg.OfflineHooks {
		return "unavailable"
	}
	project := folder.ChatProject
	if folder.Scope == "agent" {
		project = ""
	}
	if home, homed := p.Projects.Tree(who.Tenant).Home(rev.Spec.Name); homed && home != "" && !onChain(p.Projects.Tree(who.Tenant), project, home) {
		return "unavailable"
	}
	return "configured"
}
