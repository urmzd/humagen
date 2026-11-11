package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// OldBinEnv names a humagen binary of an earlier release.
// scripts/upgrade-from-release.sh builds one from a tag and sets it.
const OldBinEnv = "HUMAGEN_E2E_OLD_BIN"

// TestUpgradeFromRelease starts an earlier release's `humagen serve` on a
// fresh database, has it do real work (sessions, a finished run, a run
// parked on a question, a schedule, a delivery to GitHub that is still
// pending), stops it, and starts this build on the same database, workspace
// and blobs. This build must come up ready, list the same sessions, read the
// finished run's trace and events as they were, carry the parked run on from
// its answer, raise the schedule, and send nothing a second time.
//
// It runs only when HUMAGEN_E2E_POSTGRES_URL and HUMAGEN_E2E_OLD_BIN are
// set. A fake Anthropic model answers, and the processes are given an HTTPS
// proxy that refuses every connection and counts them, so nothing reaches
// GitHub or any other host.
func TestUpgradeFromRelease(t *testing.T) {
	admin, old := os.Getenv(URLEnv), os.Getenv(OldBinEnv)
	if admin == "" || old == "" {
		t.Skipf("%s and %s are not both set; scripts/upgrade-from-release.sh builds a release and runs this", URLEnv, OldBinEnv)
	}
	dbURL := freshDatabase(t, admin)
	model := newFakeModel(t)
	outside := newRefusingProxy(t)
	r := newReplica(t, "a", old, t.TempDir(), dbURL, model.listen("a"))
	r.env = []string{"HTTPS_PROXY=" + outside.url, "HTTP_PROXY=" + outside.url, "NO_PROXY=127.0.0.1,localhost"}
	r.start()

	// What the release is asked to do.
	for _, body := range []string{
		`{"name":"plain","provider":"anthropic","model":"claude-haiku-5-5","system_prompt":"Answer."}`,
		`{"name":"asker","provider":"anthropic","model":"claude-haiku-5-5","system_prompt":"Ask.","tools":["ask_person"],"answerers":["dev"]}`,
	} {
		r.do(http.MethodPost, "/api/agents", body, http.StatusCreated, nil)
	}
	session := func(agent, text string) (id, run string) {
		t.Helper()
		var v struct {
			ID       string `json:"id"`
			Messages []struct {
				Run *runView `json:"run"`
			} `json:"messages"`
		}
		body, _ := json.Marshal(map[string]string{"agent": agent, "text": text, "project": "~"})
		r.do(http.MethodPost, "/api/sessions", string(body), http.StatusCreated, &v)
		for _, m := range v.Messages {
			if m.Run != nil {
				return v.ID, m.Run.ID
			}
		}
		t.Fatalf("session %s started no run", v.ID)
		return "", ""
	}
	status := func(run string) runView {
		var v runView
		r.do(http.MethodGet, "/api/runs/"+run, "", http.StatusOK, &v)
		return v
	}
	await := func(run, want string) runView {
		t.Helper()
		var v runView
		eventually(t, 60*time.Second, "run "+run+" to be "+want, func() bool {
			v = status(run)
			return v.Status == want
		})
		return v
	}

	// A finished run that answered at once, and one that asked a person and
	// was answered: its trace holds a question and its answer.
	plainSession, plainRun := session("plain", "Say ok.")
	await(plainRun, "completed")
	answeredSession, answeredRun := session("asker", markAsk+" first")
	first := await(answeredRun, "parked")
	r.do(http.MethodPost, "/api/runs/"+answeredRun+"/questions/"+first.Question.ID+"/answer", `{"action":"choose","choice":"red"}`, http.StatusOK, nil)
	await(answeredRun, "completed")
	persisted(t, r, answeredRun)

	// A run parked on a question nobody answers before the upgrade.
	parkedSession, parkedRun := session("asker", markAsk+" second")
	parked := await(parkedRun, "parked")
	if parked.Question.ID == "" {
		t.Fatalf("the parked run carries no question: %+v", parked)
	}
	eventually(t, 15*time.Second, "the question to reach the inbox", func() bool { return openQuestions(r, parkedRun) == 1 })

	// A schedule. It is paused, so the release raises nothing from it and
	// what this build raises after the upgrade is this build's alone.
	r.do(http.MethodPost, "/api/schedules", `{"name":"nightly","expression":"0 3 * * *","agent":"plain","text":"Nightly check.","project":"~"}`, http.StatusCreated, nil)
	r.do(http.MethodPost, "/api/schedules/nightly/pause", "", http.StatusOK, nil)

	// A delivery to GitHub the release could not finish: a row of its sink
	// state that is still pending.
	pendingGitHub := seedPendingDelivery(t, dbURL)

	before := snapshot(t, r, plainSession, answeredSession, parkedSession)
	callsBefore := len(model.callsFor(markAsk)) + len(model.callsFor(""))
	tables := tablesOf(t, dbURL)
	rowsBefore := rowCounts(t, dbURL, tables...)
	t.Logf("seeded by %s: sessions %v; finished runs %s, %s; parked run %s on question %s; schedule nightly (paused); pending GitHub delivery %s; %d tables; rows %v",
		old, []string{plainSession, answeredSession, parkedSession}, plainRun, answeredRun, parkedRun, parked.Question.ID, pendingGitHub, len(tables), rowsBefore)

	// The upgrade: the release stops, and this build starts on what it left.
	r.signal(syscall.SIGTERM)
	if err := r.wait(60 * time.Second); err != nil {
		t.Fatalf("the release did not stop cleanly: %v", err)
	}
	outsideBefore := outside.count()
	r.bin = humagenBinary(t)
	r.start()
	if code := r.readyz(); code != http.StatusOK {
		t.Fatalf("/readyz after the upgrade = %d", code)
	}
	var health bytes.Buffer
	r.raw(http.MethodGet, "/healthz", &health)
	if strings.TrimSpace(health.String()) != "ok" {
		t.Fatalf("/healthz after the upgrade = %q", health.String())
	}

	// Nothing was renamed or dropped: every table the release made is there.
	after := tablesOf(t, dbURL)
	for _, name := range tables {
		if !slices.Contains(after, name) {
			t.Errorf("table %s is gone after the upgrade", name)
		}
	}

	// The sessions, the finished runs' traces, messages and stored events
	// read as the release left them.
	got := snapshot(t, r, plainSession, answeredSession, parkedSession)
	for name, want := range before {
		if got[name] != want {
			t.Errorf("%s changed across the upgrade:\nbefore %s\nafter  %s", name, want, got[name])
		}
	}

	// The parked run is still parked on the same question, which is still
	// open in the inbox, once.
	still := status(parkedRun)
	if still.Status != "parked" || still.Question.ID != parked.Question.ID {
		t.Fatalf("the parked run after the upgrade = %+v, want it parked on %s", still, parked.Question.ID)
	}
	if n := openQuestions(r, parkedRun); n != 1 {
		t.Errorf("the parked run has %d open questions in the inbox after the upgrade, want 1", n)
	}

	// Woken with its question still open, as the worker's poll or an expiry
	// timer wakes a parked run, it announces its park at most once more: a
	// run that parked before the park had a step of its own has no record
	// of the announcement. A second wake finds the step and says nothing,
	// and the question is not raised a second time.
	parksOf := func() int {
		var out struct {
			Events []ev `json:"events"`
		}
		r.do(http.MethodGet, "/api/runs/"+parkedRun+"/events", "", http.StatusOK, &out)
		n := 0
		for _, e := range out.Events {
			if e.Type == "run.parked.v1" {
				n++
			}
		}
		return n
	}
	if n := parksOf(); n != 1 {
		t.Fatalf("the parked run has %d parks before it is woken, want the release's one", n)
	}
	wake(t, dbURL, parkedRun)
	for deadline := time.Now().Add(15 * time.Second); parksOf() < 2 && time.Now().Before(deadline); {
		time.Sleep(100 * time.Millisecond)
	}
	afterWake := parksOf()
	wake(t, dbURL, parkedRun)
	time.Sleep(3 * time.Second)
	if n := parksOf(); n != afterWake || n > 2 {
		t.Errorf("the run announced its park %d times after one wake and %d after two, want at most 2 and no more after the first wake", afterWake, n)
	}
	if woken := status(parkedRun); woken.Status != "parked" || woken.Question.ID != parked.Question.ID {
		t.Fatalf("the woken run = %+v, want it still parked on %s", woken, parked.Question.ID)
	}
	if n := openQuestions(r, parkedRun); n != 1 {
		t.Errorf("the woken run has %d open questions in the inbox, want 1", n)
	}
	t.Logf("woken twice with its question open, the run parked on the release announced its park %d time(s) in all", afterWake)

	// Answered on this build, it carries on from the answer and completes.
	onStream := r.stream(t)
	r.do(http.MethodPost, "/api/runs/"+parkedRun+"/questions/"+parked.Question.ID+"/answer", `{"action":"choose","choice":"blue"}`, http.StatusOK, nil)
	await(parkedRun, "completed")
	persisted(t, r, parkedRun)
	var stored struct {
		Events []struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		} `json:"events"`
	}
	r.do(http.MethodGet, "/api/runs/"+parkedRun+"/events", "", http.StatusOK, &stored)
	// Every attempt after the first opens with a resume; the one that
	// carries on from the answer carries the answer.
	parks, answered := 0, 0
	for _, e := range stored.Events {
		switch e.Type {
		case "run.parked.v1":
			parks++
		case "run.resumed.v1":
			if strings.Contains(string(e.Data), `"choice":"blue"`) {
				answered++
			}
		}
	}
	// The release announced the park. This build announces it at most once
	// more: a run parked before the park's own step existed has no record
	// of it.
	if parks < 1 || parks > 2 || answered != 1 {
		t.Errorf("the run parked before the upgrade has %d parks and %d resumes with the answer, want one or two parks and one resume with the answer", parks, answered)
	}
	if n := openQuestions(r, parkedRun); n != 0 {
		t.Errorf("%d questions of the answered run are still open", n)
	}
	noDuplicates(t, "the stream after the upgrade", onStream.all())

	// The model was asked for the continuation only: the finished runs were
	// not run again, and the parked run's first turn was not repeated.
	if calls := len(model.callsFor(markAsk)) + len(model.callsFor("")); calls != callsBefore+1 {
		t.Errorf("the model was called %d times after the upgrade, want 1 (the parked run's continuation)", calls-callsBefore)
	}

	// The schedule is there, still paused, and runs on this build.
	var listed struct {
		Schedules []struct {
			Name   string `json:"name"`
			Paused bool   `json:"paused"`
			Recent []struct {
				ID string `json:"id"`
			} `json:"recent"`
		} `json:"schedules"`
	}
	r.do(http.MethodGet, "/api/schedules", "", http.StatusOK, &listed)
	schedules := listed.Schedules
	if len(schedules) != 1 || schedules[0].Name != "nightly" || !schedules[0].Paused || len(schedules[0].Recent) != 0 {
		t.Fatalf("schedules after the upgrade = %+v, want nightly, paused, with no run", schedules)
	}
	r.do(http.MethodPost, "/api/schedules/nightly/resume", "", http.StatusOK, nil)
	r.do(http.MethodPost, "/api/schedules/nightly/run", "", http.StatusAccepted, nil)
	eventually(t, 60*time.Second, "the schedule's run to complete on this build", func() bool {
		r.do(http.MethodGet, "/api/schedules", "", http.StatusOK, &listed)
		schedules = listed.Schedules
		return len(schedules) == 1 && len(schedules[0].Recent) == 1 && status(schedules[0].Recent[0].ID).Status == "completed"
	})

	// The pending GitHub delivery is as the release left it: this build
	// read the table and neither dropped the row nor rewrote it, and it
	// sent nothing to GitHub or anywhere else outside.
	if got := pendingDelivery(t, dbURL); got != pendingGitHub {
		t.Errorf("the pending GitHub delivery changed across the upgrade:\nbefore %s\nafter  %s", pendingGitHub, got)
	}
	if n := outside.count() - outsideBefore; n != 0 {
		t.Errorf("this build made %d connections outside after the upgrade: %v", n, outside.hosts())
	}
	rowsAfter := rowCounts(t, dbURL, tables...)
	t.Logf("after the upgrade: rows %v (before %v); outside connections refused: %d before, %d after the upgrade", rowsAfter, rowsBefore, outsideBefore, outside.count()-outsideBefore)
}

// snapshot is what a reader sees of the sessions, by what it is: the session
// list, and each session's messages, trace and its runs' stored events.
func snapshot(t *testing.T, r *replica, sessions ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	type listed struct {
		ID     string `json:"id"`
		Agent  string `json:"agent"`
		Status string `json:"status"`
		Title  string `json:"title"`
	}
	var list struct {
		Sessions []listed `json:"sessions"`
	}
	r.do(http.MethodGet, "/api/sessions?limit=200", "", http.StatusOK, &list)
	slices.SortFunc(list.Sessions, func(a, b listed) int { return strings.Compare(a.ID, b.ID) })
	out["the session list"] = fmt.Sprintf("%+v", list.Sessions)
	for _, id := range sessions {
		var v struct {
			Messages []struct {
				ID  string `json:"id"`
				Run *struct {
					ID     string `json:"id"`
					Status string `json:"status"`
					Answer string `json:"answer"`
				} `json:"run"`
			} `json:"messages"`
		}
		r.do(http.MethodGet, "/api/sessions/"+id, "", http.StatusOK, &v)
		said := make([]string, 0, len(v.Messages))
		finished := true
		for _, m := range v.Messages {
			line := m.ID
			if m.Run != nil {
				line += " run " + m.Run.ID + " " + m.Run.Status + " " + m.Run.Answer
				finished = finished && m.Run.Status == "completed"
			}
			said = append(said, line)
		}
		out["the messages of session "+id] = strings.Join(said, "; ")
		// A run still waiting reports how long it has waited, which moves.
		if finished {
			var trace bytes.Buffer
			r.raw(http.MethodGet, "/api/messages/"+id+"/trace", &trace)
			out["the trace of session "+id] = trace.String()
		}
		for _, m := range v.Messages {
			if m.Run == nil {
				continue
			}
			var events struct {
				Events []struct {
					ID   string          `json:"id"`
					Type string          `json:"type"`
					Seq  uint64          `json:"seq"`
					Data json.RawMessage `json:"data"`
				} `json:"events"`
			}
			r.do(http.MethodGet, "/api/runs/"+m.Run.ID+"/events", "", http.StatusOK, &events)
			out["the stored events of run "+m.Run.ID] = string(must(json.Marshal(events.Events)))
		}
	}
	return out
}

func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}

// openQuestions counts the open inbox messages that ask about a run.
func openQuestions(r *replica, run string) int {
	var page struct {
		Messages []struct {
			Source struct {
				Run string `json:"run"`
			} `json:"source"`
			State string `json:"state"`
		} `json:"messages"`
	}
	r.do(http.MethodGet, "/api/inbox?state=open&limit=100", "", http.StatusOK, &page)
	n := 0
	for _, m := range page.Messages {
		if m.Source.Run == run && m.State == "open" {
			n++
		}
	}
	return n
}

// seedPendingDelivery writes one pending row to the sink state table the
// release created, keyed as its GitHub sink keys a run's message on an
// issue, and returns the row as stored. The release cannot be made to write
// one itself here: its GitHub client reaches only api.github.com, which this
// test never calls.
func seedPendingDelivery(t *testing.T, dbURL string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	identity := `{"Tenant":"default","Scope":"github:upgrade-check","Run":"r00000000000000000000000000000000","Channel":"github://acme/web/issues/7","Target":"github://acme/web/issues/7"}`
	if _, err := conn.Exec(ctx, `INSERT INTO fiber_sink_state (tenant, key, scope, identity, pending, state) VALUES ($1, $2, $3, $4::jsonb, true, $5)`,
		"default", "upgrade-check-pending-delivery", "github:upgrade-check", identity, []byte(`{"comment":0,"text":"An answer that was not posted yet."}`)); err != nil {
		t.Fatalf("seed a pending delivery: %v", err)
	}
	return pendingDelivery(t, dbURL)
}

func pendingDelivery(t *testing.T, dbURL string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var row string
	if err := conn.QueryRow(ctx, `SELECT tenant || '|' || key || '|' || scope || '|' || identity::text || '|' || pending::text || '|' || encode(state, 'escape') || '|' || updated_at::text
 FROM fiber_sink_state WHERE key = 'upgrade-check-pending-delivery'`).Scan(&row); err != nil {
		t.Fatalf("read the pending delivery: %v", err)
	}
	return row
}

// wake makes a parked run due, as the worker's poll does when it comes round
// to it.
func wake(t *testing.T, dbURL, run string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	tag, err := conn.Exec(ctx, `UPDATE fiber_run_queue SET ready_at = now(), lease_until = NULL WHERE run_id = $1`, run)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("wake %s: %v (%d rows)", run, err, tag.RowsAffected())
	}
}

// tablesOf lists the tables of the database's current schema.
func tablesOf(t *testing.T, dbURL string) []string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	rows, err := conn.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = current_schema() ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func rowCounts(t *testing.T, dbURL string, tables ...string) map[string]int {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	out := map[string]int{}
	for _, name := range tables {
		var n int
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{name}.Sanitize()).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", name, err)
		}
		if n > 0 {
			out[name] = n
		}
	}
	return out
}
