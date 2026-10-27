package platform

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/mcptools"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/credentials"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

type citationProvider struct {
	calls int
	fail  bool
}

func (s *citationProvider) Fetch(context.Context, string, mcptools.Secret) ([]knowledge.ImportDocument, error) {
	s.calls++
	if s.fail {
		return nil, knowledge.ProviderError{Cause: knowledge.AuthExpired}
	}
	return []knowledge.ImportDocument{{ID: "pinned", Title: "Pinned", Text: "release retained"}, {ID: "fresh", Title: "Fresh", Text: "release live"}}, nil
}

func TestApprovedHybridToolUsesCurrentReaderAccessAndNeverFallsBack(t *testing.T) {
	p := sourceGroundingPlatform(t)
	provider := &citationProvider{}
	p.cfg.Ext.SourceProviders = map[string]knowledge.Provider{"test": provider}
	p.cfg.Ext.Resolver = mcptools.ResolverFunc(func(context.Context, string) (mcptools.Secret, error) { return mcptools.NewSecret("fixture-only"), nil })
	p.cfg.CredentialRefs = map[string][]string{"tenant": {"op://People/{subject}/"}}
	p.Credentials = credentials.New(nil)
	p.Threads = threads.NewStore()
	_, err := p.Knowledge.Create("tenant", "hybrid", "", "owner", p.now())
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Knowledge.Configure("tenant", "hybrid", knowledge.SourceConfig{Mode: knowledge.Hybrid, Provider: "test", Owner: "owner", Project: "Secret", Origin: "folder"}, p.now())
	if err != nil {
		t.Fatal(err)
	}
	id, _ := p.Knowledge.BeginSync("tenant", "hybrid", p.now())
	_, err = p.Knowledge.CompleteSync("tenant", "hybrid", id, []knowledge.ImportDocument{{ID: "pinned", Title: "Pinned", Text: "release retained"}}, "owner", p.now())
	if err != nil {
		t.Fatal(err)
	}
	src, _ := p.Knowledge.Get("tenant", "hybrid")
	_, err = p.Credentials.Put("tenant", "owner", sourceCredentialID(src), credentials.Input{Ref: "op://People/owner/source"}, p.now())
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Threads.Create("tenant", threads.Thread{ID: "th-0000000000000001", Project: "Secret", Channel: "general", Visibility: threads.Private, CreatedBy: "owner", Participants: []threads.Participant{{Kind: threads.Person, Name: "owner", Owner: true}}}, p.now())
	if err != nil {
		t.Fatal(err)
	}
	tag := envelope.Tag{ID: "tool", Origin: runs.OriginAPI, Tenant: "tenant", Agent: "worker", Actor: envelope.Actor{ID: "owner", User: "owner"}, At: p.now(), Meta: map[string]string{runs.MetaProject: "Secret", runs.MetaThread: "th-0000000000000001"}}
	if err := p.Runs.Record("run1", tag, 1); err != nil {
		t.Fatal(err)
	}
	req := runtime.Request{Tenant: "tenant", RunID: "run1", Revision: agentrt.Revision{Spec: agentrt.Spec{Name: "worker", Sources: []agentrt.Source{{Name: "hybrid"}}}}}
	execute := func() struct {
		Hits      []knowledge.Hit       `json:"hits"`
		Citations []runs.Citation       `json:"citations"`
		Warnings  []SourceSearchWarning `json:"warnings"`
	} {
		t.Helper()
		text, err := p.sourceTools(req)[ToolSourceSearch].Execute(t.Context(), map[string]any{"source": "hybrid", "query": "release"})
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Hits      []knowledge.Hit       `json:"hits"`
			Citations []runs.Citation       `json:"citations"`
			Warnings  []SourceSearchWarning `json:"warnings"`
		}
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	out := execute()
	if provider.calls != 1 || len(out.Hits) != 2 || len(out.Citations) != 2 || out.Citations[0].Mode != "live" || out.Citations[1].Mode != "live" || out.Citations[1].CopyAt != nil {
		t.Fatal("actual hybrid provenance missing", out)
	}
	live, ok := p.SourceEvidence.Get("tenant", "run1", out.Citations[1].ID, "")
	if !ok || live.Person != "owner" {
		t.Fatal("live evidence lost reader binding")
	}
	provider.fail = true
	text, err := p.sourceTools(req)[ToolSourceSearch].Execute(t.Context(), map[string]any{"source": "hybrid", "query": "release"})
	if text != "" || knowledge.FailureCause(err) != knowledge.AuthExpired {
		t.Fatal("revoked reader access returned retained text", text, err)
	}
}
