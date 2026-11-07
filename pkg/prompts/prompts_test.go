package prompts

import (
	"errors"
	"strings"
	"testing"
)

func TestSharedPrefixIsPinnedAndTamperingFailsClosed(t *testing.T) {
	c := Config{Workspace: "Use Acme terminology."}
	s, err := Compose(c, []Layer{NewLayer("project", "docs", "Prefer examples.")})
	if err != nil {
		t.Fatal(err)
	}
	meta := map[string]string{}
	if err := s.Pin(meta); err != nil {
		t.Fatal(err)
	}
	c.Workspace = "Changed while a run was waiting."
	stored, ok, err := Read(meta)
	if err != nil || !ok || stored.System("Write docs.") != s.System("Write docs.") {
		t.Fatalf("pin changed: %+v %v", stored, err)
	}
	if !strings.HasPrefix(stored.System("Write docs."), Platform+"\n\nUse Acme terminology.\n\nPrefer examples.") {
		t.Fatal("unstable prefix order")
	}
	meta[MetaSnapshot] = strings.Replace(meta[MetaSnapshot], "Acme", "Evil", 1)
	if _, _, err := Read(meta); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tampered layer accepted: %v", err)
	}
}

func TestSharedInstructionsAreBoundedAndUnicodeAware(t *testing.T) {
	if _, err := Compose(Config{Workspace: strings.Repeat("界", MaxLayerChars)}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Compose(Config{Workspace: strings.Repeat("界", MaxLayerChars+1)}, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversize accepted: %v", err)
	}
	if _, err := Compose(Config{SlackWords: -1}, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative word budget: %v", err)
	}
	l := NewLayer("project", "p", "unchanged")
	if l.Version != NewLayer("project", "p", " unchanged ").Version || l.Version == NewLayer("project", "q", "unchanged").Version {
		t.Fatal("version does not identify layer content and origin")
	}
}
