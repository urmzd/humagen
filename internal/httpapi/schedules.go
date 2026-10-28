package httpapi

import (
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/urmzd/fiber/pkg/connectors/cron"
	"github.com/urmzd/fiber/pkg/envelope"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
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
	Managed   string `json:"managed,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
	// CreatedByName is what the person who made it is called. CreatedBy
	// names them by subject; the console shows the name.
	CreatedByName string      `json:"created_by_name,omitempty"`
	UpdatedBy     string      `json:"updated_by,omitempty"`
	UpdatedAt     time.Time   `json:"updated_at,omitzero"`
	Next          []time.Time `json:"next"`
	Recent        []runs.Run  `json:"recent"`
}

// viewOf is a schedule as the reader seer stands for reads it. Its recent
// runs are found by its name, so they are those of every schedule that has
// had the name, filed wherever each was then: one moved out of a project, or
// a name used again, keeps them. Each is shown only to a reader who may read
// it where it lives, as GET /api/runs/{id} would.
func (s *Server) viewOf(seer *platform.Seer, sc cron.Schedule, now time.Time) scheduleView {
	about := s.P.Schedules.About(sc.Name)
	v := scheduleView{
		schedule: schedule{
			Name: sc.Name, Expression: sc.Expression, Agent: sc.Agent, Text: sc.Text,
			Project: sc.Meta[runs.MetaProject], Description: about.Description, Deliver: sc.Meta[platform.MetaDeliver],
		},
		Paused:        s.P.Schedules.Paused(sc.Name),
		Managed:       about.Managed,
		CreatedBy:     sc.Meta["created_by"],
		CreatedByName: s.P.NameOf(sc.Tenant, sc.Meta["created_by"]),
		UpdatedBy:     about.UpdatedBy,
		UpdatedAt:     about.UpdatedAt,
		Next:          []time.Time{},
		Recent:        orEmpty(s.P.Runs.ForSchedule(sc.Tenant, sc.Name, 20, seer.Run)),
	}
	if !v.Paused {
		v.Next = orEmpty(platform.Upcoming(sc.Expression, now, 3))
	}
	return v
}

func (s *Server) listSchedules(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	now := time.Now()
	out := []scheduleView{}
	seer := s.P.Seer(who)
	for _, sc := range s.P.Schedules.List(who.Tenant) {
		// A schedule filed in a project the caller cannot see is not theirs
		// to read.
		if !seer.Project(sc.Meta[runs.MetaProject]) {
			continue
		}
		out = append(out, s.viewOf(seer, sc, now))
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
	ok(w, http.StatusCreated, s.viewOf(s.P.Seer(who), sc, time.Now()))
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
	if !s.filedBy(w, who, old) {
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
	ok(w, http.StatusOK, s.viewOf(s.P.Seer(who), sc, time.Now()))
}

// filedBy holds the caller to being a member of the project a stored
// schedule files its runs in, as filing one there did, before they change,
// pause, delete or run it; or answers 404 for a project they cannot see and
// 403 for a viewer.
func (s *Server) filedBy(w http.ResponseWriter, who auth.Principal, sc cron.Schedule) bool {
	if err := s.P.MayFileIn(who, sc.Meta[runs.MetaProject]); err != nil {
		s.problem(w, err)
		return false
	}
	if platform.AssignmentSchedule(sc) {
		fail(w, http.StatusForbidden, "Manage this schedule through its assignment.")
		return false
	}
	return true
}

// scheduleMeta checks that what a schedule names exists in the caller's
// tenant now, and returns meta with its project set. A schedule runs an agent
// with nobody present, so a missing agent is refused now rather than failing
// every occurrence later, and so is one not available in the schedule's
// project. "~" is the workspace, which files a schedule in no project.
// Filing one in a project takes being a member of it, as starting a
// conversation there does. An agent the caller may not read is one that is
// not there.
func (s *Server) scheduleMeta(w http.ResponseWriter, r *http.Request, who auth.Principal, body schedule, meta map[string]string) (map[string]string, bool) {
	if _, err := s.P.Catalog().Get(r.Context(), who.Tenant, body.Agent, 0); err != nil {
		s.problem(w, err)
		return nil, false
	}
	if !s.P.Seer(who).Agent(body.Agent) {
		notFound(w)
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
		if err := s.P.MayFileIn(who, project); err != nil {
			s.problem(w, err)
			return nil, false
		}
		meta[runs.MetaProject] = project
	}
	// The agent must be available where the schedule files its runs, or
	// every occurrence would be refused. It is the answer admission would
	// give, given now.
	if err := s.P.Available(who.Tenant, meta[runs.MetaProject], body.Agent); err != nil {
		// A project deleted since it was looked up is not found, as it
		// would have been a moment earlier.
		if !s.P.HasProject(who.Tenant, meta[runs.MetaProject]) {
			err = platform.ErrProjectNotFound
		}
		s.problem(w, err)
		return nil, false
	}
	return meta, true
}

func (s *Server) deleteSchedule(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	sc, _, err := s.P.Schedules.Get(who.Tenant, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	if !s.filedBy(w, who, sc) {
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
		sc, _, err := s.P.Schedules.Get(who.Tenant, name)
		if err != nil {
			s.problem(w, err)
			return
		}
		if !s.filedBy(w, who, sc) {
			return
		}
		if err := s.P.Schedules.SetPaused(who.Tenant, name, paused); err != nil {
			s.problem(w, err)
			return
		}
		if sc, _, err = s.P.Schedules.Get(who.Tenant, name); err != nil {
			s.problem(w, err)
			return
		}
		ok(w, http.StatusOK, s.viewOf(s.P.Seer(who), sc, time.Now()))
	}
}

// runSchedule fires a schedule once, now, on a person's say-so. The run is
// attributed to that person, not to the clock, and works while paused.
//
// It acts for them only when they wrote what it runs (Schedules.Authored):
// whoever last changed its text, or its creator while nobody has, unless
// someone else has since chosen its agent or its project, whose instructions
// and notes go ahead of its text. Anyone with schedules:write may
// change a schedule's words, so anyone else's press acts for nobody, as the
// schedule's own clock does: their credentials are never put behind words
// they did not write, nor are those words run with the credentials of
// whoever presses next.
func (s *Server) runSchedule(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	sc, author, err := s.P.Schedules.Authored(who.Tenant, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	if !s.filedBy(w, who, sc) {
		return
	}
	actor := platform.ActorOf(who)
	if author != who.Subject {
		actor.User = ""
	}
	now := time.Now()
	run, err := s.P.Invoke(r.Context(), wire.NewInput(envelope.Tag{
		ID:     "manual-" + token()[:22],
		Tenant: who.Tenant,
		Agent:  sc.Agent,
		Origin: runs.OriginManual,
		Actor:  actor,
		// The run carries what its schedule says now, as a run the clock
		// raises does: its project, where its answer goes, and its creator.
		Meta: map[string]string{
			wire.MetaCronSchedule: sc.Name, wire.MetaCronExpression: sc.Expression,
			runs.MetaProject: sc.Meta[runs.MetaProject], platform.MetaDeliver: sc.Meta[platform.MetaDeliver],
			"created_by": sc.Meta["created_by"],
		},
		At: now,
	}, sc.Text))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusAccepted, run)
}

// scheduleIn is one of the caller's schedules, held to its project.
func (s *Server) scheduleIn(w http.ResponseWriter, who auth.Principal, name string) (cron.Schedule, bool) {
	sc, _, err := s.P.Schedules.Get(who.Tenant, name)
	if err != nil {
		s.problem(w, err)
		return cron.Schedule{}, false
	}
	return sc, s.filedBy(w, who, sc)
}
