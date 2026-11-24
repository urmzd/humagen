package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/saige/durable/ledger"
	dqueue "github.com/urmzd/saige/durable/queue"
	drun "github.com/urmzd/saige/durable/run"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/thread"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/internal/wire/sessionstate"
	"github.com/urmzd/humagen/pkg/state"
)

// marker is in every retrieved passage and every answer the journal store
// holds, so a test can tell whether the command printed an entry's text.
const marker = "MARKER-7f3a"

const passages = "Passage retrieved for this turn: " + marker

// journalFixture is a store holding journaled requests from before each
// request was journaled as it was asked:
//
//   - tenant acme, thread one: a request with passages and trigger context
//     (its entry has a surface message id, so three records), one already as
//     it was asked, one whose run the ledger does not hold, and one with
//     passages and no trigger context, each followed by its answer;
//   - tenant acme, thread two: a request with passages, and a run that has
//     not ended;
//   - tenant other: a request with passages.
type journalFixture struct {
	st     state.Store
	ledger *ledger.Memory
	b      platform.Backends
	runs   map[string]runtime.Input
}

const journalNS = "agent-sessions"

func newJournalFixture(t *testing.T) *journalFixture {
	t.Helper()
	f := &journalFixture{st: state.NewMemory(), ledger: ledger.NewMemory(), runs: map[string]runtime.Input{}}
	f.b = platform.Backends{State: f.st, Ledger: f.ledger, Queue: dqueue.NewMemory()}
	seed, err := platform.New(platform.Config{Backends: f.b, Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = seed.Close() }()
	journal := sessionstate.NewJournal(f.st)
	ctx := t.Context()
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

	// turn journals one exchange of run id as a turn did before: the user
	// entry as said says ("passages": the request a turn with retrieved
	// passages sent, "asked": the request as it was asked, "": none), then
	// the answer. ledgered says whether the ledger holds the run, and ended
	// whether it has ended.
	turn := func(tenant, id, source, text string, context map[string]string, said, surfaceID string, ledgered, ended bool) {
		t.Helper()
		tag := wire.NewEvent(wire.Raise{ID: id, Tenant: tenant, Origin: "api", Agent: "docs", Text: text, Context: context, Actor: event.Actor{ID: "ada"}, At: at})
		tag.Source = source
		runID := runtime.RunID(tenant, id)
		in := runtime.Input{Trigger: tag, Subscriber: "docs", Run: runID}
		f.runs[runID] = in
		if err := seed.Runs.Record(runID, tag, 1); err != nil {
			t.Fatal(err)
		}
		if ledgered {
			raw, err := json.Marshal(in)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.ledger.Accept(ctx, drun.Run{ID: runID, Name: runtime.ActivityTurn, Input: raw, Status: drun.StatusPending, MaxAttempts: 5, CreatedAt: at}); err != nil {
				t.Fatal(err)
			}
			if ended {
				if err := f.ledger.Complete(ctx, runID, 1, drun.Result{Status: drun.StatusSucceeded}); err != nil {
					t.Fatal(err)
				}
			}
		}
		var request string
		switch said {
		case "":
			return
		case "asked":
			if request, err = event.Render(tag); err != nil {
				t.Fatal(err)
			}
		case "passages":
			text, context, _ := event.Prompt(tag.Data)
			request = event.RequestText(passages+"\n\n"+text, context)
		}
		key := thread.KeyOf(tag, "docs")
		if _, err := journal.Append(ctx, key,
			thread.Entry{Role: thread.RoleUser, Text: request, Actor: tag.Data.(event.MessagePosted).Actor, RunID: runID, SurfaceID: surfaceID, At: at},
			thread.Entry{Role: thread.RoleAssistant, Text: "answered from " + marker, Actor: event.Actor{ID: "docs", Bot: true}, RunID: runID, At: at},
		); err != nil {
			t.Fatal(err)
		}
	}
	one, two := "slack://T1/C1?thread=100.1", "slack://T1/C2?thread=200.1"
	pr := map[string]string{"pr": "7"}
	turn("acme", "one-passages", one, "what changed in pull 7?", pr, "passages", "100.2", true, true)
	turn("acme", "one-asked", one, "and the tests?", nil, "asked", "", true, true)
	turn("acme", "one-unledgered", one, "who reviewed it?", nil, "passages", "", false, false)
	turn("acme", "one-plain", one, "is it merged?", nil, "passages", "", true, true)
	turn("acme", "two-passages", two, "what is the plan?", nil, "passages", "", true, true)
	turn("acme", "two-running", two, "and then?", nil, "", "", true, false)
	turn("other", "other-passages", one, "what changed?", nil, "passages", "", true, true)
	return f
}

// entries are tenant's journal records, by key, decoded.
func (f *journalFixture) entries(t *testing.T, tenant string) map[string]thread.Entry {
	t.Helper()
	recs, err := f.st.Load(t.Context(), journalNS)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]thread.Entry{}
	for _, r := range recs {
		if r.Tenant != tenant || (!strings.HasPrefix(r.Key, "e:") && !strings.HasPrefix(r.Key, "i:")) {
			continue
		}
		var e thread.Entry
		if err := json.Unmarshal(r.Value, &e); err != nil {
			t.Fatal(err)
		}
		out[r.Key] = e
	}
	return out
}

// request is the text of every record of run's user entry, and how many
// records there are.
func (f *journalFixture) request(t *testing.T, tenant, id string) ([]string, int) {
	t.Helper()
	runID := runtime.RunID(tenant, id)
	var texts []string
	for _, e := range f.entries(t, tenant) {
		if e.RunID == runID && e.Role == thread.RoleUser {
			texts = append(texts, e.Text)
		}
	}
	return texts, len(texts)
}

func (f *journalFixture) asked(t *testing.T, tenant, id string) string {
	t.Helper()
	s, err := event.Render(f.runs[runtime.RunID(tenant, id)].Trigger)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (f *journalFixture) snapshot(t *testing.T) []state.Record {
	t.Helper()
	recs, err := f.st.Load(t.Context(), journalNS)
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

func (f *journalFixture) migrate(t *testing.T, at journalTarget) string {
	t.Helper()
	var out bytes.Buffer
	if err := journalWith(t.Context(), f.b, "postgres db/humagen", at, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), marker) || strings.Contains(out.String(), "what changed") {
		t.Fatalf("the command printed an entry's text:\n%s", out.String())
	}
	return out.String()
}

func allAre(texts []string, want string) bool {
	for _, s := range texts {
		if s != want {
			return false
		}
	}
	return len(texts) > 0
}

// A dry run writes nothing and says what an apply would write, by record key
// and run; an apply rewrites each request journaled with passages to the
// request as it was asked, in every record of its entry, and leaves the rest;
// a second apply writes nothing.
func TestMigrateJournalRewritesRequestsToTheRequestAsItWasAsked(t *testing.T) {
	f := newJournalFixture(t)
	before := f.snapshot(t)
	otherBefore := f.entries(t, "other")

	got := f.migrate(t, journalTarget{tenant: "acme"})
	if !reflect.DeepEqual(before, f.snapshot(t)) {
		t.Fatal("a dry run wrote to the store")
	}
	for _, want := range []string{
		"humagen migrate journal (dry run: nothing is written",
		"store: postgres db/humagen", "unmatched requests: leave", "tenants: acme\n",
		"tenant acme: 5 requests read",
		"request rewritten: e:", "run " + runtime.RunID("acme", "one-passages") + " (3 records)",
		"run " + runtime.RunID("acme", "one-plain") + " (2 records)",
		"request not matched: e:", "run " + runtime.RunID("acme", "one-unledgered") + " (the ledger holds no such run)",
		"thread left: a run has not ended: ", "run " + runtime.RunID("acme", "two-running"),
		"request already as asked           1", "request in a thread left           1",
		"would write 2 changes in 1 tenants",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the dry run did not say %q:\n%s", want, got)
		}
	}

	answers := func(tenant string) map[string]thread.Entry {
		out := map[string]thread.Entry{}
		for k, e := range f.entries(t, tenant) {
			if e.Role == thread.RoleAssistant {
				out[k] = e
			}
		}
		return out
	}
	answersBefore := answers("acme")
	unledgered, _ := f.request(t, "acme", "one-unledgered")
	twoBefore, _ := f.request(t, "acme", "two-passages")

	got = f.migrate(t, journalTarget{tenant: "acme", apply: true})
	if !strings.Contains(got, "(applied)") || !strings.Contains(got, "wrote 2 changes in 1 tenants") {
		t.Fatalf("the apply said:\n%s", got)
	}
	for id, n := range map[string]int{"one-passages": 3, "one-plain": 2} {
		texts, records := f.request(t, "acme", id)
		if records != n || !allAre(texts, f.asked(t, "acme", id)) {
			t.Fatalf("%s: records %d, texts %q; want %d records of %q", id, records, texts, n, f.asked(t, "acme", id))
		}
	}
	if texts, _ := f.request(t, "acme", "one-asked"); !allAre(texts, f.asked(t, "acme", "one-asked")) {
		t.Fatalf("a request as it was asked changed: %q", texts)
	}
	if texts, _ := f.request(t, "acme", "one-unledgered"); !reflect.DeepEqual(texts, unledgered) {
		t.Fatalf("an unmatched request changed: %q", texts)
	}
	if texts, _ := f.request(t, "acme", "two-passages"); !reflect.DeepEqual(texts, twoBefore) {
		t.Fatalf("a request in a thread with a run not ended changed: %q", texts)
	}
	if !reflect.DeepEqual(answers("acme"), answersBefore) {
		t.Fatal("an answer changed")
	}
	if !reflect.DeepEqual(f.entries(t, "other"), otherBefore) {
		t.Fatal("another tenant's journal changed")
	}
	// The thread's history reads as it did, with the request as it was asked.
	tag := f.runs[runtime.RunID("acme", "one-passages")].Trigger
	history, err := sessionstate.NewJournal(f.st).History(t.Context(), thread.KeyOf(tag, "docs"), 8)
	if err != nil || len(history) != 8 || history[0].Text != f.asked(t, "acme", "one-passages") || history[0].Seq != 1 {
		t.Fatalf("history = %+v, %v", history, err)
	}

	applied := f.snapshot(t)
	got = f.migrate(t, journalTarget{tenant: "acme", apply: true})
	if !reflect.DeepEqual(applied, f.snapshot(t)) || !strings.Contains(got, "wrote 0 changes") || !strings.Contains(got, "request already as asked           3") {
		t.Fatalf("a second apply wrote:\n%s", got)
	}

	// Once the run has ended, its thread is rewritten too.
	if err := f.ledger.Complete(t.Context(), runtime.RunID("acme", "two-running"), 1, drun.Result{Status: drun.StatusSucceeded}); err != nil {
		t.Fatal(err)
	}
	got = f.migrate(t, journalTarget{apply: true})
	if !strings.Contains(got, "tenants: acme, other") || !strings.Contains(got, "wrote 2 changes in 2 tenants") {
		t.Fatalf("the apply over every tenant said:\n%s", got)
	}
	for _, c := range [][2]string{{"acme", "two-passages"}, {"other", "other-passages"}} {
		if texts, _ := f.request(t, c[0], c[1]); !allAre(texts, f.asked(t, c[0], c[1])) {
			t.Fatalf("%s %s: %q", c[0], c[1], texts)
		}
	}
}

// With -unmatched replace, a request not matched to its run is set to a
// fixed line in every record of its entry, and a second apply leaves it.
func TestMigrateJournalReplacesAnUnmatchedRequestWhenAsked(t *testing.T) {
	f := newJournalFixture(t)
	got := f.migrate(t, journalTarget{tenant: "acme", replace: true, apply: true})
	if !strings.Contains(got, "request replaced: e:") || !strings.Contains(got, "wrote 3 changes") {
		t.Fatalf("the apply said:\n%s", got)
	}
	if texts, n := f.request(t, "acme", "one-unledgered"); n != 2 || !allAre(texts, sessionstate.UnkeptRequest) {
		t.Fatalf("the unmatched request is %q in %d records", texts, n)
	}
	if texts, _ := f.request(t, "acme", "one-asked"); !allAre(texts, f.asked(t, "acme", "one-asked")) {
		t.Fatalf("a request as it was asked changed: %q", texts)
	}
	applied := f.snapshot(t)
	got = f.migrate(t, journalTarget{tenant: "acme", replace: true, apply: true})
	if !reflect.DeepEqual(applied, f.snapshot(t)) || !strings.Contains(got, "wrote 0 changes") || !strings.Contains(got, "request already replaced           1") {
		t.Fatalf("a second apply wrote:\n%s", got)
	}
}

// The command refuses the in-memory store and an unknown -unmatched.
func TestMigrateJournalRefusesTheMemoryStore(t *testing.T) {
	var out bytes.Buffer
	err := migrateJournal(t.Context(), []string{"-apply"}, &out, noEnv)
	if !errors.Is(err, errNothingToMigrate) || !strings.HasPrefix(err.Error(), "migrate journal: nothing to migrate: ") {
		t.Fatalf("the memory store was written: %v", err)
	}
	empty := platform.Backends{State: state.NewMemory()}
	err = journalWith(t.Context(), empty, "postgres db/humagen", journalTarget{}, &out)
	if !errors.Is(err, errNothingToMigrate) || strings.Contains(err.Error(), "sessions") || !strings.Contains(err.Error(), "postgres db/humagen holds no runs") {
		t.Fatalf("a store with no runs was read: %v", err)
	}
	if err := migrateJournal(t.Context(), []string{"-unmatched", "drop", "-allow-empty-store"}, &out, noEnv); err == nil {
		t.Fatal("an unknown -unmatched was accepted")
	}
	out.Reset()
	if err := migrateCmd([]string{"help"}, &out); err != nil || !strings.Contains(out.String(), "humagen migrate journal") {
		t.Fatalf("the usage does not name the journal migration: %v\n%s", err, out.String())
	}
	out.Reset()
	if err := migrateJournal(t.Context(), []string{"-allow-empty-store"}, &out, noEnv); err != nil || !strings.Contains(out.String(), "would write 0 changes in 0 tenants") {
		t.Fatalf("-allow-empty-store = %v\n%s", err, out.String())
	}
}

// migration starts a journal migration on f's stores, as journalWith does.
func (f *journalFixture) migration(t *testing.T) *platform.JournalMigration {
	t.Helper()
	p, err := platform.New(platform.Config{Backends: f.b, Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	m, err := p.StartJournalMigration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// accept puts a turn that has not ended in the ledger, with input as its
// input, and records it in tenant's run index when indexed.
func (f *journalFixture) accept(t *testing.T, tenant, id string, input []byte, indexed bool) string {
	t.Helper()
	tag := wire.NewEvent(wire.Raise{ID: id, Tenant: tenant, Origin: "api", Agent: "docs", Text: "later", Actor: event.Actor{ID: "ada"}, At: time.Now()})
	tag.Source = "slack://T1/C1?thread=100.1"
	runID := runtime.RunID(tenant, id)
	if input == nil {
		raw, err := json.Marshal(runtime.Input{Trigger: tag, Subscriber: "docs", Run: runID})
		if err != nil {
			t.Fatal(err)
		}
		input = raw
	}
	if indexed {
		p, err := platform.New(platform.Config{Backends: f.b, Workspace: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Runs.Record(runID, tag, 1); err != nil {
			t.Fatal(err)
		}
		_ = p.Close()
	}
	if err := f.ledger.Accept(t.Context(), drun.Run{ID: runID, Name: runtime.ActivityTurn, Input: input, Status: drun.StatusPending, MaxAttempts: 5, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return runID
}

// A turn that starts in a thread after the migration read the runs that
// have not ended is found just before the thread is written, and the thread
// is left.
func TestMigrateJournalChecksAgainBeforeWriting(t *testing.T) {
	f := newJournalFixture(t)
	m := f.migration(t)
	before, _ := f.request(t, "acme", "one-passages")
	later := f.accept(t, "acme", "one-later", nil, true)

	rep, err := m.Migrate(t.Context(), "acme", true, false)
	if err != nil {
		t.Fatal(err)
	}
	var line sessionstate.RequestLine
	for _, l := range rep.Lines {
		if l.Kind == sessionstate.ThreadLeft && l.Run == later {
			line = l
		}
	}
	if line.Kind == "" || line.Detail == "" {
		t.Fatalf("the thread of the later turn is not listed: %+v", rep.Lines)
	}
	if texts, _ := f.request(t, "acme", "one-passages"); !reflect.DeepEqual(texts, before) {
		t.Fatalf("a thread with a turn that started later was written: %q", texts)
	}
}

// A run that has not ended and whose input cannot be read leaves the tenant
// the run index holds it in, listed with the run, and the other tenants are
// migrated. One the run index holds in no tenant leaves every tenant.
func TestMigrateJournalLeavesTheTenantOfARunItCannotRead(t *testing.T) {
	f := newJournalFixture(t)
	unread := f.accept(t, "acme", "acme-unread", []byte("not a run input"), true)
	got := f.migrate(t, journalTarget{apply: true})
	for _, want := range []string{
		"tenant acme: 0 requests read", "tenant left: a run cannot be read: acme run " + unread,
		"wrote 1 changes in 2 tenants",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the apply did not say %q:\n%s", want, got)
		}
	}
	if texts, _ := f.request(t, "other", "other-passages"); !allAre(texts, f.asked(t, "other", "other-passages")) {
		t.Fatalf("the other tenant was not migrated: %q", texts)
	}
	if texts, _ := f.request(t, "acme", "one-plain"); allAre(texts, f.asked(t, "acme", "one-plain")) {
		t.Fatal("the tenant of a run that cannot be read was written")
	}

	g := newJournalFixture(t)
	g.accept(t, "acme", "nowhere-unread", []byte("not a run input"), false)
	got = g.migrate(t, journalTarget{apply: true})
	if !strings.Contains(got, "wrote 0 changes in 2 tenants") || strings.Count(got, "tenant left: a run cannot be read: ") != 2 {
		t.Fatalf("a run held in no tenant did not leave every tenant:\n%s", got)
	}
}
