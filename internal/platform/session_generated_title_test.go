package platform_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// titles is a Titler that answers name, and keeps what it was asked.
type titles struct {
	mu    sync.Mutex
	asked []platform.TitleInput
	name  string
	err   error
	// hold, when set, keeps every answer until it is closed.
	hold chan struct{}
}

func (f *titles) Title(ctx context.Context, in platform.TitleInput) (string, error) {
	f.mu.Lock()
	f.asked = append(f.asked, in)
	f.mu.Unlock()
	if f.hold != nil {
		select {
		case <-f.hold:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return f.name, f.err
}

func (f *titles) calls() []platform.TitleInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]platform.TitleInput(nil), f.asked...)
}

func startTitled(t *testing.T, f *titles) *platform.Platform {
	t.Helper()
	cfg := platform.Config{Limits: call.Limits{}}
	if f != nil {
		cfg.Titler = func(string, string) platform.Titler { return f }
	} else {
		cfg.Titler = func(string, string) platform.Titler { return nil }
	}
	p := startWith(t, cfg)
	field(t, p)
	return p
}

func record(t *testing.T, p *platform.Platform, id string) threads.Thread {
	t.Helper()
	th, err := p.ThreadRecord(admin.Tenant, id)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

const longFirst = "Write four short sentences about the sea, the tide and the boats that leave the harbour before dawn"

func TestASessionShowsItsPlaceholderThenItsGeneratedTitle(t *testing.T) {
	f := &titles{name: "Sea and harbour sentences", hold: make(chan struct{})}
	p := startTitled(t, f)
	v := titleSession(t, p, longFirst)
	// At once: the start of the message, cut on a word, short enough to
	// show whole.
	if got := v.Thread.Title; got != "Write four short sentences about the…" || v.Thread.TitleSource != threads.TitlePlaceholder {
		t.Fatalf("placeholder %q (%s)", got, v.Thread.TitleSource)
	}
	close(f.hold)
	p.WaitTitles()
	th := record(t, p, v.ID)
	if th.Title != "Sea and harbour sentences" || th.TitleSource != threads.TitleGenerated {
		t.Fatalf("title %q (%s)", th.Title, th.TitleSource)
	}
	asked := f.calls()
	if len(asked) != 1 || asked[0].Message != longFirst || asked[0].Answer != "" {
		t.Fatalf("asked %+v", asked)
	}
	list, _, err := p.SessionsPage(t.Context(), admin, platform.SessionQuery{Roots: true}, 50, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("no session listed")
	}
	for _, s := range list {
		if s.ID == v.ID && (s.Conversation.Title != th.Title || s.Conversation.TitleSource != threads.TitleGenerated) {
			t.Fatalf("listed as %q (%s)", s.Conversation.Title, s.Conversation.TitleSource)
		}
	}
}

func TestAPersonsRenameIsNeverOverwrittenByAGeneratedTitle(t *testing.T) {
	f := &titles{name: "Sea and harbour sentences", hold: make(chan struct{})}
	p := startTitled(t, f)
	v := titleSession(t, p, longFirst)
	th := record(t, p, v.ID)
	if _, err := p.UpdateThread(t.Context(), admin, v.ID, platform.ThreadUpdate{Version: th.Version, Title: "My sea notes", Visibility: th.Visibility, Default: th.Default, Agents: th.Agents}); err != nil {
		t.Fatal(err)
	}
	// The name arrives after the rename.
	close(f.hold)
	p.WaitTitles()
	got := record(t, p, v.ID)
	if got.Title != "My sea notes" || got.TitleSource != threads.TitlePerson {
		t.Fatalf("title %q (%s)", got.Title, got.TitleSource)
	}
	// A later message names nothing either.
	if title := postTitle(t, p, v.ID, "now add a fifth sentence about gulls"); title != "My sea notes" {
		t.Fatalf("title %q", title)
	}
	p.WaitTitles()
	if n := len(f.calls()); n != 1 {
		t.Fatalf("asked %d times", n)
	}
}

func TestASessionWithNoModelKeepsItsPlaceholder(t *testing.T) {
	for name, f := range map[string]*titles{
		"no titler":   nil,
		"model fails": {err: errors.New("provider unavailable")},
		"empty name":  {name: " \n\"\" "},
	} {
		t.Run(name, func(t *testing.T) {
			p := startTitled(t, f)
			v := titleSession(t, p, "check the build on the release branch")
			p.WaitTitles()
			th := record(t, p, v.ID)
			if th.Title != "Check the build on the release branch" || th.TitleSource != threads.TitlePlaceholder {
				t.Fatalf("title %q (%s)", th.Title, th.TitleSource)
			}
		})
	}
	// With no titler set, the offline agent's provider names nothing.
	p := start(t, call.Limits{})
	field(t, p)
	v := titleSession(t, p, "check the build on the release branch")
	p.WaitTitles()
	if th := record(t, p, v.ID); th.TitleSource != threads.TitlePlaceholder {
		t.Fatalf("title %q (%s)", th.Title, th.TitleSource)
	}
}

func TestAGeneratedTitleIsShortAndPlain(t *testing.T) {
	f := &titles{name: "Title: \"**Planning the quarterly launch review with the whole extended team.**\"\nMore words"}
	p := startTitled(t, f)
	v := titleSession(t, p, longFirst)
	p.WaitTitles()
	th := record(t, p, v.ID)
	if th.Title != "Planning the quarterly launch review" || th.TitleSource != threads.TitleGenerated {
		t.Fatalf("title %q (%s)", th.Title, th.TitleSource)
	}
	if n := utf8.RuneCountInString(th.Title); n > runs.MaxSessionTitle || len(strings.Fields(th.Title)) > 6 {
		t.Fatalf("title %q is %d runes", th.Title, n)
	}
}

func TestAThinFirstMessageIsNamedFromItsAnswer(t *testing.T) {
	f := &titles{name: "Help with a request"}
	p := startTitled(t, f)
	v := titleSession(t, p, "help me")
	deadline := time.Now().Add(10 * time.Second)
	for len(f.calls()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	p.WaitTitles()
	asked := f.calls()
	if len(asked) != 1 || asked[0].Message != "help me" || asked[0].Answer == "" {
		t.Fatalf("asked %+v", asked)
	}
	if th := record(t, p, v.ID); th.Title != "Help with a request" {
		t.Fatalf("title %q", th.Title)
	}
}

func TestAGreetingIsNamedFromTheFirstRequest(t *testing.T) {
	f := &titles{name: "Open incidents summary"}
	p := startTitled(t, f)
	v := titleSession(t, p, "hi")
	time.Sleep(200 * time.Millisecond)
	p.WaitTitles()
	if n := len(f.calls()); n != 0 {
		t.Fatalf("a greeting was named %d times", n)
	}
	postTitle(t, p, v.ID, "summarize the open incidents")
	p.WaitTitles()
	asked := f.calls()
	if len(asked) != 1 || asked[0].Message != "summarize the open incidents" {
		t.Fatalf("asked %+v", asked)
	}
	if th := record(t, p, v.ID); th.Title != "Open incidents summary" || th.TitleSource != threads.TitleGenerated {
		t.Fatalf("title %q (%s)", th.Title, th.TitleSource)
	}
}

func TestCleanTitle(t *testing.T) {
	for in, want := range map[string]string{
		"Sea and harbour":                                 "Sea and harbour",
		"  \n\"Launch review.\"\n":                        "Launch review",
		"Title: fix the build":                            "Fix the build",
		"# **Quarterly plan**":                            "Quarterly plan",
		"Calling the `trace_message` tool":                "Calling the trace_message tool",
		"one two three four five six seven":               "One two three four five six",
		"Internationalization localization globalization": "Internationalization localization",
		"":     "",
		"\"\"": "",
	} {
		if got := platform.CleanTitle(in); got != want {
			t.Errorf("CleanTitle(%q) = %q, want %q", in, got, want)
		}
		if got := platform.CleanTitle(in); utf8.RuneCountInString(got) > runs.MaxSessionTitle {
			t.Errorf("CleanTitle(%q) = %q is too long", in, got)
		}
	}
	for in, want := range map[string]bool{"hi": true, "help me": true, "@lead fix it": true, "fix the build": false, "Can you help?": false} {
		if got := platform.ThinTitle(in); got != want {
			t.Errorf("ThinTitle(%q) = %v, want %v", in, got, want)
		}
	}
}
