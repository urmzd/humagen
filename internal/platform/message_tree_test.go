package platform_test

import (
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/threads"
	saigetypes "github.com/urmzd/saige/agent/types"
)

// treeAnswered waits until every child session beneath root's first message
// shows an answer that is done, as reader reads it, and returns the tree two
// levels deep.
func treeAnswered(t *testing.T, p *platform.Platform, reader auth.Principal, root string, sessions int) platform.MessageNode {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		n, err := p.ReadMessage(t.Context(), reader, root, 2)
		if err != nil {
			t.Fatal(err)
		}
		done := 0
		for _, c := range n.Children {
			if c.Session != root && len(c.Children) == 1 && c.Children[0].Status == "done" {
				done++
			}
		}
		if done == sessions && n.Status == "done" {
			return n
		}
		if time.Now().After(deadline) {
			t.Fatalf("the answers beneath %s never finished: %+v", root, n)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

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

// A root whose person's message the entry agent hands to two agents reads
// as that message, with the pick and the two child sessions beneath it, in
// time order; one level further down are the children's answers. Counts
// and latest changes add up at every level.
func TestAMessageTreeShowsWhoAnsweredBeneathTheMessage(t *testing.T) {
	c := entryCast([][]saigetypes.Delta{
		pickJSON(platform.PickDelegate, "Deploys are ops's and spend is analyst's.", "ops", "analyst"),
	}, map[string][][]saigetypes.Delta{"ops": {say("A migration was missing.")}, "analyst": {say("It cost three dollars.")}})
	p := stageEntry(t, c)
	ask := "Why did the deploy fail and what did it cost?"
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: ask})
	if err != nil {
		t.Fatal(err)
	}
	deep := treeAnswered(t, p, admin, v.ID, 2)

	n, err := p.ReadMessage(t.Context(), admin, v.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n.ID != v.ID || n.Session != v.ID || n.Role != "user" || n.Author.Kind != "person" || len(n.Content) != 1 || n.Content[0].Text != ask {
		t.Fatalf("the root reads as %+v", n)
	}
	if n.ChildrenCount != 3 || len(n.Children) != 3 {
		t.Fatalf("the root has %d children (%d shown), want the pick and two sessions: %+v", n.ChildrenCount, len(n.Children), n.Children)
	}
	answers := map[string]string{}
	var pick platform.MessageNode
	for i, child := range n.Children {
		if i > 0 && child.At.Before(n.Children[i-1].At) {
			t.Errorf("children are not in time order: %v before %v", n.Children[i-1].At, child.At)
		}
		if child.Session == v.ID {
			pick = child
			continue
		}
		if child.ID != child.Session || child.Role != "user" || child.ChildrenCount != 1 || len(child.Children) != 0 {
			t.Errorf("a child session at depth 1 = %+v, want its own node with one answer counted and none shown", child)
		}
	}
	if pick.ID != v.ID+":pick-m-1" || pick.Role != "assistant" || pick.Author.Display == "" || pick.ChildrenCount != 0 || len(pick.Content) != 3 || pick.Content[0].Type != "pick" {
		t.Fatalf("the pick = %+v", pick)
	}
	if !n.LatestAt.Equal(latestOf(n.Children)) || n.LatestAt.Before(n.At) {
		t.Errorf("the root's latest change is %v, its children's %v", n.LatestAt, latestOf(n.Children))
	}

	if deep.ChildrenCount != 3 || len(deep.Children) != 3 {
		t.Fatalf("two levels deep the root has %d children", deep.ChildrenCount)
	}
	for _, child := range deep.Children {
		if child.Session == v.ID {
			continue
		}
		answer := child.Children[0]
		if answer.Role != "assistant" || answer.Author.Kind != "agent" || answer.Session != child.ID || answer.ChildrenCount != 0 {
			t.Errorf("the answer in %s = %+v", child.ID, answer)
		}
		if !child.LatestAt.Equal(answer.LatestAt) {
			t.Errorf("child %s changed last at %v, its answer at %v", child.ID, child.LatestAt, answer.LatestAt)
		}
		answers[answer.Author.Name] = answer.Content[0].Text
	}
	if answers["ops"] != "A migration was missing." || answers["analyst"] != "It cost three dollars." {
		t.Fatalf("the answers two levels deep = %v", answers)
	}

	// The first message read by its own id is the session's node; the pick
	// read by its id has nothing beneath it.
	if same, err := p.ReadMessage(t.Context(), admin, v.ID+":m-1", 1); err != nil || same.ID != v.ID || same.ChildrenCount != 3 {
		t.Fatalf("the first message by its id = %+v, %v", same, err)
	}
	if leaf, err := p.ReadMessage(t.Context(), admin, pick.ID, 3); err != nil || leaf.ID != pick.ID || leaf.ChildrenCount != 0 {
		t.Fatalf("the pick by its id = %+v, %v", leaf, err)
	}

	// A later person's message hangs beneath the first, with its own.
	if _, err := p.PostSession(t.Context(), admin, v.ID, platform.MessageInput{Text: "Thanks, both.", PostOnly: true}); err != nil {
		t.Fatal(err)
	}
	n, err = p.ReadMessage(t.Context(), admin, v.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	last := n.Children[len(n.Children)-1]
	if n.ChildrenCount != 4 || last.ID != v.ID+":m-2" || last.Role != "user" || last.ChildrenCount != 0 || !n.LatestAt.Equal(latestOf(n.Children)) {
		t.Fatalf("after a second message the root = %d children, last %+v", n.ChildrenCount, last)
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
// counted, and its call is left out of the pick: the reader learns of the
// sessions they may read alone.
func TestAMessageTreeWithholdsAChildSessionItsReaderMayNotRead(t *testing.T) {
	c := entryCast([][]saigetypes.Delta{
		pickJSON(platform.PickDelegate, "Deploys are ops's and spend is analyst's.", "ops", "analyst"),
	}, map[string][][]saigetypes.Delta{"ops": {say("A migration was missing.")}, "analyst": {say("It cost three dollars.")}})
	p := stageEntry(t, c)
	ana := operator
	seePeople(t, p, ana)
	v, err := p.StartSession(t.Context(), ana, platform.SessionInput{Project: "~", Text: "Why did the deploy fail and what did it cost?"})
	if err != nil {
		t.Fatal(err)
	}
	treeAnswered(t, p, ana, v.ID, 2)
	if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "olu"}, platform.ProjectInput{Name: "Vault", Kind: projects.Account}); err != nil {
		t.Fatal(err)
	}
	if err := p.SetAgentHome(t.Context(), tenant, "analyst", "Vault", govern); err != nil {
		t.Fatal(err)
	}
	if p.Seer(ana).Agent("analyst") {
		t.Fatal("the test needs a reader who may not read analyst")
	}
	n, err := p.ReadMessage(t.Context(), ana, v.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if n.ChildrenCount != 2 || len(n.Children) != 2 {
		t.Fatalf("the reader is shown %d children (%d counted), want the pick and ops's session: %+v", len(n.Children), n.ChildrenCount, n.Children)
	}
	for _, child := range n.Children {
		for _, part := range child.Content {
			if part.Agent == "analyst" {
				t.Errorf("the reader is shown analyst's work: %+v", child)
			}
		}
		for _, grand := range child.Children {
			if grand.Author.Name == "analyst" {
				t.Errorf("the reader is shown analyst's answer: %+v", grand)
			}
		}
	}
	roots, err := p.ListRootMessages(t.Context(), ana, 50, "")
	if err != nil || len(roots.Messages) != 1 || roots.Messages[0].ChildrenCount != 2 {
		t.Fatalf("the reader's roots = %+v, %v", roots, err)
	}
	if full, err := p.ReadMessage(t.Context(), admin, v.ID, 1); err != nil || full.ChildrenCount != 3 {
		t.Fatalf("an admin reads %+v, %v", full, err)
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
