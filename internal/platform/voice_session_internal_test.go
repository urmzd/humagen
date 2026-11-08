package platform

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// speakAll says a final answer part by part, as voice mode asks for it.
func speakAll(t *testing.T, answer string) []string {
	t.Helper()
	var out []string
	from := 0
	for range 100 {
		say, next, done := nextSpoken(answer, from, true)
		if say != "" {
			out = append(out, say)
		}
		if done {
			return out
		}
		if next <= from {
			t.Fatalf("stuck at %d of %q", from, answer)
		}
		from = next
	}
	t.Fatal("never done")
	return nil
}

func TestAnAnswerIsSpokenAsPlainWords(t *testing.T) {
	answer := "## Plan\n\nHere is **the plan** for [Friday](https://example.com/f).\n\n- Book the room.\n- Send the *invite*.\n\n```go\nfmt.Println(1)\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\nSee https://example.com for more. Done!"
	got := strings.Join(speakAll(t, answer), " ")
	want := "Plan Here is the plan for Friday. Book the room. Send the invite. The code is in the session. See a link for more. Done!"
	if got != want {
		t.Fatalf("said\n%q\nwant\n%q", got, want)
	}
}

func TestAStreamingAnswerWaitsForWholeSentences(t *testing.T) {
	// Half a sentence: nothing yet.
	if say, next, done := nextSpoken("The meeting is", 0, false); say != "" || next != 0 || done {
		t.Fatalf("half a sentence: %q %d %v", say, next, done)
	}
	// A whole sentence is said; the rest waits.
	answer := "The meeting is at ten. It runs"
	say, next, done := nextSpoken(answer, 0, false)
	if say != "The meeting is at ten." || done || !strings.HasPrefix(answer[next:], "It runs") {
		t.Fatalf("one sentence: %q %d %v", say, next, done)
	}
	// An open code block waits for its fence.
	if say, _, _ := nextSpoken("```go\nfmt", 0, false); say != "" {
		t.Fatalf("open code block: %q", say)
	}
	// The same answer, once final, is said to its end.
	say, next, done = nextSpoken(answer, next, true)
	if say != "It runs" || !done || next != len(answer) {
		t.Fatalf("final: %q %d %v", say, next, done)
	}
	// Nothing left of a final answer is done; of a running one, pending.
	if say, _, done := nextSpoken(answer, len(answer), true); say != "" || !done {
		t.Fatal("end of a final answer is not done")
	}
	if _, _, done := nextSpoken(answer, len(answer)+50, false); done {
		t.Fatal("an offset past a running answer is done")
	}
}

func TestOnePartIsBounded(t *testing.T) {
	long := strings.Repeat("word ", 300) + "end."
	parts := speakAll(t, long)
	for _, p := range parts {
		if n := utf8.RuneCountInString(p); n > maxSpoken {
			t.Fatalf("a part of %d runes", n)
		}
	}
	if len(parts) < 2 {
		t.Fatalf("a long sentence spoke in %d parts", len(parts))
	}
	// No word of it is lost between parts.
	if words := strings.Fields(strings.Join(parts, " ")); len(words) != 301 || words[300] != "end." {
		t.Fatalf("%d words said of 301", len(words))
	}
	// Short sentences are joined until there is enough to say.
	short := strings.Repeat("Yes. ", 60)
	first, _, _ := nextSpoken(short, 0, false)
	if n := utf8.RuneCountInString(first); n < enoughSpoken-1 || n > maxSpoken {
		t.Fatalf("first part of short sentences = %d runes", n)
	}
}

func TestAnAnswerClipIsBoundedAndSaysTheRestIsInTheThread(t *testing.T) {
	short, whole := answerParts("The room is booked. Invites are out.")
	if !whole || strings.Join(short, " ") != "The room is booked. Invites are out." {
		t.Fatalf("short: %q %v", short, whole)
	}
	long, whole := answerParts(strings.Repeat("This sentence is part of a long answer. ", 80))
	size := 0
	for _, p := range long {
		size += utf8.RuneCountInString(p)
	}
	if whole || long[len(long)-1] != RestInThread || size > MaxSpokenAnswer+utf8.RuneCountInString(RestInThread) {
		t.Fatalf("long: %d parts, %d runes, whole %v", len(long), size, whole)
	}
	if none, _ := answerParts("```\ncode only\n```"); len(none) != 1 || none[0] != CodeSpoken {
		t.Fatalf("code only: %q", none)
	}
	if empty, _ := answerParts(""); len(empty) != 0 {
		t.Fatalf("empty: %q", empty)
	}
}
