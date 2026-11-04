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
	g, err := speech.NewGroq(speech.GroqConfig{BaseURL: srv.URL + "/", Key: key})
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
	g, _ := speech.NewGroq(speech.GroqConfig{BaseURL: srv.URL, Key: key, MaxBytes: 4})
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
	if _, err := speech.NewGroq(speech.GroqConfig{}); err == nil {
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
	g, _ := speech.NewGroq(speech.GroqConfig{BaseURL: srv.URL, Key: key, Timeout: 50 * time.Millisecond})
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
	g, _ := speech.NewGroq(speech.GroqConfig{BaseURL: srv.URL, Key: key})
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
	g, _ := speech.NewGroq(speech.GroqConfig{BaseURL: srv.URL, Key: key})
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
