package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/saige/flow/turn/native"
	ragtypes "github.com/urmzd/saige/rag/types"
)

func (r retriever) recordRetrieval(input, query string, ac *ragtypes.AssembledContext) error {
	snapshot := evals.Retrieval{Input: input, Query: query, Sources: slices.Clone(r.sources), Blocks: []evals.ContextBlock{}}
	if ac != nil {
		raw, err := json.Marshal(ac)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			return err
		}
	}
	if err := r.p.Evals.RecordRetrieval(r.tenant, r.runID, max(r.attempt, 1), snapshot.Seal()); err != nil {
		return fmt.Errorf("record retrieval context: %w", err)
	}
	return nil
}

// recordedRetriever has no store or network dependency. Even an empty result
// is replayed exactly, instead of falling through to a live source.
type recordedRetriever struct{ snapshot evals.Retrieval }

func (r recordedRetriever) Retrieve(_ context.Context, query string) (*ragtypes.AssembledContext, error) {
	if query != r.snapshot.Input {
		return nil, fmt.Errorf("recorded retrieval input changed")
	}
	if err := r.snapshot.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(r.snapshot)
	if err != nil {
		return nil, err
	}
	var out ragtypes.AssembledContext
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p *Platform) recordedRetrievalFor(tenant, own, runID, input string) (*evals.Retrieval, error) {
	proofs, err := p.recordedSourceEvidence(tenant, own, runID)
	if err != nil {
		return nil, err
	}
	r, err := p.Evals.LastRetrieval(tenant, runID)
	if errors.Is(err, evals.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	open := p.openFor(tenant, own)
	for _, name := range r.Sources {
		src, err := p.Knowledge.Get(tenant, name)
		// Retained provider material needs a current per-file authorizer. A
		// project-visible run does not grant that authority to an eval suite.
		if err != nil || src.RequiresUpstreamRead() || src.Lifecycle != nil && !open(src.Lifecycle.Project) {
			return nil, ErrForbidden
		}
	}
	for _, block := range r.Blocks {
		digest := knowledge.EvidenceDigest(block.Provenance.DocumentUUID, block.Provenance.DocumentTitle, block.Provenance.SourceURI, block.Text, block.Provenance.SectionIndex)
		if !slices.ContainsFunc(proofs, func(proof knowledge.CitationEvidence) bool {
			return proof.Label == block.Citation && proof.Digest == digest && slices.Contains(r.Sources, proof.Source)
		}) {
			return nil, ErrForbidden
		}
	}
	r.Input = input
	r = r.Seal()
	return &r, nil
}

func (p *Platform) recordedSourceEvidence(tenant, own, runID string) ([]knowledge.CitationEvidence, error) {
	proofs, err := p.SourceEvidence.ForRun(tenant, runID)
	if err != nil {
		return nil, err
	}
	open := p.openFor(tenant, own)
	for _, proof := range proofs {
		if proof.Provider != "manual" || proof.Person != "" || !open(proof.Project) {
			return nil, ErrForbidden
		}
		src, err := p.Knowledge.Get(tenant, proof.Source)
		if err != nil || src.RequiresUpstreamRead() {
			return nil, ErrForbidden
		}
		project := ""
		if src.Lifecycle != nil {
			project = src.Lifecycle.Project
		}
		if project != proof.Project {
			return nil, ErrForbidden
		}
	}
	return proofs, nil
}

func replayRetrieval(c evals.Case, sources []native.Source) (native.Retriever, []native.Source, error) {
	if c.Trace == nil || c.Trace.Retrieval == nil {
		if len(sources) > 0 {
			return nil, nil, fmt.Errorf("recorded retrieval context is required")
		}
		return nil, sources, nil
	}
	r := *c.Trace.Retrieval
	if err := r.Validate(); err != nil {
		return nil, nil, err
	}
	if r.Input != c.Input {
		return nil, nil, fmt.Errorf("recorded retrieval input changed")
	}
	for _, src := range sources {
		if !slices.Contains(r.Sources, src.Name) {
			return nil, nil, fmt.Errorf("no recorded context for source %s", src.Name)
		}
	}
	// Include sources inherited from the original project as well as those
	// declared by the definition. Native retrieval is invoked only with sources.
	selected := make([]native.Source, 0, len(r.Sources))
	for _, name := range r.Sources {
		selected = append(selected, native.Source{Name: name})
	}
	return recordedRetriever{snapshot: r}, selected, nil
}
