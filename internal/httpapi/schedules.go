package httpapi

import (
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/urmzd/fiber/pkg/connectors/cron"
	"github.com/urmzd/fiber/pkg/envelope"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
)

// schedule is the wire form of a cron schedule. Tenant is absent on purpose.
type schedule struct {
	Name       string `json:"name"`
	Expression string `json:"expression"`
	Agent      string `json:"agent"`
	Text       string `json:"text"`
	// Project files every run the schedule raises under a project.
	Project string `json:"project,omitempty"`
	// Description says what the schedule is for. The agent is not shown it.
	Description string `json:"description,omitempty"`
	// Deliver is where the answer of each run goes: nowhere (""), the
	// creator's inbox, or the project's.
	Deliver string `json:"deliver,omitempty"`
}

// scheduleView is a schedule with what a person needs to trust it: whether it
// is running, when it fires next, and how its recent occurrences went.
type scheduleView struct {
	schedule
	Paused bool `json:"paused"`
	// Managed is "file" while the platform file declares the schedule: it
	// can be paused, resumed and run, not changed or deleted.
	Managed   string      `json:"managed,omitempty"`
	CreatedBy string      `json:"created_by,omitempty"`
	UpdatedBy string      `json:"updated_by,omitempty"`
	UpdatedAt time.Time   `json:"updated_at,omitzero"`
	Next      []time.Time `json:"next"`
	Recent    []runs.Run  `json:"recent"`
}

func (s *Server) viewOf(sc cron.Schedule, now time.Time) scheduleView {
	about := s.P.Schedules.About(sc.Name)
	v := scheduleView{
		schedule: schedule{
			Name: sc.Name, Expression: sc.Expression, Agent: sc.Agent, Text: sc.Text,
			Project: sc.Meta[runs.MetaProject], Description: about.Description, Deliver: sc.Meta[platform.MetaDeliver],
		},
		Paused:    s.P.Schedules.Paused(sc.Name),
		Managed:   about.Managed,
		CreatedBy: sc.Meta["created_by"],
		UpdatedBy: about.UpdatedBy,
		UpdatedAt: about.UpdatedAt,
		Next:      []time.Time{},
		Recent:    orEmpty(s.P.Runs.ForSchedule(sc.Tenant, sc.Name, 20)),
	}
	if !v.Paused {
		v.Next = orEmpty(platform.Upcoming(sc.Expression, now, 3))
	}
	return v
}

func (s *Server) listSchedules(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	now := time.Now()
	out := []scheduleView{}
	for _, sc := range s.P.Schedules.List(who.Tenant) {
		out = append(out, s.viewOf(sc, now))
	}
	ok(w, http.StatusOK, map[string]any{"schedules": out})
}

func (s *Server) createSchedule(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body schedule
	if !decode(w, r, &body) {
		return
	}
	meta, valid := s.scheduleMeta(w, r, who, body, map[string]string{"created_by": who.Subject})
	if !valid {
		return
	}
	sc := cron.Schedule{
		Name: body.Name, Expression: body.Expression, Agent: body.Agent, Text: body.Text,
		Tenant: who.Tenant,
		Meta:   meta,
	}
	if err := s.filed(who.Tenant, meta[runs.MetaProject], func() error {
		return s.P.Schedules.Add(sc, platform.About{Description: strings.TrimSpace(body.Description), CreatedAt: time.Now().UTC()})
	}); err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, s.viewOf(sc, time.Now()))
}

// updateSchedule changes what a schedule runs and when. Its name cannot
// change. Whether it is paused, who created it and the runs it raised are
// kept: they are found by its name.
func (s *Server) updateSchedule(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	name := r.PathValue("name")
	var body schedule
	if !decode(w, r, &body) {
		return
	}
	// Another workspace's schedule, or one that does not exist, is not
	// found before what the body names is looked up.
	old, found := s.scheduleIn(w, who, name)
	if !found {
		return
	}
	if body.Name == "" {
		body.Name = name
	}
	if body.Name != name {
		fail(w, http.StatusBadRequest, "a schedule's name cannot change; delete it and create another")
		return
	}
	meta, valid := s.scheduleMeta(w, r, who, body, maps.Clone(old.Meta))
	if !valid {
		return
	}
	sc := cron.Schedule{
		Name: name, Expression: body.Expression, Agent: body.Agent, Text: body.Text,
		Tenant: who.Tenant, Routes: old.Routes,
		Meta: meta,
	}
	about := platform.About{
		Description: strings.TrimSpace(body.Description), UpdatedBy: who.Subject, UpdatedAt: time.Now().UTC(),
		CreatedAt: s.P.Schedules.About(name).CreatedAt,
	}
	if err := s.filed(who.Tenant, meta[runs.MetaProject], func() error { return s.P.Schedules.Replace(sc, about) }); err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, s.viewOf(sc, time.Now()))
}

// scheduleMeta checks that what a schedule names exists in the caller's
// tenant now, and returns meta with its project set. A schedule runs an agent
// with nobody present, so a missing agent is refused now rather than failing
// every occurrence later, and so is one not available in the schedule's
// project. "~" is the workspace, which files a schedule in no project.
func (s *Server) scheduleMeta(w http.ResponseWriter, r *http.Request, who auth.Principal, body schedule, meta map[string]string) (map[string]string, bool) {
	if _, err := s.P.Agents.Get(r.Context(), who.Tenant, body.Agent, 0); err != nil {
		s.problem(w, err)
		return nil, false
	}
	if meta == nil {
		meta = map[string]string{}
	}
	delete(meta, runs.MetaProject)
	delete(meta, platform.MetaDeliver)
	switch {
	case !platform.ValidDeliver(body.Deliver):
		fail(w, http.StatusBadRequest, "deliver is inbox, project, or left out to keep the answer on the run")
		return nil, false
	case body.Deliver == platform.DeliverProject && body.Project == "":
		fail(w, http.StatusBadRequest, "deliver: project posts to the schedule's project, so name one, or ~ for the workspace")
		return nil, false
	case body.Deliver != platform.DeliverNone:
		meta[platform.MetaDeliver] = body.Deliver
	}
	if project := projects.NameOf(body.Project); project != projects.Root {
		if !s.P.HasProject(who.Tenant, project) {
			s.problem(w, platform.ErrProjectNotFound)
			return nil, false
		}
		meta[runs.MetaProject] = project
	}
	if !s.inProject(w, who, meta[runs.MetaProject]) {
		return nil, false
	}
	// The agent must be available where the schedule files its runs, or
	// every occurrence would be refused. It is the answer admission would
	// give, given now.
	if err := s.P.Available(who.Tenant, meta[runs.MetaProject], body.Agent); err != nil {
		s.problem(w, err)
		return nil, false
	}
	return meta, true
}

func (s *Server) deleteSchedule(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if _, found := s.scheduleIn(w, who, r.PathValue("name")); !found {
		return
	}
	if err := s.P.Schedules.Remove(who.Tenant, r.PathValue("name")); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) pauseSchedule(paused bool) handler {
	return func(w http.ResponseWriter, r *http.Request, who auth.Principal) {
		name := r.PathValue("name")
		if _, found := s.scheduleIn(w, who, name); !found {
			return
		}
		if err := s.P.Schedules.SetPaused(who.Tenant, name, paused); err != nil {
			s.problem(w, err)
			return
		}
		sc, _, err := s.P.Schedules.Get(who.Tenant, name)
		if err != nil {
			s.problem(w, err)
			return
		}
		ok(w, http.StatusOK, s.viewOf(sc, time.Now()))
	}
}

// runSchedule fires a schedule once, now, on a person's say-so. The run is
// attributed to that person, not to the clock, and works while paused.
func (s *Server) runSchedule(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	sc, found := s.scheduleIn(w, who, r.PathValue("name"))
	if !found {
		return
	}
	now := time.Now()
	run, err := s.P.Invoke(r.Context(), envelope.Tag{
		ID:     "manual-" + token()[:22],
		Tenant: who.Tenant,
		Agent:  sc.Agent,
		Origin: runs.OriginManual,
		Text:   sc.Text,
		Actor:  envelope.Actor{ID: who.Subject, Display: who.Name, Bot: who.Kind == auth.Machine},
		// The run carries what its schedule says now, as a run the clock
		// raises does: its project, where its answer goes, and its creator.
		Meta: map[string]string{
			"cron_schedule": sc.Name, "cron_expression": sc.Expression,
			runs.MetaProject: sc.Meta[runs.MetaProject], platform.MetaDeliver: sc.Meta[platform.MetaDeliver],
			"created_by": sc.Meta["created_by"],
		},
		At: now,
	})
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusAccepted, run)
}

// inProject holds a write on a schedule to its project: the caller needs to be
// a member of the project the schedule is filed in, the workspace's included
// ("" is the workspace, where everyone is a member unless it says otherwise).
// A project the caller cannot see answers as one that does not exist.
func (s *Server) inProject(w http.ResponseWriter, who auth.Principal, project string) bool {
	if _, _, err := s.P.ProjectRole(who, projects.NameOf(project), projects.Member); err != nil {
		s.problem(w, err)
		return false
	}
	return true
}

// scheduleIn is one of the caller's schedules, held to its project.
func (s *Server) scheduleIn(w http.ResponseWriter, who auth.Principal, name string) (cron.Schedule, bool) {
	sc, _, err := s.P.Schedules.Get(who.Tenant, name)
	if err != nil {
		s.problem(w, err)
		return cron.Schedule{}, false
	}
	return sc, s.inProject(w, who, sc.Meta[runs.MetaProject])
}
