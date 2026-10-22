package httpapi

import (
	"net/http"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/policy"
	"github.com/urmzd/warp/pkg/projects"
)

// projectMembersView is who holds a role in a project, the version they were
// read at, and the owners of the projects above it, who own it too.
type projectMembersView struct {
	Version   int                       `json:"version"`
	Members   []projects.Entry          `json:"members"`
	Inherited []projects.InheritedOwner `json:"inherited"`
}

func (s *Server) membersOf(who auth.Principal, name string) projectMembersView {
	tree := s.P.ProjectTree(who.Tenant)
	p, _ := tree.Get(name)
	members := p.Members
	if members == nil {
		members = []projects.Entry{}
	}
	return projectMembersView{Version: p.Version, Members: members, Inherited: tree.InheritedOwners(name)}
}

// projectMembers lists who holds a role in a project, and who owns it from
// above.
func (s *Server) projectMembers(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	p, _, found := s.visible(w, who, r.PathValue("name"), projects.Viewer)
	if !found {
		return
	}
	ok(w, http.StatusOK, s.membersOf(who, p.Name))
}

// putProjectMembers replaces who holds a role in a project, as its owner.
// A project with no entry for everyone is members only, and the platform
// decides where one may be (SetProjectMembers).
func (s *Server) putProjectMembers(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	// A project that is not there is said so before the body is read, as
	// one the caller may not see is.
	if !s.P.HasProject(who.Tenant, projects.NameOf(r.PathValue("name"))) {
		s.problem(w, platform.ErrProjectNotFound)
		return
	}
	var body struct {
		Version int              `json:"version"`
		Members []projects.Entry `json:"members"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Members == nil {
		fail(w, http.StatusBadRequest, "members lists who holds a role, everyone (*) among them")
		return
	}
	p, err := s.P.SetProjectMembers(r.Context(), who, r.PathValue("name"), body.Version, body.Members)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, s.membersOf(who, p.Name))
}

// moveProject puts a project, and everything under it, under another parent:
// "" or "~" is the top level. It needs ownership of the project and
// membership of the new parent, and leaving a project that holds a limit
// needs billing:write as well.
func (s *Server) moveProject(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Version int    `json:"version"`
		Parent  string `json:"parent"`
	}
	if !decode(w, r, &body) {
		return
	}
	p, err := s.P.MoveProject(r.Context(), who, r.PathValue("name"), body.Version, body.Parent)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, s.projectView(s.projectFacts(who), p))
}

// projectLimitBody is a tighten-only limit an owner of a project above sets.
type projectLimitBody struct {
	Period        policy.Period `json:"period"`
	MaxRuns       int           `json:"max_runs"`
	MaxConcurrent int           `json:"max_concurrent"`
	SpendLimit    int64         `json:"spend_limit"`
	MaxLease      int64         `json:"max_lease"`
	MaxStorage    int64         `json:"max_storage"`
	MaxTokens     int64         `json:"max_tokens"`
	WarnAt        int           `json:"warn_at"`
	Action        policy.Action `json:"action"`
}

// putProjectLimit sets a project's limit as an owner of a project above it.
// It only tightens: one looser than a limit over it is refused, naming the
// scope that binds, and a limit an admin set is changed only with
// billing:write, on PUT /api/policies.
func (s *Server) putProjectLimit(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body projectLimitBody
	if !decode(w, r, &body) {
		return
	}
	saved, err := s.P.SetProjectLimit(who, r.PathValue("name"), policy.Policy{
		Period: body.Period, MaxRuns: body.MaxRuns, MaxConcurrent: body.MaxConcurrent, SpendLimit: body.SpendLimit,
		MaxLease: body.MaxLease, MaxStorage: body.MaxStorage, MaxTokens: body.MaxTokens, WarnAt: body.WarnAt, Action: body.Action,
	})
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, saved)
}

// deleteProjectLimit removes a limit an owner of a project above set.
func (s *Server) deleteProjectLimit(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.DeleteProjectLimit(who, r.PathValue("name")); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
