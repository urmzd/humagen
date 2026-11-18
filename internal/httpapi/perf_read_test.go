package httpapi

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/readaudit"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
)

// countingStore counts the reads and writes a store is asked for, each of
// which is one statement on Postgres.
type countingStore struct {
	state.Store
	lookups, many, lists, writes atomic.Int64
	mu                           sync.Mutex
	by                           map[string]int
}

// note counts one statement against its kind and namespace.
func (c *countingStore) note(kind, namespace string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.by == nil {
		c.by = map[string]int{}
	}
	c.by[kind+" "+namespace]++
}

func (c *countingStore) Get(ctx context.Context, namespace, tenant, key string) ([]byte, error) {
	c.lookups.Add(1)
	c.note("lookup", namespace)
	return c.Store.Get(ctx, namespace, tenant, key)
}

func (c *countingStore) Lookup(ctx context.Context, namespace, tenant, key string) (state.Record, error) {
	c.lookups.Add(1)
	c.note("lookup", namespace)
	return c.Store.Lookup(ctx, namespace, tenant, key)
}

// LookupMany counts as the one statement it is on Postgres.
func (c *countingStore) LookupMany(ctx context.Context, namespace, tenant string, keys []string) ([]state.Record, error) {
	c.many.Add(1)
	c.note("many", namespace)
	return c.Store.(state.Many).LookupMany(ctx, namespace, tenant, keys)
}

func (c *countingStore) List(ctx context.Context, namespace, tenant string, q state.Query) ([]state.Record, string, error) {
	c.lists.Add(1)
	c.note("list", namespace)
	return c.Store.List(ctx, namespace, tenant, q)
}

func (c *countingStore) Write(ctx context.Context, namespace string, ops ...state.Op) error {
	c.writes.Add(1)
	c.note("write", namespace)
	return c.Store.Write(ctx, namespace, ops...)
}

// reads is what one request asked the store for.
type reads struct {
	lookups, many, lists, writes int64
	// by is the count of each kind of statement in each namespace.
	by map[string]int
}

func (r reads) total() int64 { return r.lookups + r.many + r.lists + r.writes }

// during counts the store's statements while fn runs.
func (c *countingStore) during(fn func()) reads {
	c.mu.Lock()
	c.by = nil
	c.mu.Unlock()
	before := reads{c.lookups.Load(), c.many.Load(), c.lists.Load(), c.writes.Load(), nil}
	fn()
	c.mu.Lock()
	defer c.mu.Unlock()
	return reads{c.lookups.Load() - before.lookups, c.many.Load() - before.many, c.lists.Load() - before.lists, c.writes.Load() - before.writes, c.by}
}

// quietFixture is a workspace whose store counts only what requests ask of
// it. A platform that runs writes the workspace through seed and is stopped;
// the platform that is then served is opened on the same store and never
// run, so it has no workers, no leader loops and no housekeeping, and the
// only statements the store sees are those of the requests a test makes.
// Each test checks that: the store must see nothing while no request is made.
func quietFixture(t *testing.T, st state.Store, seed func(p *platform.Platform)) (*fixture, *countingStore) {
	t.Helper()
	store := &countingStore{Store: st}
	cfg := platform.Config{
		Backends: platform.Backends{State: store}, Now: seedClock(),
		Workspace: t.TempDir(), Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4},
		Router: func(string) join.Asker { return join.Words },
	}
	writer, err := platform.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- writer.Run(ctx) }()
	seed(writer)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("the seeding platform stopped with %v", err)
	}
	served, err := platform.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	f := serveFixture(t, served, nil)
	t.Cleanup(f.srv.Close)
	return f, store
}

// idle fails the test unless the store sees no statement while no request is
// made: what a test then counts is its own requests' and nothing else's.
func (c *countingStore) idle(t *testing.T) {
	t.Helper()
	// What the seeding platform or an earlier request left in flight ends first.
	for last, same := int64(-1), 0; same < 3; {
		now := c.lookups.Load() + c.many.Load() + c.lists.Load() + c.writes.Load()
		if now == last {
			same++
		} else {
			last, same = now, 0
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := c.during(func() { time.Sleep(250 * time.Millisecond) }); got.total() != 0 {
		t.Fatalf("the store saw %d statements with no request made: %v", got.total(), got.by)
	}
}

// The lists a console reads on every load cost a bounded number of
// statements however much the workspace holds: none reads a record per run,
// per session or per alert it passes over. The workspace here holds 66
// sessions, 400 runs and 18 alerts, and the reader is an operator, who reads
// nothing by exception, so no read is audited.
func TestListReadsAreBoundedByThePageNotTheWorkspace(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		reader := auth.Principal{Subject: "ana", Tenant: "acme", Kind: auth.User, Name: "Ana", Roles: []auth.Role{auth.Operator}}
		f, store := quietFixture(t, open(t), func(p *platform.Platform) {
			seedVolume(t, p, reader, volume{Sessions: 60, Children: 6, Runs: 400, Alerts: 18, Parallel: 4})
		})
		tok := f.person(t, "acme", "ana", "Ana", "humagen-operators")
		for _, c := range []struct {
			path string
			// most is the most statements the read may make, and rows the
			// most of them that may be a read of one record.
			most, rows int64
		}{
			// The newest 200 runs in one page, and their threads in one read.
			{"/api/runs", 12, 8},
			// A page of 50: the turn keys of each session on it, and no more.
			{"/api/sessions", 64, 8},
			{"/api/sessions?roots=true&mine=true&limit=1", 14, 8},
			{"/api/sessions?limit=5&status=done", 80, 8},
			{"/api/alerts?mine=true&state=open", 10, 6},
			{"/api/alerts/count", 8, 5},
		} {
			// The first read of a path may warm what later ones reuse.
			f.do(t, nil, "GET", c.path, tok, "")
			store.idle(t)
			var status int
			got := store.during(func() {
				res, _ := f.do(t, nil, "GET", c.path, tok, "")
				status = res.StatusCode
			})
			t.Logf("GET %s = %d: %d statements: %d lookups, %d batched reads, %d lists, %d writes %v", c.path, status, got.total(), got.lookups, got.many, got.lists, got.writes, got.by)
			if status != http.StatusOK {
				t.Errorf("GET %s = %d", c.path, status)
			}
			if got.total() > c.most || got.lookups > c.rows {
				t.Errorf("GET %s made %d statements, %d of them single reads; want at most %d and %d: %v", c.path, got.total(), got.lookups, c.most, c.rows, got.by)
			}
			if got.writes != 0 {
				t.Errorf("GET %s wrote %d times: %v", c.path, got.writes, got.by)
			}
		}
	})
}

// An alert a run raised is placed by its run, and the runs of every alert a
// read asks about are read together: ten alerts raised by runs cost what
// thirty do, for the alert list, the count, and the session listing that
// their alerts raise rows in.
func TestRunLinkedAlertsAreReadTogetherHoweverMany(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		paths := []string{"/api/alerts?mine=true&state=open", "/api/alerts/count", "/api/sessions?limit=5", "/api/sessions?limit=5&status=done"}
		cost := map[int]map[string]reads{}
		for _, n := range []int{1, 10, 30} {
			reader := auth.Principal{Subject: "ana", Tenant: "acme", Kind: auth.User, Name: "Ana", Roles: []auth.Role{auth.Operator}}
			f, store := quietFixture(t, open(t), func(p *platform.Platform) {
				seedVolume(t, p, reader, volume{Sessions: 20, Children: 2, Runs: 50, Alerts: n, Parallel: 4})
			})
			tok := f.person(t, "acme", "ana", "Ana", "humagen-operators")
			cost[n] = map[string]reads{}
			for _, path := range paths {
				f.do(t, nil, "GET", path, tok, "")
				store.idle(t)
				cost[n][path] = store.during(func() {
					if res, body := f.do(t, nil, "GET", path, tok, ""); res.StatusCode != http.StatusOK {
						t.Fatalf("GET %s = %d %v", path, res.StatusCode, body)
					}
				})
				t.Logf("%d alerts: GET %s made %d statements %v", n, path, cost[n][path].total(), cost[n][path].by)
			}
			if res, body := f.do(t, nil, "GET", "/api/alerts/count", tok, ""); int(body["waiting"].(float64)) != n-n/3 {
				t.Fatalf("%d alerts: count = %d %v, want %d", n, res.StatusCode, body, n-n/3)
			}
		}
		for _, path := range paths {
			one, ten, thirty := cost[1][path], cost[10][path], cost[30][path]
			if thirty.total() != ten.total() || one.total() > ten.total() {
				t.Errorf("GET %s made %d, %d and %d statements with 1, 10 and 30 alerts raised by runs; want no more for more alerts: %v", path, one.total(), ten.total(), thirty.total(), thirty.by)
			}
			if n := thirty.by["lookup runs.v2"]; n != 0 {
				t.Errorf("GET %s read %d runs one at a time with 30 alerts: %v", path, n, thirty.by)
			}
		}
	})
}

// A reader who reads by exception (projects:govern) is recorded for exactly
// what a page of sessions shows them: every returned session they see only
// by exception, with the session above it when the row names one they also
// see only by exception, and nothing that was examined to order, filter or
// cut the page. It holds for each paging filter, across pages.
func TestASessionPageRecordsExactlyTheExceptionalReadsItShows(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		// Ana, an administrator, starts three sessions in five; the rest are
		// other people's private sessions, which she reads by exception.
		ana := auth.Principal{Subject: "ana", Tenant: "acme", Kind: auth.User, Name: "Ana", Roles: []auth.Role{auth.Admin}}
		f := newFixtureWith(t, platform.Config{Backends: platform.Backends{State: open(t)}, Now: seedClock()})
		tok := f.person(t, "acme", "ana", "Ana", "humagen-admins")
		seedVolume(t, f.p, ana, volume{Sessions: 25, Children: 8, Runs: 60, Alerts: 6, Parallel: 4})
		// Whose each session is, and the session above it.
		owner, parent := map[string]string{}, map[string]string{}
		recs, err := f.p.Threads.Records("acme", func(threads.Thread) bool { return true })
		if err != nil {
			t.Fatal(err)
		}
		for _, th := range recs {
			if th.Session != nil {
				owner[th.ID], parent[th.ID] = th.CreatedBy, th.Session.Parent
			}
		}
		audited := func() []readaudit.Record {
			t.Helper()
			all, err := f.p.ReadAudit.List("acme", time.Now().Add(-25*24*time.Hour), time.Now().Add(24*time.Hour), readaudit.MaxRows)
			if err != nil {
				t.Fatal(err)
			}
			if len(all) >= readaudit.MaxRows {
				t.Fatal("the test wrote more audit records than one read returns")
			}
			return all
		}
		exceptional := 0
		for _, filter := range []string{"", "&roots=true", "&mine=true", "&status=done", "&project=launch"} {
			after := ""
			for page := 0; page < 3; page++ {
				before := len(audited())
				res, body := f.do(t, nil, "GET", "/api/sessions?limit=3"+filter+"&after="+after, tok, "")
				if res.StatusCode != http.StatusOK {
					t.Fatalf("%q page %d = %d %v", filter, page, res.StatusCode, body)
				}
				want := map[string]bool{}
				for _, raw := range body["sessions"].([]any) {
					id := raw.(map[string]any)["id"].(string)
					if owner[id] == "" {
						t.Fatalf("%q page %d returned %s, which has no record", filter, page, id)
					}
					if owner[id] == "ana" {
						continue
					}
					want[id] = true
					exceptional++
					// The row names the session above it, which is the same
					// person's and as private.
					for up := parent[id]; strings.HasPrefix(up, "s_"); up = parent[up] {
						want[up] = true
					}
				}
				now := audited()
				got := map[string]bool{}
				// The records are newest first: the request's are the new ones.
				for _, r := range now[:len(now)-before] {
					if r.Subject != "ana" || r.Route != "GET /api/sessions" || r.Thread == "" {
						t.Errorf("%q page %d recorded %+v, which is no session ana was shown", filter, page, r)
					}
					got[r.Thread] = true
				}
				if !maps.Equal(got, want) {
					t.Errorf("%q page %d: recorded reads of %v, showed by exception %v", filter, page, slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
				}
				next, _ := body["next"].(string)
				if next == "" {
					break
				}
				after = next
			}
		}
		if exceptional < 6 {
			t.Fatalf("only %d rows were shown by exception, too few to say anything", exceptional)
		}
	})
}

// When the record of a read by exception cannot be written, the response
// that would show it is refused whole: an error, no rows and no cursor,
// whichever row's record failed, and no record of any of its rows is kept,
// since nothing was shown. The same request answers once records can be
// written again, and then exactly its rows are recorded.
func TestASessionPageIsRefusedWholeWhenARecordCannotBeWritten(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		store := &failingAudit{Store: open(t)}
		f := newFixtureWith(t, platform.Config{Backends: platform.Backends{State: store}, Now: seedClock()})
		// Olu starts everything; Ana, an administrator, reads it by exception.
		olu := auth.Principal{Subject: "olu", Tenant: "acme", Kind: auth.User, Name: "Olu", Roles: []auth.Role{auth.Admin}}
		seedVolume(t, f.p, olu, volume{Sessions: 12, Children: 2, Runs: 20, Alerts: 0, Parallel: 4})
		ana := f.person(t, "acme", "ana", "Ana", "humagen-admins")
		// kept is the threads of the read records the store holds, in the
		// order they were written.
		kept := func() []string {
			t.Helper()
			all, err := f.p.ReadAudit.List("acme", time.Now().Add(-25*24*time.Hour), time.Now().Add(24*time.Hour), readaudit.MaxRows)
			if err != nil || len(all) >= readaudit.MaxRows {
				t.Fatalf("the read records = %d, %v", len(all), err)
			}
			out := make([]string, len(all))
			for i, r := range all {
				out[len(all)-1-i] = r.Thread
			}
			return out
		}
		page := func(path string) ([]string, string) {
			t.Helper()
			res, body := f.do(t, nil, "GET", path, ana, "")
			if res.StatusCode != http.StatusOK {
				t.Fatalf("GET %s = %d %v", path, res.StatusCode, body)
			}
			var ids []string
			for _, raw := range body["sessions"].([]any) {
				ids = append(ids, raw.(map[string]any)["id"].(string))
			}
			next, _ := body["next"].(string)
			return ids, next
		}
		// refused makes a request that must be refused, tell of no row, and
		// leave no read record behind.
		refused := func(path, why string, ids []string) {
			t.Helper()
			before := kept()
			res, body := f.do(t, nil, "GET", path, ana, "")
			if res.StatusCode != http.StatusNotFound || len(body) != 1 || body["error"] != "not found" {
				t.Fatalf("%s, %s: GET = %d %v, want 404 and nothing but that it was not found", path, why, res.StatusCode, body)
			}
			for name, values := range res.Header {
				for _, v := range values {
					for _, id := range ids {
						if strings.Contains(v, id) || strings.Contains(strings.ToLower(name), "next") {
							t.Fatalf("%s, %s: the refusal's header %s: %s tells of a row", path, why, name, v)
						}
					}
				}
			}
			if after := kept(); !slices.Equal(after, before) {
				t.Fatalf("%s, %s: the refused request left read records of %v", path, why, after[len(before):])
			}
		}
		// answered makes a request that must answer, and returns the threads
		// it left read records of.
		answered := func(get func()) []string {
			t.Helper()
			before := len(kept())
			get()
			return slices.Sorted(slices.Values(kept()[before:]))
		}
		for _, path := range []string{"/api/sessions?limit=3", "/api/sessions?limit=3&status=done"} {
			ids, next := page(path)
			if len(ids) != 3 || next == "" {
				t.Fatalf("GET %s = %v, next %q; want a page of 3 and a page after it", path, ids, next)
			}
			// What a page that answers records: its rows, each with the
			// session above it.
			want := map[string]bool{}
			for _, id := range ids {
				want[id] = true
				_, body := f.do(t, nil, "GET", "/api/sessions/"+id, ana, "")
				if up, _ := body["parent"].(string); strings.HasPrefix(up, "s_") {
					want[up] = true
				}
			}
			store.all.Store(true)
			refused(path, "no record can be written", ids)
			store.all.Store(false)
			for i, where := range []string{"the first row", "the middle row", "the last row, which the cursor names"} {
				store.thread.Store(ids[i])
				refused(path, "the record of "+where+" cannot be written", ids)
				store.thread.Store("")
				got := answered(func() {
					if again, after := page(path); !slices.Equal(again, ids) || after != next {
						t.Fatalf("GET %s once records can be written = %v, next %q; want %v, %q", path, again, after, ids, next)
					}
				})
				if !slices.Equal(got, slices.Sorted(maps.Keys(want))) {
					t.Fatalf("GET %s once records can be written recorded %v, want exactly its rows %v", path, got, slices.Sorted(maps.Keys(want)))
				}
			}
		}
		// A session's own read shows the sessions under it too: when the
		// record of one of those cannot be written the read is refused, and
		// no record is kept of the session itself either.
		all, _ := page("/api/sessions?limit=200")
		under := map[string][]string{}
		for _, id := range all {
			_, body := f.do(t, nil, "GET", "/api/sessions/"+id, ana, "")
			if up, _ := body["parent"].(string); strings.HasPrefix(up, "s_") {
				under[up] = append(under[up], id)
			}
		}
		if len(under) == 0 {
			t.Fatal("no session under another was seeded")
		}
		for up, children := range under {
			path := "/api/sessions/" + up
			for _, fail := range []string{up, children[0]} {
				store.thread.Store(fail)
				refused(path, "the record of "+fail+" cannot be written", append([]string{up}, children...))
				store.thread.Store("")
			}
			store.all.Store(true)
			refused(path, "no record can be written", append([]string{up}, children...))
			store.all.Store(false)
			got := answered(func() {
				if res, _ := f.do(t, nil, "GET", path, ana, ""); res.StatusCode != http.StatusOK {
					t.Fatalf("GET %s once records can be written = %d", path, res.StatusCode)
				}
			})
			if want := slices.Sorted(slices.Values(append([]string{up}, children...))); !slices.Equal(got, want) {
				t.Fatalf("GET %s recorded %v, want the session and those under it %v", path, got, want)
			}
		}
	})
}

// failingAudit fails the writes of read records: all of them while all is
// set, or those of one thread while thread names it.
type failingAudit struct {
	state.Store
	all    atomic.Bool
	thread atomic.Value
}

func (s *failingAudit) Write(ctx context.Context, namespace string, ops ...state.Op) error {
	if namespace == "read-audit" {
		thread, _ := s.thread.Load().(string)
		for _, op := range ops {
			if s.all.Load() || (thread != "" && strings.Contains(string(op.Value), `"thread":"`+thread+`"`)) {
				return statetest.ErrInjected
			}
		}
	}
	return s.Store.Write(ctx, namespace, ops...)
}

func (s *failingAudit) LookupMany(ctx context.Context, namespace, tenant string, keys []string) ([]state.Record, error) {
	return state.LookupMany(ctx, s.Store, namespace, tenant, keys)
}

// A session read whose runs cannot be read is shown without them, and still
// names its people as the directory names them, as the same read does when
// its runs can be read.
func TestASessionShownWithoutItsRunsNamesPeopleAsTheDirectoryDoes(t *testing.T) {
	store := &failingRuns{Store: state.NewMemory()}
	f := newFixtureWith(t, platform.Config{Backends: platform.Backends{State: store}, Now: seedClock()})
	// A subject that is not the handle the directory gives the person.
	tok := f.person(t, "acme", "auth0|7f3a", "Ana Lima", "humagen-admins")
	reader := auth.Principal{Subject: "auth0|7f3a", Tenant: "acme", Kind: auth.User, Name: "Ana Lima", Roles: []auth.Role{auth.Admin}}
	seedVolume(t, f.p, reader, volume{Sessions: 1, Children: 0, Runs: 2, Alerts: 0, Parallel: 1})
	_, list := f.do(t, nil, "GET", "/api/sessions?limit=1", tok, "")
	id := list["sessions"].([]any)[0].(map[string]any)["id"].(string)
	read := func() map[string]any {
		t.Helper()
		res, body := f.do(t, nil, "GET", "/api/sessions/"+id, tok, "")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("session = %d %v", res.StatusCode, body)
		}
		return body["conversation"].(map[string]any)
	}
	whole := read()
	store.fail.Store(true)
	without := read()
	store.fail.Store(false)
	if store.failed.Load() == 0 {
		t.Fatal("no read of the session's runs failed, so the fallback was not taken")
	}
	people := fmt.Sprint(whole["people"])
	if strings.Contains(people, "auth0|7f3a") || people == "[]" {
		t.Fatalf("with its runs, the session names its people %s", people)
	}
	if got := fmt.Sprint(without["people"]); got != people {
		t.Errorf("without its runs the session names its people %s, with them %s", got, people)
	}
	if by, _ := without["last_by"].(string); strings.Contains(by, "auth0|7f3a") {
		t.Errorf("without its runs the session says %q spoke last, a subject", by)
	}
}

// failingRuns fails the reads of several runs at once while fail is set, as
// a database that drops a statement does.
type failingRuns struct {
	state.Store
	fail   atomic.Bool
	failed atomic.Int64
}

func (s *failingRuns) LookupMany(ctx context.Context, namespace, tenant string, keys []string) ([]state.Record, error) {
	if s.fail.Load() && namespace == "runs.v2" && len(keys) > 0 && strings.HasPrefix(keys[0], "o:") {
		s.failed.Add(1)
		return nil, errors.New("the statement was dropped")
	}
	return state.LookupMany(ctx, s.Store, namespace, tenant, keys)
}

// The count of what waits on a person is the count of what their alert list
// holds, is theirs alone, and never counts another workspace's.
func TestAlertCountIsWhatTheListHoldsForTheCallerAlone(t *testing.T) {
	f := newFixtureWith(t, platform.Config{Now: seedClock()})
	ana := f.person(t, "acme", "ana", "Ana", "humagen-admins")
	reader := auth.Principal{Subject: "ana", Tenant: "acme", Kind: auth.User, Name: "Ana", Roles: []auth.Role{auth.Admin}}
	seeded := seedVolume(t, f.p, reader, volume{Sessions: 10, Children: 2, Runs: 20, Alerts: 9, Parallel: 4})

	count := func(tok string) int {
		t.Helper()
		res, body := f.do(t, nil, "GET", "/api/alerts/count", tok, "")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("count = %d %v", res.StatusCode, body)
		}
		return int(body["waiting"].(float64))
	}
	listed := func(tok string) int {
		t.Helper()
		res, body := f.do(t, nil, "GET", "/api/alerts?mine=true&state=open", tok, "")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("list = %d %v", res.StatusCode, body)
		}
		n := 0
		for _, raw := range body["alerts"].([]any) {
			a := raw.(map[string]any)
			if a["session"] != nil && a["session"] != "" && a["priority"] != "low" {
				n++
			}
		}
		return n
	}
	if got := count(ana); got != seeded.Waiting || got != listed(ana) || got != 6 {
		t.Fatalf("ana's count = %d, her list holds %d, seeded %d", got, listed(ana), seeded.Waiting)
	}
	// An operator in the same workspace is addressed by none of them, and
	// reads none of ana's private sessions.
	if got := count(f.person(t, "acme", "olu", "Olu", "humagen-operators")); got != 0 {
		t.Fatalf("another person's count = %d, want 0", got)
	}
	// Another workspace's person, with the same subject, counts their own.
	if got := count(f.person(t, "elsewhere", "ana", "Ana", "humagen-admins")); got != 0 {
		t.Fatalf("another workspace's count = %d, want 0", got)
	}
	// A machine without alerts:read is refused.
	if res, _ := f.do(t, nil, "GET", "/api/alerts/count", f.machine(t, "acme", "runs:read"), ""); res.StatusCode != http.StatusForbidden {
		t.Fatalf("without alerts:read = %d, want 403", res.StatusCode)
	}
	// Resolving one takes it off the count.
	_, body := f.do(t, nil, "GET", "/api/alerts?mine=true&state=open&priority=urgent", ana, "")
	id := body["alerts"].([]any)[0].(map[string]any)["id"].(string)
	if res, out := f.do(t, nil, "PATCH", "/api/alerts/"+id, ana, `{"state":"resolved"}`); res.StatusCode != http.StatusOK {
		t.Fatalf("resolve = %d %v", res.StatusCode, out)
	}
	if got := count(ana); got != 5 || got != listed(ana) {
		t.Fatalf("after resolving one, count = %d and the list holds %d, want 5", got, listed(ana))
	}
}

// Paging the sessions reads each once, in the order one whole page holds
// them, with the reader's open alerts first: only a page is summarized, and
// the cursor still names a place in the whole listing.
func TestSessionPagesAreTheWholeListingInOrder(t *testing.T) {
	f := newFixtureWith(t, platform.Config{Now: seedClock()})
	tok := f.person(t, "acme", "ana", "Ana", "humagen-admins")
	reader := auth.Principal{Subject: "ana", Tenant: "acme", Kind: auth.User, Name: "Ana", Roles: []auth.Role{auth.Admin}}
	seedVolume(t, f.p, reader, volume{Sessions: 23, Children: 4, Runs: 60, Alerts: 6, Parallel: 4})
	ids := func(path string) ([]string, string) {
		t.Helper()
		res, body := f.do(t, nil, "GET", path, tok, "")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d %v", path, res.StatusCode, body)
		}
		var out []string
		for _, raw := range body["sessions"].([]any) {
			out = append(out, raw.(map[string]any)["id"].(string))
		}
		next, _ := body["next"].(string)
		return out, next
	}
	for _, filter := range []string{"", "&roots=true", "&mine=true", "&status=done", "&project=launch"} {
		whole, next := ids("/api/sessions?limit=200" + filter)
		if next != "" || len(whole) == 0 {
			t.Fatalf("%q: the whole listing = %d sessions, next %q", filter, len(whole), next)
		}
		var paged []string
		for after, pages := "", 0; ; pages++ {
			if pages > 20 {
				t.Fatalf("%q: the pages do not end", filter)
			}
			page, next := ids("/api/sessions?limit=4" + filter + "&after=" + after)
			paged = append(paged, page...)
			if next == "" {
				break
			}
			after = next
		}
		if !slices.Equal(paged, whole) {
			t.Errorf("%q: paged = %v\nwhole = %v", filter, paged, whole)
		}
	}
	// The reader's open alerts raise their sessions to the top.
	whole, _ := ids("/api/sessions?limit=200")
	_, body := f.do(t, nil, "GET", "/api/alerts?mine=true&state=open", tok, "")
	raised := map[string]bool{}
	for _, raw := range body["alerts"].([]any) {
		raised[raw.(map[string]any)["session"].(string)] = true
	}
	for i, id := range whole {
		if raised[id] != (i < len(raised)) {
			t.Fatalf("session %d of the listing (%s): raised = %v, with %d raised sessions", i, id, raised[id], len(raised))
		}
	}
}
