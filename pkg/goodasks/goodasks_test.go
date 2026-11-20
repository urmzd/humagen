package goodasks

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func TestAsksAreKeptPerAgentNewestFirst(t *testing.T) {
	s := Open(nil)
	for i, a := range []Ask{
		{Agent: "ops", Run: "r1", Text: "why did the deploy fail", At: t0},
		{Agent: "ops", Run: "r2", Text: "roll back the release", At: t0.Add(time.Minute)},
		{Agent: "analyst", Run: "r3", Text: "what did it cost", At: t0},
	} {
		if err := s.Keep("acme", a); err != nil {
			t.Fatalf("keep %d: %v", i, err)
		}
	}
	all, err := s.All("acme")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(all["ops"], "|"); got != "roll back the release|why did the deploy fail" {
		t.Fatalf("ops = %q", got)
	}
	if got := strings.Join(all["analyst"], "|"); got != "what did it cost" {
		t.Fatalf("analyst = %q", got)
	}
	if other, _ := s.All("globex"); len(other) != 0 {
		t.Fatalf("another tenant read %v", other)
	}
}

func TestARatingTakenBackDropsItsAsk(t *testing.T) {
	s := Open(nil)
	_ = s.Keep("acme", Ask{Agent: "ops", Run: "r1", Text: "deploy", At: t0})
	_ = s.Keep("acme", Ask{Agent: "ops", Run: "r1", Text: "deploy again", At: t0})
	all, _ := s.All("acme")
	if len(all["ops"]) != 1 || all["ops"][0] != "deploy again" {
		t.Fatalf("one run keeps one ask, got %v", all["ops"])
	}
	if err := s.Forget("acme", "ops", "r1"); err != nil {
		t.Fatal(err)
	}
	if all, _ = s.All("acme"); len(all["ops"]) != 0 {
		t.Fatalf("forgotten ask still kept: %v", all["ops"])
	}
}

func TestAnAgentKeepsOnlyItsNewestAsks(t *testing.T) {
	s := Open(nil)
	for i := range MaxPerAgent + 5 {
		if err := s.Keep("acme", Ask{Agent: "ops", Run: fmt.Sprint("r", i), Text: fmt.Sprint("ask ", i), At: t0.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := s.All("acme")
	if len(all["ops"]) != MaxPerAgent {
		t.Fatalf("kept %d, want %d", len(all["ops"]), MaxPerAgent)
	}
	if all["ops"][0] != fmt.Sprint("ask ", MaxPerAgent+4) {
		t.Fatalf("newest = %q", all["ops"][0])
	}
}

func TestAnAskIsBoundedAndNeedsItsParts(t *testing.T) {
	s := Open(nil)
	if err := s.Keep("acme", Ask{Agent: "ops", Run: "r1", Text: "  "}); err == nil {
		t.Fatal("an empty ask was kept")
	}
	if err := s.Keep("acme", Ask{Agent: "ops", Run: "r1", Text: strings.Repeat("é", MaxText)}); err != nil {
		t.Fatal(err)
	}
	all, _ := s.All("acme")
	if got := all["ops"][0]; len(got) > MaxText || !strings.HasPrefix(got, "é") || strings.HasSuffix(got, "\xc3") {
		t.Fatalf("ask not clipped on a rune boundary: %d bytes", len(got))
	}
}
