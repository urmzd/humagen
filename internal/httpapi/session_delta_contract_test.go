package httpapi

import (
	"fmt"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestSessionChangesUseBoundedResponseContract(t *testing.T) {
	f := newFixture(t)
	c := &contract{t: t, f: f, spec: loadSpec(t), seen: map[string]bool{}}
	const tenant = "session-delta-contract"
	token := f.person(t, tenant, "ana", "Ana", "humagen-admins")
	c.call(token, "POST", "/api/agents", "/api/agents", helperSpec, 201)
	root := c.call(token, "POST", "/api/sessions", "/api/sessions", `{"agent":"helper","project":"~","text":"Begin"}`, 201)
	id := root.str("id")
	person, err := f.p.People.ByHandle(tenant, "ana")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	for i := 0; i <= platform.MaxTurnPage; i++ {
		if _, _, err := f.p.Threads.Post(tenant, id, threads.Message{By: person.Subject, Name: "Ana", Text: fmt.Sprintf("Catch-up change %d", i)}, at); err != nil {
			t.Fatal(err)
		}
	}
	page := c.call(token, "GET", "/api/sessions/{id}", "/api/sessions/"+id+"?since="+root.str("cursor"), "", 200)
	if page.field("more") != true || len(sliceOf(page.field("messages"))) != platform.MaxTurnPage {
		t.Fatal("changed-message page lost its continuation contract")
	}
	c.call(token, "GET", "/api/sessions/{id}", "/api/sessions/"+id+"?since="+page.str("cursor"), "", 200)
}
