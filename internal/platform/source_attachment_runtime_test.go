package platform_test

import (
	"strings"
	"testing"

	"github.com/urmzd/humagen/fiber/pkg/turn/native"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"
)

func TestChannelSourceAttachmentChangesActualGroundingAndSurvivesRestart(t *testing.T) {
	eachBackend(t, func(t *testing.T, b platform.Backends, again func() platform.Backends) {
		model := &agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{say("first"), say("second"), say("third")}}
		config := func(backends platform.Backends) platform.Config {
			return platform.Config{Backends: backends, AgentOptions: []native.Option{native.WithProvider(native.ProviderOffline, func(native.Spec) (saigetypes.Provider, error) { return model, nil })}}
		}

		p, stop := runningWith(t, config(b))
		if _, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: "source-worker", Provider: "offline", Model: "m", SystemPrompt: "Answer."}, "ada"); err != nil {
			t.Fatal(err)
		}
		if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "ada", Govern: true}, platform.ProjectInput{Name: "Field"}); err != nil {
			t.Fatal(err)
		}
		if _, err := p.CreateChannel(t.Context(), tenant, "Field", projects.Writer{By: "ada", Govern: true}, platform.ChannelInput{Name: "launch"}); err != nil {
			t.Fatal(err)
		}
		shelf(t, p, map[string]string{"channel-copy": "Zora"})
		who := auth.Principal{Tenant: tenant, Subject: "ada", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
		if _, err := p.AttachSource(who, "channel-copy", "Field", "launch"); err != nil {
			t.Fatal(err)
		}
		invoke := func(p *platform.Platform, id, ch string) runs.Run {
			t.Helper()
			r, err := p.Invoke(t.Context(), wire.NewEvent(wire.Raise{Tenant: tenant, ID: id, Agent: "source-worker", Origin: runs.OriginAPI, Text: "Who signs renewals?", Context: map[string]string{runs.MetaProject: "Field", runs.MetaChannel: ch}}))
			if err != nil {
				t.Fatal(err)
			}
			return await(t, p, r.ID, runs.Completed)
		}
		grounded := invoke(p, "channel-first", "launch")
		if len(grounded.Citations) != 1 || grounded.Citations[0].Mode != "indexed" {
			t.Fatalf("actual retrieval provenance missing: %+v", grounded.Citations)
		}
		invoke(p, "channel-other", "general")
		if !strings.Contains(sent(t, model.Calls[0]), "Zora") || strings.Contains(sent(t, model.Calls[1]), "Zora") {
			t.Fatal("channel attachment did not constrain real provider grounding")
		}
		stop()
		p2, stop2 := runningWith(t, config(again()))
		defer stop2()
		invoke(p2, "channel-restart", "launch")
		if !strings.Contains(sent(t, model.Calls[2]), "Zora") {
			t.Fatal("channel attachment lost after restart")
		}
		if err := p2.DetachSource(who, "channel-copy", "Field", "launch"); err != nil {
			t.Fatal(err)
		}
		attachments, err := p2.SourceAttachmentsAs(who, "channel-copy")
		if err != nil || len(attachments) != 0 {
			t.Fatal("detached placement still active", attachments, err)
		}
	})
}

func TestNativePostMessageRetrievesFromOriginalAskWithRepeatedContext(t *testing.T) {
	eachBackend(t, func(t *testing.T, b platform.Backends, _ func() platform.Backends) {
		model := &agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{say("Packaging is scheduled.")}}
		p, stop := runningWith(t, platform.Config{Backends: b, AgentOptions: []native.Option{native.WithProvider(native.ProviderOffline, func(native.Spec) (saigetypes.Provider, error) { return model, nil })}})
		defer stop()
		if _, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: "source-worker", Provider: "offline", Model: "offline", SystemPrompt: "Answer the person."}, "ada"); err != nil {
			t.Fatal(err)
		}
		if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "ada", Govern: true}, platform.ProjectInput{Name: "Field", Instructions: strings.Repeat("Packaging context and instructions. ", 100)}); err != nil {
			t.Fatal(err)
		}
		shelf(t, p, map[string]string{"copy": "Zora"})
		name(t, p, "Field", "copy")
		who := auth.Principal{Tenant: tenant, Subject: "ada", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
		initial, err := p.StartThread(t.Context(), who, platform.ThreadInput{Project: "Field", Visibility: "shared", Text: "Initial context", PostOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		posted, err := p.PostMessage(t.Context(), who, initial.Thread.ID, platform.MessageInput{Text: "@source-worker " + strings.Repeat("renewals ", 20) + "Who signs renewals?"})
		if err != nil || len(posted.Runs) != 1 {
			t.Fatalf("native post %+v %v", posted, err)
		}
		run := await(t, p, posted.Runs[0].ID, runs.Completed)
		if len(run.Citations) != 1 || run.Citations[0].Mode != "indexed" || !strings.Contains(sent(t, model.Calls[0]), "Zora") {
			t.Fatalf("native message lacked actual indexed grounding: %+v", run.Citations)
		}
		blind := auth.Principal{Tenant: tenant, Subject: "ada", Kind: auth.Machine, Scopes: []auth.Permission{auth.RunsRead, auth.ProjectsRead}}
		shown := p.ShownRunAs(blind, run)
		if shown.Citations == nil || len(shown.Citations) != 0 {
			t.Fatal("denied citations did not project an explicit empty list")
		}
	})
}
