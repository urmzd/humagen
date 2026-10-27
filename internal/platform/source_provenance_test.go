package platform

import (
	"encoding/json"
	"testing"

	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestCitationProjectionRetainsQueryTimeFactsAndClearsRevokedEvidence(t *testing.T) {
	p := sourceGroundingPlatform(t)
	p.Threads = threads.NewStore()
	tag := envelope.Tag{ID: "turn", Tenant: "tenant", Agent: "worker", Text: "ask", At: p.now(), Meta: map[string]string{runs.MetaProject: "Secret"}}
	if err := p.Runs.Record("run1", tag, 1); err != nil {
		t.Fatal(err)
	}
	proof := knowledge.CitationEvidence{Provider: "manual", Source: "private", Project: "Secret", DocumentID: "doc", Title: "Runbook", Text: "private text", Mode: knowledge.Indexed}
	evidence, err := p.SourceEvidence.RecordQuery("tenant", "run1", "rag:1", []knowledge.CitationEvidence{proof})
	if err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"label":"[1]","text":"private text","source":{"document_uuid":"doc","document_title":"Runbook"},"mode":"live","refresh_failed":true}`)
	event := envelope.Event{Tenant: "tenant", RunID: "run1", Kind: envelope.KindCitation, Payload: raw}
	metadata, _, ok := p.trustedCitation(event)
	if !ok {
		t.Fatal("actual query not recognized", evidence)
	}
	if err := p.Runs.RecordCitation(event, metadata); err != nil {
		t.Fatal(err)
	}
	owner := auth.Principal{Tenant: "tenant", Subject: "owner", Kind: auth.User, Scopes: []auth.Permission{auth.KnowledgeRead}}
	run, _ := p.Runs.Get("tenant", "run1")
	shown := p.ShownRunAs(owner, run)
	if len(shown.Citations) != 1 || shown.Citations[0].Mode != "indexed" || *shown.Citations[0].RefreshFailed {
		t.Fatal("model claimed live facts", shown.Citations)
	}
	rawShown, _ := json.Marshal(p.enrichCitation(event))
	if !json.Valid(rawShown) {
		t.Fatal("bad projected event")
	}
	outsider := owner
	outsider.Subject = "peer"
	if p.CitationEventVisible(outsider, event) {
		t.Fatal("private citation event visible")
	}
	denied := p.ShownRunAs(outsider, run)
	body, _ := json.Marshal(denied)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(body, &fields)
	if string(fields["citations"]) != "[]" {
		t.Fatal("denied citations not explicitly cleared", string(body))
	}
	noKnowledge := owner
	noKnowledge.Scopes = nil
	if p.CitationEventVisible(noKnowledge, event) || len(p.ShownRunAs(noKnowledge, run).Citations) != 0 {
		t.Fatal("knowledge permission revocation retained evidence")
	}
	forged := event
	forged.Payload = json.RawMessage(`{"label":"[1]","text":"forged text","source":{"document_uuid":"doc","document_title":"Runbook"},"mode":"live"}`)
	if _, _, ok := p.trustedCitation(forged); ok {
		t.Fatal("label alone authorized forged metadata")
	}
	malformed := event
	malformed.Payload = json.RawMessage(`{"label":"[1]","source":"invalid","mode":"live"}`)
	if p.enrichCitation(malformed).Payload != nil {
		t.Fatal("malformed raw event retained model-claimed facts")
	}
}

func TestRetainedCitationAuthorityFailsClosedAcrossReplayAndSourceRecreation(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		p := sourceGroundingPlatform(t)
		p.Threads = threads.NewStore()
		var err error
		p.Runs, err = runs.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		p.SourceEvidence, err = knowledge.OpenEvidenceStore(st)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = p.Knowledge.Create("tenant", "external", "", "owner", p.now())
		cfg := knowledge.SourceConfig{Mode: knowledge.Indexed, Provider: "drive", Owner: "owner", Origin: "folder", Project: "Secret"}
		if _, err := p.Knowledge.Configure("tenant", "external", cfg, p.now()); err != nil {
			t.Fatal(err)
		}
		owner := auth.Principal{Tenant: "tenant", Subject: "owner", Kind: auth.User, Scopes: []auth.Permission{auth.KnowledgeRead}}
		cases := []struct {
			id, source, provider string
			visible              bool
		}{
			{id: "local", source: "private", provider: "manual", visible: true},
			{id: "external", source: "external", provider: "drive"},
			{id: "unknown", source: "private"},
			{id: "missing", source: "missing", provider: "manual"},
			{id: "unproven", source: "private", provider: "manual"},
		}
		events := map[string]envelope.Event{}
		for _, test := range cases {
			tag := envelope.Tag{ID: test.id, Tenant: "tenant", Agent: "worker", Text: "ask", At: p.now(), Meta: map[string]string{runs.MetaProject: "Secret"}}
			if err := p.Runs.Record(test.id, tag, 1); err != nil {
				t.Fatal(err)
			}
			proof := knowledge.CitationEvidence{Source: test.source, Provider: test.provider, Project: "Secret", DocumentID: "doc", Title: "Retained title", Text: "retained excerpt", Mode: knowledge.Indexed}
			if test.id != "unproven" {
				if _, err := p.SourceEvidence.RecordQuery("tenant", test.id, "rag:1", []knowledge.CitationEvidence{proof}); err != nil {
					t.Fatal(err)
				}
			}
			body := payload.Citation{Label: "[1]", Text: proof.Text, Source: payload.Provenance{DocumentUUID: proof.DocumentID, DocumentTitle: proof.Title}}
			raw, _ := json.Marshal(body)
			event := envelope.Event{Tenant: "tenant", RunID: test.id, Kind: envelope.KindCitation, Payload: raw}
			metadata, _, _ := p.trustedCitation(event)
			if err := p.Runs.RecordCitation(event, metadata); err != nil {
				t.Fatal(err)
			}
			events[test.id] = event
		}
		check := func() {
			t.Helper()
			for _, test := range cases {
				run, err := p.Runs.Get("tenant", test.id)
				if err != nil {
					t.Fatal(err)
				}
				if got := p.CitationEventVisible(owner, events[test.id]); got != test.visible {
					t.Fatalf("event %s visible=%v, want %v", test.id, got, test.visible)
				}
				shown := p.ShownRunAs(owner, run)
				if got := len(shown.Citations) == 1; got != test.visible {
					t.Fatalf("run %s retained citations=%+v", test.id, shown.Citations)
				}
				if len(run.Citations) != 1 {
					t.Fatal("projection mutated retained citation data", run.Citations)
				}
			}
		}
		check()
		p.SourceEvidence, err = knowledge.OpenEvidenceStore(st)
		if err != nil {
			t.Fatal(err)
		}
		p.Runs, err = runs.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		check()
		// An external query must not become a local grant after deletion or
		// recreation under the same source name.
		if err := p.Knowledge.Delete("tenant", "external"); err != nil {
			t.Fatal(err)
		}
		check()
		if _, err := p.Knowledge.Create("tenant", "external", "", "owner", p.now()); err != nil {
			t.Fatal(err)
		}
		check()
		other := owner
		other.Tenant = "other"
		if p.CitationEventVisible(other, events["local"]) {
			t.Fatal("cross-tenant event evidence exposed")
		}
		run, _ := p.Runs.Get("tenant", "local")
		if len(p.ShownRunAs(other, run).Citations) != 0 {
			t.Fatal("cross-tenant run evidence exposed")
		}
		if err := p.Knowledge.Delete("tenant", "private"); err != nil {
			t.Fatal(err)
		}
		if p.CitationEventVisible(owner, events["local"]) || len(p.ShownRunAs(owner, run).Citations) != 0 {
			t.Fatal("deleted source metadata granted historical file access")
		}
	})
}

func TestSourceToolEvidenceKeepsNativeProviderAuthority(t *testing.T) {
	for _, provider := range []string{"manual", "drive"} {
		t.Run(provider, func(t *testing.T) {
			p := sourceGroundingPlatform(t)
			p.Threads = threads.NewStore()
			tag := envelope.Tag{ID: "tool", Tenant: "tenant", Agent: "worker", At: p.now(), Meta: map[string]string{runs.MetaProject: "Secret"}}
			if err := p.Runs.Record("run1", tag, 1); err != nil {
				t.Fatal(err)
			}
			src := knowledge.Source{Name: "private", Lifecycle: &knowledge.Lifecycle{SourceConfig: knowledge.SourceConfig{Provider: provider, Project: "Secret"}}}
			mode := knowledge.Indexed
			if provider != "manual" {
				mode = knowledge.Live
			}
			citations, err := p.recordSourceToolCitations(runtime.Request{Tenant: "tenant", RunID: "run1"}, src, "owner", []knowledge.Hit{{Source: "private", DocumentID: "doc", Title: "Tool result", Text: "actual result", Mode: mode}})
			if err != nil || len(citations) != 1 {
				t.Fatal(citations, err)
			}
			proof, ok := p.SourceEvidence.Get("tenant", "run1", citations[0].ID, "doc")
			if !ok || proof.Provider != provider || (mode == knowledge.Live && proof.Person != "owner") {
				t.Fatal("tool evidence lost native query authority", proof)
			}
		})
	}
}

func TestRecordedSourceAuthoritySurvivesRecreationForPublication(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		p, err := New(Config{Workspace: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.Close() })
		p.SourceEvidence, err = knowledge.OpenEvidenceStore(st)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Agents.Create(t.Context(), "tenant", agentspec.AgentSpec{Name: "worker", Provider: "offline", Model: "offline", SystemPrompt: "Answer."}, "owner"); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Knowledge.Create("tenant", "copy", "", "owner", p.now()); err != nil {
			t.Fatal(err)
		}
		cfg := knowledge.SourceConfig{Mode: knowledge.Indexed, Provider: "drive", Origin: "folder", Owner: "owner"}
		if _, err := p.Knowledge.Configure("tenant", "copy", cfg, p.now()); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Knowledge.Create("tenant", "local-copy", "", "owner", p.now()); err != nil {
			t.Fatal(err)
		}
		for _, test := range []struct{ id, provider, source string }{
			{id: "external", provider: "drive", source: "copy"},
			{id: "unknown", source: "copy"},
			{id: "local", provider: "manual", source: "local-copy"},
		} {
			tag := envelope.Tag{ID: test.id, Tenant: "tenant", Agent: "worker", Text: "ask", At: p.now(), Meta: map[string]string{runs.MetaSources: test.source}}
			if err := p.Runs.Record(test.id, tag, 1); err != nil {
				t.Fatal(err)
			}
			proof := knowledge.CitationEvidence{Source: test.source, Provider: test.provider, DocumentID: "doc", Text: "retained excerpt", Mode: knowledge.Indexed}
			if _, err := p.SourceEvidence.RecordQuery("tenant", test.id, "rag:1", []knowledge.CitationEvidence{proof}); err != nil {
				t.Fatal(err)
			}
		}
		if err := p.Knowledge.Delete("tenant", "copy"); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Knowledge.Create("tenant", "copy", "", "owner", p.now()); err != nil {
			t.Fatal(err)
		}
		if !p.SourceMayPublishIn("tenant", "copy", "") {
			t.Fatal("recreated local fixture must exercise the name-only publication gap")
		}
		for attempt := range 2 {
			if attempt == 1 {
				p.SourceEvidence, err = knowledge.OpenEvidenceStore(st)
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, id := range []string{"external", "unknown", "local"} {
				run, _ := p.Runs.Get("tenant", id)
				if got := p.runNeedsSummary(t.Context(), run, ""); got != (id != "local") {
					t.Fatalf("query authority for %s changed after recreation: summary=%v", id, got)
				}
			}
		}
	})
}
