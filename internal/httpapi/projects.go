package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/memory"
	"github.com/urmzd/warp/pkg/policy"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
)

// A workspace is a tree of projects. The workspace itself is the root, written
// "~" in a path, and every other project sits under it, at most four levels
// down. A project gathers what belongs together: conversations, the schedules
// that feed it, instructions and notes every agent working in it is sent, and
// limits that hold everything below it.
//
// Reading needs projects:read and changing projects:write. Projects have no
// roles yet, so the route's permission decides, except that the workspace and
// company projects, their notes included, are changed only with
// projects:govern as well.
func (s *Server) projectRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects", s.authed(auth.ProjectsRead, s.listProjects))
	mux.HandleFunc("POST /api/projects", s.authed(auth.ProjectsWrite, s.createProject))
	mux.HandleFunc("GET /api/projects/{name}", s.authed(auth.ProjectsRead, s.getProject))
	mux.HandleFunc("PUT /api/projects/{name}", s.authed(auth.ProjectsWrite, s.updateProject))
	mux.HandleFunc("DELETE /api/projects/{name}", s.authed(auth.ProjectsWrite, s.deleteProject))
	mux.HandleFunc("GET /api/projects/{name}/history", s.authed(auth.ProjectsRead, s.projectHistory))
	mux.HandleFunc("GET /api/projects/{name}/memory", s.authed(auth.ProjectsRead, s.projectMemory))
	mux.HandleFunc("POST /api/projects/{name}/memory", s.authed(auth.ProjectsWrite, s.addProjectMemory))
	mux.HandleFunc("POST /api/projects/{name}/memory/approve", s.authed(auth.ProjectsWrite, s.approveProjectMemory))
	mux.HandleFunc("DELETE /api/projects/{name}/memory/{id}", s.authed(auth.ProjectsWrite, s.removeProjectMemory))
}

// writer is who a change to the tree is made by, and whether they may change
// the workspace and company projects.
func writer(who auth.Principal) projects.Writer {
	return projects.Writer{By: who.Subject, Govern: who.Can(auth.ProjectsGovern)}
}

// pathStep is one project on the way down to another.
type pathStep struct {
	Name  string        `json:"name"`
	Title string        `json:"title,omitempty"`
	Kind  projects.Kind `json:"kind"`
}

// projectView is a project, where it sits, and what is in it.
type projectView struct {
	projects.Project
	// Depth is how many levels below the workspace it sits: 0 for the
	// workspace itself.
	Depth int `json:"depth"`
	// Path is the projects from the top level down to this one, the
	// workspace left out, so it is as long as Depth.
	Path []pathStep `json:"path"`
	// Children is how many sub-projects it has.
	Children int `json:"children"`
	// Conversations is how many conversations are filed in it, its
	// sub-projects' not counted, and Active how many of their runs are in
	// flight. ActiveAt is when one of them last changed.
	Conversations int       `json:"conversations"`
	Active        int       `json:"active"`
	ActiveAt      time.Time `json:"active_at,omitzero"`
	Schedules     int       `json:"schedules"`
	// Notes is how many shared notes are in use, and Proposed how many wait
	// for someone to approve them.
	Notes    int `json:"notes"`
	Proposed int `json:"proposed"`
}

// projectFacts is what a tenant's project views are made of, read once for a
// whole answer.
type projectFacts struct {
	tenant    string
	tree      *projects.Tree
	counts    map[string]runs.ProjectCount
	schedules map[string]int
}

func (s *Server) projectFacts(tenant string) projectFacts {
	f := projectFacts{tenant: tenant, tree: s.P.ProjectTree(tenant), counts: s.P.Runs.ProjectCounts(tenant), schedules: map[string]int{}}
	for _, sc := range s.P.Schedules.List(tenant) {
		f.schedules[sc.Meta[runs.MetaProject]]++
	}
	return f
}

func (s *Server) projectView(f projectFacts, p projects.Project) projectView {
	chain := f.tree.Chain(p.Name)
	v := projectView{Project: p, Depth: max(len(chain)-1, 0), Path: []pathStep{}, Children: len(f.tree.Children(p.Name)), Schedules: f.schedules[p.Name]}
	// The chain is nearest first and ends at the workspace, which a path
	// leaves out.
	for i := len(chain) - 2; i >= 0; i-- {
		v.Path = append(v.Path, pathStep{Name: chain[i].Name, Title: chain[i].Title, Kind: chain[i].Kind})
	}
	c := f.counts[p.Name]
	v.Conversations, v.Active, v.ActiveAt = c.Threads, c.Active, c.ActiveAt
	for _, n := range s.P.Memory.List(f.tenant, memory.ProjectOwner(p.Name)) {
		if n.Status == memory.Approved {
			v.Notes++
		} else {
			v.Proposed++
		}
	}
	return v
}

// named resolves a project an address names, "~" for the workspace, or
// answers 404.
func (s *Server) named(w http.ResponseWriter, tree *projects.Tree, address string) (string, bool) {
	name := projects.NameOf(address)
	if !tree.Has(name) {
		s.problem(w, fmt.Errorf("%w: %q", projects.ErrNotFound, address))
		return "", false
	}
	return name, true
}

// listProjects lists a workspace's projects in tree order: each project before
// its sub-projects, siblings by name. The workspace itself is not one of them;
// GET /api/projects/~ reads it. parent keeps one project's sub-projects, and
// within keeps a project and everything below it.
func (s *Server) listProjects(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	f := s.projectFacts(who.Tenant)
	q := r.URL.Query()
	parent, byParent := "", q.Get("parent") != ""
	if byParent {
		var found bool
		if parent, found = s.named(w, f.tree, q.Get("parent")); !found {
			return
		}
	}
	var within map[string]bool
	if q.Get("within") != "" {
		name, found := s.named(w, f.tree, q.Get("within"))
		if !found {
			return
		}
		within = map[string]bool{}
		for _, n := range f.tree.Subtree(name) {
			within[n] = true
		}
	}
	out := []projectView{}
	for _, p := range f.tree.Projects() {
		if (byParent && p.Parent != parent) || (within != nil && !within[p.Name]) {
			continue
		}
		out = append(out, s.projectView(f, p))
	}
	ok(w, http.StatusOK, map[string]any{"projects": out})
}

// projectLimit is a limit a run filed in a project falls under for being
// there, and the project it is set on: the project itself, an ancestor, or
// "" for the workspace's own. What the limit allows is read with billing:read.
type projectLimit struct {
	Scope policy.Scope `json:"scope"`
	From  string       `json:"from"`
}

// projectDetail is one project, where it sits, what is directly below it, and
// which limits hold it.
type projectDetail struct {
	Project projectView `json:"project"`
	// Ancestors are the projects above it, the workspace first, so they read
	// as a breadcrumb. The workspace has none.
	Ancestors []projectView `json:"ancestors"`
	// Children are its sub-projects, by name.
	Children []projectView `json:"children"`
	// Limits are the project's own, each ancestor's nearest first, then the
	// workspace's. The strictest of them refuses a run.
	Limits []projectLimit `json:"limits"`
}

func (s *Server) detailOf(tenant, name string) (projectDetail, error) {
	f := s.projectFacts(tenant)
	chain := f.tree.Chain(name)
	if chain == nil {
		return projectDetail{}, fmt.Errorf("%w: %q", projects.ErrNotFound, name)
	}
	d := projectDetail{Project: s.projectView(f, chain[0]), Ancestors: []projectView{}, Children: []projectView{}, Limits: []projectLimit{}}
	for i := len(chain) - 1; i >= 1; i-- {
		d.Ancestors = append(d.Ancestors, s.projectView(f, chain[i]))
	}
	for _, c := range f.tree.Children(name) {
		d.Children = append(d.Children, s.projectView(f, c))
	}
	limits, err := s.P.ProjectLimits(tenant, name)
	if err != nil {
		return projectDetail{}, err
	}
	for _, pol := range limits {
		d.Limits = append(d.Limits, projectLimit{Scope: pol.Scope, From: pol.Scope.Name})
	}
	return d, nil
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	d, err := s.detailOf(who.Tenant, projects.NameOf(r.PathValue("name")))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, d)
}

// createProject adds a project. A parent the workspace does not have is not
// found, as a project named anywhere else in a body is.
func (s *Server) createProject(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body platform.ProjectInput
	if !decode(w, r, &body) {
		return
	}
	if body.Parent != "" {
		if _, found := s.named(w, s.P.ProjectTree(who.Tenant), body.Parent); !found {
			return
		}
	}
	p, err := s.P.CreateProject(r.Context(), who.Tenant, writer(who), body)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, s.projectView(s.projectFacts(who.Tenant), p))
}

// projectUpdateBody is a project's new title, description, account and
// instructions. Its name, kind and parent are fixed when it is created, and
// are read here only to say so.
type projectUpdateBody struct {
	platform.ProjectUpdate
	Name   json.RawMessage `json:"name"`
	Kind   json.RawMessage `json:"kind"`
	Parent json.RawMessage `json:"parent"`
}

// updateProject replaces what a person may change on a project, as a whole: a
// field left out is cleared. The version is the one the caller read, so a
// change made since answers 409 instead of being overwritten.
func (s *Server) updateProject(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body projectUpdateBody
	if !decode(w, r, &body) {
		return
	}
	if body.Name != nil || body.Kind != nil || body.Parent != nil {
		fail(w, http.StatusBadRequest, "a project's name, kind and parent are fixed when it is created")
		return
	}
	p, err := s.P.UpdateProject(r.Context(), who.Tenant, r.PathValue("name"), writer(who), body.ProjectUpdate)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, s.projectView(s.projectFacts(who.Tenant), p))
}

// deleteProject removes a project and its shared notes. It is refused, naming
// them, while sub-projects, schedules, alert rules or limits point at it. Its
// conversations keep its name.
func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.DeleteProject(r.Context(), who.Tenant, r.PathValue("name"), writer(who)); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// A history cursor is the sequence number of the last change on a page. It is
// opaque to callers.
func historyCursor(seq int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(seq, 10)))
}

func historySeq(cursor string) (int64, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, false
	}
	seq, err := strconv.ParseInt(string(raw), 10, 64)
	return seq, err == nil && seq > 0
}

// projectHistory pages what changed on a project, newest first. A deleted
// project's history is not read here: its name is gone from the tree.
func (s *Server) projectHistory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	q := r.URL.Query()
	limit := projects.DefaultPage
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > projects.MaxPage {
			fail(w, http.StatusBadRequest, fmt.Sprintf("limit is a number from 1 to %d", projects.MaxPage))
			return
		}
		limit = n
	}
	var before int64
	if raw := q.Get("before"); raw != "" {
		seq, valid := historySeq(raw)
		if !valid {
			fail(w, http.StatusBadRequest, "before is not a cursor this server issued")
			return
		}
		before = seq
	}
	changes, next, err := s.P.ProjectHistory(who.Tenant, r.PathValue("name"), limit, before)
	if err != nil {
		s.problem(w, err)
		return
	}
	var cursor *string
	if next > 0 {
		c := historyCursor(next)
		cursor = &c
	}
	ok(w, http.StatusOK, map[string]any{"changes": changes, "next": cursor})
}

// projectOwner resolves the project in the path to its memory owner, or
// answers 404. Changing the notes of the workspace or of a company project,
// which every agent in a wide part of the tree is sent, needs projects:govern,
// as changing the project does.
func (s *Server) projectOwner(w http.ResponseWriter, r *http.Request, who auth.Principal, change bool) (string, bool) {
	name := projects.NameOf(r.PathValue("name"))
	owner, err := s.P.ProjectMemoryOwner(who.Tenant, name)
	if err != nil {
		s.problem(w, err)
		return "", false
	}
	if change && !who.Can(auth.ProjectsGovern) {
		if p, _ := s.P.ProjectTree(who.Tenant).Get(name); p.IsRoot() || p.Kind == projects.Company {
			s.problem(w, fmt.Errorf("%w: the notes of the workspace and of company projects are changed with projects:govern", projects.ErrForbidden))
			return "", false
		}
	}
	return owner, true
}

func (s *Server) projectMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	owner, found := s.projectOwner(w, r, who, false)
	if !found {
		return
	}
	ok(w, http.StatusOK, map[string]any{"notes": orEmpty(s.P.Memory.List(who.Tenant, owner))})
}

func (s *Server) addProjectMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &body) {
		return
	}
	owner, found := s.projectOwner(w, r, who, true)
	if !found {
		return
	}
	note, err := s.P.Memory.Add(who.Tenant, owner, body.Text, memory.Person, memory.Approved, who.Subject, time.Now())
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, note)
}

func (s *Server) approveProjectMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &body) {
		return
	}
	owner, found := s.projectOwner(w, r, who, true)
	if !found {
		return
	}
	n, err := s.P.ApproveMemory(who.Tenant, owner, body.IDs)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, map[string]int{"approved": n})
}

func (s *Server) removeProjectMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	owner, found := s.projectOwner(w, r, who, true)
	if !found {
		return
	}
	if err := s.P.RemoveMemory(who.Tenant, owner, r.PathValue("id")); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// filed runs fn while a project, when one is named, exists and cannot be
// deleted, so a delete that checks what points at a project cannot miss what
// fn files under it. The workspace is always there.
func (s *Server) filed(tenant, project string, fn func() error) error {
	if project == projects.Root {
		return fn()
	}
	return s.P.InProject(tenant, project, fn)
}

// projectsWithin is the projects a conversation filter names: project= keeps
// one project, within= a project and everything below it, and both keep what
// both allow. Nil keeps every conversation, which is what within=~ is. A name
// the tenant does not have answers 404.
func (s *Server) projectsWithin(w http.ResponseWriter, tenant, project, within string) ([]string, bool) {
	if project == "" && within == "" {
		return nil, true
	}
	tree := s.P.ProjectTree(tenant)
	var keep []string
	if project != "" {
		name, found := s.named(w, tree, project)
		if !found {
			return nil, false
		}
		keep = []string{name}
	}
	if within != "" {
		name, found := s.named(w, tree, within)
		if !found {
			return nil, false
		}
		if name == projects.Root {
			return keep, true
		}
		sub := tree.Subtree(name)
		if keep == nil {
			return sub, true
		}
		if !slices.Contains(sub, keep[0]) {
			return []string{}, true
		}
	}
	return keep, true
}
