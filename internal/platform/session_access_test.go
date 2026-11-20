package platform_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestSessionChildRequiresParentPostingPermission(t *testing.T) {
	p := start(t, call.Limits{})
	root, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Agent: "lead", Text: "Private parent"})
	if err != nil {
		t.Fatal(err)
	}
	auditor := admin
	auditor.Subject = "auditor"
	if !p.SeesSession(auditor, root.ID) {
		t.Fatal("governance reader should be able to audit the parent")
	}
	if _, err := p.StartSession(t.Context(), auditor, platform.SessionInput{Parent: root.ID, Agent: "helper", Text: "Run against private context"}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("nonparticipant child creation: %v", err)
	}
	children, err := p.ListSessions(t.Context(), admin, root.ID, "", "", "")
	if err != nil || len(children) != 0 {
		t.Fatalf("denied child left a session: %+v, %v", children, err)
	}
}

func TestSessionInvitationsRequireCurrentAncestorAccess(t *testing.T) {
	for _, through := range []string{"people", "mention"} {
		t.Run(through, func(t *testing.T) {
			p := start(t, call.Limits{})
			field(t, p)
			seePeople(t, p, admin, operator)
			workIn(t, p, "Field", operator)
			root, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "Field", Agent: "lead", Text: "Private parent"})
			if err != nil {
				t.Fatal(err)
			}
			child, err := p.StartSession(t.Context(), admin, platform.SessionInput{Parent: root.ID, Agent: "helper", Text: "Private child"})
			if err != nil {
				t.Fatal(err)
			}
			invite := func(id string) error {
				th, err := p.Threads.Get(tenant, id)
				if err != nil {
					return err
				}
				_, err = p.SetParticipants(t.Context(), admin, id, platform.ParticipantsUpdate{Version: th.Version, Add: []threads.Participant{{Kind: threads.Person, Name: "opal"}}})
				return err
			}
			if through == "people" {
				if err := invite(child.ID); !errors.Is(err, threads.ErrInvalid) || !strings.Contains(err.Error(), "parent session") {
					t.Fatalf("child invitation without parent: %v", err)
				}
			} else {
				posted := post(t, p, admin, child.ID, platform.MessageInput{Text: "@opal inspect this"})
				if len(posted.Notified) != 0 || len(posted.Refused) != 1 || !strings.Contains(posted.Refused[0].Refused, "parent session") {
					t.Fatalf("child mention without parent: %+v", posted)
				}
			}
			th, err := p.Threads.Get(tenant, child.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, person := range th.Participants {
				if person.Kind == threads.Person && person.Name == operator.Subject {
					t.Fatal("refused invitation added the person")
				}
			}
			if err := invite(root.ID); err != nil {
				t.Fatal(err)
			}
			if through == "people" {
				if err := invite(child.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				posted := post(t, p, admin, child.ID, platform.MessageInput{Text: "@opal now inspect this"})
				if len(posted.Notified) != 1 || len(posted.Refused) != 0 {
					t.Fatalf("authorized child mention: %+v", posted)
				}
			}
			if !p.SeesSession(operator, child.ID) {
				t.Fatal("invited ancestor participant cannot read child")
			}
			parent, err := p.Threads.Get(tenant, root.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.SetParticipants(t.Context(), admin, root.ID, platform.ParticipantsUpdate{Version: parent.Version, Remove: []string{"opal"}}); err != nil {
				t.Fatal(err)
			}
			if _, err := p.PostMessage(t.Context(), operator, child.ID, platform.MessageInput{Text: "Continue after revocation"}); !errors.Is(err, threads.ErrNotFound) {
				t.Fatalf("revoked ancestor participant could post directly: %v", err)
			}
		})
	}
}
