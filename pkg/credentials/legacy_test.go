package credentials_test

import (
	"testing"

	"github.com/urmzd/humagen/pkg/credentials"
)

// An env: start under the old WARP_ prefix is the same start under HUMAGEN_:
// both read one variable (legacy.Lookup). So it is checked the same way,
// owns the same references whichever prefix they were written with, and
// cannot be given to two workspaces under two spellings.
func TestAnEnvStartMeansOneVariableUnderEitherPrefix(t *testing.T) {
	for _, start := range []string{"env:WARP_PERSON_{subject}_", "env:HUMAGEN_PERSON_{subject}_"} {
		if err := credentials.CheckStart(start); err != nil {
			t.Errorf("%s: %v", start, err)
		}
	}
	for _, start := range []string{
		"env:WARP_{subject}_", "env:HUMAGEN_{subject}_", // the operator's own variables
		"env:WARP_OIDC_{subject}_", "env:HUMAGEN_OIDC{subject}_", // the sign-in secret
		"env:OTHER_{subject}_",
	} {
		if err := credentials.CheckStart(start); err == nil {
			t.Errorf("%s was accepted", start)
		}
	}
	for _, c := range []struct {
		start, ref string
		owns       bool
	}{
		{"env:WARP_PERSON_{subject}_", "env:WARP_PERSON_ANA_ISSUES", true},
		{"env:WARP_PERSON_{subject}_", "env:HUMAGEN_PERSON_ANA_ISSUES", true},
		{"env:HUMAGEN_PERSON_{subject}_", "env:WARP_PERSON_ANA_ISSUES", true},
		{"env:HUMAGEN_PERSON_{subject}_", "env:WARP_PERSON_BEN_ISSUES", false},
		{"keychain:warp-people-{subject}/", "keychain:warp-people-ana/issues", true},
		{"keychain:warp-people-{subject}/", "keychain:humagen-people-ana/issues", false},
	} {
		if got := credentials.Owns(c.start, "ana", c.ref); got != c.owns {
			t.Errorf("Owns(%s, ana, %s) = %v", c.start, c.ref, got)
		}
	}
	err := credentials.CheckStarts(map[string][]string{
		"acme":   {"env:WARP_PERSON_{subject}_"},
		"globex": {"env:HUMAGEN_PERSON_{subject}_"},
	})
	if err == nil {
		t.Error("two workspaces were given one variable under two spellings")
	}
	if err := credentials.CheckStarts(map[string][]string{"acme": {"env:WARP_PERSON_{subject}_", "env:HUMAGEN_PERSON_{subject}_"}}); err != nil {
		t.Errorf("one workspace's two spellings of its own start: %v", err)
	}
}
