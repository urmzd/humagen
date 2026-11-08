package app

import (
	"context"
	"fmt"

	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/spec"
	"github.com/urmzd/humagen/pkg/speech"
)

// openSpeech builds the System spec's speech slot. None, the default, is
// nil: voice notes stay attachments and nothing is transcribed, unless a
// workspace sets up a voice provider of its own. The key is a reference
// read on every request, never held.
func openSpeech(ctx context.Context, o Options, l loaded, keys credref.Resolver) (speech.Transcriber, error) {
	slot := l.cfg.System.Spec.Speech
	switch slot.Kind {
	case "", "none":
		return nil, nil
	case speech.KindGroq, speech.KindOpenAI:
		b, err := spec.Block[spec.GroqSpeech](slot)
		if err != nil {
			return nil, fmt.Errorf("speech: %w", err)
		}
		ref := b.KeyRefFor(slot.Kind)
		return speech.New(slot.Kind, speech.Config{
			BaseURL: b.BaseURL, Model: b.Model, SpeechModel: b.SpeechModel, Voice: b.Voice,
			MaxBytes: b.MaxBytes, Timeout: b.TimeoutOr(speech.GroqTimeout),
			Key: func(ctx context.Context) (string, error) { return keys.Resolve(ctx, ref) },
		})
	default:
		t, err := build[speech.Transcriber](ctx, o, spec.SlotSpeech, slot)
		if err != nil {
			return nil, fmt.Errorf("speech: %w", err)
		}
		return t, nil
	}
}
