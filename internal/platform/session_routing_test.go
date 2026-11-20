package platform_test

import (
	"errors"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestSessionCompatibilityRoutingCannotChangeItsOwner(t *testing.T) {
	p := start(t, call.Limits{})
	root, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Agent: "lead", Text: "Start a session"})
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range root.Messages {
		if message.Run != nil {
			await(t, p, message.Run.ID, runs.Completed)
		}
	}
	for _, change := range []struct {
		name, def string
		agents    []string
	}{
		{name: "default", def: "helper", agents: []string{"lead"}},
		{name: "selected", def: "lead", agents: []string{"helper"}},
		{name: "both", def: "helper", agents: []string{"helper"}},
		{name: "empty", def: "", agents: []string{}},
	} {
		t.Run(change.name, func(t *testing.T) {
			before, err := p.Threads.Get(admin.Tenant, root.ID)
			if err != nil {
				t.Fatal(err)
			}
			_, err = p.UpdateThread(t.Context(), admin, root.ID, platform.ThreadUpdate{Version: before.Version, Title: before.Title, Visibility: before.Visibility, Default: change.def, Agents: change.agents})
			if !errors.Is(err, threads.ErrInvalid) {
				t.Fatalf("compatibility routing edit was not refused: %v", err)
			}
			after, err := p.Threads.Get(admin.Tenant, root.ID)
			if err != nil || after.Session == nil || after.Session.Agent != "lead" || after.Default != "lead" || len(after.Agents) != 1 || after.Agents[0] != "lead" || after.Version != before.Version {
				t.Fatalf("refused edit changed stored session: %+v err=%v", after, err)
			}
		})
	}
	th, err := p.Threads.Get(admin.Tenant, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpdateThread(t.Context(), admin, root.ID, platform.ThreadUpdate{Version: th.Version, Title: "Renamed session", Visibility: th.Visibility, Default: "lead", Agents: []string{"lead"}}); err != nil {
		t.Fatalf("title edit was refused: %v", err)
	}
	continued, err := p.PostSession(t.Context(), admin, root.ID, platform.MessageInput{Text: "Continue without an agent mention"})
	if err != nil {
		t.Fatal(err)
	}
	var ownerRuns []string
	for _, message := range continued.Messages {
		if message.Run == nil {
			continue
		}
		if message.Run.Agent != "lead" {
			t.Fatalf("another agent answered in this session: %+v", message.Run)
		}
		ownerRuns = append(ownerRuns, message.Run.ID)
	}
	if len(ownerRuns) != 2 {
		t.Fatalf("owner stopped answering after refused edits: %v", ownerRuns)
	}
	for _, id := range ownerRuns {
		await(t, p, id, runs.Completed)
	}
}
