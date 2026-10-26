// Package httpapi is the platform's HTTP edge: sign-in, the JSON API, the event
// stream, and the web console's files.
//
// Every /api route authenticates, then checks one permission, then confines
// what it reads and writes to the caller's tenant. The tenant always comes from
// the Principal and never from the request.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/bus"
	"github.com/urmzd/fiber/pkg/connectors/cron"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/registry"
	"github.com/urmzd/fiber/pkg/topic"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/callpolicy"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/credentials"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/people"
	"github.com/urmzd/humagen/pkg/pick"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/prefs"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
)

// maxBody bounds every request body. Nothing this API accepts is large.
const maxBody = 1 << 20

// heartbeat keeps an idle event stream from being closed by a proxy.
const heartbeat = 15 * time.Second

// Server serves the platform over HTTP.
type Server struct {
	P     *platform.Platform
	Authn *Authn
	Log   *slog.Logger
	// WebDir, when set, is a built web console to serve at /.
	WebDir string
}

// Handler returns the server's routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	// A client's own secret authorizes these routes, not a browser role. The
	// request goes to the client whose secret verifies it, and takes its
	// tenant from that client.
	mux.Handle("POST /connectors/github", s.P.Webhook("github"))
	mux.Handle("POST /connectors/slack", s.P.Webhook("slack"))
	mux.Handle("POST /connectors/slack/commands", s.P.Webhook("slack"))

	mux.HandleFunc("GET /auth/login", s.Authn.handleLogin)
	mux.HandleFunc("GET /auth/callback", s.Authn.handleCallback)
	mux.HandleFunc("POST /auth/logout", s.Authn.handleLogout)

	mux.HandleFunc("GET /api/agent-roster", s.authed(auth.AgentsRead, s.agentRoster))
	mux.HandleFunc("GET /api/agent-controls", s.authed(auth.AgentsRead, s.getOperationControl))
	mux.HandleFunc("PUT /api/agent-controls", s.authed(auth.AgentsWrite, s.putOperationControl))
	mux.HandleFunc("GET /api/agents/{name}/control", s.authed(auth.AgentsRead, agentNamed(s.seenAgent(s.getOperationControl))))
	mux.HandleFunc("PUT /api/agents/{name}/control", s.authed(auth.AgentsWrite, agentNamed(s.seenAgent(s.putOperationControl))))
	mux.HandleFunc("GET /api/action-policy", s.authed(auth.AgentsRead, s.getActionPolicy))
	mux.HandleFunc("PUT /api/action-policy", s.authed(auth.AgentsWrite, s.putActionPolicy))
	mux.HandleFunc("GET /api/agents/{name}/action-policy", s.authed(auth.AgentsRead, agentNamed(s.seenAgent(s.getActionPolicy))))
	mux.HandleFunc("PUT /api/agents/{name}/action-policy", s.authed(auth.AgentsWrite, agentNamed(s.seenAgent(s.putActionPolicy))))
	mux.HandleFunc("GET /api/projects/{name}/action-policy", s.authed(auth.ProjectsRead, s.seenProject(s.getActionPolicy)))
	mux.HandleFunc("PUT /api/projects/{name}/action-policy", s.authed(auth.ProjectsWrite, s.seenProject(s.putActionPolicy)))
	mux.HandleFunc("GET /api/me/action-policy", s.authed("", s.getActionPolicy))
	mux.HandleFunc("PUT /api/me/action-policy", s.authed("", s.putActionPolicy))
	mux.HandleFunc("GET /api/assignments", s.authed(auth.RunsRead, s.listAssignments))
	mux.HandleFunc("POST /api/assignments", s.authed(auth.RunsWrite, s.putAssignment))
	mux.HandleFunc("GET /api/assignments/{id}", s.authed(auth.RunsRead, s.getAssignment))
	mux.HandleFunc("PUT /api/assignments/{id}", s.authed(auth.RunsWrite, s.putAssignment))
	mux.HandleFunc("POST /api/assignments/{id}/run", s.authed(auth.RunsWrite, s.runAssignment))
	mux.HandleFunc("GET /api/handoffs/{id}", s.authed(auth.RunsRead, s.getHandoff))
	mux.HandleFunc("PUT /api/handoffs/{id}", s.authed(auth.RunsWrite, s.putHandoff))
	mux.HandleFunc("GET /api/handoffs", s.authed(auth.RunsRead, s.listHandoffs))
	mux.HandleFunc("GET /api/agent-network", s.authed(auth.AgentsRead, s.agentNetwork))
	mux.HandleFunc("GET /api/me", s.authed("", s.me))
	// Any signed-in caller may ask, and only about themselves: it takes a
	// permission and no subject.
	mux.HandleFunc("GET /api/access/explain", s.authed("", s.explainAccess))
	// The caller's own credentials for the tools that act as them: reading
	// them needs what reading runs does, and changing them what starting one
	// does. They take no subject, so nobody can name someone else.
	mux.HandleFunc("GET /api/me/credentials", s.authed(auth.RunsRead, s.listToolCredentials))
	mux.HandleFunc("PUT /api/me/credentials/{provider}", s.authed(auth.RunsWrite, s.putToolCredential))
	mux.HandleFunc("DELETE /api/me/credentials/{provider}", s.authed(auth.RunsWrite, s.deleteToolCredential))
	mux.HandleFunc("GET /api/agent-hues", s.authed(auth.RunsRead, s.agentHues))
	mux.HandleFunc("GET /api/project-agent-hues", s.authed(auth.ProjectsRead, s.agentHues))
	mux.HandleFunc("GET /api/agents", s.authed(auth.AgentsRead, s.listAgents))
	mux.HandleFunc("POST /api/agents", s.authed(auth.AgentsWrite, s.createAgent))
	mux.HandleFunc("PUT /api/agents/{name}", s.authed(auth.AgentsWrite, agentNamed(s.seenAgent(s.reviseAgent))))
	mux.HandleFunc("PUT /api/agents/{name}/home", s.authed(auth.AgentsWrite, agentNamed(s.seenAgent(s.putAgentHome))))
	mux.HandleFunc("GET /api/agents/{name}/history", s.authed(auth.AgentsRead, agentNamed(s.seenAgent(s.agentHistory))))
	mux.HandleFunc("GET /api/agents/{name}/call-policy", s.authed(auth.AgentsRead, agentNamed(s.seenAgent(s.getCallPolicy))))
	mux.HandleFunc("PUT /api/agents/{name}/call-policy", s.authed(auth.AgentsWrite, agentNamed(s.seenAgent(s.putCallPolicy))))
	mux.HandleFunc("GET /api/agents/{name}/reachable", s.authed(auth.AgentsRead, agentNamed(s.seenAgent(s.reachable))))
	mux.HandleFunc("GET /api/agents/{name}/memory", s.authed(auth.AgentsRead, agentNamed(s.seenAgent(s.listMemory))))
	mux.HandleFunc("POST /api/agents/{name}/memory", s.authed(auth.AgentsWrite, agentNamed(s.seenAgent(s.addMemory))))
	mux.HandleFunc("DELETE /api/agents/{name}/memory/{id}", s.authed(auth.AgentsWrite, agentNamed(s.seenAgent(s.removeMemory))))
	mux.HandleFunc("POST /api/agents/{name}/memory/approve", s.authed(auth.AgentsWrite, agentNamed(s.seenAgent(s.approveMemory))))
	mux.HandleFunc("GET /api/memory/pending", s.authed(auth.AgentsRead, s.pendingMemory))
	mux.HandleFunc("GET /api/runs", s.authed(auth.RunsRead, s.listRuns))
	mux.HandleFunc("POST /api/runs", s.authed(auth.RunsWrite, s.createRun))
	mux.HandleFunc("GET /api/threads", s.authed(auth.RunsRead, s.listThreads))
	mux.HandleFunc("GET /api/threads/{id}", s.authed(auth.RunsRead, s.seenThread(s.getThread)))
	mux.HandleFunc("GET /api/pick", s.authed(auth.AgentsRead, s.pickAgent))
	mux.HandleFunc("GET /api/read-audit", s.authed(auth.ProjectsGovern, s.listReadAudit))
	mux.HandleFunc("GET /api/settings", s.authed(auth.AgentsRead, s.getSettings))
	mux.HandleFunc("PUT /api/settings", s.authed(auth.AgentsWrite, s.putSettings))
	mux.HandleFunc("GET /api/runs/{id}", s.authed(auth.RunsRead, s.seenRun(s.getRun)))
	mux.HandleFunc("PUT /api/runs/{id}/feedback", s.authed(auth.RunsRead, s.seenRun(s.putFeedback)))
	mux.HandleFunc("GET /api/runs/{id}/feedback", s.authed(auth.RunsRead, s.seenRun(s.listFeedback)))
	mux.HandleFunc("GET /api/runs/{id}/tree", s.authed(auth.RunsRead, s.seenRun(s.runTree)))
	mux.HandleFunc("POST /api/runs/{id}/calls", s.authed(auth.RunsWrite, s.seenRun(s.createCall)))
	mux.HandleFunc("POST /api/runs/{id}/stop", s.authed(auth.RunsWrite, s.seenRun(s.stopRun)))
	mux.HandleFunc("POST /api/runs/{id}/questions/{question}/answer", s.authed(auth.RunsWrite, s.seenRun(s.answerQuestion)))
	mux.HandleFunc("GET /api/schedules", s.authed(auth.SchedulesRead, s.listSchedules))
	mux.HandleFunc("POST /api/schedules", s.authed(auth.SchedulesWrite, s.createSchedule))
	mux.HandleFunc("PUT /api/schedules/{name}", s.authed(auth.SchedulesWrite, s.updateSchedule))
	mux.HandleFunc("DELETE /api/schedules/{name}", s.authed(auth.SchedulesWrite, s.deleteSchedule))
	mux.HandleFunc("POST /api/schedules/{name}/pause", s.authed(auth.SchedulesWrite, s.pauseSchedule(true)))
	mux.HandleFunc("POST /api/schedules/{name}/resume", s.authed(auth.SchedulesWrite, s.pauseSchedule(false)))
	mux.HandleFunc("POST /api/schedules/{name}/run", s.authed(auth.SchedulesWrite, s.runSchedule))
	mux.HandleFunc("GET /api/events", s.authed(auth.RunsRead, s.events))
	s.inboxRoutes(mux)
	s.artifactRoutes(mux)
	s.observeRoutes(mux)
	s.preferenceRoutes(mux)
	s.projectRoutes(mux)
	s.peopleRoutes(mux)
	s.threadRoutes(mux)
	s.billingRoutes(mux)
	s.clientRoutes(mux)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) { fail(w, http.StatusNotFound, "no such route") })

	if s.WebDir != "" {
		mux.Handle("/", spa(s.WebDir))
	}

	// A cookie authenticates a browser without the page's consent, so a
	// state-changing request from another origin is refused before it reaches
	// a handler. Bearer callers are not browsers and are unaffected.
	return headers(http.NewCrossOriginProtection().Handler(mux))
}

// headers sets the response headers every route should carry.
func headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data: blob: https:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

type handler func(w http.ResponseWriter, r *http.Request, who auth.Principal)

// agentNamed answers 404 for a path that names an agent other than by its
// canonical name, in lower case with no spaces around it, as the registry and
// every store keep it. Every check on the agent, its home's owners and
// whether the platform provides it among them, then reads the name the
// change is made under, so another spelling of it is no way around one.
func agentNamed(h handler) handler {
	return func(w http.ResponseWriter, r *http.Request, who auth.Principal) {
		if name := r.PathValue("name"); name != agentspec.NormalizeName(name) {
			fail(w, http.StatusNotFound, fmt.Sprintf("no agent called %q: agents are named in lower case", name))
			return
		}
		h(w, r, who)
	}
}

// authed authenticates, authorizes, bounds the body, and records every change.
// An empty permission means any signed-in caller.
func (s *Server) authed(perm auth.Permission, h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		who, err := s.Authn.Authenticate(r)
		if err != nil {
			fail(w, http.StatusUnauthorized, "sign in to continue")
			return
		}
		if perm != "" && !who.Can(perm) {
			s.Log.Warn("httpapi: denied", "subject", who.Subject, "tenant", who.Tenant, "permission", perm, "path", r.URL.Path)
			fail(w, http.StatusForbidden, fmt.Sprintf("you need %s", perm))
			return
		}
		// Whoever signs in with something they may do is in the
		// workspace's directory. Nothing is written while nothing changed.
		if len(who.Granted()) > 0 {
			s.P.SeePerson(who)
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		if r.Method != http.MethodGet {
			s.Log.Info("httpapi: change", "subject", who.Subject, "kind", who.Kind, "tenant", who.Tenant,
				"method", r.Method, "path", r.URL.Path)
		}
		who = s.P.AuditedReader(who, r.Pattern)
		h(w, r.WithContext(auth.With(r.Context(), who)), who)
	}
}

func (s *Server) me(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	// first_admin says the caller is the one who should set the workspace
	// up: an admin, in a workspace nobody has set up yet.
	first := who.Can(auth.AgentsWrite) && s.P.Settings(who.Tenant).SetupAt.IsZero()
	// handle is how the platform names the caller elsewhere, as a thread's
	// created_by and participants name people: their handle, or their
	// subject when they have none and it holds no address.
	out := map[string]any{"principal": who, "permissions": who.Granted(), "first_admin": first, "handle": s.P.Names(who.Tenant).Key(who.Subject)}
	if who.Kind == auth.User && who.Subject != "" && who.Tenant != "" && len(who.Granted()) > 0 {
		personal, err := s.P.PersonalProject(who)
		if err != nil {
			s.problem(w, err)
			return
		}
		out["personal_project"] = personal.Name
		if who.Can(auth.ProjectsRead) {
			welcome, err := s.P.PostWelcome(who.Tenant)
			if err != nil {
				s.problem(w, err)
				return
			}
			out["welcome_announcement"] = welcome.ID
		}
	}
	ok(w, http.StatusOK, out)
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.EnsureBuiltins(r.Context(), who.Tenant); err != nil {
		s.Log.Warn("httpapi: built-in agents", "error", err)
	}
	var all []agentspec.Revision
	page := registry.Page{Size: 100}
	for {
		revs, next, err := s.P.AgentDirectory(r.Context(), who.Tenant, page)
		if err != nil {
			s.problem(w, err)
			return
		}
		all = append(all, revs...)
		if next == "" {
			break
		}
		page.Token = next
	}
	// builtin names the agents the platform provides, which cannot be edited.
	// homes says where each agent that lives in a project lives, and
	// enabled_in which projects enable each agent, "~" for the workspace.
	names := make([]string, len(all))
	for i, rev := range all {
		names[i] = rev.Spec.Name
	}
	// An agent that lives in a project the caller cannot see is left out,
	// and so is every project they cannot see that enables an agent.
	seer := s.P.Seer(who)
	all = slices.DeleteFunc(all, func(rev agentspec.Revision) bool { return !seer.Agent(rev.Spec.Name) })
	names = slices.DeleteFunc(names, func(n string) bool { return !seer.Agent(n) })
	hues, err := s.P.AgentColors.Assign(who.Tenant, names)
	if err != nil {
		s.problem(w, err)
		return
	}
	homes, enabled := map[string]string{}, map[string][]string{}
	for name, place := range s.P.AgentPlaces(who.Tenant, names) {
		if place.Home != "" {
			homes[name] = place.Home
		}
		enabled[name] = slices.DeleteFunc(place.EnabledIn, func(p string) bool { return !seer.Project(projects.NameOf(p)) })
	}
	ok(w, http.StatusOK, map[string]any{"agents": viewRevisions(s.P.Names(who.Tenant), seer.Revisions(all)), "builtin": platform.Builtins(), "homes": homes, "enabled_in": enabled, "hues": hues})
}

// revisionView is an agent's revision and what the person who saved it is
// called. The revision names them by subject; the console shows the name.
type revisionView struct {
	agentspec.Revision
	CreatedByName string `json:"created_by_name,omitempty"`
}

func viewRevisions(names platform.Names, revs []agentspec.Revision) []revisionView {
	out := make([]revisionView, len(revs))
	for i, rev := range revs {
		out[i] = revisionView{Revision: rev, CreatedByName: names.Of(rev.CreatedBy)}
	}
	return out
}

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var spec agentspec.AgentSpec
	if !decode(w, r, &spec) {
		return
	}
	if platform.IsBuiltin(spec.Name) {
		fail(w, http.StatusForbidden, "that name belongs to an agent the platform provides")
		return
	}
	// A tag names an agent or a person, so a new agent cannot take a name a
	// person goes by, or one the platform keeps.
	if err := s.P.CheckAgentName(who.Tenant, spec.Name); err != nil {
		s.problem(w, err)
		return
	}
	if err := s.P.CheckAgentModel(who.Tenant, spec); err != nil {
		s.problem(w, err)
		return
	}
	// Every agent it may call is one the caller may read.
	spec, err := s.P.SpecWrittenBy(r.Context(), who, nil, spec)
	if err != nil {
		s.problem(w, err)
		return
	}
	rev, err := s.P.Agents.Create(r.Context(), who.Tenant, spec, who.Subject)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, viewRevisions(s.P.Names(who.Tenant), []agentspec.Revision{s.P.Seer(who).Revision(rev)})[0])
}

func (s *Server) reviseAgent(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if platform.IsBuiltin(r.PathValue("name")) {
		fail(w, http.StatusForbidden, "this agent is provided by the platform and cannot be edited")
		return
	}
	// One that is not there is said so before the body is read, as one the
	// caller may not see is, so neither tells them apart.
	if _, err := s.P.Agents.Get(r.Context(), who.Tenant, r.PathValue("name"), 0); err != nil {
		s.problem(w, err)
		return
	}
	var body struct {
		Spec        agentspec.AgentSpec `json:"spec"`
		ExpectedRev int                 `json:"expected_rev"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Spec.Name != r.PathValue("name") {
		fail(w, http.StatusBadRequest, "the spec's name must match the agent being revised")
		return
	}
	if platform.IsBuiltin(body.Spec.Name) {
		fail(w, http.StatusForbidden, "this agent is provided by the platform and cannot be edited")
		return
	}
	// An agent that lives in a project is that project's to change.
	if err := s.P.OwnsHome(who, body.Spec.Name); err != nil {
		s.problem(w, err)
		return
	}
	if err := s.P.CheckAgentModel(who.Tenant, body.Spec); err != nil {
		s.problem(w, err)
		return
	}
	// Every agent it may call is one the caller may read, and the ones it
	// may call that they were not shown stay.
	rev, err := s.P.ReviseBy(r.Context(), who, body.Spec, body.ExpectedRev)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, viewRevisions(s.P.Names(who.Tenant), []agentspec.Revision{s.P.Seer(who).Revision(rev)})[0])
}

// agentHistory returns every revision of an agent, newest first.
func (s *Server) agentHistory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	revs, err := s.P.Agents.History(r.Context(), who.Tenant, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	slices.SortFunc(revs, func(a, b agentspec.Revision) int { return b.Rev - a.Rev })
	ok(w, http.StatusOK, map[string]any{"revisions": viewRevisions(s.P.Names(who.Tenant), s.P.Seer(who).Revisions(revs))})
}

// agentOwner resolves the agent in the path to its memory owner, or answers
// 404. A name that is a project's owner is no agent's, whatever the agents
// are called, so a project's notes are reached only through the project's
// routes, where projects:govern is asked for the wide ones.
func (s *Server) agentOwner(w http.ResponseWriter, r *http.Request, who auth.Principal) (string, bool) {
	name := r.PathValue("name")
	if _, isProject := memory.OwnerProject(name); isProject {
		s.problem(w, fmt.Errorf("%w: no agent called %q", registry.ErrNotFound, name))
		return "", false
	}
	if _, err := s.P.Agents.Get(r.Context(), who.Tenant, name, 0); err != nil {
		s.problem(w, err)
		return "", false
	}
	return name, true
}

// agentToChange is agentOwner for a change to what the agent remembers. An
// agent that lives in a project is that project's to change, as its spec is.
func (s *Server) agentToChange(w http.ResponseWriter, r *http.Request, who auth.Principal) (string, bool) {
	owner, found := s.agentOwner(w, r, who)
	if !found {
		return "", false
	}
	if err := s.P.OwnsHome(who, owner); err != nil {
		s.problem(w, err)
		return "", false
	}
	return owner, true
}

func (s *Server) listMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	owner, found := s.agentOwner(w, r, who)
	if !found {
		return
	}
	ok(w, http.StatusOK, map[string]any{"notes": viewNotes(s.P.Names(who.Tenant), s.P.Memory.List(who.Tenant, owner))})
}

// noteView is a note and what the person who wrote it is called. The note
// names them by subject; the console shows the name.
type noteView struct {
	memory.Note
	ByName string `json:"by_name,omitempty"`
}

func viewNote(names platform.Names, n memory.Note) noteView {
	return noteView{Note: n, ByName: names.Of(n.By)}
}

func viewNotes(names platform.Names, notes []memory.Note) []noteView {
	out := make([]noteView, len(notes))
	for i, n := range notes {
		out[i] = viewNote(names, n)
	}
	return out
}

func (s *Server) addMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &body) {
		return
	}
	owner, found := s.agentToChange(w, r, who)
	if !found {
		return
	}
	// A person writing a note is the approval.
	note, err := s.P.Memory.Add(who.Tenant, owner, body.Text, memory.Person, memory.Approved, who.Subject, time.Now())
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, viewNote(s.P.Names(who.Tenant), note))
}

// approveMemory keeps the proposals a person selected. The rest stay pending
// until they are approved or discarded.
func (s *Server) approveMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &body) {
		return
	}
	owner, found := s.agentToChange(w, r, who)
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

// pendingMemory lists every proposal waiting in the tenant. A proposal is for
// an agent or for a project, and says which.
func (s *Server) pendingMemory(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	type proposal struct {
		Agent   string   `json:"agent,omitempty"`
		Project string   `json:"project,omitempty"`
		Note    noteView `json:"note"`
	}
	out := []proposal{}
	seer := s.P.Seer(who)
	names := s.P.Names(who.Tenant)
	for _, p := range s.P.Memory.Pending(who.Tenant) {
		if !seer.MemoryOwner(p.Agent) {
			continue
		}
		if project, isProject := memory.OwnerProject(p.Agent); isProject {
			// The workspace's own notes are the root's, named as a path
			// names it.
			if project == projects.Root {
				project = projects.RootAddress
			}
			out = append(out, proposal{Project: project, Note: viewNote(names, p.Note)})
		} else {
			out = append(out, proposal{Agent: p.Agent, Note: viewNote(names, p.Note)})
		}
	}
	ok(w, http.StatusOK, map[string]any{"proposals": out})
}

func (s *Server) removeMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	owner, found := s.agentToChange(w, r, who)
	if !found {
		return
	}
	if err := s.P.RemoveMemory(who.Tenant, owner, r.PathValue("id")); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listRuns(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	seer := s.P.Seer(who)
	ok(w, http.StatusOK, map[string]any{"runs": orEmpty(s.P.ShownRuns(who.Tenant, slices.DeleteFunc(s.P.Runs.List(who.Tenant, 200), func(r runs.Run) bool { return !seer.Run(r) })))})
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		// Agent is who should answer. "auto" is @auto, which picks among the
		// agents available where the conversation is filed, and for a person
		// a client's credential acts for, only among the agents the client
		// may route to. Empty is the default the conversation's place
		// inherits: its thread's, the client's, its channel's, then its
		// project chain's; with none, 409.
		Agent string `json:"agent"`
		Text  string `json:"text"`
		// After is the turn this message follows. Empty starts a conversation.
		After string `json:"after"`
		// Project files a new conversation under a project.
		Project string `json:"project"`
		// ActAs is the person a surface asks for: a Slack user its client
		// maps. The run is theirs, so they do not approve its calls.
		ActAs *platform.ActAs `json:"act_as"`
	}
	if !decode(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		fail(w, http.StatusBadRequest, "a run needs a request")
		return
	}
	// The run acts for the caller, or for the person a surface asks for:
	// their credentials are the ones its person-scoped tools use, and their
	// own permission and role on the project are the ones that count, never
	// the credential's, as for posting and answering for them.
	actor := platform.ActorOf(who)
	client := ""
	asks := who
	if body.ActAs != nil {
		person, err := s.P.Acting(who, *body.ActAs)
		if err != nil {
			s.problem(w, err)
			return
		}
		if !person.Can(auth.RunsWrite) {
			s.problem(w, fmt.Errorf("%w: %s may not start runs (asking needs %s)", platform.ErrForbidden, s.P.NameOf(who.Tenant, person.Subject), auth.RunsWrite))
			return
		}
		asks, actor, client = person, platform.PersonActor(person.Subject), body.ActAs.Client
	}
	// Starting work in a project, or adding a turn to work there, takes
	// being a member of it. A project the person may not see answers as
	// one that is not there. A follow-up is filed where its conversation
	// is: a turn after a run the caller may not read answers as one after
	// a run that is not there, and adding one takes what posting where it
	// lives takes, a member, and in a private thread a participant. A turn
	// that follows nothing, or names no project there is, is Converse's to
	// refuse.
	if body.After == "" && body.Project != "" && s.P.HasProject(who.Tenant, projects.NameOf(body.Project)) && !s.P.Seer(asks).Project(projects.NameOf(body.Project)) {
		s.problem(w, platform.NoProjectCalled(body.Project))
		return
	}
	project := body.Project
	if body.After != "" {
		prev, err := s.P.Runs.Get(who.Tenant, body.After)
		if err != nil || !s.P.Seer(who).Run(prev) {
			notFound(w)
			return
		}
		if err := s.P.MayFollow(asks, prev); err != nil {
			s.problem(w, err)
			return
		}
		project = prev.Project
	}
	if !s.P.HasProject(who.Tenant, projects.NameOf(project)) {
		project = ""
	}
	if err := s.P.MayFileIn(asks, project); err != nil {
		s.problem(w, err)
		return
	}
	// A caller that retries sends the same key and joins the run it started.
	id := "api-r-" + token()[:22]
	if key := r.Header.Get("Idempotency-Key"); key != "" {
		id = APITagID(who.Subject, key)
	}
	// What the memory agent learns from the turn is kept unasked, when the
	// workspace chose that, only if the person could keep it themselves.
	keeps := project != "" && s.P.MayChangeNotes(asks, project) == nil
	run, err := s.P.Converse(r.Context(), who.Tenant, platform.Turn{
		ID: id, Agent: body.Agent, Text: body.Text, After: body.After, Project: body.Project, Actor: actor, Client: client, KeepsNotes: keeps,
		// An agent the person may not read is one that is not there.
		Sees: s.P.Seer(asks).Agent,
	})
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusAccepted, run)
}

// listThreads pages a tenant's conversations, newest activity first.
//
// limit, before, origin, agent, standing, project and within are all optional.
// project keeps the conversations filed in one project, "~" for the
// workspace's own, and within those filed in a project or below it. The answer
// carries next, the cursor for the page after this one, or null at the end.
func (s *Server) listThreads(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	q := r.URL.Query()
	query := runs.ThreadQuery{
		Origin: q.Get("origin"), Agent: q.Get("agent"), Before: q.Get("before"),
		Standing: runs.Standing(q.Get("standing")), Channel: q.Get("channel"),
	}
	if query.Channel != "" && !projects.ValidChannel(query.Channel) {
		fail(w, http.StatusBadRequest, "channel is a channel's name")
		return
	}
	var found bool
	if query.Projects, found = s.projectsWithin(w, who, q.Get("project"), q.Get("within")); !found {
		return
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			fail(w, http.StatusBadRequest, "limit is a number above zero")
			return
		}
		query.Limit = n
	}
	switch query.Standing {
	case "", runs.StandingRunning, runs.StandingFailed:
	default:
		fail(w, http.StatusBadRequest, "standing is running or failed")
		return
	}
	page, next, err := s.P.ThreadPageAs(who, query)
	if err != nil {
		fail(w, http.StatusBadRequest, "before is not a cursor this server issued")
		return
	}
	var cursor *string
	if next != "" {
		cursor = &next
	}
	ok(w, http.StatusOK, map[string]any{"threads": s.P.ShowSummaries(who.Tenant, page), "next": cursor})
}

// pickAgent previews which agent @auto would pick for a message in a project,
// "~" or nothing for the workspace, and why.
func (s *Server) pickAgent(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	q := r.URL.Query()
	if !s.P.Seer(who).Project(projects.NameOf(q.Get("project"))) {
		notFound(w)
		return
	}
	choice, err := s.P.Pick(r.Context(), who.Tenant, q.Get("project"), q.Get("q"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, choice)
}

// getSettings reads the workspace's settings. A title or memory agent that
// lives in a project the caller may not read is left out.
func (s *Server) getSettings(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	ok(w, http.StatusOK, s.P.Seer(who).Settings(s.P.Settings(who.Tenant)))
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		platform.Settings
		// Baseline is gone: the workspace's default agent replaces it. It
		// is read only to say so.
		Baseline *string `json:"baseline"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Baseline != nil {
		fail(w, http.StatusBadRequest, "baseline is now the workspace default: set it with PUT /api/projects/~/agents")
		return
	}
	next := body.Settings
	before := s.P.Settings(who.Tenant)
	if err := s.P.SetSettingsBy(r.Context(), who, next); err != nil {
		s.problem(w, err)
		return
	}
	// The first admin to save settings is on record as having set the
	// workspace up.
	saved, err := s.P.MarkSetup(who.Tenant, who.Subject)
	if err != nil {
		s.problem(w, err)
		return
	}
	s.Log.Info("httpapi: settings changed", "subject", who.Subject, "tenant", who.Tenant,
		"workspace_before", before.WorkspaceName, "workspace_after", saved.WorkspaceName, "first_setup", before.SetupAt.IsZero())
	ok(w, http.StatusOK, s.P.Seer(who).Settings(saved))
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	person, allowed := s.actingFor(w, r, who, auth.RunsRead, "a run")
	if !allowed {
		return
	}
	run, err := s.P.Runs.Get(who.Tenant, r.PathValue("id"))
	if err == nil && !s.P.Seer(person).Run(run) {
		notFound(w)
		return
	}
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, s.P.ShownRun(who.Tenant, run))
}

func (s *Server) runTree(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	tree, err := s.P.Runs.Tree(who.Tenant, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, map[string]any{"runs": s.P.ShownRuns(who.Tenant, tree)})
}

func (s *Server) createCall(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Key      string         `json:"key"`
		Agent    string         `json:"agent"`
		Text     string         `json:"text"`
		Mode     call.Mode      `json:"mode"`
		Lease    int64          `json:"lease"`
		Input    *call.Input    `json:"input,omitempty"`
		Contract *call.Contract `json:"contract,omitempty"`
	}
	if !decode(w, r, &body) {
		return
	}
	// A call is a turn of the run's conversation, filed where it lives, so
	// it takes what adding one there takes: a member, and in a private
	// thread a participant. A viewer starts nothing.
	if parent, err := s.P.Runs.Get(who.Tenant, r.PathValue("id")); err == nil {
		if err := s.P.MayFollow(who, parent); err != nil {
			s.problem(w, err)
			return
		}
	}
	run, err := s.P.CallAs(r.Context(), who, r.PathValue("id"), call.Request{Contract: body.Contract, Key: body.Key, Agent: body.Agent, Text: body.Text, Input: body.Input, Mode: body.Mode, Lease: body.Lease})
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusAccepted, run)
}

// stopRun asks a run and everything attached to it to stop. Runs it delegated
// keep going. It takes the person who asked for the run, or a member where it
// lives (Platform.MayStop). It does not wait: a run that has not ended yet is
// answered 202 with stopping set, and its stream says how it ended. A run that
// has ended is answered 200 as it is, so asking twice is safe.
func (s *Server) stopRun(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	run, err := s.P.StopRun(r.Context(), who, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	if run.Stopping && !run.Status.Terminal() {
		ok(w, http.StatusAccepted, run)
		return
	}
	ok(w, http.StatusOK, run)
}

// events streams the caller's tenant's events as server-sent events.
//
// topic narrows it: "agent" is everything, "agent:<name>" one agent,
// "agent:<name>:<run>" one run. from resumes after a sequence number.
func (s *Server) events(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	t := topic.All()
	if raw := r.URL.Query().Get("topic"); raw != "" {
		parsed, err := topic.Parse(raw)
		if err != nil {
			fail(w, http.StatusBadRequest, "topic is not valid")
			return
		}
		t = parsed
	}
	sub := envelope.Subscription{Topic: t}
	if raw := r.URL.Query().Get("from"); raw != "" {
		if _, err := fmt.Sscan(raw, &sub.From); err != nil {
			fail(w, http.StatusBadRequest, "from is not a sequence number")
			return
		}
	}
	stream, err := s.P.Subscribe(r.Context(), who.Tenant, sub)
	if err != nil {
		s.problem(w, err)
		return
	}
	defer func() { _ = stream.Close() }()
	// Inbox changes ride the same stream as a named event, so a reader that
	// only listens for run events never sees them. A notice names a message
	// and nothing more: the reader fetches it and is shown what they may see.
	// The watches start before the stream is answered, as the run events'
	// subscription does, so a reader that acts once it is connected cannot
	// miss the notice its action raises: the inbox's, and the threads', whose
	// notices name a thread and the message posted or settled.
	notices, unwatch := s.P.Inbox.Watch(who.Tenant)
	defer unwatch()
	posts, unwatchThreads := s.P.WatchThreads(who.Tenant)
	defer unwatchThreads()

	rc := http.NewResponseController(w)
	// A stream outlives the server's write timeout by design.
	_ = rc.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_ = rc.Flush()

	out := make(chan envelope.Event)
	go func() {
		defer close(out)
		for {
			e, err := stream.Recv(r.Context())
			if err != nil {
				if !errors.Is(err, bus.ErrClosed) && r.Context().Err() == nil {
					s.Log.Warn("httpapi: event stream ended", "subject", who.Subject, "error", err)
				}
				return
			}
			select {
			case out <- e:
			case <-r.Context().Done():
				return
			}
		}
	}()

	// What the reader may read is decided once a run, from a snapshot of the
	// tree taken again every refresh, so a role taken away stops the next
	// run's events within one.
	gate := newRunGate(s.P, who)
	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case n := <-notices:
			// A notice reaches only a reader the message is for, so an id
			// from a project they cannot see, or an announcement for
			// others, never reaches their stream.
			sent := []any{n}
			if !s.P.InboxNoticeFor(who, n) {
				// A client's machine hears of a message addressed to
				// people it maps, once for each of them, to tell them
				// in Slack.
				sent = sent[:0]
				for _, as := range s.P.SurfaceNoticesFor(who, n) {
					sent = append(sent, inboxNotice{Notice: n, For: &as})
				}
			}
			for _, one := range sent {
				b, err := json.Marshal(one)
				if err != nil {
					continue
				}
				if _, err := fmt.Fprintf(w, "event: inbox\ndata: %s\n\n", b); err != nil {
					return
				}
			}
		case n := <-posts:
			if !s.P.SeesThread(who, n.ID) {
				continue
			}
			b, err := json.Marshal(n)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: thread\ndata: %s\n\n", b); err != nil {
				return
			}
		case <-tick.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
		case e, open := <-out:
			if !open {
				// The client reconnects and resumes. A consumer that fell
				// behind is ended here rather than fed a gap.
				return
			}
			if !gate.seen(e.RunID) {
				continue
			}
			e = s.P.ShownEvent(who, e)
			// Who answered a question is called by name (shownEvent).
			if e.Kind == envelope.KindResumed {
				e = shownEvent(s.P.Names(who.Tenant), e)
			}
			b, err := json.Marshal(e)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return
			}
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}

// problem maps a domain error to a response. Anything unrecognized is logged
// and reported without detail, so an internal error never leaks through.
func (s *Server) problem(w http.ResponseWriter, err error) {
	var tooBig *http.MaxBytesError
	if denied(w, err) || threadProblem(w, err) {
		return
	}
	switch {
	case errors.Is(err, operations.ErrInvalid):
		fail(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, operations.ErrNotFound):
		fail(w, http.StatusNotFound, "not found")
	case errors.Is(err, operations.ErrConflict), errors.Is(err, operations.ErrPaused), errors.Is(err, operations.ErrEscalation):
		fail(w, http.StatusConflict, err.Error())
	case errors.Is(err, operations.ErrDenied):
		fail(w, http.StatusForbidden, err.Error())
	case errors.Is(err, runs.ErrNotFound), errors.Is(err, registry.ErrNotFound), errors.Is(err, platform.ErrScheduleNotFound),
		errors.Is(err, platform.ErrProjectNotFound), errors.Is(err, errNotSeen),
		errors.Is(err, alerts.ErrNotFound), errors.Is(err, knowledge.ErrNotFound), errors.Is(err, evals.ErrNotFound),
		errors.Is(err, memory.ErrNotFound), errors.Is(err, policy.ErrNotFound), errors.Is(err, inbox.ErrNotFound),
		errors.Is(err, artifacts.ErrNotFound), errors.Is(err, clients.ErrNotFound),
		errors.Is(err, platform.ErrProviderNotFound), errors.Is(err, credentials.ErrNotFound), errors.Is(err, platform.ErrNoMentionKind):
		fail(w, http.StatusNotFound, "not found")
	case errors.Is(err, platform.ErrNotPersonScoped):
		fail(w, http.StatusBadRequest, detail(err, platform.ErrNotPersonScoped))
	case errors.Is(err, credentials.ErrInvalid):
		fail(w, http.StatusBadRequest, detail(err, credentials.ErrInvalid))
	case errors.Is(err, credentials.ErrLimit):
		fail(w, http.StatusTooManyRequests, detail(err, credentials.ErrLimit))
	case errors.Is(err, registry.ErrExists), errors.Is(err, platform.ErrScheduleExists), errors.Is(err, platform.ErrProjectExists),
		errors.Is(err, alerts.ErrExists), errors.Is(err, knowledge.ErrExists), errors.Is(err, evals.ErrExists):
		fail(w, http.StatusConflict, "already exists")
	case errors.Is(err, clients.ErrExists):
		// The message names only a client of the caller's own workspace.
		fail(w, http.StatusConflict, err.Error())
	case errors.Is(err, clients.ErrLimit):
		fail(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, clients.ErrManaged):
		fail(w, http.StatusConflict, "that client is configured by the server and cannot be changed here")
	case errors.Is(err, platform.ErrScheduleManaged):
		fail(w, http.StatusConflict, detail(err, platform.ErrScheduleManaged))
	case errors.Is(err, clients.ErrInvalid), errors.Is(err, callpolicy.ErrInvalid):
		fail(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, projects.ErrInvalid):
		fail(w, http.StatusBadRequest, detail(err, projects.ErrInvalid))
	case errors.Is(err, projects.ErrLimit):
		fail(w, http.StatusTooManyRequests, detail(err, projects.ErrLimit))
	case errors.Is(err, projects.ErrManaged):
		fail(w, http.StatusConflict, detail(err, projects.ErrManaged))
	case errors.Is(err, projects.ErrInUse):
		fail(w, http.StatusConflict, detail(err, projects.ErrInUse))
	case errors.Is(err, platform.ErrProjectDeleted):
		fail(w, http.StatusConflict, detail(err, platform.ErrProjectDeleted))
	case errors.Is(err, projects.ErrVersion):
		fail(w, http.StatusConflict, detail(err, projects.ErrVersion)+"; reload and try again")
	case errors.Is(err, projects.ErrForbidden):
		fail(w, http.StatusForbidden, detail(err, projects.ErrForbidden))
	case errors.Is(err, projects.ErrConfinement):
		// "members-only projects need read confinement, not built: Globex
		// needs an entry for everyone (*) of at least viewer".
		fail(w, http.StatusConflict, strings.TrimPrefix(err.Error(), "projects: "))
	case errors.Is(err, policy.ErrAdmin):
		fail(w, http.StatusConflict, detail(err, policy.ErrAdmin))
	case errors.Is(err, inbox.ErrLimit):
		fail(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, callpolicy.ErrLimit):
		fail(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, pick.ErrNoAgent):
		fail(w, http.StatusConflict, "no agent fits; tag one")
	case errors.Is(err, platform.ErrNoDefault):
		fail(w, http.StatusConflict, detail(err, platform.ErrNoDefault))
	case errors.Is(err, platform.ErrNotAvailable):
		fail(w, http.StatusForbidden, detail(err, platform.ErrNotAvailable))
	case errors.Is(err, platform.ErrNameTaken):
		fail(w, http.StatusConflict, detail(err, platform.ErrNameTaken))
	case errors.Is(err, people.ErrNotFound):
		fail(w, http.StatusNotFound, "not found")
	case errors.Is(err, people.ErrInvalid):
		fail(w, http.StatusBadRequest, detail(err, people.ErrInvalid))
	case errors.Is(err, platform.ErrSettings), errors.Is(err, platform.ErrModelPolicy):
		fail(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, platform.ErrModelPolicyInUse):
		fail(w, http.StatusConflict, err.Error())
	case errors.Is(err, platform.ErrSourceInUse):
		fail(w, http.StatusConflict, detail(err, platform.ErrSourceInUse))
	case errors.Is(err, platform.ErrNoBaseline):
		fail(w, http.StatusBadRequest, "that setting names an agent that does not exist")
	case errors.Is(err, evals.ErrBusy):
		fail(w, http.StatusConflict, "that suite is already running")
	case errors.Is(err, registry.ErrConflict):
		fail(w, http.StatusConflict, "the agent was revised by someone else, reload and try again")
	case errors.Is(err, platform.ErrForbidden), errors.Is(err, call.ErrDepth), errors.Is(err, call.ErrFanOut), errors.Is(err, call.ErrBudget),
		errors.Is(err, platform.ErrBusy):
		fail(w, http.StatusForbidden, err.Error())
	case errors.Is(err, call.ErrInvalid), errors.Is(err, registry.ErrInvalid), errors.Is(err, agentspec.ErrInvalid),
		errors.Is(err, envelope.ErrInvalid), errors.Is(err, cron.ErrSchedule),
		errors.Is(err, alerts.ErrInvalid), errors.Is(err, knowledge.ErrInvalid), errors.Is(err, evals.ErrInvalid),
		errors.Is(err, prefs.ErrInvalid), errors.Is(err, memory.ErrInvalid), errors.Is(err, policy.ErrInvalid),
		errors.Is(err, inbox.ErrInvalid), errors.Is(err, platform.ErrAction):
		fail(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, platform.ErrSandbox), errors.Is(err, artifacts.ErrInvalid):
		fail(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, memory.ErrSensitive):
		fail(w, http.StatusBadRequest, "that note carries personal data, such as an email address or a card number, so it was not stored")
	case errors.Is(err, platform.ErrStopUnavailable):
		// The run was marked as asked, so it starts nothing more, but the
		// work in flight was not interrupted.
		fail(w, http.StatusServiceUnavailable, "this server cannot interrupt a run; it will start nothing more and ends when its current work does")
	case errors.As(err, &tooBig):
		fail(w, http.StatusRequestEntityTooLarge, "request is too large")
	case errors.Is(err, context.Canceled):
		// The caller went away, as a browser does when a page moves on, and
		// what it asked stopped with it. Nothing went wrong here.
		s.Log.Debug("httpapi: request cancelled", "error", err)
		fail(w, statusClientClosed, "the request was cancelled")
	default:
		s.Log.Error("httpapi: internal error", "error", err)
		fail(w, http.StatusInternalServerError, "something went wrong on our side")
	}
}

// statusClientClosed answers a request its caller cancelled. Nobody reads
// it; it keeps the request out of the server's own errors (5xx).
const statusClientClosed = 499

// detail is what an error says after the error it wraps, for an answer: the
// project store's "projects: in use: "Launch" is still used by the schedule
// nightly" is answered as "Launch" is still used by the schedule nightly. It
// names only what is in the caller's own workspace.
func detail(err, sentinel error) string {
	msg, prefix := err.Error(), sentinel.Error()+": "
	if i := strings.Index(msg, prefix); i >= 0 {
		return msg[i+len(prefix):]
	}
	return sentinel.Error()
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			fail(w, http.StatusRequestEntityTooLarge, "request is too large")
		} else {
			fail(w, http.StatusBadRequest, "request body is not the expected JSON")
		}
		return false
	}
	return true
}

func ok(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	ok(w, status, map[string]string{"error": msg})
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// spa serves a built single-page app, falling back to index.html for routes the
// app handles itself.
func spa(dir string) http.Handler {
	// The web app manifest has its own type, which not every system's table knows.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := filepath.Join(dir, filepath.FromSlash(filepath.Clean("/"+r.URL.Path)))
		if info, err := os.Stat(p); err != nil || info.IsDir() {
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		files.ServeHTTP(w, r)
	})
}

// APITagID is the trigger id of a run a caller started with an idempotency
// key: api-<length of subject>-<subject>-<key>. The subject's length in bytes
// makes it unambiguous within a tenant, so subject "a" with key "b-c" and
// subject "a-b" with key "c" name different runs. A run started without a key
// is api-r-<random>, which no keyed id can be. The run's id is the runtime's,
// derived from this and the tenant.
func APITagID(subject, key string) string {
	return "api-" + strconv.Itoa(len(subject)) + "-" + subject + "-" + key
}
