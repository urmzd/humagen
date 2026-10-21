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
// Reading needs projects:read and changing projects:write. Then the caller's
// role on the project the route names decides, and only narrows: a viewer
// reads it, a member creates projects under it and proposes notes, an owner
// changes it and everything below it. A project the caller has no role in is
// not found. The workspace and company projects are changed only with
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
	mux.HandleFunc("GET /api/projects/{name}/members", s.authed(auth.ProjectsRead, s.projectMembers))
	mux.HandleFunc("PUT /api/projects/{name}/members", s.authed(auth.ProjectsWrite, s.putProjectMembers))
	mux.HandleFunc("POST /api/projects/{name}/move", s.authed(auth.ProjectsWrite, s.moveProject))
	mux.HandleFunc("PUT /api/projects/{name}/limits", s.authed(auth.ProjectsWrite, s.putProjectLimit))
	mux.HandleFunc("DELETE /api/projects/{name}/limits", s.authed(auth.ProjectsWrite, s.deleteProjectLimit))
}

// writer is who a change to the tree is made by, and whether they may change
// the workspace and company projects.
func writer(who auth.Principal) projects.Writer { return platform.WriterOf(who) }

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
	// Access is the caller's role here, the project it comes from, and the
	// entry or rule that gave it.
	Access projects.Access `json:"access"`
	// Actions are what the caller may do here: their permission and their
	// role together.
	Actions []string `json:"actions"`
}

// What a caller may do on a project, as a view lists it.
const (
	// actCreate is creating a project under it.
	actCreate = "create"
	// actEdit is changing its record: title, description, instructions,
	// account, kind, members and who may announce.
	actEdit = "edit"
	// actMove, actArchive and actDelete are what they say.
	actMove    = "move"
	actArchive = "archive"
	actDelete  = "delete"
	// actPropose is proposing a note, which its owners keep or discard.
	actPropose = "propose"
	// actNotes is adding, keeping and removing notes.
	actNotes = "notes"
	// actLimits is setting a tighten-only limit on it, as an owner above.
	actLimits = "limits"
)

// projectFacts is what a tenant's project views are made of, read once for a
// whole answer: the tree, the caller's role in every project of it, and the
// counts.
type projectFacts struct {
	tenant    string
	who       auth.Principal
	tree      *projects.Tree
	access    map[string]projects.Access
	counts    map[string]runs.ProjectCount
	schedules map[string]int
}

func (s *Server) projectFacts(who auth.Principal) projectFacts {
	tenant := who.Tenant
	f := projectFacts{tenant: tenant, who: who, tree: s.P.ProjectTree(tenant), counts: s.P.Runs.ProjectCounts(tenant), schedules: map[string]int{}}
	f.access = f.tree.Roles(platform.ReaderOf(who))
	for _, sc := range s.P.Schedules.List(tenant) {
		f.schedules[sc.Meta[runs.MetaProject]]++
	}
	return f
}

// sees reports whether the caller has any role in a project.
func (f projectFacts) sees(name string) bool { return f.access[name].Role != projects.None }

// actionsOf is what the caller may do on a project: projects:write and the
// role each action needs, and projects:govern for the workspace and company
// projects, which every agent in a wide part of the tree answers under.
func actionsOf(f projectFacts, p projects.Project) []string {
	out := []string{}
	if !f.who.Can(auth.ProjectsWrite) {
		return out
	}
	role := f.access[p.Name].Role
	wide := (p.IsRoot() || p.Kind == projects.Company) && !f.who.Can(auth.ProjectsGovern)
	_, archived := f.tree.ArchivedOn(p.Name)
	if role.AtLeast(projects.Member) && !archived && f.tree.Depth(p.Name) < projects.DefaultCaps().Depth {
		out = append(out, actCreate)
	}
	if role.AtLeast(projects.Member) {
		out = append(out, actPropose)
	}
	if role == projects.Owner {
		out = append(out, actNotes)
		if !wide {
			out = append(out, actEdit)
			if !p.IsRoot() {
				if !slices.Contains(p.FileOwns, projects.FieldParent) {
					out = append(out, actMove)
				}
				out = append(out, actArchive)
				if p.Managed == "" {
					out = append(out, actDelete)
				}
			}
		}
	}
	if !p.IsRoot() && f.access[p.Parent].Role == projects.Owner {
		out = append(out, actLimits)
	}
	return out
}

func (s *Server) projectView(f projectFacts, p projects.Project) projectView {
	chain := f.tree.Chain(p.Name)
	v := projectView{Project: p, Depth: max(len(chain)-1, 0), Path: []pathStep{}, Children: len(f.tree.Children(p.Name)), Schedules: f.schedules[p.Name]}
	v.Access, v.Actions = f.access[p.Name], actionsOf(f, p)
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

// visible resolves a project an address names, "~" for the workspace, and
// holds the caller's role there to need, or answers 404 for one they cannot
// see and 403 for too low a role.
func (s *Server) visible(w http.ResponseWriter, who auth.Principal, address string, need projects.Role) (projects.Project, projects.Access, bool) {
	p, a, err := s.P.ProjectRole(who, address, need)
	if err != nil {
		s.problem(w, err)
		return projects.Project{}, projects.Access{}, false
	}
	return p, a, true
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

// listProjects pages a workspace's projects in tree order: each project
// before its sub-projects, siblings by name, the ones the caller can see. The
// workspace itself is not one of them; GET /api/projects/~ reads it. parent
// keeps one project's sub-projects, within keeps a project and everything
// below it, q searches name, title, description and customer, and kind keeps
// one kind. limit is at most 200, and before is the next of the page before.
func (s *Server) listProjects(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	f := s.projectFacts(who)
	q := r.URL.Query()
	page := projects.Query{Q: q.Get("q"), Kind: projects.Kind(q.Get("kind")), After: q.Get("before")}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > projects.MaxList {
			fail(w, http.StatusBadRequest, fmt.Sprintf("limit is a number from 1 to %d", projects.MaxList))
			return
		}
		page.Limit = n
	}
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
	page.Keep = func(p projects.Project) bool {
		return f.sees(p.Name) && (!byParent || p.Parent == parent) && (within == nil || within[p.Name])
	}
	rows, next, err := f.tree.Page(page)
	if err != nil {
		s.problem(w, err)
		return
	}
	out := make([]projectView, 0, len(rows))
	for _, p := range rows {
		out = append(out, s.projectView(f, p))
	}
	var cursor *string
	if next != "" {
		cursor = &next
	}
	ok(w, http.StatusOK, map[string]any{"projects": out, "next": cursor})
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

func (s *Server) detailOf(who auth.Principal, name string) (projectDetail, error) {
	tenant := who.Tenant
	f := s.projectFacts(who)
	chain := f.tree.Chain(name)
	if chain == nil || !f.sees(name) {
		return projectDetail{}, fmt.Errorf("%w: %q", projects.ErrNotFound, name)
	}
	d := projectDetail{Project: s.projectView(f, chain[0]), Ancestors: []projectView{}, Children: []projectView{}, Limits: []projectLimit{}}
	for i := len(chain) - 1; i >= 1; i-- {
		d.Ancestors = append(d.Ancestors, s.projectView(f, chain[i]))
	}
	for _, c := range f.tree.Children(name) {
		if f.sees(c.Name) {
			d.Children = append(d.Children, s.projectView(f, c))
		}
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
	d, err := s.detailOf(who, projects.NameOf(r.PathValue("name")))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, d)
}

// createProject adds a project under one the caller is a member of, the
// workspace when it names no parent. A parent the caller cannot see is not
// found, as a project named anywhere else in a body is.
func (s *Server) createProject(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body platform.ProjectInput
	if !decode(w, r, &body) {
		return
	}
	if _, _, found := s.visible(w, who, body.Parent, projects.Member); !found {
		return
	}
	p, err := s.P.CreateProject(r.Context(), who.Tenant, writer(who), body)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, s.projectView(s.projectFacts(who), p))
}

// projectUpdateBody is a project's new title, description, account and
// instructions, and, when given, its kind, archiving, who may announce and,
// on the workspace, its own kinds. Its name is fixed when it is created and
// its parent changes by moving, so both are read here only to say so.
type projectUpdateBody struct {
	platform.ProjectUpdate
	Name   json.RawMessage `json:"name"`
	Parent json.RawMessage `json:"parent"`
}

// updateProject replaces what an owner may change on a project: title,
// description, account and instructions as a whole, so one left out is
// cleared, and kind, archived, announce and kinds when they are given. The
// version is the one the caller read, so a change made since answers 409
// instead of being overwritten.
func (s *Server) updateProject(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body projectUpdateBody
	if !decode(w, r, &body) {
		return
	}
	switch {
	case body.Name != nil:
		fail(w, http.StatusBadRequest, "a project's name is fixed when it is created")
		return
	case body.Parent != nil:
		fail(w, http.StatusBadRequest, "a project changes parent by moving: POST /api/projects/{name}/move")
		return
	}
	if _, _, found := s.visible(w, who, r.PathValue("name"), projects.Owner); !found {
		return
	}
	p, err := s.P.UpdateProject(r.Context(), who.Tenant, r.PathValue("name"), writer(who), body.ProjectUpdate)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, s.projectView(s.projectFacts(who), p))
}

// deleteProject removes a project and its shared notes. It needs ownership,
// and is refused, naming them, while sub-projects, schedules, alert rules or
// limits point at it. Its conversations keep its name.
func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if _, _, found := s.visible(w, who, r.PathValue("name"), projects.Owner); !found {
		return
	}
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
	if _, _, found := s.visible(w, who, r.PathValue("name"), projects.Viewer); !found {
		return
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

// projectOwner resolves the project in the path to its memory owner, holding
// the caller's role there to need, or answers 404 for a project they cannot
// see and 403 for too low a role.
func (s *Server) projectOwner(w http.ResponseWriter, r *http.Request, who auth.Principal, need projects.Role) (string, bool) {
	p, _, found := s.visible(w, who, r.PathValue("name"), need)
	if !found {
		return "", false
	}
	owner, err := s.P.ProjectMemoryOwner(who.Tenant, p.Name)
	if err != nil {
		s.problem(w, err)
		return "", false
	}
	return owner, true
}

func (s *Server) projectMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	owner, found := s.projectOwner(w, r, who, projects.Viewer)
	if !found {
		return
	}
	ok(w, http.StatusOK, map[string]any{"notes": orEmpty(s.P.Memory.List(who.Tenant, owner))})
}

// addProjectMemory adds a note. An owner's is in use at once; a member's is
// a proposal, kept or discarded by the project's owners in their inbox.
func (s *Server) addProjectMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &body) {
		return
	}
	note, err := s.P.AddProjectNote(who, r.PathValue("name"), body.Text)
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
	owner, found := s.projectOwner(w, r, who, projects.Owner)
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
	owner, found := s.projectOwner(w, r, who, projects.Owner)
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
