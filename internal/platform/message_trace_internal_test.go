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
