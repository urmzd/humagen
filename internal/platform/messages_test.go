package platform_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/threads"
)

// slackWorkspace is a platform that ships its agents, the entry agent among
// them, with a Slack client for team T0TEAM mapping U0ANA to ana.
func slackWorkspace(t *testing.T) (*platform.Platform, auth.Principal) {
	t.Helper()
	p := withEntry(t, platform.Config{})
	field(t, p)
	specialists(t, p)
	seePeople(t, p, auth.Principal{Subject: "ana"})
	in := slackClient("app", "A0APP", "T0TEAM", "*")
	in.Agents = []string{"*"}
	in.Slack.Machine, in.Slack.People = "svc-slack", map[string]string{"U0ANA": "ana"}
	in.Home = &clients.Home{Project: "Initech"}
	create(t, p, tenant, in)
	pol, err := auth.ParsePolicy("test", "role viewer { allow read on * }\nrole operator extends viewer { allow write on runs }\ngrant operator to subject \"ana\"\n")
	if err != nil {
		t.Fatal(err)
	}
	ana := auth.Principal{Subject: "ana", Tenant: tenant, Kind: auth.User}.WithPolicy(pol)
	return p, ana
}

func sayIn(text string, ref *platform.ExternalRef) platform.MessagesInput {
	return platform.MessagesInput{Messages: []platform.Message{{Role: "user", Content: platform.Content{{Type: "text", Text: text}}}}, Ref: ref}
}

func slackRef(channel, ts, thread string) *platform.ExternalRef {
	return &platform.ExternalRef{Type: platform.RefSlack, Client: "app", Team: "T0TEAM", Channel: channel, TS: ts, ThreadTS: thread}
}

func TestASlackDirectMessageIsAlwaysAnsweredInItsPlacesRoot(t *testing.T) {
	p, ana := slackWorkspace(t)
	out, err := p.SendMessages(t.Context(), ana, sayIn("howdy", slackRef("D0ANA", "1700000000.000100", "")))
	if err != nil {
		t.Fatalf("a direct message was refused: %v", err)
	}
	if !out.Created || out.Root == "" || !strings.HasPrefix(out.ID, out.Root+":") {
		t.Fatalf("message = %+v", out)
	}
	root, err := p.ReadSession(t.Context(), ana, out.Root, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if root.Agent != platform.BuiltinEntry || root.Parent != "slack://T0TEAM/D0ANA/1700000000.000100" || root.Thread.Visibility != threads.Private {
		t.Fatalf("root = %s, %s, %s", root.Agent, root.Parent, root.Thread.Visibility)
	}
	if len(out.Posted.Runs) == 0 && pickOf(t, p, out.Root).Parts == nil {
		t.Fatal("nothing answered the direct message")
	}
	if strings.Contains(out.Posted.Reason, "No agent took it") {
		t.Fatalf("a person was told %q", out.Posted.Reason)
	}
}

func TestAReplyInTheSameThreadPicksTheRootUpAndPostsOnce(t *testing.T) {
	p, ana := slackWorkspace(t)
	first, err := p.SendMessages(t.Context(), ana, sayIn("why did the deploy fail?", slackRef("C01ANY", "1700000000.000100", "")))
	if err != nil {
		t.Fatal(err)
	}
	reply, err := p.SendMessages(t.Context(), ana, sayIn("and what did it cost?", slackRef("C01ANY", "1700000000.000200", "1700000000.000100")))
	if err != nil || reply.Created || reply.Root != first.Root {
		t.Fatalf("a reply in the thread = root %s (created %v), want %s: %v", reply.Root, reply.Created, first.Root, err)
	}
	again, err := p.SendMessages(t.Context(), ana, sayIn("and what did it cost?", slackRef("C01ANY", "1700000000.000200", "1700000000.000100")))
	if err != nil || !again.Posted.Repeated || again.ID != reply.ID {
		t.Fatalf("the same Slack message again = %+v, %v", again, err)
	}
}

func TestAMentionedAgentAnswersInItsChildAndThePickSaysWhy(t *testing.T) {
	p, ana := slackWorkspace(t)
	out, err := p.SendMessages(t.Context(), ana, platform.MessagesInput{Ref: slackRef("C01ANY", "1700000000.000100", ""), Messages: []platform.Message{{Role: "user", Content: platform.Content{
		{Type: "mention", Agent: "analyst"},
		{Type: "text", Text: "what did it cost?"},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Posted.Runs) != 1 || out.Posted.Runs[0].Agent != "analyst" {
		t.Fatalf("runs = %+v", out.Posted.Runs)
	}
	m := pickOf(t, p, out.Root)
	if m.Parts[0].Text != "@analyst was mentioned." || len(m.Parts) != 2 || m.Parts[1].Agent != "analyst" {
		t.Fatalf("pick = %+v", m.Parts)
	}
}

func TestAPlaceElsewhereHasOneRootAndTheConsoleStartsANewOne(t *testing.T) {
	p, ana := slackWorkspace(t)
	pr := &platform.ExternalRef{Type: platform.RefGitHub, Repo: "acme/web", Kind: "pull", Number: 5}
	a, err := p.SendMessages(t.Context(), ana, sayIn("review the deploy change", pr))
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.SendMessages(t.Context(), ana, sayIn("and check the migration", pr))
	if err != nil || b.Root != a.Root || b.Created {
		t.Fatalf("a second message on the pull request = %s (created %v), want %s: %v", b.Root, b.Created, a.Root, err)
	}
	if root, _ := p.ReadSession(t.Context(), ana, a.Root, "", "", false); root.Parent != "github://acme/web/pull/5" {
		t.Fatalf("the pull request's root is at %q", root.Parent)
	}
	page := &platform.ExternalRef{Type: platform.RefURL, URL: "HTTPS://Example.com/Docs/#intro"}
	u, err := p.SendMessages(t.Context(), ana, sayIn("summarise this page", page))
	if err != nil {
		t.Fatal(err)
	}
	if root, _ := p.ReadSession(t.Context(), ana, u.Root, "", "", false); root.Parent != "https://example.com/Docs" || root.Thread.Visibility != threads.Private {
		t.Fatalf("the page's root = %q, %s", root.Parent, root.Thread.Visibility)
	}
	c1, err := p.SendMessages(t.Context(), ana, sayIn("why did the deploy fail?", nil))
	if err != nil {
		t.Fatal(err)
	}
	c2, err := p.SendMessages(t.Context(), ana, sayIn("why did the deploy fail?", nil))
	if err != nil || c1.Root == c2.Root || !c1.Created || !c2.Created {
		t.Fatalf("console messages share a root: %s, %s (%v)", c1.Root, c2.Root, err)
	}
	follow, err := p.SendMessagesUnder(t.Context(), ana, c1.ID, sayIn("and what did it cost?", nil))
	if err != nil || follow.Root != c1.Root {
		t.Fatalf("a follow-up under a message = %s, want %s: %v", follow.Root, c1.Root, err)
	}
}

func TestABatchPostsItsEarlierMessagesQuietly(t *testing.T) {
	p, ana := slackWorkspace(t)
	in := platform.MessagesInput{Ref: slackRef("C01ANY", "1700000000.000300", ""), Messages: []platform.Message{
		{Role: "user", Content: platform.Content{{Type: "text", Text: "the deploy failed at 2am"}}, Ref: slackRef("C01ANY", "1700000000.000100", "1700000000.000300")},
		{Role: "user", Content: platform.Content{{Type: "text", Text: "@ops why?"}}, Ref: slackRef("C01ANY", "1700000000.000300", "")},
	}}
	out, err := p.SendMessages(t.Context(), ana, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Posted.Runs) != 1 || out.Posted.Runs[0].Agent != "ops" {
		t.Fatalf("the batch started %+v", out.Posted.Runs)
	}
	root, _ := p.ReadSession(t.Context(), ana, out.Root, "", "", false)
	people := 0
	for _, m := range root.Messages {
		if m.Message != nil {
			people++
		}
	}
	if people != 2 {
		t.Fatalf("the root holds %d people's messages, want 2", people)
	}
}

func TestAMessagesRequestIsPeoplesMessagesFromAPlaceThatExists(t *testing.T) {
	p, ana := slackWorkspace(t)
	for name, in := range map[string]platform.MessagesInput{
		"an agent's message":       {Messages: []platform.Message{{Role: "assistant", Content: platform.Content{{Type: "text", Text: "hi"}}}}},
		"no messages":              {},
		"an image":                 {Messages: []platform.Message{{Role: "user", Content: platform.Content{{Type: "image"}}}}},
		"an unknown origin":        sayIn("hi", &platform.ExternalRef{Type: "fax"}),
		"a slack ref with no team": sayIn("hi", &platform.ExternalRef{Type: platform.RefSlack, Channel: "C01", TS: "1.2"}),
		"another team's client":    sayIn("hi", &platform.ExternalRef{Type: platform.RefSlack, Client: "app", Team: "T0OTHER", Channel: "C01", TS: "1.2"}),
	} {
		if _, err := p.SendMessages(t.Context(), ana, in); !errors.Is(err, threads.ErrInvalid) {
			t.Errorf("%s = %v, want invalid", name, err)
		}
	}
}
