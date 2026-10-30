package app

import (
	"context"
	"fmt"

	dense "github.com/urmzd/humagen/fiber/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/spec"
)

// embedderKeyEnv carries an openai embedder's key from the spec's reference
// to openEmbedder, the way the other secrets in a spec travel.
const embedderKeyEnv = "HUMAGEN_EMBEDDER_KEY"

// openEmbedder builds the System spec's knowledge.embedder. None, the
// default, is nil: search stays lexical.
func openEmbedder(ctx context.Context, o Options, l loaded, getenv func(string) string) (knowledge.Embedder, error) {
	slot := l.cfg.System.Spec.Knowledge.Embedder
	switch slot.Kind {
	case "", "none":
		return nil, nil
	case "hashing":
		b, err := spec.Block[spec.HashingEmbedder](slot)
		if err != nil {
			return nil, err
		}
		return dense.NewHashing(b.Dimensions), nil
	case "openai":
		b, err := spec.Block[spec.OpenAIEmbedder](slot)
		if err != nil {
			return nil, err
		}
		return dense.NewHTTP(dense.HTTPConfig{BaseURL: b.BaseURL, Model: b.Model, Dimensions: b.Dimensions, APIKey: getenv(embedderKeyEnv)})
	case "vertex":
		b, err := spec.Block[spec.VertexEmbedder](slot)
		if err != nil {
			return nil, err
		}
		return dense.NewVertex(ctx, dense.VertexConfig{Project: b.Project, Location: b.Location, Model: b.Model, Dimensions: b.Dimensions})
	default:
		e, err := build[knowledge.Embedder](ctx, o, spec.SlotEmbedder, slot)
		if err != nil {
			return nil, fmt.Errorf("knowledge.embedder: %w", err)
		}
		return e, nil
	}
}
