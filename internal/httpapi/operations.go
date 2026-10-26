package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/projects"
)

type controlInput struct {
	Paused                      bool   `json:"paused"`
	Manager                     string `json:"manager,omitempty"`
	StopRules                   string `json:"stop_rules,omitempty"`
	ReminderAfterSeconds        *int   `json:"reminder_after_seconds,omitempty"`
	BlockedReminderAfterSeconds *int   `json:"blocked_reminder_after_seconds,omitempty"`
	Version                     int    `json:"version"`
}

func (s *Server) getOperationControl(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if name := r.PathValue("name"); name != "" {
		if _, err := s.P.Agents.Get(r.Context(), who.Tenant, name, 0); err != nil {
			s.problem(w, err)
			return
		}
	}
	ok(w, http.StatusOK, s.P.AgentState(who, r.PathValue("name")))
}
func (s *Server) putOperationControl(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if name := r.PathValue("name"); name != "" {
		if _, err := s.P.Agents.Get(r.Context(), who.Tenant, name, 0); err != nil {
			s.problem(w, err)
			return
		}
	}
	var body controlInput
	if !decode(w, r, &body) {
		return
	}
	_, err := s.P.SetAgentControl(who, r.PathValue("name"), operations.Control{Paused: body.Paused, Manager: body.Manager, StopRules: body.StopRules, ReminderAfterSeconds: body.ReminderAfterSeconds, BlockedReminderAfterSeconds: body.BlockedReminderAfterSeconds}, body.Version)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, s.P.AgentState(who, r.PathValue("name")))
}
func operationScope(r *http.Request, who auth.Principal) operations.Scope {
	if r.Pattern == "GET /api/me/action-policy" || r.Pattern == "PUT /api/me/action-policy" {
		return operations.Scope{Kind: "personal", Name: who.Subject}
	}
	if r.PathValue("name") == "" {
		return operations.Scope{Kind: "workspace"}
	}
	if r.Pattern == "GET /api/projects/{name}/action-policy" || r.Pattern == "PUT /api/projects/{name}/action-policy" {
		return operations.Scope{Kind: "project", Name: projects.NameOf(r.PathValue("name"))}
	}
	return operations.Scope{Kind: "agent", Name: r.PathValue("name")}
}
func (s *Server) getActionPolicy(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	scope := operationScope(r, who)
	if scope.Kind == "agent" {
		if _, err := s.P.Agents.Get(r.Context(), who.Tenant, scope.Name, 0); err != nil {
			s.problem(w, err)
			return
		}
	}
	if scope.Kind == "project" && !s.P.HasProject(who.Tenant, scope.Name) {
		notFound(w)
		return
	}
	ok(w, http.StatusOK, s.P.ActionPolicy(who, scope))
}
func (s *Server) putActionPolicy(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	scope := operationScope(r, who)
	if scope.Kind == "agent" {
		if _, err := s.P.Agents.Get(r.Context(), who.Tenant, scope.Name, 0); err != nil {
			s.problem(w, err)
			return
		}
	}
	if scope.Kind == "project" && !s.P.HasProject(who.Tenant, scope.Name) {
		notFound(w)
		return
	}
	var body struct {
		Rules   map[operations.Category]operations.Level `json:"rules"`
		Version int                                      `json:"version"`
	}
	if !decode(w, r, &body) {
		return
	}
	_, err := s.P.SetActionPolicy(who, operationScope(r, who), body.Rules, body.Version)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, s.P.ActionPolicy(who, scope))
}
func operationLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return operations.MaxResults, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > operations.MaxResults {
		fail(w, http.StatusBadRequest, "limit must be between 1 and 200")
		return 0, false
	}
	return n, true
}
func (s *Server) listAssignments(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	limit, good := operationLimit(w, r)
	if !good {
		return
	}
	agent := r.URL.Query().Get("agent")
	if len(agent) > 256 {
		fail(w, http.StatusBadRequest, "agent filter is too long")
		return
	}
	if agent != "" {
		agent = agentspec.NormalizeName(agent)
	}
	items := s.P.Assignments(who, limit, agent)
	views := make([]assignmentView, len(items))
	for i, a := range items {
		views[i] = s.assignmentView(who, a)
	}
	ok(w, http.StatusOK, views)
}
func (s *Server) getAssignment(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	a, err := s.P.Assignment(who, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, s.assignmentView(who, a))
}

type assignmentInput struct {
	ID       string               `json:"id"`
	Agent    string               `json:"agent"`
	Project  string               `json:"project"`
	Thread   string               `json:"thread,omitempty"`
	Owner    string               `json:"owner"`
	Goal     string               `json:"goal"`
	DoneWhen string               `json:"done_when"`
	DueAt    time.Time            `json:"due_at,omitempty"`
	Cron     string               `json:"cron,omitempty"`
	Trigger  string               `json:"trigger,omitempty"`
	Source   string               `json:"source,omitempty"`
	State    operations.WorkState `json:"state"`
	Blocker  string               `json:"blocker,omitempty"`
	Version  int                  `json:"version"`
}

func (s *Server) putAssignment(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if id := r.PathValue("id"); id != "" {
		if _, err := s.P.Assignment(who, id); err != nil {
			s.problem(w, err)
			return
		}
	}
	var b assignmentInput
	if !decode(w, r, &b) {
		return
	}
	if id := r.PathValue("id"); id != "" && id != b.ID {
		fail(w, http.StatusBadRequest, "id must match the path")
		return
	}
	if r.Method == http.MethodPost && b.Version != 0 {
		fail(w, http.StatusBadRequest, "new assignments start at version 0")
		return
	}
	a, err := s.P.SetAssignment(r.Context(), who, operations.Assignment{ID: b.ID, Agent: b.Agent, Project: projects.NameOf(b.Project), Thread: b.Thread, Owner: b.Owner, Goal: b.Goal, DoneWhen: b.DoneWhen, DueAt: b.DueAt, Cron: b.Cron, Trigger: b.Trigger, Source: b.Source, State: b.State, Blocker: b.Blocker}, b.Version)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, s.assignmentView(who, a))
}
func (s *Server) runAssignment(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	run, err := s.P.RunAssignment(r.Context(), who, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, s.P.ShownRunAs(who, run))
}
func (s *Server) listHandoffs(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	limit, good := operationLimit(w, r)
	if !good {
		return
	}
	items := s.P.Handoffs(who, limit)
	out := make([]handoffView, 0, len(items))
	for _, item := range items {
		out = append(out, s.handoffView(who, item))
	}
	ok(w, http.StatusOK, out)
}
func (s *Server) agentNetwork(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	ok(w, http.StatusOK, s.P.AgentNetwork(who))
}

func (s *Server) getHandoff(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	h, err := s.P.Handoff(who, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, s.handoffView(who, h))
}
func (s *Server) putHandoff(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if _, err := s.P.Handoff(who, r.PathValue("id")); err != nil {
		s.problem(w, err)
		return
	}
	var body platform.HandoffUpdate
	if !decode(w, r, &body) {
		return
	}
	h, err := s.P.UpdateHandoff(who, r.PathValue("id"), body)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, s.handoffView(who, h))
}

func (s *Server) agentRoster(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	names := strings.Split(r.URL.Query().Get("names"), ",")
	if len(names) > 100 || len(names) == 0 || names[0] == "" {
		fail(w, http.StatusBadRequest, "names must contain between 1 and 100 exact agent names")
		return
	}
	controls := map[string]platform.AgentControlView{}
	seer := s.P.Seer(who)
	for _, name := range names {
		if name == "" || len(name) > 63 || agentspec.NormalizeName(name) != name {
			fail(w, http.StatusBadRequest, "invalid agent name")
			return
		}
		if _, err := s.P.Agents.Get(r.Context(), who.Tenant, name, 0); err == nil && seer.Agent(name) {
			controls[name] = s.P.AgentState(who, name)
		}
	}
	ok(w, http.StatusOK, map[string]any{"controls": controls, "window_days": 31})
}

type assignmentView struct {
	operations.Assignment
	OwnerName string `json:"owner_name,omitempty"`
}

func (s *Server) assignmentView(who auth.Principal, a operations.Assignment) assignmentView {
	return assignmentView{Assignment: a, OwnerName: s.P.NameOf(who.Tenant, a.Owner)}
}

type handoffView struct {
	operations.Handoff
	OwnerName string `json:"owner_name,omitempty"`
}

func (s *Server) handoffView(who auth.Principal, h operations.Handoff) handoffView {
	return handoffView{Handoff: h, OwnerName: s.P.NameOf(who.Tenant, h.Owner)}
}
