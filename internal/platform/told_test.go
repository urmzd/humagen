package platform_test

import (
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/threads"
)

// Only someone who can read a conversation is told of it. A person who is
// not at least a viewer of its project is not notified, nor invited to a
// private one, and the poster is told plainly. A role's holders are told
// only in a shared conversation: a private one is read by its participants,
// and a role is none.
func TestOnlySomeoneWhoCanReadAConversationIsToldOfIt(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	if _, err := p.CreateProject(t.Context(), tenant, ada, platform.ProjectInput{Name: "Vault"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Projects.Update(tenant, "Vault", 1, ada, time.Now(), func(pr *projects.Project) error {
		pr.Members = []projects.Entry{{Who: "opal", Role: projects.Member}, {Who: "ada", Role: projects.Owner}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	seePeople(t, p, operator, bo)
	got := startThread(t, p, operator, platform.ThreadInput{Project: "Vault", Visibility: threads.Private, Text: "@bo can you look?"})
	if len(got.Notified) != 0 || len(got.Refused) != 1 || got.Refused[0].Refused != "they cannot read this project, so they were not told" {
		t.Fatalf("tagging someone who cannot read Vault did %+v", got)
	}
	if _, ok := p.Inbox.ByKey(tenant, inbox.MentionKey(got.Thread.ID, "m-1", "bo")); ok {
		t.Fatal("bo was sent a mention of a conversation they cannot read")
	}
	if th, _ := p.Threads.Get(tenant, got.Thread.ID); slicesHas(th.Participants, "bo") {
		t.Fatal("bo was invited to a conversation in a project they cannot read")
	}

	cur, err := p.ProjectExperts(tenant, "Field")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.SetProjectExperts(tenant, "Field", ada, platform.Experts{Version: cur.Version, Experts: []projects.Expert{{Who: "role:security", Topics: []string{"security"}}}}); err != nil {
		t.Fatal(err)
	}
	workIn(t, p, "Field", operator)
	private := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Visibility: threads.Private, Text: "@sme:security is this safe?"})
	if len(private.Notified) != 0 || len(private.Refused) != 1 || !strings.Contains(private.Refused[0].Refused, "Find Help") {
		t.Fatalf("tagging a role's experts in a private conversation did %+v", private)
	}
	shared := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Visibility: threads.Shared, Text: "@sme:security is this safe?"})
	if len(shared.Notified) != 0 || len(shared.Refused) != 1 || !strings.Contains(shared.Refused[0].Refused, "Find Help") {
		t.Fatalf("tagging a role's experts in a shared conversation did %+v", shared)
	}
}
