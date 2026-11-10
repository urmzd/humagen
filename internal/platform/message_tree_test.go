package platform_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	saigetypes "github.com/urmzd/saige/agent/types"
)

// latestOf is the latest latest_at of nodes.
func latestOf(nodes []platform.MessageNode) time.Time {
	var at time.Time
	for _, n := range nodes {
		if n.LatestAt.After(at) {
			at = n.LatestAt
		}
	}
	return at
}

// latestIn is the latest change a session's node reports: the latest of
// what stands beneath it and of the session's own record. The record moves
// when the agent that answers is joined to the session, which is after its
// run was started, and so after a quick run has ended.
func latestIn(t *testing.T, p *platform.Platform, session string, beneath []platform.MessageNode) time.Time {
	t.Helper()
	th, err := p.Threads.Get(tenant, session)
	if err != nil {
		t.Fatal(err)
	}
	at := latestOf(beneath)
	if th.UpdatedAt.After(at) {
		at = th.UpdatedAt
	}
	return at
}

// A root reads as its first message with what answered it beneath: one
// answer, the entry agent's, whatever it called. A later person's message
// hangs beneath the first with its own answer, and a child session asked for
// by option stands beneath the message it was started for. Counts and latest
// changes add up at every level.
func TestAMessageTreeShowsTheOneAnswerBeneathTheMessage(t *testing.T) {
	c := entryCast([][]saigetypes.Delta{say("## @ops\n\nA migration was missing.\n\n## @analyst\n\nIt cost three dollars.\n\nBoth point at the migration."), say("You are welcome.")}, map[string][][]saigetypes.Delta{
		"ops":     {say("A migration was missing."), say("The rollback is clean.")},
		"analyst": {say("It cost three dollars.")},
	})
	p := stageEntry(t, c)
	ask := "@ops @analyst why did the deploy fail and what did it cost?"
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: ask})
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, sessionRun(t, v).ID, runs.Completed)

	n, err := p.ReadMessage(t.Context(), admin, v.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n.ID != v.ID || n.Session != v.ID || n.Role != "user" || n.Author.Kind != "person" || len(n.Content) != 1 || n.Content[0].Text != ask || n.Status != "done" {
		t.Fatalf("the root reads as %+v", n)
	}
	if n.ChildrenCount != 1 || len(n.Children) != 1 {
		t.Fatalf("the root has %d children (%d shown), want its one answer: %+v", n.ChildrenCount, len(n.Children), n.Children)
	}
	answer := n.Children[0]
	if answer.Role != "assistant" || answer.Author.Name != platform.BuiltinEntry || answer.Session != v.ID || answer.ChildrenCount != 0 || len(answer.Content) != 1 || answer.Content[0].Type != "text" || !strings.Contains(answer.Content[0].Text, "## @ops") || !strings.HasSuffix(answer.Content[0].Text, "Both point at the migration.") {
		t.Fatalf("the answer = %+v", answer)
	}
	if want := latestIn(t, p, v.ID, n.Children); !n.LatestAt.Equal(want) || n.LatestAt.Before(n.At) {
		t.Errorf("the root's latest change is %v, want %v", n.LatestAt, want)
	}
	// The first message read by its own id is the session's node; the answer
	// read by its id has nothing beneath it.
	if same, err := p.ReadMessage(t.Context(), admin, v.ID+":m-1", 1); err != nil || same.ID != v.ID || same.ChildrenCount != 1 {
		t.Fatalf("the first message by its id = %+v, %v", same, err)
	}
	if leaf, err := p.ReadMessage(t.Context(), admin, answer.ID, 3); err != nil || leaf.ID != answer.ID || leaf.ChildrenCount != 0 {
		t.Fatalf("the answer by its id = %+v, %v", leaf, err)
	}

	// A later person's message hangs beneath the first, with its own answer.
	if _, err := p.PostSession(t.Context(), admin, v.ID, platform.MessageInput{Text: "Thanks, both."}); err != nil {
		t.Fatal(err)
	}
	answers(t, p, v.ID, 2)
	n, err = p.ReadMessage(t.Context(), admin, v.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	last := n.Children[len(n.Children)-1]
	if n.ChildrenCount != 2 || last.ID != v.ID+":m-2" || last.Role != "user" || last.ChildrenCount != 1 || len(last.Children) != 1 || last.Children[0].Author.Name != platform.BuiltinEntry || !n.LatestAt.Equal(latestOf(n.Children)) {
		t.Fatalf("after a second message the root = %d children, last %+v", n.ChildrenCount, last)
	}

	// A child session asked for by option stands beneath the message it was
	// started for, as its own first message, with its answer one level down.
	if _, err := p.PostSession(t.Context(), admin, v.ID, platform.MessageInput{Text: "@ops check the rollback"}, platform.SessionMessageOptions{Agent: "ops", Fresh: true}); err != nil {
		t.Fatal(err)
	}
	var third platform.MessageNode
	within(t, "the child session's answer", func() bool {
		n, err = p.ReadMessage(t.Context(), admin, v.ID, 3)
		if err != nil || len(n.Children) != 3 {
			return false
		}
		third = n.Children[2]
		return len(third.Children) == 1 && len(third.Children[0].Children) == 1 && third.Children[0].Children[0].Status == "done"
	})
	child := third.Children[0]
	if third.ID != v.ID+":m-3" || third.ChildrenCount != 1 || child.ID != child.Session || child.Session == v.ID || child.Role != "user" || child.ChildrenCount != 1 {
		t.Fatalf("the child session beneath its message = %+v", third)
	}
	if said := child.Children[0]; said.Role != "assistant" || said.Author.Name != "ops" || said.Session != child.ID || said.Content[0].Text != "The rollback is clean." || !child.LatestAt.Equal(latestIn(t, p, child.ID, child.Children)) {
		t.Fatalf("the child's answer = %+v beneath %+v", said, child)
	}

	for name, id := range map[string]string{"an unknown message": v.ID + ":m-99", "an unknown session": "s_missing"} {
		if _, err := p.ReadMessage(t.Context(), admin, id, 1); !errors.Is(err, threads.ErrNotFound) {
			t.Errorf("%s = %v, want not found", name, err)
		}
	}
	if _, err := p.ReadMessage(t.Context(), admin, v.ID, platform.MaxMessageDepth+1); !errors.Is(err, threads.ErrInvalid) {
		t.Errorf("a read deeper than %d = %v", platform.MaxMessageDepth, err)
	}
}

// A child session its reader may not read is left out of the tree and not
// counted: the reader learns of the sessions they may read alone.
func TestAMessageTreeWithholdsAChildSessionItsReaderMayNotRead(t *testing.T) {
	c := entryCast([][]saigetypes.Delta{say("Hello.")}, map[string][][]saigetypes.Delta{"ops": {say("A migration was missing.")}, "analyst": {say("It cost three dollars.")}})
	p := stageEntry(t, c)
	ana := operator
	seePeople(t, p, ana)
	v, err := p.StartSession(t.Context(), ana, platform.SessionInput{Project: "~", Text: "Hello there"})
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, sessionRun(t, v).ID, runs.Completed)
	for _, agent := range []string{"ops", "analyst"} {
		if _, err := p.PostSession(t.Context(), ana, v.ID, platform.MessageInput{Text: "@" + agent + " look into the deploy"}, platform.SessionMessageOptions{Agent: agent, Fresh: true}); err != nil {
			t.Fatal(err)
		}
	}
	// The root's answer, then two messages, each with its child session.
	count := func(reader auth.Principal) (int, platform.MessageNode) {
		n, err := p.ReadMessage(t.Context(), reader, v.ID, 3)
		if err != nil {
			t.Fatal(err)
		}
		sessions := 0
		for _, child := range n.Children {
			for _, grand := range child.Children {
				if grand.Session != v.ID {
					sessions++
				}
			}
		}
		return sessions, n
	}
	within(t, "both child sessions", func() bool {
		sessions, n := count(ana)
		return sessions == 2 && n.Status == "done"
	})
	if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "olu"}, platform.ProjectInput{Name: "Vault", Kind: projects.Account}); err != nil {
		t.Fatal(err)
	}
	if err := p.SetAgentHome(t.Context(), tenant, "analyst", "Vault", govern); err != nil {
		t.Fatal(err)
	}
	if p.Seer(ana).Agent("analyst") {
		t.Fatal("the test needs a reader who may not read analyst")
	}
	sessions, n := count(ana)
	if sessions != 1 {
		t.Fatalf("the reader is shown %d child sessions, want ops's alone: %+v", sessions, n.Children)
	}
	for _, child := range n.Children {
		if child.ID == v.ID+":m-3" && child.ChildrenCount != 0 {
			t.Errorf("analyst's session is counted beneath its message: %+v", child)
		}
		for _, grand := range child.Children {
			for _, great := range grand.Children {
				if great.Author.Name == "analyst" {
					t.Errorf("the reader is shown analyst's answer: %+v", great)
				}
			}
		}
	}
	if full, _ := count(admin); full != 2 {
		t.Fatalf("an admin reads %d child sessions, want both", full)
	}
}

// The roots listing is each place's root its reader may read, newest
// first, with the place, the agent that owns it, who asked and the kind of
// audience asked in front of, paged by the root before.
func TestTheRootsAreListedNewestFirstWithTheirPlaceAndAudience(t *testing.T) {
	p, ana := slackWorkspace(t)
	public := slackRef("C01ANY", "1700000000.000100", "")
	public.Audience = &platform.SurfaceAudience{Type: platform.ChannelPublic}
	first, err := p.SendMessages(t.Context(), ana, sayIn("why did the deploy fail?", public))
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.SendMessages(t.Context(), ana, sayIn("howdy", slackRef("D0ANA", "1700000000.000200", "")))
	if err != nil {
		t.Fatal(err)
	}
	page, err := p.ListRootMessages(t.Context(), ana, 50, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 2 || page.Next != "" {
		t.Fatalf("roots = %+v", page)
	}
	newest, older := page.Messages[0], page.Messages[1]
	if newest.ID != second.Root || older.ID != first.Root || newest.LatestAt.Before(older.LatestAt) {
		t.Fatalf("roots are %s then %s, want %s then %s", newest.ID, older.ID, second.Root, first.Root)
	}
	if older.Parent != "slack://T0TEAM/C01ANY/1700000000.000100" || older.Agent != platform.BuiltinEntry || older.Author.Kind != "person" || older.Audience == nil || older.Audience.Kind != "public" {
		t.Fatalf("the channel's root = %+v", older)
	}
	if newest.Audience == nil || newest.Audience.Kind != "direct" {
		t.Fatalf("the direct message's root = %+v", newest)
	}
	n, err := p.ReadMessage(t.Context(), ana, first.Root, 0)
	if err != nil || n.Audience == nil || n.Audience.Kind != "public" || n.ChildrenCount != older.ChildrenCount || len(n.Children) != 0 {
		t.Fatalf("the channel's root read alone = %+v, %v", n, err)
	}

	one, err := p.ListRootMessages(t.Context(), ana, 1, "")
	if err != nil || len(one.Messages) != 1 || one.Next != second.Root {
		t.Fatalf("a page of one = %+v, %v", one, err)
	}
	rest, err := p.ListRootMessages(t.Context(), ana, 1, one.Next)
	if err != nil || len(rest.Messages) != 1 || rest.Messages[0].ID != first.Root || rest.Next != "" {
		t.Fatalf("the page after = %+v, %v", rest, err)
	}
	if _, err := p.ListRootMessages(t.Context(), ana, 1, "s_missing"); !errors.Is(err, threads.ErrCursor) {
		t.Errorf("a cursor naming no root = %v", err)
	}
	if _, err := p.ListRootMessages(t.Context(), ana, platform.MaxRootPage+1, ""); !errors.Is(err, threads.ErrInvalid) {
		t.Errorf("a page larger than %d = %v", platform.MaxRootPage, err)
	}
}
