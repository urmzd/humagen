package github_test

import (
	"context"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/integrations/github"
	transport "github.com/urmzd/saige/flow/connectors/github"
	"github.com/urmzd/saige/flow/event"
)

// A comment on an issue or pull request is the answer alone: the tools the
// agent called and the helpers it started stay in the session, which a small
// footer links to. While the run works, one status line stands in.
func TestTheCommentIsTheAnswerAndALinkToItsSession(t *testing.T) {
	fake := github.NewFake()
	c, err := github.New(github.Config{API: fake, Owners: []string{"urmzd"}, Interval: -1,
		SessionLink: func(v transport.View) string { return "https://console.example/sessions/s_" + v.RunID }})
	if err != nil {
		t.Fatal(err)
	}
	events := []event.Envelope{
		runEvent(1, event.TypeAccepted, ""),
		runEvent(2, event.TypeStarted, ""),
		runEvent(3, event.TypeToolCall, `{"id":"t1","name":"get_context","arguments":{"request":"what changed?"}}`),
		runEvent(4, event.TypeAction, `{"name":"agent","summary":"Helper started","address":"run:run_2"}`),
	}
	for _, e := range events {
		if err := c.Deliver(context.Background(), target, e); err != nil {
			t.Fatal(err)
		}
	}
	working := fake.Comments()[0].Body
	if !strings.HasPrefix(working, "**docs-bot** _(working)_") || strings.Contains(working, "get_context") || strings.Contains(working, "Helper started") {
		t.Fatalf("a working comment shows more than its status:\n%s", working)
	}
	for _, e := range []event.Envelope{textEvent(5, "It retries the sink twice."), runEvent(6, event.TypeCompleted, "")} {
		if err := c.Deliver(context.Background(), target, e); err != nil {
			t.Fatal(err)
		}
	}
	body := fake.Comments()[0].Body
	want := "It retries the sink twice.\n\n<sub>[View the session in Humagen ↗](https://console.example/sessions/s_run_1)</sub>"
	if body != want {
		t.Fatalf("the finished comment is\n%s\nwant\n%s", body, want)
	}
}

// A run that fails says so, without its trace.
func TestAFailedRunSaysWhyWithoutItsTrace(t *testing.T) {
	fake := github.NewFake()
	c := sinkConnector(t, fake)
	for _, e := range []event.Envelope{
		runEvent(1, event.TypeAccepted, ""),
		runEvent(2, event.TypeToolCall, `{"id":"t1","name":"get_context"}`),
		runEvent(3, event.TypeFailed, `{"error":"provider timeout"}`),
	} {
		if err := c.Deliver(context.Background(), target, e); err != nil {
			t.Fatal(err)
		}
	}
	body := fake.Comments()[0].Body
	if !strings.Contains(body, "provider timeout") || strings.Contains(body, "get_context") {
		t.Fatalf("a failed comment:\n%s", body)
	}
}
