package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/speech"
)

// A browser's recording, as its leading bytes show: a WebM with an Opus track.
const webmVoice = "\x1a\x45\xdf\xa3....webm....A_OPUS...."

func TestTheConsoleTranscribesARecording(t *testing.T) {
	fake := &speech.Fake{Text: "Ship it on Friday.", Language: "english"}
	f := newFixtureWith(t, platform.Config{Speech: fake})
	tok := f.machine(t, "acme", "humagen:speech:write")

	out := want(t, f, tok, "GET", "/api/voice", "", http.StatusOK)
	if out["transcribe"] != true || out["max_bytes"] != float64(platform.MaxRecordingBytes) {
		t.Fatalf("voice settings = %v", out)
	}
	res, got := f.do(t, nil, "POST", "/api/voice/transcriptions", tok, webmVoice, "Content-Type", "application/octet-stream")
	if res.StatusCode != http.StatusOK || got["text"] != "Ship it on Friday." || got["language"] != "english" {
		t.Fatalf("transcription = %d %v", res.StatusCode, got)
	}
	// The type is sniffed from the bytes, never taken from the sender.
	if _, mimes := fake.Heard(); len(mimes) != 1 || mimes[0] != "audio/webm" {
		t.Fatalf("heard as %v", mimes)
	}
	if res, _ := f.do(t, nil, "POST", "/api/voice/transcriptions", tok, "<html>not audio</html>", "Content-Type", "audio/webm"); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("not audio = %d", res.StatusCode)
	}
	big := "OggS" + strings.Repeat("x", platform.MaxRecordingBytes)
	if res, _ := f.do(t, nil, "POST", "/api/voice/transcriptions", tok, big); res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("over the cap = %d", res.StatusCode)
	}
	if _, mimes := fake.Heard(); len(mimes) != 1 {
		t.Fatalf("%d recordings reached the transcriber, want 1", len(mimes))
	}
}

// The routes take speech:write, which runs:write does not grant.
func TestTranscribingTakesSpeechWrite(t *testing.T) {
	f := newFixtureWith(t, platform.Config{Speech: &speech.Fake{Text: "hi"}})
	tok := f.machine(t, "acme", "humagen:runs:write", "humagen:runs:read")
	for _, r := range [][2]string{{"GET", "/api/voice"}, {"POST", "/api/voice/transcriptions"}} {
		if res, _ := f.do(t, nil, r[0], r[1], tok, webmVoice); res.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s without speech:write = %d", r[0], r[1], res.StatusCode)
		}
	}
	viewer := f.signIn(t, "humagen-viewers")
	if res, _ := f.do(t, viewer, "POST", "/api/voice/transcriptions", "", webmVoice); res.StatusCode != http.StatusForbidden {
		t.Errorf("a viewer transcribed: %d", res.StatusCode)
	}
	operator := f.signIn(t, "humagen-operators")
	if res, out := f.do(t, operator, "GET", "/api/voice", "", ""); res.StatusCode != http.StatusOK || out["transcribe"] != true {
		t.Errorf("an operator reading voice settings = %d %v", res.StatusCode, out)
	}
}

// With speech left out of the spec, the console is told voice notes are off.
func TestWithoutSpeechTheConsoleHasNoVoiceNotes(t *testing.T) {
	f := newFixture(t)
	tok := f.machine(t, "acme", "humagen:speech:write")
	if out := want(t, f, tok, "GET", "/api/voice", "", http.StatusOK); out["transcribe"] != false {
		t.Fatalf("voice settings = %v", out)
	}
	if res, _ := f.do(t, nil, "POST", "/api/voice/transcriptions", tok, webmVoice); res.StatusCode != http.StatusNotFound {
		t.Fatalf("transcribing with speech off = %d", res.StatusCode)
	}
}
