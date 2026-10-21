package people_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/warp/pkg/people"
	"github.com/urmzd/warp/pkg/state"
	"github.com/urmzd/warp/pkg/state/statetest"
)

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

func signIn(subject, name string, roles ...string) people.Sighting {
	if roles == nil {
		roles = []string{}
	}
	return people.Sighting{Subject: subject, Name: name, Roles: roles, Source: people.SignIn}
}

func mustSee(t *testing.T, s *people.Store, tenant string, at people.Sighting) people.Person {
	t.Helper()
	p, err := s.Seen(tenant, at, t0)
	if err != nil {
		t.Fatalf("seen %q: %v", at.Subject, err)
	}
	return p
}

func TestFortyRacersSeeingOneSubjectKeepOne(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := people.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var failed atomic.Int64
		for i := range 40 {
			wg.Go(func() {
				at := signIn("okta|00u1dana", "Dana Scully", "viewer")
				if i%2 == 1 {
					at = people.Sighting{Subject: "okta|00u1dana", Source: people.Slack}
				}
				if _, err := s.Seen("acme", at, t0.Add(time.Duration(i)*time.Second)); err != nil {
					failed.Add(1)
				}
			})
		}
		wg.Wait()
		recs, err := st.Load(context.Background(), "people")
		if err != nil {
			t.Fatal(err)
		}
		p, err := s.Get("acme", "okta|00u1dana")
		if err != nil || failed.Load() != 0 || len(recs) != 1 || s.Len("acme") != 1 {
			t.Fatalf("%d records, %d in memory, %d failed: %v", len(recs), s.Len("acme"), failed.Load(), err)
		}
		if p.Handle != "dana-scully" || p.Name != "Dana Scully" || !slices.Equal(p.Sources, []string{people.SignIn, people.Slack}) {
			t.Fatalf("kept %+v", p)
		}
		again, err := people.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if back, _ := again.Get("acme", "okta|00u1dana"); !reflect.DeepEqual(back, p) {
			t.Fatalf("after restart %+v, want %+v", back, p)
		}
	})
}

func TestACollidingHandleGetsASuffix(t *testing.T) {
	s := people.NewStore()
	for i, want := range []string{"dana-scully", "dana-scully-2", "dana-scully-3"} {
		if p := mustSee(t, s, "acme", signIn(fmt.Sprintf("okta|%d", i), "Dana Scully")); p.Handle != want {
			t.Fatalf("person %d goes by %q, want %q", i, p.Handle, want)
		}
	}
	long := strings.Repeat("Bartholomew ", 4)
	first, second := mustSee(t, s, "acme", signIn("okta|long1", long)), mustSee(t, s, "acme", signIn("okta|long2", long))
	if len(first.Handle) > people.MaxHandle || len(second.Handle) > people.MaxHandle || !strings.HasSuffix(second.Handle, "-2") ||
		!people.ValidHandle(first.Handle) || !people.ValidHandle(second.Handle) || first.Handle == second.Handle {
		t.Fatalf("long names go by %q and %q", first.Handle, second.Handle)
	}
	if p, err := s.ByHandle("acme", "Dana-Scully-2"); err != nil || p.Subject != "okta|1" {
		t.Fatalf("ByHandle = %+v, %v", p, err)
	}
}

func TestReservedHandlesAndAgentNamesAreTaken(t *testing.T) {
	agents := map[string][]string{"acme": {"ignite"}}
	s := people.NewStore(people.WithTaken(func(tenant, handle string) bool { return slices.Contains(agents[tenant], handle) }))
	for subject, want := range map[string]string{"okta|a": "auto-2", "okta|s": "sme-2", "okta|i": "ignite-2"} {
		name := map[string]string{"okta|a": "Auto", "okta|s": "SME", "okta|i": "Ignite"}[subject]
		if p := mustSee(t, s, "acme", signIn(subject, name)); p.Handle != want {
			t.Errorf("%s goes by %q, want %q", name, p.Handle, want)
		}
	}
	// An agent's name in one workspace is free in another.
	if p := mustSee(t, s, "globex", signIn("okta|i", "Ignite")); p.Handle != "ignite" {
		t.Fatalf("in another workspace Ignite goes by %q", p.Handle)
	}
	for _, h := range people.Reserved {
		if !people.IsReserved(h) {
			t.Errorf("%q is not reserved", h)
		}
	}
}

func TestAHandleFromTheSubjectIsMadeAgainFromTheFirstName(t *testing.T) {
	s := people.NewStore()
	p, err := s.Ensure("acme", "okta|00u1ana", people.Slack, t0)
	if err != nil || p.Handle != "00u1ana" || !p.SeenAt.IsZero() {
		t.Fatalf("a mapped person = %+v, %v", p, err)
	}
	p = mustSee(t, s, "acme", signIn("okta|00u1ana", "Ana Lopez"))
	if p.Handle != "ana-lopez" || p.SeenAt != t0 {
		t.Fatalf("at first sign-in = %+v", p)
	}
	if _, err := s.ByHandle("acme", "00u1ana"); !errors.Is(err, people.ErrNotFound) {
		t.Fatalf("the old handle still names someone: %v", err)
	}
	// From then on a tag keeps meaning the same person.
	p = mustSee(t, s, "acme", signIn("okta|00u1ana", "Ana María López"))
	if p.Handle != "ana-lopez" || p.Name != "Ana María López" {
		t.Fatalf("after a new name = %+v", p)
	}
	// Accents are folded, and a name with no letters a handle can hold falls
	// back to the subject.
	if p := mustSee(t, s, "acme", signIn("okta|zoe", "Zoë Núñez")); p.Handle != "zoe-nunez" {
		t.Fatalf("Zoë Núñez goes by %q", p.Handle)
	}
	if p := mustSee(t, s, "acme", signIn("okta|bruce", "李小龍")); p.Handle != "bruce" {
		t.Fatalf("a name with no Latin letters goes by %q", p.Handle)
	}
}

func TestNoEmailReachesTheDirectory(t *testing.T) {
	s := people.NewStore()
	// Some providers put the address where the name goes.
	p := mustSee(t, s, "acme", signIn("okta|00u1ada", "ada@acme.test"))
	if p.Name != "" || strings.Contains(p.Handle, "acme") {
		t.Fatalf("an address became a name or a handle: %+v", p)
	}
	// A subject that holds an address never gives its domain to a handle.
	q := mustSee(t, s, "acme", signIn("tailscale:grace@acme.test", ""))
	if q.Handle != "grace" {
		t.Fatalf("a login's handle = %q", q.Handle)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for k := range fields {
		if !slices.Contains([]string{"subject", "name", "handle", "roles", "sources", "seen_at", "created_at"}, k) {
			t.Errorf("a person carries %q", k)
		}
	}
	if strings.Contains(string(raw), "@") {
		t.Fatalf("an address reached the JSON: %s", raw)
	}
}

func TestTheCapHolds(t *testing.T) {
	s := people.NewStore()
	for i := range people.MaxPeople {
		if _, err := s.Ensure("acme", fmt.Sprintf("okta|%05d", i), people.Slack, t0); err != nil {
			t.Fatalf("person %d: %v", i, err)
		}
	}
	if _, err := s.Seen("acme", signIn("okta|late", "Late Comer"), t0); !errors.Is(err, people.ErrLimit) {
		t.Fatalf("one past the cap = %v, want ErrLimit", err)
	}
	if p, err := s.Seen("acme", signIn("okta|00007", "Seven"), t0); err != nil || p.Name != "Seven" {
		t.Fatalf("someone already listed, past the cap = %+v, %v", p, err)
	}
	if _, err := s.Seen("globex", signIn("okta|late", "Late Comer"), t0); err != nil {
		t.Fatalf("another workspace has its own cap: %v", err)
	}
}

func TestRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := people.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		mustSee(t, s, "acme", signIn("okta|1", "Dana Scully", "operator", "viewer"))
		mustSee(t, s, "acme", signIn("okta|2", "Dana Scully"))
		if _, err := s.Ensure("acme", "okta|3", people.Policy, t0); err != nil {
			t.Fatal(err)
		}
		mustSee(t, s, "globex", signIn("okta|1", "Fox Mulder"))
		again, err := people.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		for _, tenant := range []string{"acme", "globex"} {
			want, _, _ := s.List(tenant, people.Query{})
			got, _, _ := again.List(tenant, people.Query{})
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s after restart = %+v, want %+v", tenant, got, want)
			}
		}
		// Handles are held after a restart too.
		if p := mustSee(t, again, "acme", signIn("okta|4", "Dana Scully")); p.Handle != "dana-scully-3" {
			t.Fatalf("a third Dana after restart goes by %q", p.Handle)
		}
	})
}

func TestAFailedWriteChangesNothing(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	s, err := people.Open(f)
	if err != nil {
		t.Fatal(err)
	}
	before := mustSee(t, s, "acme", people.Sighting{Subject: "okta|1", Source: people.Slack})
	f.Fail(statetest.ErrInjected)
	if _, err := s.Seen("acme", signIn("okta|2", "Dana Scully"), t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("a new person = %v", err)
	}
	if _, err := s.Seen("acme", signIn("okta|1", "Fox Mulder"), t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("a change = %v", err)
	}
	if got, _ := s.Get("acme", "okta|1"); !reflect.DeepEqual(got, before) || s.Len("acme") != 1 {
		t.Fatalf("a failed write changed %+v to %+v", before, got)
	}
	if _, err := s.ByHandle("acme", "fox-mulder"); !errors.Is(err, people.ErrNotFound) {
		t.Fatal("a failed write took a handle")
	}
	if _, err := s.ByHandle("acme", before.Handle); err != nil {
		t.Fatalf("a failed write let a handle go: %v", err)
	}
	f.Fail(nil)
	again, err := people.Open(f)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := again.Get("acme", "okta|1"); !reflect.DeepEqual(got, before) {
		t.Fatalf("after restart %+v, want %+v", got, before)
	}
}

func TestWorkspacesAreIsolated(t *testing.T) {
	s := people.NewStore()
	mustSee(t, s, "acme", signIn("okta|1", "Dana Scully"))
	if p := mustSee(t, s, "globex", signIn("okta|9", "Dana Scully")); p.Handle != "dana-scully" {
		t.Fatalf("a handle in one workspace was taken in another: %q", p.Handle)
	}
	if _, err := s.Get("globex", "okta|1"); !errors.Is(err, people.ErrNotFound) {
		t.Fatalf("another workspace's person = %v", err)
	}
	if page, _, _ := s.List("globex", people.Query{}); len(page) != 1 || page[0].Subject != "okta|9" {
		t.Fatalf("globex lists %+v", page)
	}
	if page, next, err := s.List("initech", people.Query{}); page == nil || len(page) != 0 || next != "" || err != nil {
		t.Fatalf("an empty workspace lists %#v, %q, %v", page, next, err)
	}
}

// counting is a state.Store that counts writes.
type counting struct {
	state.Store
	writes atomic.Int64
}

func (c *counting) Write(ctx context.Context, namespace string, ops ...state.Op) error {
	c.writes.Add(1)
	return c.Store.Write(ctx, namespace, ops...)
}

func TestASightingThatChangesNothingWritesNothingForAnHour(t *testing.T) {
	st := &counting{Store: state.NewMemory()}
	s, err := people.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	at := signIn("okta|1", "Dana Scully", "viewer")
	for _, when := range []time.Duration{0, time.Minute, 59 * time.Minute} {
		if _, err := s.Seen("acme", at, t0.Add(when)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Ensure("acme", "okta|1", people.SignIn, t0.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if n := st.writes.Load(); n != 1 {
		t.Fatalf("%d writes in the first hour, want 1", n)
	}
	if p, _ := s.Get("acme", "okta|1"); p.SeenAt != t0 {
		t.Fatalf("seen at %v", p.SeenAt)
	}
	p, _ := s.Seen("acme", at, t0.Add(61*time.Minute))
	if st.writes.Load() != 2 || p.SeenAt != t0.Add(61*time.Minute) {
		t.Fatalf("after an hour: %d writes, seen at %v", st.writes.Load(), p.SeenAt)
	}
	// Something new is written at once.
	if _, err := s.Seen("acme", signIn("okta|1", "Dana Scully", "operator"), t0.Add(62*time.Minute)); err != nil || st.writes.Load() != 3 {
		t.Fatalf("a new role: %d writes, %v", st.writes.Load(), err)
	}
}

func TestASignInsRolesWinOverThePolicys(t *testing.T) {
	s := people.NewStore()
	policy := func(roles ...string) people.Sighting {
		return people.Sighting{Subject: "okta|1", Roles: roles, Source: people.Policy}
	}
	if p := mustSee(t, s, "acme", policy("operator")); !slices.Equal(p.Roles, []string{"operator"}) {
		t.Fatalf("roles from the policy = %v", p.Roles)
	}
	if p := mustSee(t, s, "acme", signIn("okta|1", "Dana", "viewer", "field-eng", "viewer")); !slices.Equal(p.Roles, []string{"field-eng", "viewer"}) {
		t.Fatalf("roles from a sign-in = %v", p.Roles)
	}
	if p := mustSee(t, s, "acme", policy("operator")); !slices.Equal(p.Roles, []string{"field-eng", "viewer"}) {
		t.Fatalf("the policy's grant to the subject replaced a sign-in's roles: %v", p.Roles)
	}
	if p := mustSee(t, s, "acme", people.Sighting{Subject: "okta|1", Source: people.Slack}); !slices.Equal(p.Roles, []string{"field-eng", "viewer"}) {
		t.Fatalf("a sighting with no roles changed them: %v", p.Roles)
	}
}

func TestListPagesByHandleAndSearches(t *testing.T) {
	s := people.NewStore()
	for i, name := range []string{"Dana Scully", "Fox Mulder", "Walter Skinner", "Monica Reyes", "John Doggett", "Alex Krycek", "Jeffrey Spender"} {
		mustSee(t, s, "acme", signIn(fmt.Sprintf("okta|%d", i), name))
	}
	var handles []string
	cursor := ""
	for range 4 {
		page, next, err := s.List("acme", people.Query{Limit: 3, Before: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range page {
			handles = append(handles, p.Handle)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	want := []string{"alex-krycek", "dana-scully", "fox-mulder", "jeffrey-spender", "john-doggett", "monica-reyes", "walter-skinner"}
	if !slices.Equal(handles, want) {
		t.Fatalf("paged %v, want %v", handles, want)
	}
	for q, want := range map[string][]string{"SCUL": {"dana-scully"}, "j": {"jeffrey-spender", "john-doggett"}, "okta|4": {"john-doggett"}, "nobody": {}} {
		page, _, _ := s.List("acme", people.Query{Q: q})
		got := []string{}
		for _, p := range page {
			got = append(got, p.Handle)
		}
		if !slices.Equal(got, want) {
			t.Errorf("search %q = %v, want %v", q, got, want)
		}
	}
	if page, _, _ := s.List("acme", people.Query{Limit: 500}); len(page) != len(want) {
		t.Fatalf("a large limit = %d", len(page))
	}
	for _, q := range []people.Query{{Before: "Not A Cursor"}, {Q: strings.Repeat("x", people.MaxQuery+1)}} {
		if _, _, err := s.List("acme", q); !errors.Is(err, people.ErrInvalid) {
			t.Errorf("List(%+v) = %v, want ErrInvalid", q, err)
		}
	}
}

func TestASightingIsValidated(t *testing.T) {
	s := people.NewStore()
	many := make([]string, people.MaxRoles+1)
	for i := range many {
		many[i] = fmt.Sprintf("r%d", i)
	}
	for name, at := range map[string]people.Sighting{
		"no subject":         {Source: people.SignIn},
		"a control in it":    {Subject: "okta|\x00", Source: people.SignIn},
		"a long subject":     {Subject: strings.Repeat("s", people.MaxSubject+1), Source: people.SignIn},
		"no source":          {Subject: "okta|1"},
		"an unknown source":  {Subject: "okta|1", Source: "github"},
		"too many roles":     {Subject: "okta|1", Source: people.SignIn, Roles: many},
		"a role with spaces": {Subject: "okta|1", Source: people.SignIn, Roles: []string{"field eng"}},
	} {
		if _, err := s.Seen("acme", at, t0); !errors.Is(err, people.ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
	if s.Len("acme") != 0 {
		t.Fatal("a refused sighting was kept")
	}
}

func TestOpenRefusesWhatIsNotADirectory(t *testing.T) {
	put := func(t *testing.T, st state.Store, key string, v any) {
		t.Helper()
		raw, _ := json.Marshal(v)
		if err := st.Write(context.Background(), "people", state.Put("acme", key, raw)); err != nil {
			t.Fatal(err)
		}
	}
	for name, write := range map[string]func(*testing.T, state.Store){
		"kept under another subject": func(t *testing.T, st state.Store) {
			put(t, st, "okta|1", people.Person{Subject: "okta|2", Handle: "dana"})
		},
		"not a handle": func(t *testing.T, st state.Store) {
			put(t, st, "okta|1", people.Person{Subject: "okta|1", Handle: "Dana Scully"})
		},
		"one handle twice": func(t *testing.T, st state.Store) {
			put(t, st, "okta|1", people.Person{Subject: "okta|1", Handle: "dana"})
			put(t, st, "okta|2", people.Person{Subject: "okta|2", Handle: "dana"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			st := state.NewMemory()
			write(t, st)
			if _, err := people.Open(st); !errors.Is(err, people.ErrInvalid) {
				t.Fatalf("Open = %v, want ErrInvalid", err)
			}
		})
	}
}
