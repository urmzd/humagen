package spec

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The speech slot is off by default, groq needs nothing but its kind, and a
// block that could not be built is refused when the file is read.
func TestTheSpeechSlotIsCheckedWhenRead(t *testing.T) {
	var kinds Kinds
	for _, ok := range []Slot{{}, {Kind: "none"}, {Kind: "groq"}, {Kind: "groq", Block: MustJSON(GroqSpeech{Timeout: "2m", MaxBytes: 1 << 20})}} {
		if err := kinds.check(SlotSpeech, ok); err != nil {
			t.Errorf("%+v: %v", ok, err)
		} else if err := checkSpeech(ok); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
	for want, bad := range map[string]GroqSpeech{
		"timeout":   {Timeout: "soon"},
		"10m0s":     {Timeout: "1h"},
		"max_bytes": {MaxBytes: MaxSpeechBytes + 1},
		"base_url":  {BaseURL: "ftp://example.com"},
		"use key":   {BaseURL: "https://user:pass@api.groq.com/openai/v1"},
	} {
		err := checkSpeech(Slot{Kind: "groq", Block: MustJSON(bad)})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%+v: %v, want an error naming %q", bad, err, want)
		}
	}
	if err := kinds.check(SlotSpeech, Slot{Kind: "groq", Block: []byte(`{"key":"gsk_plain"}`)}); err == nil {
		t.Error("a key given as a value was accepted")
	}
}

// A groq model needs no block: Groq's base URL and GROQ_API_KEY fill it.
func TestAGroqModelNeedsOnlyItsKind(t *testing.T) {
	if err := checkModel("groq", ModelBlock{}); err != nil {
		t.Fatal(err)
	}
	if err := checkModel("groq", ModelBlock{Vertex: &Vertex{Project: "p", Location: "l"}}); err == nil {
		t.Fatal("vertex on groq was accepted")
	}
	if err := checkModel("openai", ModelBlock{}); err == nil {
		t.Fatal("a hosted openai with no key was accepted")
	}
}

// The voice guide's examples load and pass the strict check.
func TestTheVoiceGuideExamplesLoad(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "guides", "voice.md"))
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile("(?s)```yaml\n(.*?)```").FindAllStringSubmatch(string(doc), -1)
	if len(blocks) != 2 {
		t.Fatalf("found %d yaml examples", len(blocks))
	}
	path := filepath.Join(t.TempDir(), "humagen.yaml")
	writeFile(t, path, blocks[0][1]+"---\n"+blocks[1][1])
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Check(Kinds{}); err != nil {
		t.Fatal(err)
	}
	if cfg.System.Spec.Speech.Kind != "groq" || cfg.Platforms[0].Spec.Models[0].Kind != "groq" {
		t.Fatalf("config = %+v", cfg)
	}
}
