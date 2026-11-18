package httpapi

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/rooms"
)

// volume is how much seedVolume writes.
type volume struct {
	// Sessions is how many root sessions are started, and Children how many
	// of them get one child session.
	Sessions, Children int
	// Runs is how many runs the sessions hold between them: each session's
	// first message and its follow-ups.
	Runs int
	// Alerts is how many session alerts are raised for the reader, a third
	// of them low priority.
	Alerts int
	// Parallel is how many sessions are written at once.
	Parallel int
}

// seeded counts what seedVolume wrote.
type seeded struct {
	Sessions, Children, Runs, Alerts, Waiting int
}

// seedClock is a clock that starts three weeks back and moves on four seconds
// with every reading, so what is written is spread over days and no limit on
// posting, which counts by the minute, is met.
func seedClock() func() time.Time {
	var ticks atomic.Int64
	base := time.Now().Add(-21 * 24 * time.Hour)
	return func() time.Time { return base.Add(time.Duration(ticks.Add(1)) * 4 * time.Second) }
}

// seedVolume fills a workspace through the platform's own write paths: three
// shared projects, root and child sessions started by the reader and by four
// other people, a run per message answered by the offline model, and session
// alerts addressed to the reader. Every text is generated. It returns when
// every run has ended.
func seedVolume(t testing.TB, p *platform.Platform, reader auth.Principal, v volume) seeded {
	t.Helper()
	ctx := context.Background()
	if _, err := p.Agents.Create(ctx, reader.Tenant, wire.AgentSpec{Name: "helper", Model: "offline", Provider: "offline", SystemPrompt: "Help."}, reader.Subject); err != nil {
		t.Fatal(err)
	}
	people := []auth.Principal{reader}
	for i := range 4 {
		people = append(people, auth.Principal{Subject: fmt.Sprintf("seed-person-%d", i), Tenant: reader.Tenant, Kind: auth.User, Name: fmt.Sprintf("Person %d", i), Roles: []auth.Role{auth.Admin}})
	}
	shared := []string{"launch", "operations", "research"}
	for _, name := range shared {
		if _, err := p.CreateProject(ctx, reader.Tenant, projects.Writer{By: reader.Subject}, platform.ProjectInput{Name: name, Title: strings.ToUpper(name[:1]) + name[1:]}); err != nil {
			t.Fatal(err)
		}
	}
	text := func(session, message int) string {
		words := []string{"review", "the", "quarterly", "plan", "and", "list", "what", "changed", "since", "last", "week", "with", "owners", "dates", "risks", "next", "steps"}
		var b strings.Builder
		fmt.Fprintf(&b, "Generated request %d.%d:", session, message)
		for i := range 8 + (session*7+message*3)%40 {
			b.WriteString(" " + words[(session+message+i)%len(words)])
		}
		return b.String()
	}
	// Each session's share of the runs: most hold a few, every tenth many.
	per := make([]int, v.Sessions+v.Children)
	left := v.Runs - len(per)
	for i := 0; left > 0; i = (i + 1) % len(per) {
		n := 1
		if i%10 == 0 {
			n = 6
		}
		n = min(n, left)
		per[i] += n
		left -= n
	}
	// settle waits until a session's want runs are indexed and none is in
	// flight, by the summary the index keeps: one read a poll.
	settle := func(who auth.Principal, id string, want int) {
		deadline := time.Now().Add(5 * time.Minute)
		for {
			sum, err := p.Runs.ConversationSummary(who.Tenant, id)
			if err == nil && sum.Turns >= want && sum.Active == 0 {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("session %s did not settle", id)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	type made struct {
		id  string
		who auth.Principal
	}
	roots := make([]made, v.Sessions)
	var wg sync.WaitGroup
	sem := make(chan struct{}, v.Parallel)
	var mu sync.Mutex
	var out seeded
	fill := func(i int, who auth.Principal, in platform.SessionInput) string {
		view, err := p.StartSession(ctx, who, in)
		if err != nil {
			t.Errorf("start session %d: %v", i, err)
			return ""
		}
		settle(who, view.ID, 1)
		for m := 1; m <= per[i]; m++ {
			if _, err := p.PostSession(ctx, who, view.ID, platform.MessageInput{Text: text(i, m)}); err != nil {
				t.Errorf("post to session %d: %v", i, err)
				return view.ID
			}
			settle(who, view.ID, 1+m)
		}
		mu.Lock()
		out.Runs += 1 + per[i]
		mu.Unlock()
		return view.ID
	}
	for i := range v.Sessions {
		// Three in five are the reader's; two in five of everyone's are in a
		// shared project, the rest in the person's own.
		who := people[0]
		if i%5 >= 3 {
			who = people[1+i%4]
		}
		in := platform.SessionInput{Agent: "helper", Text: text(i, 0)}
		if i%5 == 1 || i%5 == 4 {
			in.Project = shared[i%len(shared)]
		}
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			roots[i] = made{id: fill(i, who, in), who: who}
		})
	}
	wg.Wait()
	for c := range v.Children {
		parent := roots[(c*3)%len(roots)]
		if parent.id == "" {
			continue
		}
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			fill(v.Sessions+c, parent.who, platform.SessionInput{Agent: "helper", Parent: parent.id, Fresh: true, Text: text(v.Sessions+c, 0)})
		})
	}
	wg.Wait()
	out.Sessions, out.Children = v.Sessions, v.Children
	// Alerts on the reader's own sessions, addressed to the reader.
	mine := []made{}
	for _, r := range roots {
		if r.who.Subject == reader.Subject && r.id != "" {
			mine = append(mine, r)
		}
	}
	for a := 0; a < v.Alerts && len(mine) > 0; a++ {
		session := mine[(a*7)%len(mine)]
		th, err := p.SessionRecord(ctx, reader, session.id)
		if err != nil {
			t.Fatal(err)
		}
		priority := []alerts.Priority{alerts.Urgent, alerts.Normal, alerts.Low}[a%3]
		who, err := p.SessionAlertAudience(reader, th.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.CreateSessionAlert(reader.Tenant, alerts.Alert{
			Session: th.ID, Project: th.Project, Title: fmt.Sprintf("Generated alert %d", a), Body: text(a, 99),
			Priority: priority, Who: who, Action: "answer", At: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
		out.Alerts++
		if priority != alerts.Low {
			out.Waiting++
		}
	}
	return out
}

// A small volume seeds through the write paths and is all there to read.
func TestSeedVolumeWritesWhatItCounts(t *testing.T) {
	f := newFixture(t)
	tok := f.person(t, "acme", "ana", "Ana", "humagen-admins")
	reader := auth.Principal{Subject: "ana", Tenant: "acme", Kind: auth.User, Name: "Ana", Roles: []auth.Role{auth.Admin}}
	got := seedVolume(t, f.p, reader, volume{Sessions: 10, Children: 2, Runs: 30, Alerts: 6, Parallel: 4})
	if got.Runs != 30 || got.Alerts != 6 || got.Waiting != 4 {
		t.Fatalf("seeded = %+v", got)
	}
	res, body := f.do(t, nil, "GET", "/api/sessions?limit=200", tok, "")
	if res.StatusCode != 200 || len(body["sessions"].([]any)) != 12 {
		t.Fatalf("sessions = %d %v", res.StatusCode, len(body["sessions"].([]any)))
	}
}

// TestSeedPerfDatabase fills the Postgres database $HUMAGEN_PERF_SEED_URL
// names with a realistic volume for the reader a server under -auth=dev
// signs in (subject "dev", tenant "default"), so read paths can be timed
// against a real server on it. It is skipped without the variable, and is
// meant for an empty throwaway database.
func TestSeedPerfDatabase(t *testing.T) {
	url := os.Getenv("HUMAGEN_PERF_SEED_URL")
	if url == "" {
		t.Skip("set HUMAGEN_PERF_SEED_URL to seed a throwaway database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	backends, release, err := platform.OpenPostgres(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = release() }()
	clock := seedClock()
	p, err := platform.New(platform.Config{
		Workspace: t.TempDir(), Backends: backends, Bus: backends.Bus, Concurrency: 64, Now: clock,
		Rooms:  rooms.Config{RoomRunsPerMinute: 100000, PersonRuns: 1000},
		Router: func(string) join.Asker { return join.Words },
	})
	if err != nil {
		t.Fatal(err)
	}
	run, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- p.Run(run) }()
	reader := auth.Principal{Subject: "dev", Tenant: "default", Kind: auth.User, Name: "Developer", Roles: []auth.Role{auth.Admin}}
	started := time.Now()
	got := seedVolume(t, p, reader, volume{Sessions: 320, Children: 40, Runs: 4000, Alerts: 60, Parallel: 48})
	cancel()
	if err := <-done; err != nil {
		t.Errorf("platform stopped with %v", err)
	}
	t.Logf("seeded %+v in %s; the clock ended at %s", got, time.Since(started).Round(time.Second), clock().Format(time.RFC3339))
}
