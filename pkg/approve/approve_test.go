package approve

import (
	"strings"
	"testing"
)

func TestTheAnswerIsOneOfThreeDecisionsWithAReason(t *testing.T) {
	for _, tc := range []struct {
		answer  string
		verdict Verdict
		refs    int
		err     bool
	}{
		{`{"decision": "allow", "reason": "The person asked for this file.", "references": ["policy: write_files asks", "tool: file_write notes.md", "ada: please write notes.md"]}`, Allow, 3, false},
		{"Here:\n```json\n{\"decision\": \"Deny\", \"reason\": \"It deletes the branch.\"}\n```", Deny, 0, false},
		{`{"decision": "ask", "reason": "It emails a customer.", "references": ["", "  spaced\n out  "]}`, Ask, 1, false},
		{`{"decision": "allow", "reason": "  "}`, "", 0, true},
		{`{"decision": "maybe", "reason": "?"}`, "", 0, true},
		{`{"reason": "forgot"}`, "", 0, true},
		{`yes`, "", 0, true},
	} {
		d, err := Parse(tc.answer)
		if (err != nil) != tc.err || d.Verdict != tc.verdict || len(d.References) != tc.refs {
			t.Errorf("%q: %+v, %v", tc.answer, d, err)
		}
	}
}

func TestWhatIsKeptOfAnAnswerIsBounded(t *testing.T) {
	long := strings.Repeat("why ", 200)
	refs := `"` + strings.Repeat("ref ", 100) + `"`
	all := refs
	for range 20 {
		all += "," + refs
	}
	d, err := Parse(`{"decision": "deny", "reason": "` + long + `", "references": [` + all + `]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Reason) > maxReason+4 || len(d.References) != maxReferences || len(d.References[0]) > maxReference+4 {
		t.Fatalf("kept %d bytes of reason and %d references", len(d.Reason), len(d.References))
	}
}

// The agent reads the call, the policy and the session as data, and the
// newest turns are kept when the session is long.
func TestTheCallIsDescribedWithItsPolicyAndNewestTurns(t *testing.T) {
	long := strings.Repeat("old words ", 200)
	session := []Turn{}
	for range 40 {
		session = append(session, Turn{Who: "ada", Text: long})
	}
	session = append(session, Turn{Who: "ada", Text: "please publish the release notes"})
	text := Describe(Call{
		Agent:         "writer",
		Tool:          "publish",
		Arguments:     []byte(`{"path":"notes.md"}`),
		ActsForPerson: true,
		Policy:        []string{"The workspace asks a person before publishing."},
		Session:       session,
	})
	for _, want := range []string{"tool: publish", `{"path":"notes.md"}`, "acts as: the person", "- The workspace asks a person before publishing.", "please publish the release notes", "(earlier messages left out)"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q", want)
		}
	}
	if len(text) > maxSession+3000 {
		t.Errorf("the description is %d bytes", len(text))
	}
	if !strings.Contains(Describe(Call{Agent: "writer", Tool: "publish"}), "(nothing yet)") {
		t.Error("an empty session is not said")
	}
}
