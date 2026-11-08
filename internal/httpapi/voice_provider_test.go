package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/speech"
)

// voiceBuilds records each voice provider the platform builds, and the key
// it would send.
type voiceBuilds struct {
	mu    sync.Mutex
	kinds []string
	keys  []func(context.Context) (string, error)
	fake  *speech.Fake
}

func (b *voiceBuilds) build(kind string, c speech.Config) (platform.Voice, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.kinds = append(b.kinds, kind)
	b.keys = append(b.keys, c.Key)
	return b.fake, nil
}

func (b *voiceBuilds) lastKey(t *testing.T) string {
	t.Helper()
	b.mu.Lock()
	key := b.keys[len(b.keys)-1]
	b.mu.Unlock()
	k, err := key(context.Background())
	if err != nil {
		return "error: " + err.Error()
	}
	return k
}

// An admin sets a workspace's voice up: voice mode turns on, a recording is
// heard by the workspace's provider and counted, and the key is never shown.
func TestAnAdminSetsUpTheWorkspaceVoice(t *testing.T) {
	deployment := &speech.Fake{Text: "from the deployment"}
	builds := &voiceBuilds{fake: &speech.Fake{Text: "Book the room.", Language: "english", Duration: 90 * time.Second}}
	f := newFixtureWith(t, platform.Config{Speech: deployment, VoiceBuild: builds.build})
	admin := f.person(t, "acme", "00u-ada", "Ada", "humagen-admins")

	before := want(t, f, admin, "GET", "/api/voice", "", http.StatusOK)
	if before["transcribe"] != true || before["speak"] != false {
		t.Fatalf("before = %v: the deployment hears, nothing speaks", before)
	}
	view := want(t, f, admin, "GET", "/api/voice/provider", "", http.StatusOK)
	if _, has := view["provider"]; has || len(view["kinds"].([]any)) != 2 {
		t.Fatalf("view = %v", view)
	}

	set := want(t, f, admin, "PUT", "/api/voice/provider", `{"kind":"groq","key":"gsk_secret","voice":"diana"}`, http.StatusOK)
	if set["key_stored"] != true || set["model"] != speech.GroqTranscriptionModel || set["voice"] != "diana" || set["set_by"] == "" {
		t.Fatalf("set = %v", set)
	}
	price := set["price"].(map[string]any)
	if price["hear_per_hour"] != float64(speech.GroqPrice.HearPerHour) || price["min_seconds"] != float64(10) {
		t.Fatalf("price = %v", price)
	}
	for _, path := range []string{"/api/voice/provider", "/api/settings"} {
		res, body := rawCall(t, f, admin, "GET", path, "")
		if res != http.StatusOK || strings.Contains(body, "gsk_secret") {
			t.Fatalf("GET %s = %d, shows the key: %s", path, res, body)
		}
		if path == "/api/settings" && strings.Contains(body, "voice_provider") {
			t.Fatalf("settings show the voice provider: %s", body)
		}
	}
	// Saving the workspace's settings leaves the voice provider alone.
	settings := want(t, f, admin, "GET", "/api/settings", "", http.StatusOK)
	want(t, f, admin, "PUT", "/api/settings", mustJSON(t, settings), http.StatusOK)

	after := want(t, f, admin, "GET", "/api/voice", "", http.StatusOK)
	if after["speak"] != true || after["provider"] != "groq" {
		t.Fatalf("after = %v", after)
	}
	res, got := f.do(t, nil, "POST", "/api/voice/transcriptions", admin, webmVoice, "Content-Type", "application/octet-stream")
	if res.StatusCode != http.StatusOK || got["text"] != "Book the room." {
		t.Fatalf("transcription = %d %v", res.StatusCode, got)
	}
	if heard, _ := deployment.Heard(); len(heard) != 0 {
		t.Fatal("the deployment's speech kind heard a workspace recording")
	}
	if k := builds.lastKey(t); k != "gsk_secret" {
		t.Fatalf("the provider was built with key %q", k)
	}
	// 90 seconds at $0.04 an hour is 1,000 micro-USD.
	if u, err := f.p.Usage.Usage("acme", policy.Scope{Kind: policy.Tenant}, time.Now().Add(-time.Hour)); err != nil || u.Spend != 1000 {
		t.Fatalf("usage = %+v %v", u, err)
	}

	check := want(t, f, admin, "POST", "/api/voice/provider/check", "", http.StatusOK)
	if check["heard"] != "Book the room." || check["media_type"] != "audio/wav" || check["audio"] == "" {
		t.Fatalf("check = %v", check)
	}

	// Same kind with no key keeps the key; another kind needs its own.
	want(t, f, admin, "PUT", "/api/voice/provider", `{"kind":"groq","voice":"troy"}`, http.StatusOK)
	if k := builds.lastKeyAfterUse(t, f, admin); k != "gsk_secret" {
		t.Fatalf("a replaced provider's key = %q", k)
	}
	if res, body := rawCall(t, f, admin, "PUT", "/api/voice/provider", `{"kind":"openai"}`); res != http.StatusBadRequest || !strings.Contains(body, "key") {
		t.Fatalf("a new kind with no key = %d %s", res, body)
	}

	want(t, f, admin, "DELETE", "/api/voice/provider", "", http.StatusNoContent)
	off := want(t, f, admin, "GET", "/api/voice", "", http.StatusOK)
	if off["speak"] != false || off["transcribe"] != true {
		t.Fatalf("off = %v: voice mode stops, the deployment still hears", off)
	}
	if res, _ := f.do(t, nil, "POST", "/api/voice/provider/check", admin, ""); res.StatusCode != http.StatusNotFound {
		t.Fatalf("a check with voice off = %d", res.StatusCode)
	}
}

// lastKeyAfterUse builds the provider again, by reading the voice settings,
// and reads the key it was built with.
func (b *voiceBuilds) lastKeyAfterUse(t *testing.T, f *fixture, who string) string {
	t.Helper()
	want(t, f, who, "GET", "/api/voice", "", http.StatusOK)
	return b.lastKey(t)
}

// A key can be an env reference, but only one this server grants the
// workspace; a refusal does not say why.
func TestAVoiceKeyReferenceMustBeGranted(t *testing.T) {
	builds := &voiceBuilds{fake: &speech.Fake{Text: "hi"}}
	env := map[string]string{"HUMAGEN_GROQ_KEY": "gsk_from_env", "HUMAGEN_OTHER": "nope"}
	f := newFixtureWith(t, platform.Config{
		VoiceBuild: builds.build,
		Env:        func(k string) (string, bool) { v, ok := env[k]; return v, ok },
		SecretRefs: map[string][]string{"acme": {"HUMAGEN_GROQ_KEY"}},
	})
	admin := f.person(t, "acme", "00u-ada", "Ada", "humagen-admins")
	for _, ref := range []string{"env:HUMAGEN_OTHER", "env:HUMAGEN_MISSING", "op://vault/key"} {
		res, body := rawCall(t, f, admin, "PUT", "/api/voice/provider", `{"kind":"groq","key_ref":"`+ref+`"}`)
		if res != http.StatusBadRequest || !strings.Contains(body, "cannot be used by this workspace") {
			t.Fatalf("%s = %d %s", ref, res, body)
		}
	}
	if res, _ := rawCall(t, f, admin, "PUT", "/api/voice/provider", `{"kind":"groq","key":"a","key_ref":"env:HUMAGEN_GROQ_KEY"}`); res != http.StatusBadRequest {
		t.Fatalf("a key and a reference = %d", res)
	}
	set := want(t, f, admin, "PUT", "/api/voice/provider", `{"kind":"groq","key_ref":"env:HUMAGEN_GROQ_KEY"}`, http.StatusOK)
	if set["key_ref"] != "env:HUMAGEN_GROQ_KEY" || set["key_stored"] != false {
		t.Fatalf("set = %v", set)
	}
	if k := builds.lastKeyAfterUse(t, f, admin); k != "gsk_from_env" {
		t.Fatalf("key = %q", k)
	}
	if res, _ := rawCall(t, f, admin, "PUT", "/api/voice/provider", `{"kind":"groq","key":"k","price":{"hear_per_hour":-1,"min_seconds":0,"speak_per_million_chars":0}}`); res != http.StatusBadRequest {
		t.Fatalf("a negative price = %d", res)
	}
	if res, _ := rawCall(t, f, admin, "PUT", "/api/voice/provider", `{"kind":"groq","key":"k","voice":"<script>"}`); res != http.StatusBadRequest {
		t.Fatalf("a voice that is not a name = %d", res)
	}
}

// Setting the voice up takes clients:write: an operator records with it,
// but does not choose it or see its key reference.
func TestTheVoiceProviderTakesClientsWrite(t *testing.T) {
	f := newFixtureWith(t, platform.Config{VoiceBuild: (&voiceBuilds{fake: &speech.Fake{Text: "hi"}}).build})
	for _, who := range []string{"humagen-operators", "humagen-viewers"} {
		c := f.person(t, "acme", "00u-"+who, who, who)
		for _, r := range [][3]string{
			{"GET", "/api/voice/provider", ""}, {"PUT", "/api/voice/provider", `{"kind":"groq","key":"k"}`},
			{"DELETE", "/api/voice/provider", ""}, {"POST", "/api/voice/provider/check", ""},
		} {
			if res, _ := rawCall(t, f, c, r[0], r[1], r[2]); res != http.StatusForbidden {
				t.Errorf("%s: %s %s = %d", who, r[0], r[1], res)
			}
		}
	}
	admin := f.person(t, "acme", "00u-ada", "Ada", "humagen-admins")
	want(t, f, admin, "PUT", "/api/voice/provider", `{"kind":"openai","key":"sk"}`, http.StatusOK)
	operator := f.person(t, "acme", "00u-humagen-operators", "Op", "humagen-operators")
	if out := want(t, f, operator, "GET", "/api/voice", "", http.StatusOK); out["speak"] != true || out["provider"] != "openai" {
		t.Fatalf("an operator's voice = %v", out)
	}
}

// rawCall is one request with a bearer token, answering its status and body.
func rawCall(t *testing.T, f *fixture, tok, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, f.srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}
