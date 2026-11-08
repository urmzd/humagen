package join

import (
	"slices"
	"strings"
	"testing"
)

var staff = []Candidate{
	{Name: "ops", Prompt: "You run deployments and on-call. Explain why a deploy failed, read logs and roll back."},
	{Name: "analyst", Prompt: "You answer questions about cost, spend and usage from the warehouse."},
	{Name: "security-reviewer", Prompt: "You review code for vulnerabilities and secrets."},
	{Name: "writer", Prompt: "You draft documents and release notes."},
}

func names(ms []Match) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Name
	}
	return out
}

func TestSearchFindsAgentsByTheirPrompts(t *testing.T) {
	for _, tc := range []struct {
		name, message string
		want          []string
	}{
		{"one topic", "Why did last night's deploy fail?", []string{"ops"}},
		{"two topics, both found", "Why did the deploy fail, and what did it cost?", []string{"ops", "analyst"}},
		{"a name is a strong match", "security check please", []string{"security-reviewer"}},
		{"nothing in common finds nobody", "hello there", nil},
		{"stop words alone find nobody", "can you please do this for me", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := names(Search(staff, tc.message, Shortlist))
			slices.Sort(got)
			want := slices.Clone(tc.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("Search(%q) = %v, want %v", tc.message, got, want)
			}
		})
	}
}

func TestSearchRanksTheBestMatchFirstAndSaysWhy(t *testing.T) {
	ms := Search(staff, "roll back the failed deploy and read the logs", Shortlist)
	if len(ms) == 0 || ms[0].Name != "ops" {
		t.Fatalf("best match = %v, want ops first", names(ms))
	}
	if got := strings.Join(ms[0].Terms, ","); got != "roll,back,fail,deploy,read,log" && got != "roll,back,failed,deploy,read,log" {
		t.Fatalf("terms = %q, want the message's matching words in its order", got)
	}
}

func TestSearchUsesAsksAnAgentAnsweredWell(t *testing.T) {
	cs := slices.Clone(staff)
	if got := names(Search(cs, "summarise the quarterly board pack", Shortlist)); len(got) != 0 {
		t.Fatalf("before any praise, found %v", got)
	}
	cs[3].Asks = []string{"Summarise the quarterly board pack for the investors"}
	if got := names(Search(cs, "summarise the quarterly board pack", Shortlist)); !slices.Equal(got, []string{"writer"}) {
		t.Fatalf("after praise, found %v, want writer", got)
	}
}

func TestSearchReturnsAtMostKAndIsStable(t *testing.T) {
	var cs []Candidate
	for _, n := range []string{"a1", "a2", "a3", "a4", "a5"} {
		cs = append(cs, Candidate{Name: n, Prompt: "You help with billing invoices."})
	}
	first := names(Search(cs, "billing invoices", Shortlist))
	if !slices.Equal(first, []string{"a1", "a2", "a3"}) {
		t.Fatalf("equal scores = %v, want the first three in order", first)
	}
	for range 20 {
		if again := names(Search(cs, "billing invoices", Shortlist)); !slices.Equal(again, first) {
			t.Fatalf("search changed between calls: %v then %v", first, again)
		}
	}
	if got := Search(cs, "billing", 0); got != nil {
		t.Fatalf("k=0 = %v, want nothing", got)
	}
	if got := Search(nil, "billing", 3); got != nil {
		t.Fatalf("no candidates = %v, want nothing", got)
	}
}

func TestSearchBoundsWhatItReads(t *testing.T) {
	long := strings.Repeat("filler ", maxPrompt) + "zebra"
	cs := []Candidate{{Name: "a", Prompt: long}}
	if got := Search(cs, "zebra", 3); len(got) != 0 {
		t.Fatalf("a word past the prompt bound matched: %v", names(got))
	}
	var asks []string
	for range MaxAsks {
		asks = append(asks, "filler")
	}
	cs = []Candidate{{Name: "b", Asks: append(asks, "zebra")}}
	if got := Search(cs, "zebra", 3); len(got) != 0 {
		t.Fatalf("an ask past the ask bound matched: %v", names(got))
	}
}

func TestStopWordsNeverMatchEvenWithASuffix(t *testing.T) {
	cs := []Candidate{{Name: "a", Prompt: "this does was its"}}
	if got := Search(cs, "this does", 3); len(got) != 0 {
		t.Fatalf("stop words matched: %v", got[0].Terms)
	}
}
