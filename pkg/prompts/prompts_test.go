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

func TestLayerLabelsAndEncodedSnapshotsAreBounded(t *testing.T) {
	for _, field := range []string{"kind", "from"} {
		for _, value := range []string{strings.Repeat("x", 600000), strings.Repeat("界", MaxLayerLabelChars+1), "invalid\xff", "nul\x00label"} {
			t.Run(field+"/"+strings.ReplaceAll(value[:min(len(value), 12)], "\x00", "nul"), func(t *testing.T) {
				kind, from := "project", "docs"
				if field == "kind" {
					kind = value
				} else {
					from = value
				}
				if _, err := Compose(Config{}, []Layer{NewLayer(kind, from, "small")}); !errors.Is(err, ErrInvalid) {
					t.Fatalf("invalid %s accepted: %v", field, err)
				}
			})
		}
	}
	// Text and labels can independently fit their character limits while JSON
	// escaping makes their aggregate exceed the persisted-reader byte limit.
	escaped := strings.Repeat("\x01", MaxLayerChars)
	projects := []Layer{NewLayer("project", "one", escaped), NewLayer("project", "two", escaped)}
	for len(projects) < 62 {
		label := strings.Repeat("\x01", MaxLayerLabelChars)
		projects = append(projects, NewLayer(label, label, ""))
	}
	if _, err := Compose(Config{Platform: escaped, Workspace: escaped}, projects); !errors.Is(err, ErrInvalid) {
		t.Fatalf("encoded snapshot overflow accepted: %v", err)
	}
	if _, err := Compose(Config{}, []Layer{NewLayer("project", "docs", "nul\x00text")}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("project NUL text accepted: %v", err)
	}
}

func TestBoundedSnapshotsAlwaysRoundtripIncludingEmptyWorkspaceOrigin(t *testing.T) {
	escaped := strings.Repeat("\x01", MaxLayerChars)
	label := strings.Repeat("界", MaxLayerLabelChars)
	s, err := Compose(Config{Platform: escaped, Workspace: escaped}, []Layer{
		NewLayer(label, label, escaped), NewLayer("workspace", "", escaped),
	})
	if err != nil {
		t.Fatal(err)
	}
	meta := map[string]string{}
	if err := s.Pin(meta); err != nil {
		t.Fatal(err)
	}
	got, found, err := Read(meta)
	if err != nil || !found || got.System("agent") != s.System("agent") {
		t.Fatalf("accepted snapshot failed roundtrip: found=%t error=%v", found, err)
	}
}
