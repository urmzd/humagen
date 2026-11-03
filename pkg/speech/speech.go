// Package speech turns a person's voice into text, and text into speech,
// behind interfaces a deployment fills from the System spec's speech slot.
//
// A voice note reaches the platform as an audio attachment, whatever the
// channel: a Slack audio clip, a recording from the console, and later a
// WhatsApp or iMessage voice note. The platform transcribes it before a run
// starts (internal/platform's TranscribeVoiceNotes), so an agent reads the
// words and the recording stays attached.
package speech

import (
	"context"
	"errors"
	"io"
	"mime"
	"strings"
	"sync"
)

// Errors. Callers match with errors.Is.
var (
	// ErrTooLarge is audio over the transcriber's size cap.
	ErrTooLarge = errors.New("speech: audio is too large")
	// ErrUnsupported is content that is not audio.
	ErrUnsupported = errors.New("speech: not audio")
	// ErrEmpty is audio with nothing in it, or text with nothing to say.
	ErrEmpty = errors.New("speech: empty")
	// ErrUnavailable is a provider that failed or did not answer in time.
	ErrUnavailable = errors.New("speech: unavailable")
)

// Transcriber turns audio into text.
type Transcriber interface {
	// Transcribe reads at most the transcriber's cap from audio, whose media
	// type is mime, and returns what was said and, when the provider
	// reports it, the language it was said in.
	Transcribe(ctx context.Context, audio io.Reader, mime string) (text, language string, err error)
}

// Synthesizer turns text into audio. A kind that has no text-to-speech
// endpoint does not implement it.
type Synthesizer interface {
	// Synthesize speaks text and returns the audio and its media type.
	Synthesize(ctx context.Context, text string) (audio []byte, mime string, err error)
}

// IsAudio reports whether a media type is audio a transcriber takes. Its
// parameters (codecs=opus) are ignored. video/webm and video/mp4 count,
// since a browser's recorder and some phones label a voice-only recording
// as video.
func IsAudio(mediaType string) bool {
	return BaseType(mediaType) != ""
}

// BaseType is mediaType without parameters, lowercased, when it is audio a
// transcriber takes, else "".
func BaseType(mediaType string) string {
	mt, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		return ""
	}
	mt = strings.ToLower(mt)
	switch {
	case strings.HasPrefix(mt, "audio/"), mt == "video/webm", mt == "video/mp4":
		return mt
	}
	return ""
}

// Fake is an in-memory Transcriber and Synthesizer for tests. It answers
// Text and Language for every audio it is given, and records what it read.
type Fake struct {
	Text     string
	Language string
	// Err, when set, is returned instead.
	Err error
	// Max caps what is read, as a real kind's size cap does. Zero is no cap.
	Max int64

	mu    sync.Mutex
	heard [][]byte
	mimes []string
}

// Transcribe answers the fake's text for any audio.
func (f *Fake) Transcribe(ctx context.Context, audio io.Reader, mimeType string) (string, string, error) {
	if !IsAudio(mimeType) {
		return "", "", ErrUnsupported
	}
	b, err := ReadCapped(audio, f.Max)
	if err != nil {
		return "", "", err
	}
	f.mu.Lock()
	f.heard = append(f.heard, b)
	f.mimes = append(f.mimes, mimeType)
	f.mu.Unlock()
	if f.Err != nil {
		return "", "", f.Err
	}
	return f.Text, f.Language, nil
}

// Synthesize answers the text's own bytes as audio/wav.
func (f *Fake) Synthesize(ctx context.Context, text string) ([]byte, string, error) {
	if f.Err != nil {
		return nil, "", f.Err
	}
	if strings.TrimSpace(text) == "" {
		return nil, "", ErrEmpty
	}
	return []byte(text), "audio/wav", nil
}

// Heard is every audio the fake was given, with its media type, in order.
func (f *Fake) Heard() ([][]byte, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]byte(nil), f.heard...), append([]string(nil), f.mimes...)
}

// ReadCapped reads all of r, or fails with ErrTooLarge past max bytes, having
// read at most max+1, or with ErrEmpty when there is nothing. A max of zero
// or less reads without a cap.
func ReadCapped(r io.Reader, max int64) ([]byte, error) {
	if max > 0 {
		r = io.LimitReader(r, max+1)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if max > 0 && int64(len(b)) > max {
		return nil, ErrTooLarge
	}
	if len(b) == 0 {
		return nil, ErrEmpty
	}
	return b, nil
}
