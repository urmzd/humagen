package credentials_test

import (
	"errors"
	"testing"

	"github.com/urmzd/warp/pkg/credentials"
)

func TestOnlyAStartTheServerCanConfineIsAccepted(t *testing.T) {
	for _, ok := range []string{
		"env:WARP_PERSON_{subject}_", "env:WARP_P{subject}_GITHUB", "keychain:warp-people-{subject}/",
		"keychain:warp{subject}/", "op://People/{subject}/", "op://People/{subject}/issues", "op://People-{subject}/",
	} {
		if err := credentials.CheckStart(ok); err != nil {
			t.Errorf("%s was refused: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"", "env:", "env:ANTHROPIC_{subject}_", "env:WARP_OIDC_{subject}_", "env:WARP_OIDC{subject}_", "env:WARP_A-B{subject}_",
		"env:WARP_person_{subject}_",
		"keychain:", "op://", "vault:{subject}/", "op://a{subject}/,op://b", "op://a{subject}/;b",
		// A start that names nobody holds everyone's.
		"env:WARP_PERSON_", "keychain:warp-people/", "op://People/", "op://People/ada/",
		// A subject that could run on into the rest of the reference.
		"env:WARP_PERSON_{subject}", "env:WARP_PERSON_{subject}X", "op://People/{subject}", "op://People/{subject}-x/",
		"keychain:warp-{subject}", "keychain:warp/{subject}/",
		// A subject named twice, or with nothing of the operator's before it.
		"op://People/{subject}/{subject}/", "env:WARP_{subject}_", "keychain:{subject}/", "op://{subject}/",
		"{subject}:x/",
	} {
		if err := credentials.CheckStart(bad); !errors.Is(err, credentials.ErrInvalid) {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// A reference is a person's own only under a start with their subject in
// it, so nobody gives a reference to somebody else's credential.
func TestAReferenceIsOnlyItsOwnersUnderAStart(t *testing.T) {
	for _, c := range []struct {
		start, subject, ref string
		owns                bool
	}{
		{"op://People/{subject}/", "ana", "op://People/ana/issues", true},
		{"op://People/{subject}/", "ben", "op://People/ana/issues", false},
		{"op://People/{subject}/", "an", "op://People/ana/issues", false},
		{"op://People/{subject}/", "ana", "op://People/ana.b/issues", false},
		{"op://People/{subject}/", "ana.b", "op://People/ana.b/issues", true},
		{"op://People/{subject}/", "ana/x", "op://People/ana/x/issues", false},
		{"op://People/{subject}/", "", "op://People//issues", false},
		{"op://People/{subject}/", "ana@example.com", "op://People/ana@example.com/issues", true},
		{"env:WARP_PERSON_{subject}_", "ana", "env:WARP_PERSON_ANA_ISSUES", true},
		{"env:WARP_PERSON_{subject}_", "ben", "env:WARP_PERSON_ANA_ISSUES", false},
		{"env:WARP_PERSON_{subject}_", "ana", "env:WARP_PERSON_ana_ISSUES", false},
		// An env: subject holds no capital, or "ANA" would be written as "ana" is.
		{"env:WARP_PERSON_{subject}_", "ANA", "env:WARP_PERSON_ANA_ISSUES", false},
		{"env:WARP_PERSON_{subject}_", "Ana", "env:WARP_PERSON_ANA_ISSUES", false},
		// Nor a _, or "ana" would own "ana_b"'s.
		{"env:WARP_PERSON_{subject}_", "ana_b", "env:WARP_PERSON_ANA_B_ISSUES", false},
		{"env:WARP_PERSON_{subject}_", "ana", "env:WARP_PERSON_ANA_B_ISSUES", true},
		{"env:WARP_PERSON_{subject}_", "mock-admin", "env:WARP_PERSON_MOCK-ADMIN_X", false},
		{"env:WARP_PERSON_{subject}_", "1234567890", "env:WARP_PERSON_1234567890_ISSUES", true},
		{"keychain:warp-people-{subject}/", "ana", "keychain:warp-people-ana/issues", true},
		{"keychain:warp-people-{subject}/", "ana", "keychain:warp-people-anab/issues", false},
		// A start CheckStart refuses owns nothing.
		{"op://People/", "ana", "op://People/ana/issues", false},
		{"op://People/{subject}", "ana", "op://People/ana/issues", false},
	} {
		if got := credentials.Owns(c.start, c.subject, c.ref); got != c.owns {
			t.Errorf("Owns(%q, %q, %q) = %v, want %v", c.start, c.subject, c.ref, got, c.owns)
		}
	}
	if own, ok := credentials.Expand("op://People/{subject}/", "ana"); !ok || own != "op://People/ana/" {
		t.Errorf("ana's own start = %q, %v", own, ok)
	}
	if own, ok := credentials.Expand("env:WARP_O{subject}_", "idc"); ok {
		t.Errorf("a subject made a sign-in secret a person's own: %q", own)
	}
}

// No reference can be under two people's starts: not two places of one
// workspace, and not two workspaces.
func TestNoTwoPeopleShareAPlace(t *testing.T) {
	for name, starts := range map[string]map[string][]string{
		"none":                    nil,
		"one workspace":           {"acme": {"op://People/{subject}/", "keychain:warp-people-{subject}/", "env:WARP_PERSON_{subject}_"}},
		"one place, two forms":    {"acme": {"op://People/{subject}/", "op://People/{subject}/issues"}},
		"a place per workspace":   {"acme": {"op://Acme/{subject}/"}, "globex": {"op://Globex/{subject}/"}},
		"one vault, two prefixes": {"acme": {"op://People/acme-{subject}/"}, "globex": {"op://People/globex-{subject}/"}},
	} {
		if err := credentials.CheckStarts(starts); err != nil {
			t.Errorf("%s was refused: %v", name, err)
		}
	}
	for name, starts := range map[string]map[string][]string{
		"a bad start":                     {"acme": {"op://People/"}},
		"one place for two workspaces":    {"acme": {"op://People/{subject}/"}, "globex": {"op://People/{subject}/"}},
		"one place inside another":        {"acme": {"op://People/{subject}/", "op://People/team-{subject}/"}},
		"a workspace inside another's":    {"acme": {"op://People/{subject}/"}, "globex": {"op://People/globex-{subject}/"}},
		"a variable inside another's":     {"acme": {"env:WARP_P{subject}_"}, "globex": {"env:WARP_PERSON_{subject}_"}},
		"a keychain inside another's":     {"acme": {"keychain:warp-{subject}/"}, "globex": {"keychain:warp-globex-{subject}/"}},
		"a vault prefix inside another's": {"acme": {"op://People{subject}/"}, "globex": {"op://PeopleX{subject}/"}},
	} {
		if err := credentials.CheckStarts(starts); !errors.Is(err, credentials.ErrInvalid) {
			t.Errorf("%s was accepted: %v", name, err)
		}
	}
}
