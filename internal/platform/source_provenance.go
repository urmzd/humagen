package platform

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/runs"
)

// SourceCitationEvidence is an internal composition seam. Public callers must
// first apply Seer.Run and KnowledgeRead; source/doc bindings are never DTOs.
func (p *Platform) SourceCitationEvidence(tenant, runID, label, documentID string) (knowledge.CitationEvidence, bool) {
	return p.SourceEvidence.Get(tenant, runID, label, documentID)
}
func (p *Platform) trustedCitation(e envelope.Event) (*runs.CitationMetadata, knowledge.CitationEvidence, bool) {
	if e.Kind != envelope.KindCitation {
		return nil, knowledge.CitationEvidence{}, false
	}
	body, err := payload.Decode[payload.Citation](e)
	if err != nil {
		return nil, knowledge.CitationEvidence{}, false
	}
	proof, ok := p.SourceCitationEvidence(e.Tenant, e.RunID, body.Label, body.Source.DocumentUUID)
	if !ok || proof.Digest != knowledge.EvidenceDigest(body.Source.DocumentUUID, body.Source.DocumentTitle, body.Source.SourceURI, body.Text, body.Source.SectionIndex) {
		return nil, knowledge.CitationEvidence{}, false
	}
	return &runs.CitationMetadata{Mode: string(proof.Mode), CopyAt: proof.CopyAt, RefreshFailed: proof.RefreshFailed}, proof, true
}

// enrichCitation strips any model-claimed metadata before adding actual facts.
func (p *Platform) enrichCitation(e envelope.Event) envelope.Event {
	if e.Kind != envelope.KindCitation {
		return e
	}
	body, err := payload.Decode[payload.Citation](e)
	if err != nil {
		e.Payload = nil
		return e
	}
	shown := runs.CitationPayload{Citation: body}
	if metadata, _, ok := p.trustedCitation(e); ok {
		shown.Mode = metadata.Mode
		shown.CopyAt = metadata.CopyAt
		failed := metadata.RefreshFailed
		shown.RefreshFailed = &failed
	}
	if raw, err := json.Marshal(shown); err == nil {
		e.Payload = raw
	}
	return e
}
func (p *Platform) citationEvidenceVisible(who auth.Principal, proof knowledge.CitationEvidence) bool {
	if !who.Can(auth.KnowledgeRead) {
		return false
	}
	if proof.Person != "" && (who.Kind == auth.Machine || who.Subject != proof.Person) {
		return false
	}
	if proof.Project != "" && !p.Sees(who, Place{Project: proof.Project}) {
		return false
	}
	if _, err := p.Knowledge.Get(who.Tenant, proof.Source); err == nil {
		return p.SourceVisible(who, proof.Source)
	}
	// Deleted metadata cannot erase its retained query-time scope.
	return true
}
func (p *Platform) CitationEventVisible(who auth.Principal, e envelope.Event) bool {
	if e.Kind != envelope.KindCitation {
		return true
	}
	if !who.Can(auth.KnowledgeRead) {
		return false
	}
	if _, proof, ok := p.trustedCitation(e); ok {
		return p.citationEvidenceVisible(who, proof)
	}
	if run, err := p.Runs.Get(who.Tenant, e.RunID); err == nil {
		shown := p.ShownRun(who.Tenant, run)
		if shown.Project != run.Project || shown.Channel != run.Channel {
			return false
		}
	}
	return true
}

// ShownRunAs clears denied evidence explicitly, including citations already
// streamed before a reader's access changed. It retains historical unknowns.
func (p *Platform) ShownRunAs(who auth.Principal, run runs.Run) runs.Run {
	original := run
	run = p.ShownRun(who.Tenant, run)
	run.Citations = []runs.Citation{}
	if !who.Can(auth.KnowledgeRead) || !p.Seer(who).Run(original) {
		return run
	}
	for _, citation := range original.Citations {
		if proof, ok := p.SourceCitationEvidence(who.Tenant, original.ID, citation.ID, ""); ok {
			if !p.citationEvidenceVisible(who, proof) {
				continue
			}
		} else if run.Project != original.Project || run.Channel != original.Channel {
			continue
		}
		run.Citations = append(run.Citations, runs.CloneCitations([]runs.Citation{citation})[0])
	}
	return run
}
func (p *Platform) ShownRunsAs(who auth.Principal, list []runs.Run) []runs.Run {
	out := make([]runs.Run, len(list))
	for i, r := range list {
		out[i] = p.ShownRunAs(who, r)
	}
	return out
}

func (p *Platform) recordSourceToolCitations(req runtime.Request, src knowledge.Source, person string, hits []knowledge.Hit) ([]runs.Citation, error) {
	out := []runs.Citation{}
	if len(hits) == 0 {
		return out, nil
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	entries := []knowledge.CitationEvidence{}
	for _, hit := range hits {
		project := ""
		if src.Lifecycle != nil {
			project = src.Lifecycle.Project
		}
		reader := ""
		if hit.Mode == knowledge.Live {
			reader = person
		}
		entries = append(entries, knowledge.CitationEvidence{Source: hit.Source, Project: project, Person: reader, DocumentID: hit.DocumentID, Index: hit.Index, Title: hit.Title, URI: hit.URI, Text: hit.Text, Mode: hit.Mode, CopyAt: hit.CopyAt, RefreshFailed: hit.RefreshFailed})
	}
	evidence, err := p.SourceEvidence.RecordQuery(req.Tenant, req.RunID, "search:"+hex.EncodeToString(nonce[:]), entries)
	if err != nil {
		return nil, fmt.Errorf("source citation evidence could not be kept: %w", err)
	}
	for _, entry := range evidence {
		body := payload.Citation{Label: entry.Label, Text: entry.Text, Index: entry.Index, Source: payload.Provenance{DocumentUUID: entry.DocumentID, DocumentTitle: entry.Title, SourceURI: entry.URI, SectionIndex: entry.Index}}
		raw, _ := json.Marshal(body)
		event := envelope.Event{Tenant: req.Tenant, RunID: req.RunID, Kind: envelope.KindCitation, Payload: raw, At: p.now()}
		metadata := &runs.CitationMetadata{Mode: string(entry.Mode), CopyAt: entry.CopyAt, RefreshFailed: entry.RefreshFailed}
		if err := p.Runs.RecordCitation(event, metadata); err != nil {
			return nil, err
		}
		failed := entry.RefreshFailed
		out = append(out, runs.Citation{ID: entry.Label, Title: entry.Title, URL: entry.URI, Snippet: entry.Text, Mode: string(entry.Mode), CopyAt: entry.CopyAt, RefreshFailed: &failed})
	}
	return out, nil
}
