package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/rooms"
)

type frame struct {
	name string
	data map[string]any
}

// follow opens a conversation's stream and returns its frames as they
// arrive. It waits until the reader has joined the room.
func (f *fixture) follow(t *testing.T, tok, thread string, p *platform.Platform, viewers int) <-chan frame {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", f.srv.URL+"/api/events?thread="+thread, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("following %s = %d", thread, res.StatusCode)
	}
	out := make(chan frame, 256)
	go func() {
		defer func() { _ = res.Body.Close() }()
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		name := ""
		for sc.Scan() {
			line := sc.Text()
			if v, ok := strings.CutPrefix(line, "event: "); ok {
				name = v
				continue
			}
			if v, ok := strings.CutPrefix(line, "data: "); ok {
				var m map[string]any
				_ = json.Unmarshal([]byte(v), &m)
				out <- frame{name, m}
				name = ""
			}
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for p.Rooms.Viewers("acme", thread) < viewers {
		if time.Now().After(deadline) {
			t.Fatalf("the room has %d viewers, want %d", p.Rooms.Viewers("acme", thread), viewers)
		}
		time.Sleep(5 * time.Millisecond)
	}
	return out
}

// next returns the first frame named name, skipping others.
func next(t *testing.T, frames <-chan frame, name string, within time.Duration) (frame, bool) {
	t.Helper()
	timeout := time.After(within)
	for {
		select {
		case fr := <-frames:
			if fr.name == name {
				return fr, true
			}
		case <-timeout:
			return frame{}, false
		}
	}
}

func startThread(t *testing.T, f *fixture, tok string) string {
	t.Helper()
	res, out := f.do(t, nil, "POST", "/api/threads", tok, `{"visibility":"shared","text":"hello room","post_only":true}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("starting a thread = %d %v", res.StatusCode, out)
	}
	return out["thread"].(map[string]any)["id"].(string)
}

// A small room is a broadcast: every reader is sent every notice and every
// run event of the conversation as it happens, as the workspace's stream
// sends them.
func TestASmallRoomStreamsEverythingAtOnce(t *testing.T) {
	f := newFixture(t)
	admin := f.machine(t, "acme", everything...)
	f.do(t, nil, "POST", "/api/agents", admin, helperSpec)
	ana := f.person(t, "acme", "00u-ana", "Ana", "humagen-admins")
	bo := f.person(t, "acme", "00u-bo", "Bo", "humagen-admins")
	id := startThread(t, f, ana)
	anas := f.follow(t, ana, id, f.p, 1)
	bos := f.follow(t, bo, id, f.p, 2)

	if res, out := f.do(t, nil, "POST", "/api/threads/"+id+"/messages", bo, `{"text":"@helper say hello"}`); res.StatusCode != http.StatusCreated {
		t.Fatalf("posting = %d %v", res.StatusCode, out)
	}
	for who, frames := range map[string]<-chan frame{"ana": anas, "bo": bos} {
		if n, ok := next(t, frames, "thread", 5*time.Second); !ok || n.data["id"] != id {
			t.Fatalf("%s was not told of the message: %v", who, n)
		}
		if e, ok := next(t, frames, "", 10*time.Second); !ok || !strings.HasPrefix(e.data["type"].(string), "run.") {
			t.Fatalf("%s was not sent the run's events: %v", who, e)
		}
	}
	if _, ok := next(t, anas, "page", 200*time.Millisecond); ok {
		t.Fatal("a small room sent a page")
	}
}

// In a large room a reader is sent at once what involves them, and the rest
// as a page.
func TestALargeRoomPagesWhatDoesNotInvolveYou(t *testing.T) {
	f := newFixtureWith(t, platform.Config{Rooms: rooms.Config{CoalesceAbove: 1, TailInterval: rooms.MinTailInterval}})
	ana := f.person(t, "acme", "00u-ana", "Ana", "humagen-admins")
	bo := f.person(t, "acme", "00u-bo", "Bo", "humagen-admins")
	id := startThread(t, f, ana)
	anas := f.follow(t, ana, id, f.p, 1)
	bos := f.follow(t, bo, id, f.p, 2)

	if res, out := f.do(t, nil, "POST", "/api/threads/"+id+"/messages", bo, `{"text":"just me"}`); res.StatusCode != http.StatusCreated {
		t.Fatalf("posting = %d %v", res.StatusCode, out)
	}
	if _, ok := next(t, bos, "thread", 5*time.Second); !ok {
		t.Fatal("Bo was not told of his own message at once")
	}
	page, ok := next(t, anas, "page", 5*time.Second)
	if !ok {
		t.Fatal("Ana was sent no page")
	}
	raw, _ := json.Marshal(page.data)
	value, _ := jsonschema.UnmarshalJSON(strings.NewReader(string(raw)))
	if err := loadSpec(t).component(t, "RoomPage").Validate(value); err != nil {
		t.Errorf("a page does not match RoomPage: %v\n%s", err, raw)
	}
	if page.data["thread"] != id || page.data["messages"].(float64) < 1 || page.data["viewers"].(float64) != 2 {
		t.Fatalf("page = %v", page.data)
	}
	// Bo's message reached Ana only as the page.
	select {
	case fr := <-anas:
		if fr.name == "thread" {
			t.Fatalf("Ana was sent Bo's message at once: %v", fr)
		}
	default:
	}
}

func TestARoomStreamIsOneConversationsAndChecked(t *testing.T) {
	f := newFixture(t)
	ana := f.person(t, "acme", "00u-ana", "Ana", "humagen-admins")
	id := startThread(t, f, ana)
	for _, q := range []string{"thread=" + id + "&run=r1", "thread=" + id + "&from=3", "thread=" + id + "&inbox=true", "thread=", "thread=" + id + "&channel=slack://T1"} {
		if res, out := f.do(t, nil, "GET", "/api/events?"+q, ana, ""); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %d %v, want 400", q, res.StatusCode, out)
		}
	}
	if res, _ := f.do(t, nil, "GET", "/api/events?thread=th-0000000000000000", ana, ""); res.StatusCode != http.StatusNotFound {
		t.Errorf("an unknown thread = %d, want 404", res.StatusCode)
	}
	other := f.person(t, "globex", "00u-gus", "Gus", "humagen-admins")
	if res, _ := f.do(t, nil, "GET", "/api/events?thread="+id, other, ""); res.StatusCode != http.StatusNotFound {
		t.Errorf("another workspace's thread = %d, want 404", res.StatusCode)
	}
}

// A conversation starts at most its rate of runs in a minute. A tag past it
// is refused in the conversation's own words, and the message stays posted.
func TestAConversationStartsAtMostItsRate(t *testing.T) {
	f := newFixtureWith(t, platform.Config{Rooms: rooms.Config{RoomRunsPerMinute: 1}})
	f.do(t, nil, "POST", "/api/agents", f.machine(t, "acme", everything...), helperSpec)
	ana := f.person(t, "acme", "00u-ana", "Ana", "humagen-admins")
	id := startThread(t, f, ana)
	if res, out := f.do(t, nil, "POST", "/api/threads/"+id+"/messages", ana, `{"text":"@helper one"}`); res.StatusCode != http.StatusCreated || len(out["runs"].([]any)) != 1 {
		t.Fatalf("the first = %d %v", res.StatusCode, out)
	}
	res, out := f.do(t, nil, "POST", "/api/threads/"+id+"/messages", ana, `{"text":"@helper two"}`)
	if res.StatusCode != http.StatusCreated || len(out["runs"].([]any)) != 0 {
		t.Fatalf("the second = %d %v", res.StatusCode, out)
	}
	refused, _ := out["refused"].([]any)
	if len(refused) != 1 || !strings.Contains(refused[0].(map[string]any)["refused"].(string), "this conversation has started 1 runs in the last minute") {
		t.Fatalf("refused = %v", refused)
	}
}
