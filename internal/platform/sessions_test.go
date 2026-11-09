package platform_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/feedback"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"
)

func TestSessionsContinueChildrenKeepOneAgentAndConfineReads(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	ctx := t.Context()
	root, err := p.StartSession(ctx, admin, platform.SessionInput{Project: "Field", Agent: "lead", Text: "Begin the investigation"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(root.ID, "s_") || root.Agent != "lead" || root.Thread.Session == nil {
		t.Fatalf("root: %+v", root)
	}
	var first string
	for _, m := range root.Messages {
		if m.Run != nil {
			first = m.Run.ID
		}
	}
	if first == "" {
		t.Fatal("accepted session lacks its run")
	}
	await(t, p, first, runs.Completed)
	child, err := p.StartSession(ctx, admin, platform.SessionInput{Parent: root.ID, Agent: "helper", Text: "Inspect one part"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := p.StartSession(ctx, admin, platform.SessionInput{Parent: root.ID, Agent: "helper", Text: "Continue that part"})
	if err != nil {
		t.Fatal(err)
	}
	if child.ID != again.ID {
		t.Fatalf("continuation changed session: %s %s", child.ID, again.ID)
	}
	fresh, err := p.StartSession(ctx, admin, platform.SessionInput{Parent: root.ID, Agent: "helper", Fresh: true, Text: "Independent check"})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ID == child.ID {
		t.Fatal("fresh reused existing child")
	}
	view, err := p.ReadSession(ctx, admin, root.ID, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Children) != 2 || !strings.Contains(view.Summary, "Begin the investigation") {
		t.Fatalf("parent view: %+v", view)
	}
	for _, m := range view.Messages {
		if m.Run != nil && m.Run.Agent != "lead" {
			t.Fatalf("child answer entered parent transcript: %+v", m)
		}
	}
	other := admin
	other.Tenant = "another"
	if _, err := p.ReadSession(ctx, other, root.ID, "", "", false); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("cross tenant read: %v", err)
	}
	if _, err := p.PostSession(ctx, admin, root.ID, platform.MessageInput{Text: "@missing-agent help"}); err == nil {
		t.Fatal("unknown mention accepted")
	}
	beforeFeedback := view.Cursor
	if _, err := p.Feedback.Save(admin.Tenant, feedback.Record{Run: first, Subject: admin.Subject, Rating: "up"}); err != nil {
		t.Fatal(err)
	}
	view, err = p.ReadSession(ctx, admin, root.ID, beforeFeedback, "", false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range view.Messages {
		if m.ID == first {
			found = m.Feedback != nil && m.Feedback.Rating == "up"
		}
	}
	if !found {
		t.Fatal("own feedback missing from session read")
	}
	ratedCursor := view.Cursor
	if err := p.Feedback.Remove(admin.Tenant, first, admin.Subject); err != nil {
		t.Fatal(err)
	}
	cleared, err := p.ReadSession(ctx, admin, root.ID, ratedCursor, "", false)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, m := range cleared.Messages {
		if m.ID == first {
			found = m.Feedback == nil
		}
	}
	if !found {
		t.Fatal("incremental read omitted cleared feedback")
	}

}

func TestNativeSessionChildReleasesSingleWorkerAndUsesGovernedCall(t *testing.T) {
	c := &cast{says: map[string][][]saigetypes.Delta{
		"lead":   {agenttest.ToolCallResponse("inspect", "session_call", map[string]any{"agent": "helper", "text": "Inspect the details"}), say("The helper finished.")},
		"helper": {say("The details are sound.")},
	}, spawn: map[string][]string{"lead": {"helper"}}}
	p := stage(t, c, platform.Config{Concurrency: 1, Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	root, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Agent: "lead", Text: "Delegate the inspection"})
	if err != nil {
		t.Fatal(err)
	}
	var runID string
	for _, m := range root.Messages {
		if m.Run != nil {
			runID = m.Run.ID
		}
	}
	parent := await(t, p, runID, runs.Completed)
	child := called(t, p, parent.ID, "helper", runs.Completed)
	if child.Parent != parent.ID || child.Thread == parent.Thread || child.Depth != 1 {
		t.Fatalf("child bypassed governed lineage: %+v", child)
	}
	view, err := p.ReadSession(t.Context(), admin, root.ID, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	childView, err := p.ReadSession(t.Context(), admin, child.Thread, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(childView.Messages) == 0 || childView.Messages[len(childView.Messages)-1].Run == nil || childView.Conversation.LastBy != "helper" || !childView.Conversation.LastAgent {
		t.Fatalf("child transcript/summary missing: %+v", childView)
	}
	if len(view.Children) != 1 || view.Children[0].ID != child.Thread {
		t.Fatalf("child session missing: %+v", view.Children)
	}
}

// A mention in a session is answered there, by the agent it names, and the
// session stays its owner's. Only a request that asks for a child by option
// opens one.
func TestSessionMentionAnswersInTheSessionAndAChildIsOpenedOnlyByOption(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	ctx := t.Context()
	root, err := p.StartSession(ctx, admin, platform.SessionInput{Project: "Field", Agent: "lead", Text: "Begin"})
	if err != nil {
		t.Fatal(err)
	}
	// A message that would start work while the session's agent works is a
	// follow-up, so the first answer ends before the mention is posted.
	await(t, p, sessionRun(t, root).ID, runs.Completed)
	view, err := p.PostSession(ctx, admin, root.ID, platform.MessageInput{Text: "@helper inspect this"})
	if err != nil {
		t.Fatal(err)
	}
	last := view.Messages[len(view.Messages)-1]
	if view.ID != root.ID || view.Agent != "lead" || len(view.Children) != 0 || last.Run == nil || last.Run.Agent != "helper" || last.Run.Thread != root.ID {
		t.Fatalf("a mention: owner %q, children %+v, last message %+v", view.Agent, view.Children, last)
	}
	view, err = p.PostSession(ctx, admin, root.ID, platform.MessageInput{Text: "@helper continue"}, platform.SessionMessageOptions{Agent: "helper"})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Children) != 1 || view.Children[0].Agent != "helper" {
		t.Fatalf("a child asked for by option: %+v", view.Children)
	}
	view, err = p.PostSession(ctx, admin, root.ID, platform.MessageInput{Text: "@helper continue again"}, platform.SessionMessageOptions{Agent: "helper"})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Children) != 1 {
		t.Fatalf("the option forked instead of continuing: %+v", view.Children)
	}
	view, err = p.PostSession(ctx, admin, root.ID, platform.MessageInput{Text: "@helper inspect independently"}, platform.SessionMessageOptions{Agent: "helper", Fresh: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Children) != 2 {
		t.Fatalf("fresh mention reused child: %+v", view.Children)
	}
	if _, err = p.PostSession(ctx, admin, root.ID, platform.MessageInput{Text: "No mention here"}, platform.SessionMessageOptions{Agent: "helper", Fresh: true}); err == nil {
		t.Fatal("unmentioned child accepted")
	}
}
