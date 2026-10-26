package httpapi

import (
	"net/http"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
)

// Skills and mention kinds: what a message posted in a project can name with
// $<name> and @<kind>:<name>, for the console to complete as a person types.
// Each read takes projects:read and a project the caller can see, which a
// hidden one answers like a missing one.

// getProjectSkills reads the skills a project declares, with their
// instructions, and what a message posted there can name.
func (s *Server) getProjectSkills(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	pr, _, found := s.visible(w, who, r.PathValue("name"), projects.Viewer)
	if !found {
		return
	}
	got, err := s.P.ProjectSkillsOf(who.Tenant, pr.Name)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, got)
}

// putProjectSkills replaces the skills a project declares, as a whole. It
// needs ownership of the project; the workspace and company projects need
// projects:govern as well, and skills the platform file declares are the
// file's (409).
func (s *Server) putProjectSkills(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	// The project is found first, so a body that does not read answers a
	// hidden project as it answers a missing one.
	pr, _, found := s.visible(w, who, r.PathValue("name"), projects.Owner)
	if !found {
		return
	}
	var in platform.SkillsInput
	if !decode(w, r, &in) {
		return
	}
	if in.Skills == nil {
		fail(w, http.StatusBadRequest, "skills is a list, [] for none")
		return
	}
	got, err := s.P.SetProjectSkills(who, pr.Name, writer(who), in)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, got)
}

// listSkills lists the skills a message posted in a project can name, the
// project's own first, then each ancestor's, without their instructions.
// project is "~", or left out, for the workspace.
func (s *Server) listSkills(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	pr, _, found := s.visible(w, who, r.URL.Query().Get("project"), projects.Viewer)
	if !found {
		return
	}
	person := ""
	if who.Kind == auth.User {
		person = who.Subject
	}
	ok(w, http.StatusOK, map[string]any{"skills": s.P.AvailableSkills(who.Tenant, pr.Name, person)})
}

// listMentions lists the mention kinds a message can use, or with kind,
// completes that kind's items where a message is posted in project: the ones
// whose name starts with q, at most 20. project is "~", or left out, for the
// workspace.
func (s *Server) listMentions(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	q := r.URL.Query()
	pr, _, found := s.visible(w, who, q.Get("project"), projects.Viewer)
	if !found {
		return
	}
	kind := q.Get("kind")
	if kind == "" {
		if q.Has("q") {
			fail(w, http.StatusBadRequest, "q completes a kind's items: name the kind")
			return
		}
		ok(w, http.StatusOK, map[string]any{"kinds": s.P.MentionKinds(who.Tenant)})
		return
	}
	items, err := s.P.MentionItems(who, pr.Name, kind, q.Get("q"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, map[string]any{"items": items})
}
