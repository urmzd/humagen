package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/speech"
)

// swappableVoice is a voice provider a test changes while the server runs.
type swappableVoice struct {
	mu   sync.Mutex
	fake *speech.Fake
}

func (v *swappableVoice) set(f *speech.Fake) {
	v.mu.Lock()
	v.fake = f
	v.mu.Unlock()
}

func (v *swappableVoice) build(string, speech.Config) (platform.Voice, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.fake, nil
}

// voiceSession starts a session with the helper and waits for its first
// answer, with the workspace's voice set up.
func voiceSession(t *testing.T, f *fixture, tok string) (id, run string) {
	t.Helper()
	if res, _ := rawCall(t, f, tok, "POST", "/api/agents", helperSpec); res != http.StatusCreated && res != http.StatusConflict {
		t.Fatalf("POST /api/agents = %d", res)
	}
	want(t, f, tok, "PUT", "/api/voice/provider", `{"kind":"groq","key":"gsk"}`, http.StatusOK)
	root := want(t, f, tok, "POST", "/api/sessions", `{"agent":"helper","project":"~","text":"Read me the plan"}`, http.StatusCreated)
	id = root["id"].(string)
	for _, m := range root["messages"].([]any) {
		if r, ok := m.(map[string]any)["run"].(map[string]any); ok {
			run = r["id"].(string)
		}
	}
	if run == "" {
		t.Fatal("no run")
	}
	waitRun(t, f, tok, run)
	return id, run
}

func waitRun(t *testing.T, f *fixture, tok, run string) {
	t.Helper()
	for range 400 {
		got := want(t, f, tok, "GET", "/api/runs/"+run, "", http.StatusOK)
		if s, _ := got["status"].(string); s == "completed" || s == "failed" || s == "stopped" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s did not end", run)
}

// A spoken turn: the words are heard, sent as a message marked spoken, and
// the answer is spoken in parts until done; failures say what happened.
func TestVoiceModeHearsAndSpeaksInASession(t *testing.T) {
	voice := &swappableVoice{fake: &speech.Fake{Text: "What is next?", Language: "english", Duration: 2 * time.Second}}
	f := newFixtureWith(t, platform.Config{VoiceBuild: voice.build})
	tok := f.person(t, "acme", "00u-ada", "Ada", "humagen-admins")
	id, run := voiceSession(t, f, tok)
	path := "/api/sessions/" + id + "/voice"

	res, heard := f.do(t, nil, "POST", path+"/transcriptions", tok, webmVoice, "Content-Type", "application/octet-stream")
	if res.StatusCode != http.StatusOK || heard["text"] != "What is next?" {
		t.Fatalf("heard = %d %v", res.StatusCode, heard)
	}
	posted := want(t, f, tok, "POST", "/api/sessions/"+id+"/messages", `{"text":"What is next?","spoken":true}`, http.StatusCreated)
	spoken := false
	for _, m := range posted["messages"].([]any) {
		if msg, ok := m.(map[string]any)["message"].(map[string]any); ok && msg["text"] == "What is next?" {
			spoken = msg["spoken"] == true
		}
	}
	if !spoken {
		t.Fatalf("the posted message is not marked spoken: %v", posted["messages"])
	}

	// The answer is spoken from 0 until done, each part a different offset.
	from, said := 0, []string{}
	for range 20 {
		got := want(t, f, tok, "POST", path+"/speech", fmt.Sprintf(`{"run":%q,"from":%d}`, run, from), http.StatusOK)
		next := int(got["next"].(float64))
		if text, _ := got["text"].(string); text != "" {
			said = append(said, text)
			if got["audio"] == "" || got["media_type"] != "audio/wav" {
				t.Fatalf("spoken without audio: %v", got)
			}
		}
		if got["done"] == true {
			break
		}
		if next <= from {
			t.Fatalf("speaking did not move on from %d: %v", from, got)
		}
		from = next
	}
	if len(said) == 0 || strings.Contains(strings.Join(said, " "), "**") {
		t.Fatalf("said %q", said)
	}

	// Another session's run is not this session's to speak.
	_, other := voiceSession(t, f, f.person(t, "acme", "00u-ben", "Ben", "humagen-admins"))
	if res, _ := rawCall(t, f, tok, "POST", path+"/speech", fmt.Sprintf(`{"run":%q,"from":0}`, other)); res != http.StatusNotFound {
		t.Fatalf("speaking another session's run = %d", res)
	}

	voice.set(&speech.Fake{Err: speech.ErrEmpty})
	if res, _ := f.do(t, nil, "POST", path+"/transcriptions", tok, webmVoice); res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("no words = %d", res.StatusCode)
	}
	voice.set(&speech.Fake{Err: speech.ErrUnavailable})
	if res, body := rawCall(t, f, tok, "POST", path+"/transcriptions", webmVoice); res != http.StatusServiceUnavailable || !strings.Contains(body, "did not answer") {
		t.Fatalf("provider down = %d %s", res, body)
	}
	if res, _ := rawCall(t, f, tok, "POST", path+"/speech", fmt.Sprintf(`{"run":%q,"from":0}`, run)); res != http.StatusServiceUnavailable {
		t.Fatalf("provider down while speaking = %d", res)
	}

	session := want(t, f, tok, "GET", "/api/sessions/"+id, "", http.StatusOK)
	var usage platform.VoiceUsage
	raw, _ := json.Marshal(session["voice"])
	if err := json.Unmarshal(raw, &usage); err != nil || usage.Calls < 2 || usage.HeardMS != 2000 || usage.SpokenChars == 0 || usage.Micros == 0 {
		t.Fatalf("voice usage = %+v %v", usage, err)
	}
}

// At a reached spend limit, voice mode sends nothing to the provider.
func TestVoiceModeStopsAtTheSpendLimit(t *testing.T) {
	fake := &speech.Fake{Text: "Hi", Duration: time.Second}
	f := newFixtureWith(t, platform.Config{VoiceBuild: (&swappableVoice{fake: fake}).build})
	tok := f.person(t, "acme", "00u-ada", "Ada", "humagen-admins")
	id, run := voiceSession(t, f, tok)
	want(t, f, tok, "PUT", "/api/policies", `{"scope":{"kind":"tenant"},"period":"day","spend_limit":50}`, http.StatusOK)
	path := "/api/sessions/" + id + "/voice"
	// The first call spends over the limit (Groq bills at least 10 seconds).
	if res, _ := f.do(t, nil, "POST", path+"/transcriptions", tok, webmVoice); res.StatusCode != http.StatusOK {
		t.Fatalf("first = %d", res.StatusCode)
	}
	for _, call := range []func() int{
		func() int { r, _ := f.do(t, nil, "POST", path+"/transcriptions", tok, webmVoice); return r.StatusCode },
		func() int {
			r, _ := rawCall(t, f, tok, "POST", path+"/speech", fmt.Sprintf(`{"run":%q,"from":0}`, run))
			return r
		},
	} {
		if got := call(); got != http.StatusPaymentRequired {
			t.Fatalf("over the limit = %d", got)
		}
	}
	if heard, _ := fake.Heard(); len(heard) != 1 {
		t.Fatalf("the provider heard %d recordings, want 1", len(heard))
	}
}

// Voice mode takes speech:write, on a session the caller can see, and is
// off until the workspace has a voice provider.
func TestVoiceModeTakesSpeechWriteAndAProvider(t *testing.T) {
	f := newFixtureWith(t, platform.Config{VoiceBuild: (&swappableVoice{fake: &speech.Fake{Text: "hi"}}).build})
	admin := f.person(t, "acme", "00u-ada", "Ada", "humagen-admins")
	id, run := voiceSession(t, f, admin)
	path := "/api/sessions/" + id + "/voice"
	viewer := f.person(t, "acme", "00u-vee", "Vee", "humagen-viewers")
	if res, _ := rawCall(t, f, viewer, "POST", path+"/speech", fmt.Sprintf(`{"run":%q}`, run)); res != http.StatusForbidden {
		t.Fatalf("a viewer = %d", res)
	}
	want(t, f, admin, "DELETE", "/api/voice/provider", "", http.StatusNoContent)
	if res, _ := rawCall(t, f, admin, "POST", path+"/speech", fmt.Sprintf(`{"run":%q}`, run)); res != http.StatusConflict {
		t.Fatalf("voice off = %d", res)
	}
}
