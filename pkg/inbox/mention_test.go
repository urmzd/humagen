package inbox

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/state"
)

func TestAnExcerptIsAtMostThreeHundredCharacters(t *testing.T) {
	if got := Excerpt("  short  "); got != "short" {
		t.Fatalf("Excerpt = %q", got)
	}
	long := strings.Repeat("é", MaxExcerpt+50)
	got := Excerpt(long)
	if n := utf8.RuneCountInString(got); n != MaxExcerpt || !strings.HasSuffix(got, "…") {
		t.Fatalf("an excerpt of %d characters ending %q", n, got[len(got)-6:])
	}
	if got := Excerpt(strings.Repeat("x", MaxExcerpt)); utf8.RuneCountInString(got) != MaxExcerpt || strings.HasSuffix(got, "…") {
		t.Fatal("a message that fits was cut")
	}
}

func TestAMentionKeepsItsPartAcrossARestart(t *testing.T) {
	st := state.NewMemory()
	s, err := OpenStore(st)
	if err != nil {
		t.Fatal(err)
	}
	part := MentionPart{Thread: "th-0123456789abcdef", Message: "m-2", Project: "", Channel: "general", By: "ada", Excerpt: "@opal look"}
	m, raised, err := s.Raise(acme, MentionKey(part.Thread, part.Message, "opal"), Message{
		Kind: KindMention, To: "opal", Parts: []Part{{Type: PartMention, Mention: &part}},
	}, t0)
	if err != nil || !raised {
		t.Fatal(err)
	}
	// What a reader is handed is a copy.
	m.Parts[0].Mention.Excerpt = "changed"
	again, err := OpenStore(st)
	if err != nil {
		t.Fatal(err)
	}
	got, err := again.Get(acme, m.ID, "opal")
	if err != nil || got.Parts[0].Mention == nil || *got.Parts[0].Mention != part || got.Kind != KindMention {
		t.Fatalf("after restart = %+v, %v", got, err)
	}
	if mine, _ := s.Get(acme, m.ID, "opal"); mine.Parts[0].Mention.Excerpt != part.Excerpt {
		t.Fatal("changing a message handed out changed the inbox")
	}
}
