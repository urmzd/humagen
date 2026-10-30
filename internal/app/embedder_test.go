package app

import (
	"context"
	"testing"

	"github.com/urmzd/humagen/pkg/spec"
)

func embedderFrom(t *testing.T, kind string, block any) loaded {
	t.Helper()
	var l loaded
	l.cfg.System.Spec.Knowledge.Embedder = spec.Slot{Kind: kind}
	if block != nil {
		l.cfg.System.Spec.Knowledge.Embedder.Block = spec.MustJSON(block)
	}
	return l
}

func TestTheSpecChoosesTheEmbedder(t *testing.T) {
	ctx := context.Background()
	none := func(string) string { return "" }
	if e, err := openEmbedder(ctx, Options{}, embedderFrom(t, "none", nil), none); err != nil || e != nil {
		t.Fatalf("none: %v %v", e, err)
	}
	if e, err := openEmbedder(ctx, Options{}, loaded{}, none); err != nil || e != nil {
		t.Fatalf("no file: %v %v", e, err)
	}
	e, err := openEmbedder(ctx, Options{}, embedderFrom(t, "hashing", spec.HashingEmbedder{Dimensions: 64}), none)
	if err != nil || e == nil || e.Dimensions() != 64 {
		t.Fatalf("hashing: %v %v", e, err)
	}
	key := func(k string) string {
		if k == embedderKeyEnv {
			return "secret"
		}
		return ""
	}
	e, err = openEmbedder(ctx, Options{}, embedderFrom(t, "openai", spec.OpenAIEmbedder{BaseURL: "http://127.0.0.1:1/v1", Model: "m", Dimensions: 8}), key)
	if err != nil || e == nil || e.Dimensions() != 8 {
		t.Fatalf("openai: %v %v", e, err)
	}
}
