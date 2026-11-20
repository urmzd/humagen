package spec

import (
	"strings"
	"testing"
)

func TestTheEntryAgentIsTheDeploymentsAndAnAgentName(t *testing.T) {
	for _, tc := range []struct {
		name   string
		b      Branding
		system bool
		want   string
	}{
		{"left out", Branding{}, true, ""},
		{"a kebab name", Branding{EntryAgent: "acme-desk"}, true, ""},
		{"not an agent name", Branding{EntryAgent: "Acme Desk"}, true, "not lowercase kebab case"},
		{"set by a workspace", Branding{EntryAgent: "acme"}, false, "set it in the System document"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.b.check(tc.system)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("check = %v, want %q", err, tc.want)
			}
		})
	}
}
