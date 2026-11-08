package speech_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/speech"
)

func key(context.Context) (string, error) { return "gsk_test", nil }

func TestGroqTranscribesAVoiceNote(t *testing.T) {
	var got struct {
		path, auth, model, format, file, filename, ctype string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path, got.auth = r.URL.Path, r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		got.model, got.format = r.FormValue("model"), r.FormValue("response_format")
		f, h, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(f)
		got.file, got.filename, got.ctype = string(b), h.Filename, h.Header.Get("Content-Type")
		_ = json.NewEncoder(w).Encode(map[string]any{"text": " Book the room for Friday. ", "language": "English", "duration": 1.5})
	}))
	defer srv.Close()
	g, err := speech.NewGroq(speech.Config{BaseURL: srv.URL + "/", Key: key})
	if err != nil {
		t.Fatal(err)
	}
	text, lang, err := g.Transcribe(context.Background(), strings.NewReader("OggS..."), "audio/webm;codecs=opus")
	if err != nil {
		t.Fatal(err)
	}
	if text != "Book the room for Friday." || lang != "english" {
		t.Fatalf("text %q language %q", text, lang)
	}
	if got.path != "/audio/transcriptions" || got.auth != "Bearer gsk_test" || got.model != speech.GroqTranscriptionModel ||
		got.format != "verbose_json" || got.file != "OggS..." || got.filename != "voice.webm" || got.ctype != "audio/webm" {
		t.Fatalf("request = %+v", got)
	}
}

func TestGroqRefusesWhatItShouldNotSend(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer srv.Close()
	g, _ := speech.NewGroq(speech.Config{BaseURL: srv.URL, Key: key, MaxBytes: 4})
	ctx := context.Background()
	if _, _, err := g.Transcribe(ctx, strings.NewReader("12345"), "audio/ogg"); !errors.Is(err, speech.ErrTooLarge) {
		t.Fatalf("over the cap: %v", err)
	}
	if _, _, err := g.Transcribe(ctx, strings.NewReader("1"), "image/png"); !errors.Is(err, speech.ErrUnsupported) {
		t.Fatalf("not audio: %v", err)
	}
	if _, _, err := g.Transcribe(ctx, strings.NewReader(""), "audio/ogg"); !errors.Is(err, speech.ErrEmpty) {
		t.Fatalf("empty: %v", err)
	}
	if calls != 0 {
		t.Fatalf("%d requests sent", calls)
	}
	if _, err := speech.NewGroq(speech.Config{}); err == nil {
		t.Fatal("a groq kind with no key was built")
	}
}

func TestGroqIsBoundedInTime(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	g, _ := speech.NewGroq(speech.Config{BaseURL: srv.URL, Key: key, Timeout: 50 * time.Millisecond})
	start := time.Now()
	if _, _, err := g.Transcribe(context.Background(), strings.NewReader("x"), "audio/ogg"); !errors.Is(err, speech.ErrUnavailable) {
		t.Fatalf("a request past its time: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("the timeout did not bound the request")
	}
}

func TestGroqReportsAProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid API Key"}}`))
	}))
	defer srv.Close()
	g, _ := speech.NewGroq(speech.Config{BaseURL: srv.URL, Key: key})
	_, _, err := g.Transcribe(context.Background(), strings.NewReader("x"), "audio/mpeg")
	if !errors.Is(err, speech.ErrUnavailable) || !strings.Contains(err.Error(), "401 Invalid API Key") || strings.Contains(err.Error(), "gsk_test") {
		t.Fatalf("err = %v", err)
	}
}

func TestGroqSpeaks(t *testing.T) {
	var body map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/speech" {
			t.Errorf("path %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte("RIFF"))
	}))
	defer srv.Close()
	g, _ := speech.NewGroq(speech.Config{BaseURL: srv.URL, Key: key})
	audio, mt, err := g.Synthesize(context.Background(), "Done.")
	if err != nil || !bytes.Equal(audio, []byte("RIFF")) || mt != "audio/wav" {
		t.Fatalf("audio %q %q %v", audio, mt, err)
	}
	if body["model"] != speech.GroqSpeechModel || body["voice"] != speech.GroqVoice || body["input"] != "Done." || body["response_format"] != "wav" {
		t.Fatalf("body = %v", body)
	}
	if _, _, err := g.Synthesize(context.Background(), " "); !errors.Is(err, speech.ErrEmpty) {
		t.Fatalf("empty text: %v", err)
	}
}

func TestTheFakeRecordsWhatItHeard(t *testing.T) {
	f := &speech.Fake{Text: "hi", Language: "english", Max: 3}
	if text, lang, err := f.Transcribe(context.Background(), strings.NewReader("abc"), "audio/ogg"); err != nil || text != "hi" || lang != "english" {
		t.Fatalf("%q %q %v", text, lang, err)
	}
	if _, _, err := f.Transcribe(context.Background(), strings.NewReader("abcd"), "audio/ogg"); !errors.Is(err, speech.ErrTooLarge) {
		t.Fatalf("over the cap: %v", err)
	}
	heard, mimes := f.Heard()
	if len(heard) != 1 || string(heard[0]) != "abc" || mimes[0] != "audio/ogg" {
		t.Fatalf("heard %q %v", heard, mimes)
	}
}

func TestIsAudio(t *testing.T) {
	for mt, want := range map[string]bool{
		"audio/webm;codecs=opus": true, "AUDIO/OGG": true, "video/webm": true, "video/mp4": true,
		"image/png": false, "text/plain": false, "": false, "audio": false,
	} {
		if speech.IsAudio(mt) != want {
			t.Errorf("IsAudio(%q) = %v", mt, !want)
		}
	}
}

func TestGroqHearsHowLongTheAudioWas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"text": "Yes.", "language": "english", "duration": 1.2341})
	}))
	defer srv.Close()
	g, _ := speech.NewGroq(speech.Config{BaseURL: srv.URL, Key: key})
	h, err := g.Hear(context.Background(), strings.NewReader("x"), "audio/webm")
	if err != nil || h.Text != "Yes." || h.Duration != 1235*time.Millisecond {
		t.Fatalf("heard %+v %v", h, err)
	}
	if g.Kind() != speech.KindGroq {
		t.Fatalf("kind %q", g.Kind())
	}
}

func TestOpenAISpeaksMP3WithItsOwnDefaults(t *testing.T) {
	var body map[string]string
	var path, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte("ID3"))
	}))
	defer srv.Close()
	o, err := speech.New(speech.KindOpenAI, speech.Config{BaseURL: srv.URL, Key: key})
	if err != nil {
		t.Fatal(err)
	}
	audio, mt, err := o.Synthesize(context.Background(), "Hello.")
	if err != nil || string(audio) != "ID3" || mt != "audio/mpeg" {
		t.Fatalf("audio %q %q %v", audio, mt, err)
	}
	if path != "/audio/speech" || auth != "Bearer gsk_test" || body["model"] != speech.OpenAISpeechModel || body["voice"] != speech.OpenAIVoice || body["response_format"] != "mp3" {
		t.Fatalf("path %s body %v", path, body)
	}
	if hear, speak := o.Models(); hear != speech.OpenAITranscriptionModel || speak != speech.OpenAISpeechModel {
		t.Fatalf("models %q %q", hear, speak)
	}
	if _, err := speech.New("elevenlabs", speech.Config{Key: key}); err == nil {
		t.Fatal("an unknown kind was built")
	}
}

func TestAProviderAnsweringNoAudioIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	g, _ := speech.NewGroq(speech.Config{BaseURL: srv.URL, Key: key})
	if _, _, err := g.Synthesize(context.Background(), "Hi."); !errors.Is(err, speech.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestPriceRoundsUpAndAppliesTheMinimum(t *testing.T) {
	g := speech.GroqPrice
	// Ten seconds at $0.04 an hour is 111.1 micro-USD.
	if got := g.Hearing(2 * time.Second); got != 112 {
		t.Fatalf("2s under a 10s minimum = %d", got)
	}
	if got := g.Hearing(0); got != 112 {
		t.Fatalf("an unknown duration = %d", got)
	}
	if got := g.Hearing(time.Hour); got != 40_000 {
		t.Fatalf("an hour = %d", got)
	}
	if got := g.Hearing(48 * time.Hour); got != 40_000 {
		t.Fatalf("a duration past the cap = %d", got)
	}
	if got := speech.OpenAIPrice.Hearing(time.Minute); got != 6000 {
		t.Fatalf("a minute on OpenAI = %d", got)
	}
	// 45 characters at $22 a million is 990 micro-USD.
	if got := g.Speaking(45); got != 990 {
		t.Fatalf("45 chars = %d", got)
	}
	if got := g.Speaking(1); got != 22 {
		t.Fatalf("1 char = %d", got)
	}
	if got := (speech.Price{}).Speaking(100); got != 0 {
		t.Fatalf("unpriced = %d", got)
	}
	if !g.Valid() || (speech.Price{HearPerHour: -1}).Valid() || (speech.Price{SpeakPerMillion: speech.MaxPrice + 1}).Valid() || (speech.Price{MinSeconds: 7200}).Valid() {
		t.Fatal("Valid")
	}
}

func TestHearFallsBackToTranscribe(t *testing.T) {
	f := &speech.Fake{Text: "ok", Duration: 3 * time.Second}
	h, err := speech.Hear(context.Background(), f, strings.NewReader("a"), "audio/ogg")
	if err != nil || h.Text != "ok" || h.Duration != 3*time.Second {
		t.Fatalf("%+v %v", h, err)
	}
	h, err = speech.Hear(context.Background(), onlyTranscribes{}, strings.NewReader("a"), "audio/ogg")
	if err != nil || h.Text != "plain" || h.Duration != 0 {
		t.Fatalf("%+v %v", h, err)
	}
}

type onlyTranscribes struct{}

func (onlyTranscribes) Transcribe(context.Context, io.Reader, string) (string, string, error) {
	return "plain", "", nil
}
