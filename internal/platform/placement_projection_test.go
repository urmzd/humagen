package platform_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestMovedHistoryDoesNotRevealPrivatePlacementMetadata(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		p, err := newOn(t, open(t))
		if err != nil {
			t.Fatal(err)
		}
		who := auth.Principal{Tenant: tenant, Subject: "ada", Kind: auth.User}
		personal, err := p.PersonalProject(who)
		if err != nil {
			t.Fatal(err)
		}
		th, _, err := p.Threads.Start(tenant, threads.Thread{Project: personal.Name, Channel: "general", Visibility: threads.Private}, threads.Message{By: "ada", Name: "Ada", Text: "a draft"}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		id := runtime.RunID(tenant, "private-run")
		tag := envelope.Tag{ID: "private-run", Tenant: tenant, Agent: "helper", Origin: runs.OriginAPI, Text: personal.Name, Meta: map[string]string{runs.MetaProject: personal.Name, runs.MetaThread: th.ID, runs.MetaSources: "private-source"}, At: time.Now()}
		if err := p.Runs.Record(id, tag, 1); err != nil {
			t.Fatal(err)
		}
		raw, err := p.Runs.Get(tenant, id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Threads.Share(tenant, th.ID, th.Version, "Field", "general", "", nil, "", nil, nil, "ada", time.Now()); err != nil {
			t.Fatal(err)
		}
		shown, err := json.Marshal(p.ShownRun(tenant, raw))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(shown), personal.Name) || strings.Contains(string(shown), "private-source") {
			t.Fatalf("private identity in operational run: %s", shown)
		}
		payload, _ := json.Marshal(map[string]any{"project": personal.Name, "meta": tag.Meta, "text": "finished private-source", "input": map[string]any{"context": personal.Name}})
		event := p.ShownEvent(who, envelope.Event{Tenant: tenant, RunID: id, Kind: envelope.KindCompleted, Payload: payload})
		if !json.Valid(event.Payload) || strings.Contains(string(event.Payload), personal.Name) || strings.Contains(string(event.Payload), "private-source") {
			t.Fatalf("private provenance in decoded event: %s", event.Payload)
		}
		after, _ := p.Runs.Get(tenant, id)
		if after.Project != personal.Name || after.Tag().Meta[runs.MetaSources] != "private-source" {
			t.Fatal("projection changed immutable execution attribution")
		}
	})
}

func TestCompletedConversationPublicationRequiresCurrentPreview(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		p, err := newOn(t, open(t))
		if err != nil {
			t.Fatal(err)
		}
		who := auth.Principal{Tenant: tenant, Subject: "ada", Kind: auth.User, Roles: []auth.Role{auth.Operator}}
		personal, err := p.PersonalProject(who)
		if err != nil {
			t.Fatal(err)
		}
		destination, err := p.Projects.Create(tenant, projects.Project{Name: "Field"}, projects.Writer{By: who.Subject}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Agents.Create(t.Context(), tenant, agentspec.AgentSpec{Name: "helper", Model: "offline", Provider: "offline", SystemPrompt: "Help."}, "ada"); err != nil {
			t.Fatal(err)
		}
		th, _, err := p.Threads.Start(tenant, threads.Thread{Project: personal.Name, Channel: "general", Visibility: threads.Private}, threads.Message{By: "ada", Name: "Ada", Text: "a draft"}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		id := runtime.RunID(tenant, "publish-run")
		tag := envelope.Tag{ID: "publish-run", Tenant: tenant, Agent: "helper", Origin: runs.OriginAPI, Text: "draft", Meta: map[string]string{runs.MetaProject: personal.Name, runs.MetaThread: th.ID}, At: time.Now()}
		if err := p.Runs.Record(id, tag, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := p.PreviewPlacement(t.Context(), who, th.ID, "Field", "general"); err == nil {
			t.Fatal("active work could be published")
		}
		p.Runs.Observe(envelope.Event{Tenant: tenant, RunID: id, Kind: envelope.KindCompleted, Payload: []byte(`{"text":"finished"}`), At: time.Now()})
		preview, err := p.PreviewPlacement(t.Context(), who, th.ID, "Field", "general")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Share(t.Context(), who, th.ID, platform.ThreadShare{Project: "Field"}); err == nil {
			t.Fatal("completed work moved without confirmation")
		}
		if _, err := p.Projects.Update(tenant, destination.Name, destination.Version, projects.Writer{By: "ada"}, time.Now(), func(p *projects.Project) error { p.Description = "Audience reviewed again"; return nil }); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Share(t.Context(), who, th.ID, platform.ThreadShare{Project: "Field", PlacementToken: preview.Token}); err == nil {
			t.Fatal("stale destination preview was accepted")
		}
		preview, err = p.PreviewPlacement(t.Context(), who, th.ID, "Field", "general")
		if err != nil {
			t.Fatal(err)
		}
		shared, err := p.Share(t.Context(), who, th.ID, platform.ThreadShare{Project: "Field", PlacementToken: preview.Token})
		if err != nil {
			t.Fatal(err)
		}
		if shared.Thread.Project != "Field" || shared.Thread.Moves != 1 {
			t.Fatal("placement did not commit")
		}
		outsider := auth.Principal{Tenant: tenant, Subject: "ben", Kind: auth.User, Roles: []auth.Role{auth.Viewer}}
		raw, _ := p.Runs.Get(tenant, id)
		if p.Seer(outsider).Project(personal.Name) || !p.Seer(outsider).Run(raw) {
			t.Fatal("destination reader did not get precisely the published audience")
		}
		counts := p.ProjectCounts(outsider)
		if counts["Field"].Threads != 1 || counts[personal.Name].Threads != 0 {
			t.Fatalf("old placement leaked into counts: %v", counts)
		}
		if raw.Project != personal.Name {
			t.Fatal("publication changed accounting attribution")
		}
	})
}

func TestPrivateSourceHistoryPublishesOnlyReviewedSummaryCopy(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		p, err := newOn(t, open(t))
		if err != nil {
			t.Fatal(err)
		}
		who := auth.Principal{Tenant: tenant, Subject: "ada", Kind: auth.User, Roles: []auth.Role{auth.Operator}}
		if _, err := p.Projects.Create(tenant, projects.Project{Name: "Field"}, projects.Writer{By: "ada"}, time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Agents.Create(t.Context(), tenant, agentspec.AgentSpec{Name: "helper", Model: "offline", Provider: "offline", SystemPrompt: "Help.", Tools: []string{"source_search"}}, "ada"); err != nil {
			t.Fatal(err)
		}
		th, _, err := p.Threads.Start(tenant, threads.Thread{Project: "Field", Channel: "general", Visibility: threads.Private}, threads.Message{By: "ada", Name: "Ada", Text: "PRIVATE-SOURCE-MARKER"}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		id := runtime.RunID(tenant, "private-connected")
		tag := envelope.Tag{ID: "private-connected", Tenant: tenant, Agent: "helper", Origin: runs.OriginAPI, Text: "PRIVATE-SOURCE-MARKER", Meta: map[string]string{runs.MetaProject: "Field", runs.MetaThread: th.ID}, At: time.Now()}
		if err := p.Runs.Record(id, tag, 1); err != nil {
			t.Fatal(err)
		}
		p.Runs.Observe(envelope.Event{Tenant: tenant, RunID: id, Kind: envelope.KindCompleted, Payload: []byte(`{"text":"PRIVATE-SOURCE-MARKER"}`), At: time.Now()})
		preview, err := p.PreviewPlacement(t.Context(), who, th.ID, "Field", "general")
		if err != nil || !preview.SummaryRequired {
			t.Fatalf("private source classification: %+v %v", preview, err)
		}
		if _, err := p.Share(t.Context(), who, th.ID, platform.ThreadShare{PlacementToken: preview.Token}); err == nil {
			t.Fatal("same-project raw publication widened private source history")
		}
		if _, err := p.UpdateThread(t.Context(), who, th.ID, platform.ThreadUpdate{Version: th.Version, Visibility: threads.Shared, PlacementToken: preview.Token}); err == nil {
			t.Fatal("visibility edit bypassed private source boundary")
		}
		input := platform.SummaryPublication{Project: "Field", Channel: "general", Summary: "Reviewed public result", PlacementToken: preview.Token}
		published, err := p.PublishSummary(t.Context(), who, th.ID, input)
		if err != nil {
			t.Fatal(err)
		}
		replay, err := p.PublishSummary(t.Context(), who, th.ID, input)
		if err != nil || replay.ID != published.ID {
			t.Fatal("publication retry duplicated the copy")
		}
		source, _ := p.Threads.Get(tenant, th.ID)
		if source.Visibility != threads.Private {
			t.Fatal("summary publication changed source audience")
		}
		messages, _, err := p.Threads.Messages(tenant, published.ID, "", 10)
		if err != nil || len(messages) != 1 || messages[0].Text != input.Summary {
			t.Fatalf("publication copied raw history: %v %v", messages, err)
		}
		bytes, _ := json.Marshal(published)
		if strings.Contains(string(bytes), th.ID) || strings.Contains(string(bytes), "PRIVATE-SOURCE-MARKER") {
			t.Fatal("publication exposed private provenance")
		}
		ben := auth.Principal{Tenant: tenant, Subject: "ben", Kind: auth.User, Roles: []auth.Role{auth.Viewer}}
		raw, _ := p.Runs.Get(tenant, id)
		if p.Seer(ben).Run(raw) || !p.Seer(ben).Place(platform.Place{Project: "Field", Thread: published.ID}) {
			t.Fatal("copy granted a raw source capability")
		}
	})
}
