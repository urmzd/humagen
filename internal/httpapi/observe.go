package httpapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/envelope"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/journal"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
)

// maxWindow bounds how far back analytics and the journal may be asked for.
const maxWindow = 30 * 24 * time.Hour

func (s *Server) observeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/analytics", s.authed(auth.RunsRead, s.analytics))
	mux.HandleFunc("GET /api/journal", s.authed(auth.RunsRead, s.journal))
	mux.HandleFunc("GET /api/runs/{id}/trace", s.authed(auth.RunsRead, s.seenRun(s.trace)))
	mux.HandleFunc("GET /api/runs/{id}/events", s.authed(auth.RunsRead, s.seenRun(s.runEvents)))
	mux.HandleFunc("GET /api/runs/{id}/manifest", s.authed(auth.RunsRead, s.seenRun(s.manifest)))

	mux.HandleFunc("GET /api/alerts", s.authed(auth.AlertsRead, s.listAlerts))
	mux.HandleFunc("POST /api/alerts/{id}/ack", s.authed(auth.AlertsWrite, s.seenAlert(s.ackAlert)))
	mux.HandleFunc("GET /api/alert-rules", s.authed(auth.AlertsRead, s.listRules))
	mux.HandleFunc("POST /api/alert-rules", s.authed(auth.AlertsWrite, s.createRule))
	mux.HandleFunc("PUT /api/alert-rules/{name}", s.authed(auth.AlertsWrite, s.seenRule(s.updateRule)))
	mux.HandleFunc("DELETE /api/alert-rules/{name}", s.authed(auth.AlertsWrite, s.seenRule(s.deleteRule)))

	mux.HandleFunc("GET /api/evals", s.authed(auth.EvalsRead, s.listSuites))
	mux.HandleFunc("POST /api/evals", s.authed(auth.EvalsWrite, s.putSuite(false)))
	mux.HandleFunc("PUT /api/evals/{name}", s.authed(auth.EvalsWrite, s.seenSuite(s.putSuite(true))))
	mux.HandleFunc("DELETE /api/evals/{name}", s.authed(auth.EvalsWrite, s.seenSuite(s.deleteSuite)))
	mux.HandleFunc("POST /api/evals/{name}/run", s.authed(auth.EvalsWrite, s.seenSuite(s.runSuite)))
	mux.HandleFunc("GET /api/evals/{name}/runs", s.authed(auth.EvalsRead, s.seenSuite(s.suiteRuns)))

	mux.HandleFunc("GET /api/knowledge", s.authed(auth.KnowledgeRead, s.listSources))
	mux.HandleFunc("POST /api/knowledge", s.authed(auth.KnowledgeWrite, s.createSource))
	mux.HandleFunc("GET /api/knowledge/search", s.authed(auth.KnowledgeRead, s.searchKnowledge))
	mux.HandleFunc("DELETE /api/knowledge/{name}", s.authed(auth.KnowledgeWrite, s.deleteSource))
	mux.HandleFunc("GET /api/knowledge/{name}/documents", s.authed(auth.KnowledgeRead, s.listDocuments))
	mux.HandleFunc("POST /api/knowledge/{name}/documents", s.authed(auth.KnowledgeWrite, s.addDocument))
	mux.HandleFunc("DELETE /api/knowledge/{name}/documents/{id}", s.authed(auth.KnowledgeWrite, s.removeDocument))
	mux.HandleFunc("PUT /api/knowledge/{name}/documents/{id}", s.authed(auth.KnowledgeWrite, s.excludeDocument))
	mux.HandleFunc("POST /api/knowledge/{name}/variants", s.authed(auth.KnowledgeWrite, s.saveVariant))
	mux.HandleFunc("DELETE /api/knowledge/{name}/variants/{variant}", s.authed(auth.KnowledgeWrite, s.deleteVariant))
}

// window reads ?window= as a duration ending now. It defaults to an hour.
func window(r *http.Request) (since, until time.Time, ok bool) {
	d := time.Hour
	if raw := r.URL.Query().Get("window"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 || parsed > maxWindow {
			return since, until, false
		}
		d = parsed
	}
	until = time.Now()
	return until.Add(-d), until, true
}

func (s *Server) analytics(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	since, until, valid := window(r)
	if !valid {
		fail(w, http.StatusBadRequest, "window is a duration up to 720h, such as 1h or 24h")
		return
	}
	bucket := until.Sub(since) / 60
	if raw := r.URL.Query().Get("bucket"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			fail(w, http.StatusBadRequest, "bucket is a duration, such as 1m")
			return
		}
		bucket = parsed
	}
	// Only what the caller may read is counted. A run names the agent that
	// ran it, so by_agent and slowest name it, even one moved since into a
	// project the caller may not read.
	summary, err := s.P.Runs.SummarizeWhere(who.Tenant, runs.SummaryQuery{
		Since: since, Until: until.Add(time.Nanosecond), Bucket: bucket, AllProjects: true, Keep: s.P.Seer(who).Run,
	})
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, summary)
}

// journal answers "what happened": events in the caller's tenant, narrowed by
// agent, run, kind, origin, text and time, newest first.
func (s *Server) journal(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	q := r.URL.Query()
	since, until, valid := window(r)
	if !valid {
		fail(w, http.StatusBadRequest, "window is a duration up to 720h, such as 1h or 24h")
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	entries := s.P.Journal.Query(journal.Query{
		Tenant: who.Tenant, Agent: q.Get("agent"), RunID: q.Get("run"), Kind: q.Get("kind"),
		Origin: q.Get("origin"), Contains: q.Get("q"), Since: since, Until: until, Limit: limit,
	})
	// An event of a run the caller may not read is left out, as is one of
	// an agent that lives in a project they cannot see.
	seer, verdict := s.P.Seer(who), map[string]bool{}
	entries = slices.DeleteFunc(entries, func(e journal.Entry) bool {
		if e.RunID == "" {
			return !seer.Agent(e.Agent)
		}
		seen, known := verdict[e.RunID]
		if !known {
			seen = seer.RunID(e.RunID)
			verdict[e.RunID] = seen
		}
		return !seen
	})
	ok(w, http.StatusOK, map[string]any{"entries": orEmpty(entries)})
}

// trace answers "what happened in this run": the run, every run under the same
// root, and the spans inside each.
func (s *Server) trace(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	tree, err := s.P.Runs.Tree(who.Tenant, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ids := make([]string, len(tree))
	for i, run := range tree {
		ids[i] = run.ID
	}
	ok(w, http.StatusOK, map[string]any{"runs": s.P.ShownRuns(who.Tenant, tree), "spans": orEmpty(s.P.Journal.Trace(who.Tenant, ids))})
}

// runEvents returns one run's events as they were published, oldest first, for
// showing a run that is no longer streaming. It is the same shape the event
// stream sends.
func (s *Server) runEvents(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	run, err := s.P.Runs.Get(who.Tenant, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	events := s.P.Journal.Events(who.Tenant, []string{run.ID})
	ok(w, http.StatusOK, map[string]any{"events": shownEvents(s.P.Names(who.Tenant), events), "complete": complete(events)})
}

// complete reports whether a run's events are all still held: the first one
// kept is the first one published.
func complete(events []envelope.Event) bool {
	return len(events) > 0 && events[0].Kind == envelope.KindAccepted
}

// manifest is one document that says everything the platform recorded about a
// run and every run under the same root: who ran, at which revision, what each
// was asked and answered, and every event in order with its body decoded.
func (s *Server) manifest(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	tree, err := s.P.Runs.Tree(who.Tenant, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	type event struct {
		Seq  uint64          `json:"seq"`
		At   time.Time       `json:"at"`
		Kind envelope.Kind   `json:"kind"`
		Body json.RawMessage `json:"body,omitempty"`
	}
	type entry struct {
		Run runs.Run `json:"run"`
		// State is where the run stands, as the platform knows it:
		// queued, running, waiting, stopping, failed, stopped or done.
		// Stopping is a run that was asked to stop and has not ended yet.
		State string `json:"state"`
		// Cause is why a failed run failed, in one line.
		Cause string `json:"cause,omitempty"`
		// Definition is the agent revision the run was pinned to.
		Definition *agentspec.Revision `json:"definition,omitempty"`
		DurationMs int64               `json:"duration_ms"`
		Events     []event             `json:"events"`
		// Complete is false when the run's first events are no longer held.
		Complete bool `json:"complete"`
	}
	states := map[runs.Status]string{
		runs.Accepted: "queued", runs.Running: "running", runs.Parked: "waiting", runs.Failed: "failed", runs.Completed: "done", runs.Stopped: "stopped",
	}
	out := make([]entry, 0, len(tree))
	seer := s.P.Seer(who)
	for _, run := range tree {
		e := entry{Run: s.P.ShownRun(who.Tenant, run), State: states[run.Status], DurationMs: run.UpdatedAt.Sub(run.CreatedAt).Milliseconds(), Events: []event{}}
		if run.Stopping {
			e.State = "stopping"
		}
		if run.Status == runs.Failed {
			e.Cause = journal.Cause(run.Error)
		}
		if run.Status == runs.Stopped {
			e.Cause = run.Cause
		}
		// The definition of an agent the caller may not read, one moved
		// since into a project they may not read, is left out.
		if rev, err := s.P.Agents.Get(r.Context(), who.Tenant, run.Agent, run.Rev); err == nil && seer.Agent(run.Agent) {
			shown := seer.Revision(rev)
			e.Definition = &shown
		}
		held := s.P.Journal.Events(who.Tenant, []string{run.ID})
		e.Complete = complete(held)
		for _, ev := range held {
			item := event{Seq: ev.Seq, At: ev.At, Kind: ev.Kind}
			if json.Valid(ev.Payload) {
				item.Body = ev.Payload
			}
			e.Events = append(e.Events, item)
		}
		out = append(out, e)
	}
	s.Log.Info("httpapi: run exported", "tenant", who.Tenant, "run", r.PathValue("id"), "runs", len(out), "by", who.Subject)
	ok(w, http.StatusOK, map[string]any{
		"version":      1,
		"generated_at": time.Now().UTC(),
		"root":         tree[0].Root,
		"runs":         out,
		// The whole tree's usage: each run's own calls, once, attached
		// and delegated runs alike.
		"usage": map[string]any{"metered": true, "total": treeUsage(tree)},
	})
}

// treeUsage is what every run in a tree used of its own. Each run's own calls
// are counted in it once, so the sum is the tree's, attached and delegated
// runs alike, with nothing counted twice.
func treeUsage(tree []runs.Run) payload.Usage {
	var total payload.Usage
	for _, r := range tree {
		if r.Usage != nil {
			total = total.Add(r.Usage.Own)
		}
	}
	return total
}

// alertView is an alert and what whoever acknowledged it is called. The
// alert names them by subject; the console shows the name.
type alertView struct {
	alerts.Alert
	AckedByName string `json:"acked_by_name,omitempty"`
}

// ruleView is a rule and what the person who made it is called.
type ruleView struct {
	alerts.Rule
	CreatedByName string `json:"created_by_name,omitempty"`
}

func viewRule(names platform.Names, r alerts.Rule) ruleView {
	return ruleView{Rule: r, CreatedByName: names.Of(r.CreatedBy)}
}

func (s *Server) listAlerts(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	seer := s.P.Seer(who)
	names := s.P.Names(who.Tenant)
	out := []alertView{}
	for _, a := range s.P.Alerts.Alerts(who.Tenant) {
		if seer.Alert(a) {
			out = append(out, alertView{Alert: a, AckedByName: names.Of(a.AckedBy)})
		}
	}
	ok(w, http.StatusOK, map[string]any{"alerts": out})
}

func (s *Server) ackAlert(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	a, err := s.P.AckAlert(who.Tenant, r.PathValue("id"), who.Subject)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, alertView{Alert: a, AckedByName: s.P.NameOf(who.Tenant, a.AckedBy)})
}

func (s *Server) listRules(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	seer := s.P.Seer(who)
	names := s.P.Names(who.Tenant)
	out := []ruleView{}
	for _, r := range s.P.Alerts.Rules(who.Tenant) {
		if seer.Rule(r) {
			out = append(out, viewRule(names, r))
		}
	}
	ok(w, http.StatusOK, map[string]any{"rules": out})
}

func (s *Server) createRule(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	// A rule as it was read can be sent back: what its maker is called is
	// ignored, as who made it is.
	var body ruleView
	if !decode(w, r, &body) {
		return
	}
	rule := body.Rule
	if !s.ruleScopeExists(w, r, who, &rule) {
		return
	}
	rule.CreatedBy, rule.CreatedAt = who.Subject, time.Now().UTC()
	rule.UpdatedBy, rule.UpdatedAt = "", time.Time{}
	if err := s.filed(who.Tenant, rule.Project, func() error { return s.P.Alerts.AddRule(who.Tenant, rule) }); err != nil {
		s.problem(w, err)
		return
	}
	stored, err := s.P.Alerts.Rule(who.Tenant, rule.Name)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, viewRule(s.P.Names(who.Tenant), stored))
}

// updateRule replaces a rule. Its name cannot change, and who created it is
// kept. The body is checked exactly as a new rule is.
func (s *Server) updateRule(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	name := r.PathValue("name")
	var body ruleView
	if !decode(w, r, &body) {
		return
	}
	rule := body.Rule
	// Another workspace's rule, or one that does not exist, is not found
	// before what the body names is looked up.
	if _, err := s.P.Alerts.Rule(who.Tenant, name); err != nil {
		s.problem(w, err)
		return
	}
	if rule.Name == "" {
		rule.Name = name
	}
	if rule.Name != name {
		fail(w, http.StatusBadRequest, "a rule's name cannot change; delete it and create another")
		return
	}
	if !s.ruleScopeExists(w, r, who, &rule) {
		return
	}
	rule.UpdatedBy, rule.UpdatedAt = who.Subject, time.Now().UTC()
	var stored alerts.Rule
	err := s.filed(who.Tenant, rule.Project, func() error {
		var err error
		stored, err = s.P.Alerts.ReplaceRule(who.Tenant, rule)
		return err
	})
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, viewRule(s.P.Names(who.Tenant), stored))
}

// ruleScopeExists checks that the agent and the project a rule is scoped to
// exist in the caller's tenant now, and that the caller may read them, and
// answers 404 when one does not, or is hidden from them, alike. A rule filed
// in a project takes a member of it, as other work filed there does
// (MayFileIn). A rule on "~", the workspace, watches no one project, as one
// with no project does, so it is stored with none.
func (s *Server) ruleScopeExists(w http.ResponseWriter, r *http.Request, who auth.Principal, rule *alerts.Rule) bool {
	rule.Project = projects.NameOf(rule.Project)
	seer := s.P.Seer(who)
	if rule.Agent != "" {
		if _, err := s.P.Agents.Get(r.Context(), who.Tenant, rule.Agent, 0); err != nil || !seer.Agent(rule.Agent) {
			notFound(w)
			return false
		}
	}
	if rule.Project != "" && (!s.P.HasProject(who.Tenant, rule.Project) || !seer.Project(rule.Project)) {
		s.problem(w, platform.ErrProjectNotFound)
		return false
	}
	if err := s.P.MayFileIn(who, rule.Project); err != nil {
		s.problem(w, err)
		return false
	}
	return true
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.Alerts.RemoveRule(who.Tenant, r.PathValue("name")); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listSources(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	ok(w, http.StatusOK, map[string]any{"sources": s.P.Knowledge.List(who.Tenant)})
}

func (s *Server) createSource(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decode(w, r, &body) {
		return
	}
	src, err := s.P.Knowledge.Create(who.Tenant, body.Name, body.Description, who.Subject, time.Now())
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, src)
}

func (s *Server) deleteSource(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.DeleteSource(who, r.PathValue("name")); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listDocuments(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	docs, err := s.P.Knowledge.Documents(who.Tenant, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, map[string]any{"documents": docs})
}

func (s *Server) addDocument(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Title string `json:"title"`
		URI   string `json:"uri"`
		Text  string `json:"text"`
	}
	if !decode(w, r, &body) {
		return
	}
	doc, err := s.P.Knowledge.Add(who.Tenant, r.PathValue("name"), body.Title, body.URI, body.Text, who.Subject, time.Now())
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, doc)
}

func (s *Server) removeDocument(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.Knowledge.Remove(who.Tenant, r.PathValue("name"), r.PathValue("id"), time.Now()); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) excludeDocument(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Excluded bool `json:"excluded"`
	}
	if !decode(w, r, &body) {
		return
	}
	doc, err := s.P.Knowledge.SetExcluded(who.Tenant, r.PathValue("name"), r.PathValue("id"), body.Excluded, time.Now())
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, doc)
}

func (s *Server) saveVariant(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &body) {
		return
	}
	v, err := s.P.Knowledge.SaveVariant(who.Tenant, r.PathValue("name"), body.Name, time.Now())
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, v)
}

func (s *Server) deleteVariant(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.Knowledge.DeleteVariant(who.Tenant, r.PathValue("name"), r.PathValue("variant")); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) searchKnowledge(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	q := r.URL.Query()
	var names []string
	if raw := q.Get("source"); raw != "" {
		names = strings.Split(raw, ",")
	}
	hits, err := s.P.Knowledge.Search(who.Tenant, names, q.Get("q"), 0, q.Get("variant"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, map[string]any{"hits": hits})
}

// suiteView is a suite with its most recent runs, newest first. Results are
// left off all but the latest, so listing suites stays small.
type suiteView struct {
	evals.Suite
	Runs []evals.Run `json:"runs"`
}

func (s *Server) listSuites(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	out := []suiteView{}
	seer := s.P.Seer(who)
	for _, suite := range s.P.Evals.List(who.Tenant) {
		if !seer.Suite(suite) {
			continue
		}
		runs, _ := s.P.Evals.Runs(who.Tenant, suite.Name)
		if len(runs) > 20 {
			runs = runs[:20]
		}
		for i := range runs {
			if i > 0 {
				runs[i].Results = []evals.Result{}
			}
		}
		out = append(out, suiteView{Suite: suite, Runs: orEmpty(runs)})
	}
	ok(w, http.StatusOK, map[string]any{"suites": out})
}

func (s *Server) putSuite(replace bool) handler {
	return func(w http.ResponseWriter, r *http.Request, who auth.Principal) {
		// A suite that is not there is said so before the body is read, as
		// one the caller may not see is.
		if replace {
			if _, err := s.P.Evals.Get(who.Tenant, r.PathValue("name")); err != nil {
				s.problem(w, err)
				return
			}
		}
		var suite evals.Suite
		if !decode(w, r, &suite) {
			return
		}
		if replace && suite.Name != r.PathValue("name") {
			fail(w, http.StatusBadRequest, "the suite's name must match the suite being changed")
			return
		}
		// What a suite tests must exist in the caller's tenant now.
		var err error
		switch suite.Target {
		case evals.Agent:
			if !s.P.Seer(who).Agent(suite.Subject) {
				notFound(w)
				return
			}
			if err = s.P.MayTry(who, suite.Subject); err == nil {
				_, err = s.P.Agents.Get(r.Context(), who.Tenant, suite.Subject, 0)
			}
		case evals.Knowledge:
			_, err = s.P.Knowledge.Documents(who.Tenant, suite.Subject)
		}
		if err != nil {
			s.problem(w, err)
			return
		}
		suite.CreatedBy, suite.UpdatedAt = who.Subject, time.Now()
		if err := s.P.Evals.Put(who.Tenant, suite, replace); err != nil {
			s.problem(w, err)
			return
		}
		status := http.StatusCreated
		if replace {
			status = http.StatusOK
		}
		ok(w, status, suite)
	}
}

func (s *Server) deleteSuite(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.Evals.Delete(who.Tenant, r.PathValue("name")); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// runSuite starts a suite's cases. An agent's are filed where it lives, so a
// project agent's take a member of its home (Platform.MayTry).
func (s *Server) runSuite(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if suite, err := s.P.Evals.Get(who.Tenant, r.PathValue("name")); err == nil && suite.Target == evals.Agent {
		if err := s.P.MayTry(who, suite.Subject); err != nil {
			s.problem(w, err)
			return
		}
	}
	run, err := s.P.StartEval(r.Context(), who.Tenant, r.PathValue("name"), who.Subject)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusAccepted, run)
}

func (s *Server) suiteRuns(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	runs, err := s.P.Evals.Runs(who.Tenant, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, map[string]any{"runs": orEmpty(runs)})
}
