package speech

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

// Groq's defaults. Groq serves the OpenAI audio API under its own base URL.
const (
	GroqBaseURL = "https://api.groq.com/openai/v1"
	// GroqTranscriptionModel is Whisper large v3 turbo, Groq's fastest.
	GroqTranscriptionModel = "whisper-large-v3-turbo"
	// GroqSpeechModel and GroqVoice are Groq's English text-to-speech.
	GroqSpeechModel = "canopylabs/orpheus-v1-english"
	GroqVoice       = "autumn"
	// GroqMaxBytes is the most audio sent in one request. Groq refuses a
	// file over 25 MB on its free tier.
	GroqMaxBytes int64 = 25 << 20
	// GroqTimeout bounds one request.
	GroqTimeout = 60 * time.Second
	// MaxSpeechText bounds the text one Synthesize speaks.
	MaxSpeechText = 10000
	// maxAnswer bounds a provider's answer: a transcript or synthesized audio.
	maxAnswer = 32 << 20
)

// GroqConfig is a Groq kind. A zero field takes its default.
type GroqConfig struct {
	BaseURL string
	// Key reads the API key when a request is sent, so it is never held.
	Key         func(context.Context) (string, error)
	Model       string
	SpeechModel string
	Voice       string
	MaxBytes    int64
	Timeout     time.Duration
	HTTP        *http.Client
}

// Groq transcribes with POST /audio/transcriptions and speaks with POST
// /audio/speech on Groq's OpenAI-compatible API.
type Groq struct{ c GroqConfig }

// NewGroq fills the defaults in.
func NewGroq(c GroqConfig) (*Groq, error) {
	if c.Key == nil {
		return nil, errors.New("speech: groq needs a key")
	}
	c.BaseURL = strings.TrimSuffix(cmp.Or(c.BaseURL, GroqBaseURL), "/")
	c.Model = cmp.Or(c.Model, GroqTranscriptionModel)
	c.SpeechModel = cmp.Or(c.SpeechModel, GroqSpeechModel)
	c.Voice = cmp.Or(c.Voice, GroqVoice)
	if c.MaxBytes <= 0 {
		c.MaxBytes = GroqMaxBytes
	}
	if c.Timeout <= 0 {
		c.Timeout = GroqTimeout
	}
	if c.HTTP == nil {
		c.HTTP = http.DefaultClient
	}
	return &Groq{c: c}, nil
}

// extensions name the upload, since the endpoint reads the format from the
// file name.
var extensions = map[string]string{
	"audio/webm": "webm", "video/webm": "webm", "audio/ogg": "ogg", "audio/opus": "ogg",
	"audio/mpeg": "mp3", "audio/mp3": "mp3", "audio/mp4": "m4a", "audio/x-m4a": "m4a", "video/mp4": "mp4",
	"audio/wav": "wav", "audio/x-wav": "wav", "audio/wave": "wav", "audio/flac": "flac", "audio/x-flac": "flac",
}

// Transcribe sends the audio as multipart form data, asking for
// verbose_json, whose answer names the language.
func (g *Groq) Transcribe(ctx context.Context, audio io.Reader, mimeType string) (string, string, error) {
	mt := BaseType(mimeType)
	ext, ok := extensions[mt]
	if !ok {
		return "", "", fmt.Errorf("%w: %q", ErrUnsupported, mimeType)
	}
	b, err := ReadCapped(audio, g.c.MaxBytes)
	if err != nil {
		return "", "", err
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="voice.%s"`, ext))
	h.Set("Content-Type", mt)
	part, err := w.CreatePart(h)
	if err != nil {
		return "", "", err
	}
	if _, err := part.Write(b); err != nil {
		return "", "", err
	}
	for k, v := range map[string]string{"model": g.c.Model, "response_format": "verbose_json", "temperature": "0"} {
		if err := w.WriteField(k, v); err != nil {
			return "", "", err
		}
	}
	if err := w.Close(); err != nil {
		return "", "", err
	}
	raw, err := g.post(ctx, "/audio/transcriptions", w.FormDataContentType(), &body)
	if err != nil {
		return "", "", err
	}
	var out struct {
		Text     string `json:"text"`
		Language string `json:"language"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", fmt.Errorf("%w: an answer that is not a transcript", ErrUnavailable)
	}
	text := strings.TrimSpace(out.Text)
	if text == "" {
		return "", "", ErrEmpty
	}
	return text, strings.ToLower(strings.TrimSpace(out.Language)), nil
}

// Synthesize speaks text as WAV with the configured voice.
func (g *Groq) Synthesize(ctx context.Context, text string) ([]byte, string, error) {
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		return nil, "", ErrEmpty
	case len(text) > MaxSpeechText:
		return nil, "", fmt.Errorf("%w: at most %d bytes of text", ErrTooLarge, MaxSpeechText)
	}
	req, err := json.Marshal(map[string]string{"model": g.c.SpeechModel, "voice": g.c.Voice, "input": text, "response_format": "wav"})
	if err != nil {
		return nil, "", err
	}
	audio, err := g.post(ctx, "/audio/speech", "application/json", bytes.NewReader(req))
	if err != nil {
		return nil, "", err
	}
	return audio, "audio/wav", nil
}

// post sends one bounded request and reads a bounded answer.
func (g *Groq) post(ctx context.Context, path, contentType string, body io.Reader) ([]byte, error) {
	key, err := g.c.Key(ctx)
	if err != nil || key == "" {
		return nil, fmt.Errorf("%w: the key is not set", ErrUnavailable)
	}
	ctx, cancel := context.WithTimeout(ctx, g.c.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := g.c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxAnswer))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	switch {
	case res.StatusCode == http.StatusRequestEntityTooLarge:
		return nil, ErrTooLarge
	case res.StatusCode >= 400:
		// Only the provider's message is kept, and bounded.
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		msg := e.Error.Message
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return nil, fmt.Errorf("%w: groq answered %d %s", ErrUnavailable, res.StatusCode, msg)
	}
	return raw, nil
}

var (
	_ Transcriber = (*Groq)(nil)
	_ Synthesizer = (*Groq)(nil)
	_ Transcriber = (*Fake)(nil)
	_ Synthesizer = (*Fake)(nil)
)
