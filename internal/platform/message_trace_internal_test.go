package platform

import (
	"encoding/json"
	"slices"
	"testing"
)

// A context read is what get_context answered: the place it read, the start
// of the text, and the sources it drew on, each as a title and a link.
func TestAContextReadIsReadFromWhatGetContextAnswered(t *testing.T) {
	out, _ := json.Marshal(map[string]any{
		"origin": "slack://T01/C01/1700000000.000100", "text": "They agreed to ship on Friday.",
		"sources": []map[string]string{{"title": "Release thread", "url": "https://slack.example/release"}, {"url": "https://slack.example/only-a-link"}, {}},
	})
	read := (&tracer{}).contextRead(&traceCall{id: "c1", input: json.RawMessage(`{"request":"what they said about the release"}`), output: string(out)})
	want := []TraceSource{{Title: "Release thread", URL: "https://slack.example/release"}, {Title: "https://slack.example/only-a-link", URL: "https://slack.example/only-a-link"}}
	if read.Request != "what they said about the release" || read.Reference != "slack://T01/C01/1700000000.000100" || read.Summary != "They agreed to ship on Friday." || !slices.Equal(read.Sources, want) || read.Truncated || read.Withheld {
		t.Fatalf("the context read = %+v", read)
	}
	if held := (&tracer{}).contextRead(&traceCall{id: "c2", withheld: true, output: string(out)}); held.Summary != "" || len(held.Sources) != 0 || !held.Withheld {
		t.Fatalf("a withheld read showed %+v", held)
	}
}

func TestAToolUseIDReadsBackIntoItsRunAndNumber(t *testing.T) {
	for id, want := range map[string]struct {
		run string
		n   int
		ok  bool
	}{
		toolUseID("r0123abc", 7): {"r0123abc", 7, true},
		"htu_r_entry_2":          {"r_entry", 2, true},
		"htu_r1_0":               {"", 0, false},
		"htu_r1_01":              {"", 0, false},
		"htu__3":                 {"", 0, false},
		"toolu_01ABC":            {"", 0, false},
		"htu_r1":                 {"", 0, false},
	} {
		run, n, ok := parseToolUseID(id)
		if run != want.run || n != want.n || ok != want.ok {
			t.Fatalf("parseToolUseID(%q) = %q, %d, %v; want %+v", id, run, n, ok, want)
		}
	}
}
