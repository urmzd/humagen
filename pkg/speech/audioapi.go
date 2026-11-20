package speech

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

// Kinds of speech provider, each an OpenAI-compatible audio API.
const (
	KindGroq   = "groq"
	KindOpenAI = "openai"
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
)

// OpenAI's defaults.
const (
	OpenAIBaseURL = "https://api.openai.com/v1"
	// OpenAITranscriptionModel answers verbose_json, which says how long
	// the audio was.
	OpenAITranscriptionModel = "whisper-1"
	OpenAISpeechModel        = "tts-1"
	OpenAIVoice              = "alloy"
	// OpenAIMaxBytes is the most audio OpenAI takes in one request.
	OpenAIMaxBytes int64 = 25 << 20
)

const (
	// MaxSpeechText bounds the text one Synthesize speaks.
	MaxSpeechText = 10000
	// maxAnswer bounds a provider's answer: a transcript or synthesized audio.
	maxAnswer = 32 << 20
)

// Prices published by each provider when these defaults were set, in
// micro-USD. A workspace can set its own.
var (
	// GroqPrice is $0.04 an hour of audio, billed at least 10 seconds a
	// request, and $22 a million characters spoken.
	GroqPrice = Price{HearPerHour: 40_000, MinSeconds: 10, SpeakPerMillion: 22_000_000}
	// OpenAIPrice is $0.006 a minute of audio and $15 a million characters
	// spoken.
	OpenAIPrice = Price{HearPerHour: 360_000, SpeakPerMillion: 15_000_000}
)

// Config is one provider. A zero field takes its kind's default.
type Config struct {
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

// GroqConfig is the Config of a Groq kind.
//
// Deprecated: use Config.
type GroqConfig = Config

// AudioAPI transcribes with POST /audio/transcriptions and speaks with POST
// /audio/speech on an OpenAI-compatible API: Groq's or OpenAI's.
type AudioAPI struct {
	kind   string
	format string
	c      Config
}

// NewGroq is Groq's audio API, with Groq's defaults filled in.
func NewGroq(c Config) (*AudioAPI, error) {
	c.BaseURL = cmp.Or(c.BaseURL, GroqBaseURL)
	c.Model = cmp.Or(c.Model, GroqTranscriptionModel)
	c.SpeechModel = cmp.Or(c.SpeechModel, GroqSpeechModel)
	c.Voice = cmp.Or(c.Voice, GroqVoice)
	if c.MaxBytes <= 0 {
		c.MaxBytes = GroqMaxBytes
	}
	// Orpheus answers WAV.
	return newAudioAPI(KindGroq, "wav", c)
}

// NewOpenAI is OpenAI's audio API, with OpenAI's defaults filled in.
func NewOpenAI(c Config) (*AudioAPI, error) {
	c.BaseURL = cmp.Or(c.BaseURL, OpenAIBaseURL)
	c.Model = cmp.Or(c.Model, OpenAITranscriptionModel)
	c.SpeechModel = cmp.Or(c.SpeechModel, OpenAISpeechModel)
	c.Voice = cmp.Or(c.Voice, OpenAIVoice)
	if c.MaxBytes <= 0 {
		c.MaxBytes = OpenAIMaxBytes
	}
	return newAudioAPI(KindOpenAI, "mp3", c)
}

// New is the kind named, groq or openai.
func New(kind string, c Config) (*AudioAPI, error) {
	switch kind {
	case KindGroq:
		return NewGroq(c)
	case KindOpenAI:
		return NewOpenAI(c)
	}
	return nil, fmt.Errorf("speech: no kind %q: groq or openai", kind)
}

// DefaultPrice is a kind's published price, or zero for a kind it does
// not know.
func DefaultPrice(kind string) Price {
	switch kind {
	case KindGroq:
		return GroqPrice
	case KindOpenAI:
		return OpenAIPrice
	}
	return Price{}
}

func newAudioAPI(kind, format string, c Config) (*AudioAPI, error) {
	if c.Key == nil {
		return nil, fmt.Errorf("speech: %s needs a key", kind)
	}
	c.BaseURL = strings.TrimSuffix(c.BaseURL, "/")
	if c.Timeout <= 0 {
		c.Timeout = GroqTimeout
	}
	if c.HTTP == nil {
		c.HTTP = http.DefaultClient
	}
	return &AudioAPI{kind: kind, format: format, c: c}, nil
}

// Kind is groq or openai.
func (a *AudioAPI) Kind() string { return a.kind }

// Models are the transcription and speech models it calls.
func (a *AudioAPI) Models() (hear, speak string) { return a.c.Model, a.c.SpeechModel }

// MaxBytes is the most audio one transcription sends.
func (a *AudioAPI) MaxBytes() int64 { return a.c.MaxBytes }

// extensions name the upload, since the endpoint reads the format from the
// file name.
var extensions = map[string]string{
	"audio/webm": "webm", "video/webm": "webm", "audio/ogg": "ogg", "audio/opus": "ogg",
	"audio/mpeg": "mp3", "audio/mp3": "mp3", "audio/mp4": "m4a", "audio/x-m4a": "m4a", "video/mp4": "mp4",
	"audio/wav": "wav", "audio/x-wav": "wav", "audio/wave": "wav", "audio/flac": "flac", "audio/x-flac": "flac",
}

// Transcribe is Hear without the duration.
func (a *AudioAPI) Transcribe(ctx context.Context, audio io.Reader, mimeType string) (string, string, error) {
	h, err := a.Hear(ctx, audio, mimeType)
	return h.Text, h.Language, err
}

// Hear sends the audio as multipart form data, asking for verbose_json,
// whose answer names the language and how long the audio was.
func (a *AudioAPI) Hear(ctx context.Context, audio io.Reader, mimeType string) (Heard, error) {
	mt := BaseType(mimeType)
	ext, ok := extensions[mt]
	if !ok {
		return Heard{}, fmt.Errorf("%w: %q", ErrUnsupported, mimeType)
	}
	b, err := ReadCapped(audio, a.c.MaxBytes)
	if err != nil {
		return Heard{}, err
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="voice.%s"`, ext))
	h.Set("Content-Type", mt)
	part, err := w.CreatePart(h)
	if err != nil {
		return Heard{}, err
	}
	if _, err := part.Write(b); err != nil {
		return Heard{}, err
	}
	for k, v := range map[string]string{"model": a.c.Model, "response_format": "verbose_json", "temperature": "0"} {
		if err := w.WriteField(k, v); err != nil {
			return Heard{}, err
		}
	}
	if err := w.Close(); err != nil {
		return Heard{}, err
	}
	raw, err := a.post(ctx, "/audio/transcriptions", w.FormDataContentType(), &body)
	if err != nil {
		return Heard{}, err
	}
	var out struct {
		Text     string  `json:"text"`
		Language string  `json:"language"`
		Duration float64 `json:"duration"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Heard{}, fmt.Errorf("%w: an answer that is not a transcript", ErrUnavailable)
	}
	text := strings.TrimSpace(out.Text)
	if text == "" {
		return Heard{}, ErrEmpty
	}
	heard := Heard{Text: text, Language: strings.ToLower(strings.TrimSpace(out.Language))}
	if out.Duration > 0 && out.Duration < float64(MaxHeard/time.Second) {
		heard.Duration = time.Duration(math.Ceil(out.Duration*1000)) * time.Millisecond
	}
	return heard, nil
}

// Synthesize speaks text with the configured voice: WAV from Groq, MP3
// from OpenAI.
func (a *AudioAPI) Synthesize(ctx context.Context, text string) ([]byte, string, error) {
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		return nil, "", ErrEmpty
	case len(text) > MaxSpeechText:
		return nil, "", fmt.Errorf("%w: at most %d bytes of text", ErrTooLarge, MaxSpeechText)
	}
	req, err := json.Marshal(map[string]string{"model": a.c.SpeechModel, "voice": a.c.Voice, "input": text, "response_format": a.format})
	if err != nil {
		return nil, "", err
	}
	audio, err := a.post(ctx, "/audio/speech", "application/json", bytes.NewReader(req))
	if err != nil {
		return nil, "", err
	}
	if len(audio) == 0 {
		return nil, "", fmt.Errorf("%w: no audio in the answer", ErrUnavailable)
	}
	if a.format == "mp3" {
		return audio, "audio/mpeg", nil
	}
	return audio, "audio/wav", nil
}

// post sends one bounded request and reads a bounded answer.
func (a *AudioAPI) post(ctx context.Context, path, contentType string, body io.Reader) ([]byte, error) {
	key, err := a.c.Key(ctx)
	if err != nil || key == "" {
		return nil, fmt.Errorf("%w: the key is not set", ErrUnavailable)
	}
	ctx, cancel := context.WithTimeout(ctx, a.c.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := a.c.HTTP.Do(req)
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
		return nil, fmt.Errorf("%w: %s answered %d %s", ErrUnavailable, a.kind, res.StatusCode, msg)
	}
	return raw, nil
}

var (
	_ Hearer      = (*AudioAPI)(nil)
	_ Synthesizer = (*AudioAPI)(nil)
	_ Hearer      = (*Fake)(nil)
	_ Synthesizer = (*Fake)(nil)
)
