package approve

import (
	"encoding/json"
	"fmt"
	"slices"
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
		{` {"decision": "Deny", "reason": "It deletes the branch."} `, Deny, 0, false},
		{`{"decision": "ask", "reason": "It emails a customer.", "references": ["", "  spaced\n out  "]}`, Ask, 1, false},
		{`{"decision": "allow", "reason": "  "}`, "", 0, true},
		{`{"decision": "maybe", "reason": "?"}`, "", 0, true},
		{`{"reason": "forgot"}`, "", 0, true},
		{`yes`, "", 0, true},
		{``, "", 0, true},
		// Structured output is the object alone: prose around it is not read.
		{"Here:\n```json\n{\"decision\": \"deny\", \"reason\": \"x\"}\n```", "", 0, true},
		{`{"decision": "allow", "reason": "ok"} {"decision": "deny"}`, "", 0, true},
	} {
		d, err := Parse([]byte(tc.answer))
		if (err != nil) != tc.err || d.Verdict != tc.verdict || len(d.References) != tc.refs {
			t.Errorf("%q: %+v, %v", tc.answer, d, err)
		}
	}
}

// A workspace's own fields are kept in the order given, read as a person
// reads them; empty ones are left out.
func TestAWorkspacesOwnFieldsAreKept(t *testing.T) {
	d, err := Parse([]byte(`{"risk": "high", "decision": "ask", "conditions": ["only on main", 2], "reason": "It pushes.", "notify": [], "expires_in": 3600, "urgent": true, "extra": null, "scope": {"repo": "web"}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []Field{
		{Name: "risk", Label: "Risk", Value: "high"},
		{Name: "conditions", Label: "Conditions", Value: "only on main, 2"},
		{Name: "expires_in", Label: "Expires in", Value: "3600"},
		{Name: "urgent", Label: "Urgent", Value: "true"},
		{Name: "scope", Label: "Scope", Value: `{"repo":"web"}`},
	}
	if d.Verdict != Ask || d.Reason != "It pushes." || !slices.Equal(d.Fields, want) {
		t.Fatalf("fields = %+v", d.Fields)
	}
}

func TestWhatIsKeptOfAnAnswerIsBounded(t *testing.T) {
	long := strings.Repeat("why ", 200)
	refs := `"` + strings.Repeat("ref ", 100) + `"`
	all := refs
	for range 20 {
		all += "," + refs
	}
	extra := ""
	for i := range 30 {
		extra += fmt.Sprintf(`, "f%d": "%s"`, i, long)
	}
	d, err := Parse([]byte(`{"decision": "deny", "reason": "` + long + `", "references": [` + all + `]` + extra + `}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Reason) > maxReason+4 || len(d.References) != maxReferences || len(d.References[0]) > maxReference+4 {
		t.Fatalf("kept %d bytes of reason and %d references", len(d.Reason), len(d.References))
	}
	if len(d.Fields) != maxFields || len(d.Fields[0].Value) > maxFieldValue+4 {
		t.Fatalf("kept %d fields, the first %d bytes", len(d.Fields), len(d.Fields[0].Value))
	}
}

func TestTheDefaultSchemaHoldsTheCore(t *testing.T) {
	if err := CheckSchema(DefaultSchema); err != nil {
		t.Fatal(err)
	}
	d, err := Parse([]byte(`{"decision": "allow", "reason": "Asked for.", "references": ["policy"]}`))
	if err != nil || len(d.Fields) != 0 {
		t.Fatalf("%+v, %v", d, err)
	}
}

func TestASchemaMustHoldTheDecisionAndAReason(t *testing.T) {
	custom := `{"type":"object","required":["decision","reason","risk"],"properties":{"decision":{"type":"string","enum":["allow","ask"]},"reason":{"type":"string"},"risk":{"type":"string","enum":["low","medium","high"]},"notify":{"type":"array","items":{"type":"string"}}}}`
	if err := CheckSchema(json.RawMessage(custom)); err != nil {
		t.Fatalf("a workspace's schema with the core: %v", err)
	}
	for name, schema := range map[string]string{
		"not an object":     `[]`,
		"not object type":   `{"type":"string"}`,
		"no decision":       `{"type":"object","required":["reason"],"properties":{"reason":{"type":"string"}}}`,
		"decision optional": `{"type":"object","required":["reason"],"properties":{"decision":{"type":"string","enum":["allow"]},"reason":{"type":"string"}}}`,
		"no enum":           `{"type":"object","required":["decision","reason"],"properties":{"decision":{"type":"string"},"reason":{"type":"string"}}}`,
		"another verdict":   `{"type":"object","required":["decision","reason"],"properties":{"decision":{"type":"string","enum":["allow","escalate"]},"reason":{"type":"string"}}}`,
		"decision a number": `{"type":"object","required":["decision","reason"],"properties":{"decision":{"type":"integer","enum":["allow"]},"reason":{"type":"string"}}}`,
		"no reason":         `{"type":"object","required":["decision"],"properties":{"decision":{"type":"string","enum":["allow"]}}}`,
	} {
		if err := CheckSchema(json.RawMessage(schema)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestTheInstructionsLeaveTheShapeToTheSchema(t *testing.T) {
	if strings.Contains(Instructions, "JSON") || strings.Contains(Instructions, `"decision"`) {
		t.Fatal("the instructions still describe the answer's shape")
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
