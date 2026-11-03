package platform

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/runs"
)

// MaxSandboxRuns is the most agent runs one sandboxes answer lists, newest
// first. More says there were others.
const MaxSandboxRuns = 50

// SessionSandbox is a conversation's own sandbox: the scratch directory every
// run in it starts from and is copied back into.
type SessionSandbox struct {
	Isolated      bool   `json:"isolated"`
	Files         int    `json:"files"`
	Bytes         int64  `json:"bytes"`
	RetentionDays int    `json:"retention_days"`
	Writable      bool   `json:"writable"`
	Reason        string `json:"reason,omitempty"`
}

// SandboxMount is a directory a run's instance is given: its work directory
// from the session sandbox, or a memory tree.
type SandboxMount struct {
	Dir      string `json:"dir"`
	Kind     string `json:"kind"`
	Name     string `json:"name,omitempty"`
	ReadOnly bool   `json:"read_only,omitempty"`
}

// RunSandbox is one agent run's sandbox, nested in the session's.
type RunSandbox struct {
	Run       string             `json:"run"`
	Agent     string             `json:"agent"`
	Parent    string             `json:"parent,omitempty"`
	Depth     int                `json:"depth"`
	Status    runs.Status        `json:"status"`
	CreatedAt time.Time          `json:"created_at"`
	Isolated  bool               `json:"isolated"`
	Mounts    []SandboxMount     `json:"mounts"`
	Files     []runs.SandboxFile `json:"files"`
	More      int                `json:"more,omitempty"`
	Conflicts []string           `json:"conflicts,omitempty"`
}

// ConversationSandboxes is a conversation's session sandbox and the nested
// sandbox of each agent run in it.
type ConversationSandboxes struct {
	Session SessionSandbox `json:"session"`
	Runs    []RunSandbox   `json:"runs"`
	More    bool           `json:"more"`
}

// Sandboxes reads a conversation's session sandbox and its runs' sandboxes,
// newest run first and at most MaxSandboxRuns of them.
func (p *Platform) Sandboxes(ctx context.Context, who auth.Principal, id string) (ConversationSandboxes, error) {
	list, err := p.WorkspaceFiles(ctx, who, id)
	if err != nil {
		return ConversationSandboxes{}, err
	}
	isolated := p.cfg.Sandbox.Isolated()
	out := ConversationSandboxes{
		Session: SessionSandbox{Isolated: isolated, Files: len(list.Files), RetentionDays: list.RetentionDays, Writable: list.Writable, Reason: list.Reason},
		Runs:    []RunSandbox{},
	}
	for _, f := range list.Files {
		out.Session.Bytes += f.Size
	}
	turns, err := p.Runs.Conversation(who.Tenant, id)
	if errors.Is(err, runs.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	var all []runs.Run
	for _, turn := range turns {
		tree, err := p.Runs.Tree(who.Tenant, turn.ID)
		if err != nil {
			return out, err
		}
		all = append(all, tree...)
		if len(all) > runs.MaxTree {
			break
		}
	}
	slices.SortStableFunc(all, func(a, b runs.Run) int { return b.CreatedAt.Compare(a.CreatedAt) })
	all = slices.CompactFunc(all, func(a, b runs.Run) bool { return a.ID == b.ID })
	if len(all) > MaxSandboxRuns {
		all, out.More = all[:MaxSandboxRuns], true
	}
	for _, r := range all {
		rs := RunSandbox{Run: r.ID, Agent: r.Agent, Parent: r.Parent, Depth: r.Depth, Status: r.Status, CreatedAt: r.CreatedAt,
			Isolated: isolated, Mounts: []SandboxMount{{Dir: "/work", Kind: "session"}}, Files: []runs.SandboxFile{}}
		if isolated {
			for _, m := range p.MemoryMounts(r) {
				name := m.Tree.Agent
				if name == "" {
					name = m.Tree.Project
				}
				if m.Tree.Person != "" {
					name = ""
				}
				rs.Mounts = append(rs.Mounts, SandboxMount{Dir: "/" + m.Dir, Kind: string(m.Tree.Kind), Name: name, ReadOnly: m.ReadOnly})
			}
		}
		if r.Sandbox != nil {
			rs.Isolated = r.Sandbox.Isolated
			rs.Files = slices.Clone(r.Sandbox.Files)
			rs.More = r.Sandbox.More
			rs.Conflicts = slices.Clone(r.Sandbox.Conflicts)
		}
		out.Runs = append(out.Runs, rs)
	}
	return out, nil
}
