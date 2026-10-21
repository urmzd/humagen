// Package httpapi is the platform's HTTP edge: sign-in, the JSON API, the event
// stream, and the web console's files.
//
// Every /api route authenticates, then checks one permission, then confines
// what it reads and writes to the caller's tenant. The tenant always comes from
// the Principal and never from the request.
package httpapi

import (
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

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/alerts"
	"github.com/urmzd/warp/pkg/artifacts"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/call"
	"github.com/urmzd/warp/pkg/callpolicy"
	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/evals"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/knowledge"
	"github.com/urmzd/warp/pkg/memory"
	"github.com/urmzd/warp/pkg/people"
	"github.com/urmzd/warp/pkg/pick"
	"github.com/urmzd/warp/pkg/policy"
	"github.com/urmzd/warp/pkg/prefs"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
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

	mux.HandleFunc("GET /api/me", s.authed("", s.me))
	// Any signed-in caller may ask, and only about themselves: it takes a
	// permission and no subject.
	mux.HandleFunc("GET /api/access/explain", s.authed("", s.explainAccess))
	mux.HandleFunc("GET /api/agents", s.authed(auth.AgentsRead, s.listAgents))
	mux.HandleFunc("POST /api/agents", s.authed(auth.AgentsWrite, s.createAgent))
	mux.HandleFunc("PUT /api/agents/{name}", s.authed(auth.AgentsWrite, s.reviseAgent))
	mux.HandleFunc("PUT /api/agents/{name}/home", s.authed(auth.AgentsWrite, s.putAgentHome))
	mux.HandleFunc("GET /api/agents/{name}/history", s.authed(auth.AgentsRead, s.agentHistory))
	mux.HandleFunc("GET /api/agents/{name}/call-policy", s.authed(auth.AgentsRead, s.getCallPolicy))
	mux.HandleFunc("PUT /api/agents/{name}/call-policy", s.authed(auth.AgentsWrite, s.putCallPolicy))
	mux.HandleFunc("GET /api/agents/{name}/reachable", s.authed(auth.AgentsRead, s.reachable))
	mux.HandleFunc("GET /api/agents/{name}/memory", s.authed(auth.AgentsRead, s.listMemory))
	mux.HandleFunc("POST /api/agents/{name}/memory", s.authed(auth.AgentsWrite, s.addMemory))
	mux.HandleFunc("DELETE /api/agents/{name}/memory/{id}", s.authed(auth.AgentsWrite, s.removeMemory))
	mux.HandleFunc("POST /api/agents/{name}/memory/approve", s.authed(auth.AgentsWrite, s.approveMemory))
	mux.HandleFunc("GET /api/memory/pending", s.authed(auth.AgentsRead, s.pendingMemory))
	mux.HandleFunc("GET /api/runs", s.authed(auth.RunsRead, s.listRuns))
	mux.HandleFunc("POST /api/runs", s.authed(auth.RunsWrite, s.createRun))
	mux.HandleFunc("GET /api/threads", s.authed(auth.RunsRead, s.listThreads))
	mux.HandleFunc("GET /api/threads/{id}", s.authed(auth.RunsRead, s.getThread))
	mux.HandleFunc("GET /api/pick", s.authed(auth.AgentsRead, s.pickAgent))
	mux.HandleFunc("GET /api/settings", s.authed(auth.AgentsRead, s.getSettings))
	mux.HandleFunc("PUT /api/settings", s.authed(auth.AgentsWrite, s.putSettings))
	mux.HandleFunc("GET /api/runs/{id}", s.authed(auth.RunsRead, s.getRun))
	mux.HandleFunc("GET /api/runs/{id}/tree", s.authed(auth.RunsRead, s.runTree))
	mux.HandleFunc("POST /api/runs/{id}/calls", s.authed(auth.RunsWrite, s.createCall))
	mux.HandleFunc("POST /api/runs/{id}/stop", s.authed(auth.RunsWrite, s.stopRun))
	mux.HandleFunc("POST /api/runs/{id}/questions/{question}/answer", s.authed(auth.RunsWrite, s.answerQuestion))
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
		h(w, r.WithContext(auth.With(r.Context(), who)), who)
	}
}

func (s *Server) me(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	// first_admin says the caller is the one who should set the workspace
	// up: an admin, in a workspace nobody has set up yet.
	first := who.Can(auth.AgentsWrite) && s.P.Settings(who.Tenant).SetupAt.IsZero()
	ok(w, http.StatusOK, map[string]any{"principal": who, "permissions": who.Granted(), "first_admin": first})
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.EnsureBuiltins(r.Context(), who.Tenant); err != nil {
		s.Log.Warn("httpapi: built-in agents", "error", err)
	}
	var all []agentspec.Revision
	page := registry.Page{Size: 100}
	for {
		revs, next, err := s.P.Agents.List(r.Context(), who.Tenant, page)
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
	homes, enabled := map[string]string{}, map[string][]string{}
	for name, place := range s.P.AgentPlaces(who.Tenant, names) {
		if place.Home != "" {
			homes[name] = place.Home
		}
		enabled[name] = place.EnabledIn
	}
	ok(w, http.StatusOK, map[string]any{"agents": orEmpty(all), "builtin": platform.Builtins(), "homes": homes, "enabled_in": enabled})
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
	rev, err := s.P.Agents.Create(r.Context(), who.Tenant, spec, who.Subject)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, rev)
}

func (s *Server) reviseAgent(w http.ResponseWriter, r *http.Request, who auth.Principal) {
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
	if err := s.P.CheckAgentModel(who.Tenant, body.Spec); err != nil {
		s.problem(w, err)
		return
	}
	rev, err := s.P.Agents.Revise(r.Context(), who.Tenant, body.Spec, body.ExpectedRev, who.Subject)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, rev)
}

// agentHistory returns every revision of an agent, newest first.
func (s *Server) agentHistory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	revs, err := s.P.Agents.History(r.Context(), who.Tenant, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	slices.SortFunc(revs, func(a, b agentspec.Revision) int { return b.Rev - a.Rev })
	ok(w, http.StatusOK, map[string]any{"revisions": orEmpty(revs)})
}

func (s *Server) listMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if _, err := s.P.Agents.Get(r.Context(), who.Tenant, r.PathValue("name"), 0); err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, map[string]any{"notes": orEmpty(s.P.Memory.List(who.Tenant, r.PathValue("name")))})
}

func (s *Server) addMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &body) {
		return
	}
	if _, err := s.P.Agents.Get(r.Context(), who.Tenant, r.PathValue("name"), 0); err != nil {
		s.problem(w, err)
		return
	}
	// A person writing a note is the approval.
	note, err := s.P.Memory.Add(who.Tenant, r.PathValue("name"), body.Text, memory.Person, memory.Approved, who.Subject, time.Now())
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, note)
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
	if _, err := s.P.Agents.Get(r.Context(), who.Tenant, r.PathValue("name"), 0); err != nil {
		s.problem(w, err)
		return
	}
	n, err := s.P.ApproveMemory(who.Tenant, r.PathValue("name"), body.IDs)
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
		Agent   string      `json:"agent,omitempty"`
		Project string      `json:"project,omitempty"`
		Note    memory.Note `json:"note"`
	}
	out := []proposal{}
	for _, p := range s.P.Memory.Pending(who.Tenant) {
		if project, isProject := memory.OwnerProject(p.Agent); isProject {
			// The workspace's own notes are the root's, named as a path
			// names it.
			if project == projects.Root {
				project = projects.RootAddress
			}
			out = append(out, proposal{Project: project, Note: p.Note})
		} else {
			out = append(out, proposal{Agent: p.Agent, Note: p.Note})
		}
	}
	ok(w, http.StatusOK, map[string]any{"proposals": out})
}

func (s *Server) removeMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.RemoveMemory(who.Tenant, r.PathValue("name"), r.PathValue("id")); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listRuns(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	ok(w, http.StatusOK, map[string]any{"runs": orEmpty(s.P.Runs.List(who.Tenant, 200))})
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		// Agent is who should answer. "auto" is @auto, which picks among the
		// agents available where the conversation is filed. Empty is the
		// default the conversation's place inherits; with none, 409.
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
	actor := envelope.Actor{ID: who.Subject, Display: who.Name, Bot: who.Kind == auth.Machine}
	if body.ActAs != nil {
		person, err := s.P.ActsFor(who, *body.ActAs)
		if err != nil {
			s.problem(w, err)
			return
		}
		actor = envelope.Actor{ID: person, Display: person}
	}
	// A caller that retries sends the same key and joins the run it started.
	id := "api-r-" + token()[:22]
	if key := r.Header.Get("Idempotency-Key"); key != "" {
		id = APITagID(who.Subject, key)
	}
	run, err := s.P.Converse(r.Context(), who.Tenant, platform.Turn{
		ID: id, Agent: body.Agent, Text: body.Text, After: body.After, Project: body.Project, Actor: actor,
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
	if query.Projects, found = s.projectsWithin(w, who.Tenant, q.Get("project"), q.Get("within")); !found {
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
	page, next, err := s.P.ThreadPage(who.Tenant, query)
	if err != nil {
		fail(w, http.StatusBadRequest, "before is not a cursor this server issued")
		return
	}
	var cursor *string
	if next != "" {
		cursor = &next
	}
	ok(w, http.StatusOK, map[string]any{"threads": page, "next": cursor})
}

// pickAgent previews which agent @auto would pick for a message in a project,
// "~" or nothing for the workspace, and why.
func (s *Server) pickAgent(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	q := r.URL.Query()
	choice, err := s.P.Pick(r.Context(), who.Tenant, q.Get("project"), q.Get("q"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, choice)
}

func (s *Server) getSettings(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	ok(w, http.StatusOK, s.P.Settings(who.Tenant))
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
	if err := s.P.SetSettings(r.Context(), who.Tenant, next); err != nil {
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
	ok(w, http.StatusOK, saved)
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	run, err := s.P.Runs.Get(who.Tenant, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, run)
}

func (s *Server) runTree(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	tree, err := s.P.Runs.Tree(who.Tenant, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, map[string]any{"runs": tree})
}

func (s *Server) createCall(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Key   string      `json:"key"`
		Agent string      `json:"agent"`
		Text  string      `json:"text"`
		Mode  call.Mode   `json:"mode"`
		Lease int64       `json:"lease"`
		Input *call.Input `json:"input,omitempty"`
	}
	if !decode(w, r, &body) {
		return
	}
	run, err := s.P.Call(r.Context(), who.Tenant, r.PathValue("id"), call.Request{Key: body.Key, Agent: body.Agent, Text: body.Text, Input: body.Input, Mode: body.Mode, Lease: body.Lease})
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusAccepted, run)
}

// stopRun asks a run and everything attached to it to stop. Runs it delegated
// keep going. It does not wait: a run that has not ended yet is answered 202
// with stopping set, and its stream says how it ended. A run that has ended is
// answered 200 as it is, so asking twice is safe.
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
	// The watch starts before the answer does, so a change made as soon as
	// the caller has the stream is on it.
	notices, unwatch := s.P.Inbox.Watch(who.Tenant)
	defer unwatch()
	// So do threads: a notice names a thread and the message posted or
	// settled. Every thread is shared until reads are confined, so whoever
	// may read conversations is told of each.
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

	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case n := <-notices:
			if !platform.InboxSees(who, n.Kind) {
				continue
			}
			b, err := json.Marshal(n)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: inbox\ndata: %s\n\n", b); err != nil {
				return
			}
		case n := <-posts:
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
	case errors.Is(err, runs.ErrNotFound), errors.Is(err, registry.ErrNotFound), errors.Is(err, platform.ErrScheduleNotFound),
		errors.Is(err, platform.ErrProjectNotFound),
		errors.Is(err, alerts.ErrNotFound), errors.Is(err, knowledge.ErrNotFound), errors.Is(err, evals.ErrNotFound),
		errors.Is(err, memory.ErrNotFound), errors.Is(err, policy.ErrNotFound), errors.Is(err, inbox.ErrNotFound),
		errors.Is(err, artifacts.ErrNotFound), errors.Is(err, clients.ErrNotFound):
		fail(w, http.StatusNotFound, "not found")
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
	default:
		s.Log.Error("httpapi: internal error", "error", err)
		fail(w, http.StatusInternalServerError, "something went wrong on our side")
	}
}

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
