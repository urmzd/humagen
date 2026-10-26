package httpapi

import (
	"context"
	"github.com/urmzd/fiber/pkg/mcptools"
	"github.com/urmzd/humagen/pkg/knowledge"
)

// contractSourceBoundary is an explicit offline external-provider boundary.
// Contract tests never claim its inventory or import was a live provider trial.
type contractSourceBoundary struct{}

func (contractSourceBoundary) Fetch(context.Context, string, mcptools.Secret) ([]knowledge.ImportDocument, error) {
	return []knowledge.ImportDocument{{ID: "doc", Title: "Contract runbook", Text: "release safely"}}, nil
}
func (contractSourceBoundary) Resources(context.Context, string, mcptools.Secret) ([]knowledge.Resource, error) {
	return []knowledge.Resource{{ID: "doc", Name: "Contract runbook", Kind: "text/plain", URI: "https://example.test/doc", Supported: true}}, nil
}
func (c contractSourceBoundary) FetchSelected(ctx context.Context, origin string, ids []string, secret mcptools.Secret) ([]knowledge.ImportDocument, error) {
	return c.Fetch(ctx, origin, secret)
}
