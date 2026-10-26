package platform

import (
	"encoding/json"
	"testing"

	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestCitationProjectionRetainsQueryTimeFactsAndClearsRevokedEvidence(t *testing.T) {
	p := sourceGroundingPlatform(t)
	p.Threads = threads.NewStore()
	tag := envelope.Tag{ID: "turn", Tenant: "tenant", Agent: "worker", Text: "ask", At: p.now(), Meta: map[string]string{runs.MetaProject: "Secret"}}
	if err := p.Runs.Record("run1", tag, 1); err != nil {
		t.Fatal(err)
	}
	proof := knowledge.CitationEvidence{Source: "private", Project: "Secret", DocumentID: "doc", Title: "Runbook", Text: "private text", Mode: knowledge.Indexed}
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
}
