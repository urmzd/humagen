package threads_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/warp/pkg/threads"
)

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

// run is a run's id, as fiber makes them.
const run = "r0123456789abcdef0123456789abcdef"

func shared(project, channel string) threads.Thread {
	return threads.Thread{Project: project, Channel: channel, Visibility: threads.Shared}
}

func msg(by, text string) threads.Message {
	return threads.Message{By: by, Name: strings.ToUpper(by[:1]) + by[1:], Text: text}
}

func start(t *testing.T, s *threads.Store, tenant string, th threads.Thread, m threads.Message) (threads.Thread, threads.Message) {
	t.Helper()
	got, first, err := s.Start(tenant, th, m, t0)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return got, first
}

func post(t *testing.T, s *threads.Store, tenant, id string, m threads.Message, at time.Time) threads.Message {
	t.Helper()
	got, created, err := s.Post(tenant, id, m, at)
	if err != nil || !created {
		t.Fatalf("post: %+v, %v, %v", got, created, err)
	}
	return got
}

func TestStartingAThreadMakesThePosterItsOwner(t *testing.T) {
	s := threads.NewStore()
	th, first := start(t, s, "acme", shared("Globex", "renewals"), msg("dana", "Where are we on the renewal?"))
	switch {
	case !threads.ValidID(th.ID) || !strings.HasPrefix(th.ID, "th-"):
		t.Fatalf("id = %q", th.ID)
	case th.Version != 1 || th.CreatedBy != "dana" || len(th.Participants) != 1 || !th.Participants[0].Owner || th.Participants[0].Kind != threads.Person:
		t.Fatalf("thread = %+v", th)
	case first.ID != "m-1" || first.Thread != th.ID || first.Prev != "" || first.Via != threads.ViaConsole || first.Mentions == nil:
		t.Fatalf("first message = %+v", first)
	}
	if _, _, err := s.Start("acme", threads.Thread{Project: "Globex", Channel: "renewals", Visibility: threads.Shared, CreatedBy: "eve"}, msg("dana", "x"), t0); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("a thread started by someone other than its first poster = %v", err)
	}
	// A conversation from before threads gains its record under its own id.
	legacy, err := s.Create("acme", threads.Thread{ID: run, Project: "Globex", Channel: "general", Visibility: threads.Shared, CreatedBy: "dana"}, t0)
	if err != nil || legacy.ID != run {
		t.Fatalf("a record for an old conversation = %+v, %v", legacy, err)
	}
	if _, err := s.Create("acme", threads.Thread{ID: run, Project: "Globex", Channel: "general", Visibility: threads.Shared, CreatedBy: "dana"}, t0); !errors.Is(err, threads.ErrExists) {
		t.Fatalf("the same id twice = %v", err)
	}
	if m := post(t, s, "acme", run, threads.Message{By: "dana", Text: "and then?", Prev: run}, t0); m.Prev != run {
		t.Fatalf("the first message after the old runs = %+v", m)
	}
}

func TestFortyRacersPostingToOneThreadKeepOneLineAndTheCap(t *testing.T) {
	s := threads.NewStore()
	th, _ := start(t, s, "acme", shared("Globex", "renewals"), msg("dana", "go"))
	// Fill the thread to 30 short of the cap, then race 40 posters for the
	// last 30 places.
	for i := 1; i < threads.MaxMessages-30; i++ {
		post(t, s, "acme", th.ID, msg("dana", "x"), t0)
	}
	var wg sync.WaitGroup
	var posted, full atomic.Int64
	for i := range 40 {
		wg.Go(func() {
			_, _, err := s.Post("acme", th.ID, msg(fmt.Sprintf("p%02d", i), "me too"), t0.Add(time.Second))
			switch {
			case err == nil:
				posted.Add(1)
			case errors.Is(err, threads.ErrFull):
				full.Add(1)
			default:
				t.Errorf("post: %v", err)
			}
		})
	}
	wg.Wait()
	if posted.Load() != 30 || full.Load() != 10 {
		t.Fatalf("%d posted and %d refused, want 30 and 10", posted.Load(), full.Load())
	}
	var all []threads.Message
	for after := ""; ; {
		page, next, err := s.Messages("acme", th.ID, after, 0)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, page...)
		if next == "" {
			break
		}
		after = next
	}
	if len(all) != threads.MaxMessages {
		t.Fatalf("%d messages kept, want %d", len(all), threads.MaxMessages)
	}
	for i, m := range all {
		want := ""
		if i > 0 {
			want = all[i-1].ID
		}
		if m.ID != threads.MessageID(i+1) || m.Prev != want {
			t.Fatalf("message %d = %s after %q, want %s after %q", i, m.ID, m.Prev, threads.MessageID(i+1), want)
		}
	}
	got, _ := s.Get("acme", th.ID)
	if len(got.Participants) != 31 || got.Version != 1 {
		t.Fatalf("%d participants at version %d; want every poster who got in, and no new version", len(got.Participants), got.Version)
	}
}

// A private thread is kept as one, with the person who started it taking
// part as its owner; a shared thread can be made private, and a thread with
// no visibility is refused.
func TestAPrivateThreadIsKeptWithItsOwnerTakingPart(t *testing.T) {
	s := threads.NewStore()
	private := threads.Thread{Project: "Globex", Channel: "general", Visibility: threads.Private}
	th, _, err := s.Start("acme", private, msg("dana", "x"), t0)
	if err != nil || th.Visibility != threads.Private {
		t.Fatalf("starting a private thread = %+v, %v", th, err)
	}
	if len(th.Participants) != 1 || th.Participants[0].Name != "dana" || !th.Participants[0].Owner {
		t.Fatalf("its participants = %+v", th.Participants)
	}
	if _, _, err := s.Start("acme", threads.Thread{Project: "Globex", Channel: "general"}, msg("dana", "x"), t0); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("a thread with no visibility = %v", err)
	}
	shared, _ := start(t, s, "acme", shared("Globex", "general"), msg("dana", "x"))
	got, err := s.Update("acme", shared.ID, 1, t0, func(t *threads.Thread) error { t.Visibility = threads.Private; return nil })
	if err != nil || got.Visibility != threads.Private {
		t.Fatalf("making a thread private = %+v, %v", got, err)
	}
	if _, _, err := s.Post("acme", th.ID, msg("dana", "still mine"), t0); err != nil {
		t.Fatalf("posting in a private thread: %v", err)
	}
}

func TestAPostWithASurfaceIDIsPostedOnce(t *testing.T) {
	s := threads.NewStore()
	th, _ := start(t, s, "acme", shared("", "general"), msg("dana", "x"))
	m := threads.Message{By: "dana", Text: "from Slack", Via: "acme-slack", SurfaceID: "1700000000.000200"}
	first := post(t, s, "acme", th.ID, m, t0)
	m.Text = "edited later"
	again, created, err := s.Post("acme", th.ID, m, t0.Add(time.Minute))
	if err != nil || created || again.ID != first.ID || again.Text != "from Slack" {
		t.Fatalf("the same ts again = %+v, created %v, %v", again, created, err)
	}
	if page, _, _ := s.Messages("acme", th.ID, "", 0); len(page) != 2 {
		t.Fatalf("%d messages, want 2", len(page))
	}
}

func TestMessageRules(t *testing.T) {
	s := threads.NewStore()
	th, _ := start(t, s, "acme", shared("Globex", "renewals"), msg("dana", "x"))
	many := make([]threads.Mention, threads.MaxMentions+1)
	for i := range many {
		many[i] = threads.Mention{Kind: threads.Person, Name: fmt.Sprintf("p%d", i)}
	}
	for _, c := range []struct {
		name string
		m    threads.Message
	}{
		{"no author", threads.Message{Text: "x"}},
		{"nothing said", threads.Message{By: "dana", Text: "  \n"}},
		{"too long", threads.Message{By: "dana", Text: strings.Repeat("x", threads.MaxText+1)}},
		{"a control character", threads.Message{By: "dana", Text: "a\x00b"}},
		{"too many tags", threads.Message{By: "dana", Text: "x", Mentions: many}},
		{"a run started by a person's tag", threads.Message{By: "dana", Text: "x", Mentions: []threads.Mention{{Kind: threads.Person, Name: "eve", Run: run}}}},
		{"an unknown way in", threads.Message{By: "dana", Text: "x", Via: "Slack App"}},
		{"after a message that is not there", threads.Message{By: "dana", Text: "x", Prev: "m-9"}},
		{"after something that is not a turn", threads.Message{By: "dana", Text: "x", Prev: "yesterday"}},
		{"an answer to no run", threads.Message{By: "dana", Text: "x", Answers: &threads.AnswerRef{Run: "r1", Question: "q1"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, _, err := s.Post("acme", th.ID, c.m, t0); !errors.Is(err, threads.ErrInvalid) {
				t.Fatalf("post = %v, want ErrInvalid", err)
			}
		})
	}
	if _, _, err := s.Post("acme", "th-0000000000000000", msg("dana", "x"), t0); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("a thread that is not there = %v", err)
	}
	answer := post(t, s, "acme", th.ID, threads.Message{By: "ana", Text: "Yes, renew.", Answers: &threads.AnswerRef{Run: run, Question: "q1"}}, t0)
	if answer.Answers == nil || answer.Answers.Run != run {
		t.Fatalf("an answer = %+v", answer)
	}
	if branch := post(t, s, "acme", th.ID, threads.Message{By: "ana", Text: "x", Prev: "m-1"}, t0); branch.Prev != "m-1" {
		t.Fatalf("a branch from the first message = %+v", branch)
	}
}

func TestSettleRecordsWhatTagsCameToAndNothingElse(t *testing.T) {
	s := threads.NewStore()
	tags := []threads.Mention{{Kind: threads.Agent, Name: "ignite"}, {Kind: threads.Person, Name: "okta|ana"}}
	th, m := start(t, s, "acme", shared("Globex", "renewals"), threads.Message{By: "dana", Text: "@ignite @ana", Mentions: tags})
	settled := []threads.Mention{{Kind: threads.Agent, Name: "ignite", Run: run}, {Kind: threads.Person, Name: "okta|ana"}}
	got, err := s.Settle("acme", th.ID, m.ID, settled)
	if err != nil || got.Mentions[0].Run != run {
		t.Fatalf("settle = %+v, %v", got, err)
	}
	if again, _ := s.Message("acme", th.ID, m.ID); again.Mentions[0].Run != run || again.Text != "@ignite @ana" {
		t.Fatalf("the message after settling = %+v", again)
	}
	for _, c := range [][]threads.Mention{
		{{Kind: threads.Agent, Name: "scribe", Run: run}, {Kind: threads.Person, Name: "okta|ana"}},
		{{Kind: threads.Agent, Name: "ignite"}},
	} {
		if _, err := s.Settle("acme", th.ID, m.ID, c); !errors.Is(err, threads.ErrInvalid) {
			t.Fatalf("settling with other tags %+v = %v", c, err)
		}
	}
	if _, err := s.Settle("acme", th.ID, "m-2", settled); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("settling a message that is not there = %v", err)
	}
}

func TestParticipantsChangeByVersionAndTheOwnerStays(t *testing.T) {
	s := threads.NewStore()
	th, _ := start(t, s, "acme", shared("Globex", "renewals"), msg("dana", "x"))
	got, err := s.Participants("acme", th.ID, 1, []threads.Participant{{Kind: threads.Agent, Name: "ignite"}, {Kind: threads.Person, Name: "okta|ana"}}, nil, nil, "dana", t0)
	if err != nil || got.Version != 2 || len(got.Participants) != 3 || got.Participants[1].AddedBy != "dana" {
		t.Fatalf("adding = %+v, %v", got, err)
	}
	if _, err := s.Participants("acme", th.ID, 1, nil, []string{"ignite"}, nil, "dana", t0); !errors.Is(err, threads.ErrVersion) {
		t.Fatalf("a stale version = %v", err)
	}
	if _, err := s.Participants("acme", th.ID, 2, nil, []string{"dana"}, nil, "ana", t0); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("removing the owner = %v", err)
	}
	if same, err := s.Participants("acme", th.ID, 2, []threads.Participant{{Kind: threads.Agent, Name: "ignite"}}, []string{"nobody"}, nil, "dana", t0); err != nil || same.Version != 2 {
		t.Fatalf("a change that changes nothing = %+v, %v", same, err)
	}
	if got, err := s.Participants("acme", th.ID, 2, nil, []string{"ignite"}, nil, "dana", t0); err != nil || got.Version != 3 || len(got.Participants) != 2 {
		t.Fatalf("removing = %+v, %v", got, err)
	}
	var crowd []threads.Participant
	for i := range threads.MaxParticipants - 1 {
		crowd = append(crowd, threads.Participant{Kind: threads.Person, Name: fmt.Sprintf("p%02d", i)})
	}
	if _, err := s.Participants("acme", th.ID, 3, crowd, nil, nil, "dana", t0); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("the 51st participant = %v", err)
	}
	if _, err := s.Participants("acme", th.ID, 3, append(crowd, crowd[:2]...), nil, nil, "dana", t0); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("more than 50 changes at once = %v", err)
	}
	if _, err := s.Participants("acme", th.ID, 3, []threads.Participant{{Kind: "bot", Name: "x"}}, nil, nil, "dana", t0); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("a participant that is neither = %v", err)
	}
	// Joining by acting changes no version, and stops at the cap.
	joined, err := s.Join("acme", th.ID, []threads.Participant{{Kind: threads.Agent, Name: "scribe"}}, t0)
	if err != nil || joined.Version != 3 || len(joined.Participants) != 3 {
		t.Fatalf("joining = %+v, %v", joined, err)
	}
}

func TestUpdateChangesTheTitleVisibilityAndDefaultOnly(t *testing.T) {
	s := threads.NewStore()
	th, _ := start(t, s, "acme", shared("Globex", "renewals"), msg("dana", "x"))
	got, err := s.Update("acme", th.ID, 1, t0, func(t *threads.Thread) error { t.Title, t.Default = "Renewal note", "ignite"; return nil })
	if err != nil || got.Version != 2 || got.Title != "Renewal note" || got.Default != "ignite" {
		t.Fatalf("update = %+v, %v", got, err)
	}
	for _, c := range []struct {
		name string
		fn   func(*threads.Thread)
		want error
	}{
		{"a stale version", func(t *threads.Thread) { t.Title = "y" }, threads.ErrVersion},
		{"another channel", func(t *threads.Thread) { t.Channel = "general" }, threads.ErrInvalid},
		{"another project", func(t *threads.Thread) { t.Project = "Initech" }, threads.ErrInvalid},
		{"participants", func(t *threads.Thread) { t.Participants = nil }, threads.ErrInvalid},
		{"a default that names nothing", func(t *threads.Thread) { t.Default = "@ignite" }, threads.ErrInvalid},
		{"a title of two lines", func(t *threads.Thread) { t.Title = "a\nb" }, threads.ErrInvalid},
	} {
		t.Run(c.name, func(t *testing.T) {
			version := 2
			if c.want == threads.ErrVersion {
				version = 1
			}
			if _, err := s.Update("acme", th.ID, version, t0, func(th *threads.Thread) error { c.fn(th); return nil }); !errors.Is(err, c.want) {
				t.Fatalf("update = %v, want %v", err, c.want)
			}
		})
	}
	if same, err := s.Update("acme", th.ID, 2, t0, func(*threads.Thread) error { return nil }); err != nil || same.Version != 2 {
		t.Fatalf("an update that changes nothing = %+v, %v", same, err)
	}
}

func TestMessagesPageInOrderAndAPathFollowsItsBranch(t *testing.T) {
	s := threads.NewStore()
	th, _ := start(t, s, "acme", shared("Globex", "renewals"), msg("dana", "one"))
	post(t, s, "acme", th.ID, msg("ana", "two"), t0)
	post(t, s, "acme", th.ID, msg("dana", "three"), t0)
	post(t, s, "acme", th.ID, threads.Message{By: "ana", Text: "two, again", Prev: "m-1"}, t0)
	page, next, err := s.Messages("acme", th.ID, "", 2)
	if err != nil || len(page) != 2 || page[0].ID != "m-1" || next != "m-2" {
		t.Fatalf("first page = %+v, %q, %v", page, next, err)
	}
	page, next, _ = s.Messages("acme", th.ID, next, 2)
	if len(page) != 2 || page[1].ID != "m-4" || next != "" {
		t.Fatalf("last page = %+v, %q", page, next)
	}
	if _, _, err := s.Messages("acme", th.ID, "m-99", 2); !errors.Is(err, threads.ErrCursor) {
		t.Fatalf("a bad cursor = %v", err)
	}
	path, err := s.Path("acme", th.ID, "m-4")
	if err != nil || len(path) != 2 || path[0].ID != "m-1" || path[1].ID != "m-4" {
		t.Fatalf("the branch's path = %+v, %v", path, err)
	}
	if path, _ := s.Path("acme", th.ID, "m-3"); len(path) != 3 {
		t.Fatalf("the main line's path = %+v", path)
	}
}

func TestListPagesAChannelsThreadsMostRecentlyActiveFirst(t *testing.T) {
	s := threads.NewStore()
	var ids []string
	for i := range 5 {
		th, _, err := s.Start("acme", shared("Globex", "renewals"), msg("dana", "x"), t0.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, th.ID)
	}
	start(t, s, "acme", shared("Globex", "general"), msg("dana", "elsewhere"))
	start(t, s, "acme", shared("Initech", "renewals"), msg("dana", "another project"))
	// The oldest becomes the most recently active.
	post(t, s, "acme", ids[0], msg("ana", "bump"), t0.Add(time.Hour))
	want := []string{ids[0], ids[4], ids[3], ids[2], ids[1]}
	var got []string
	for before := ""; ; {
		page, next, err := s.List("acme", threads.Query{Project: "Globex", Channel: "renewals", Limit: 2, Before: before})
		if err != nil {
			t.Fatal(err)
		}
		for _, th := range page {
			got = append(got, th.ID)
		}
		if next == "" {
			break
		}
		before = next
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
	if all, _, _ := s.List("acme", threads.Query{Project: "Globex"}); len(all) != 6 {
		t.Fatalf("every channel of Globex = %d threads", len(all))
	}
	if _, _, err := s.List("acme", threads.Query{Project: "Globex", Before: "not a cursor"}); !errors.Is(err, threads.ErrCursor) {
		t.Fatalf("a bad cursor = %v", err)
	}
	if empty, next, err := s.List("acme", threads.Query{Project: "Nowhere"}); err != nil || empty == nil || len(empty) != 0 || next != "" {
		t.Fatalf("a project with no threads = %v, %q, %v", empty, next, err)
	}
}

func TestThreadsAreTheirTenants(t *testing.T) {
	s := threads.NewStore()
	th, m := start(t, s, "acme", shared("Globex", "renewals"), msg("dana", "x"))
	if _, err := s.Get("globex", th.ID); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("another tenant's thread = %v", err)
	}
	if _, err := s.Message("globex", th.ID, m.ID); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("another tenant's message = %v", err)
	}
	if _, _, err := s.Post("globex", th.ID, msg("eve", "x"), t0); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("posting to another tenant's thread = %v", err)
	}
	if _, err := s.Participants("globex", th.ID, 1, nil, []string{"dana"}, nil, "eve", t0); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("changing another tenant's thread = %v", err)
	}
	if page, _, _ := s.List("globex", threads.Query{Project: "Globex"}); len(page) != 0 {
		t.Fatalf("another tenant's list = %+v", page)
	}
}

func TestWatchTellsWhatHappenedAndNothingAboutIt(t *testing.T) {
	s := threads.NewStore()
	ch, cancel := s.Watch("acme")
	defer cancel()
	other, cancelOther := s.Watch("globex")
	defer cancelOther()
	th, _ := start(t, s, "acme", shared("Globex", "renewals"), msg("dana", "secret plans"))
	post(t, s, "acme", th.ID, msg("ana", "more"), t0)
	for _, want := range []string{"m-1", "m-2"} {
		select {
		case n := <-ch:
			if n.ID != th.ID || n.Message != want {
				t.Fatalf("notice = %+v, want %s", n, want)
			}
		case <-time.After(time.Second):
			t.Fatal("no notice")
		}
	}
	select {
	case n := <-other:
		t.Fatalf("another tenant was told %+v", n)
	default:
	}
}

// A workspace holds at most MaxThreads threads and a project at most
// MaxProjectThreads, so nobody who may post can grow the store without end.
// The thread one past a cap is refused (ErrLimit) and nothing is kept.
func TestThreadsAreCappedByWorkspaceAndByProject(t *testing.T) {
	s := threads.NewStore()
	full := func(project string) {
		for range threads.MaxProjectThreads {
			if _, _, err := s.Start("acme", shared(project, "general"), msg("dana", "hi"), t0); err != nil {
				t.Fatalf("a thread in %s under its cap = %v", project, err)
			}
		}
	}
	full("Globex")
	if _, _, err := s.Start("acme", shared("Globex", "general"), msg("dana", "one more"), t0); !errors.Is(err, threads.ErrLimit) {
		t.Fatalf("one past a project's cap = %v, want ErrLimit", err)
	}
	if _, err := s.Create("acme", threads.Thread{ID: run, Project: "Globex", Channel: "general", Visibility: threads.Shared, CreatedBy: "dana"}, t0); !errors.Is(err, threads.ErrLimit) {
		t.Fatalf("a conversation's record one past a project's cap = %v, want ErrLimit", err)
	}
	for i := 1; i < threads.MaxThreads/threads.MaxProjectThreads; i++ {
		full(fmt.Sprintf("P%d", i))
	}
	if _, _, err := s.Start("acme", shared("Elsewhere", "general"), msg("dana", "one more"), t0); !errors.Is(err, threads.ErrLimit) {
		t.Fatalf("one past a workspace's cap = %v, want ErrLimit", err)
	}
	if _, _, err := s.Start("initech", shared("Globex", "general"), msg("dana", "hi"), t0); err != nil {
		t.Fatalf("another workspace's thread = %v", err)
	}
	if got, _ := s.Summaries("acme", func(threads.Thread) bool { return true }); len(got) != threads.MaxThreads {
		t.Fatalf("acme holds %d threads, want %d", len(got), threads.MaxThreads)
	}
}

// A list copies only the records it keeps, names the rest as elsewhere, and
// carries no more of a first message than a title holds.
func TestSummariesCopyOnlyWhatTheyKeep(t *testing.T) {
	s := threads.NewStore()
	here, _ := start(t, s, "acme", shared("Globex", "renewals"), msg("dana", strings.Repeat("long ", 2*threads.MaxTitle)))
	there, _ := start(t, s, "acme", shared("Initech", "general"), msg("dana", "elsewhere"))
	got, elsewhere := s.Summaries("acme", func(th threads.Thread) bool { return th.Project == "Globex" })
	if len(got) != 1 || got[0].ID != here.ID || len([]rune(got[0].First)) != threads.MaxTitle || got[0].Messages != 1 {
		t.Fatalf("kept %+v", got)
	}
	if len(elsewhere) != 1 || !elsewhere[there.ID] {
		t.Fatalf("elsewhere = %v", elsewhere)
	}
}
