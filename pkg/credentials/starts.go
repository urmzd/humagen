package credentials

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/urmzd/humagen/internal/legacy"
)

// Where a person's references may point.
//
// A reference is read with the server's own access: its environment, its
// keychain, the 1Password account it signed in to. So the operator lists,
// per workspace, how the references its people give may start (System
// person_refs), and every start names the person whose references it holds
// with SubjectField: "op://People/{subject}/", "keychain:humagen-{subject}/",
// "env:HUMAGEN_PERSON_{subject}_". A reference is a person's own only under a
// start with their subject in place of SubjectField (Owns), so nobody can
// connect a reference that names somebody else's credential and have their
// runs act with it. An env: start writes the subject in capitals, as the
// variables a reference may name are written: "ana" is HUMAGEN_PERSON_ANA_.
//
// That holds because no reference can be under two people's starts:
//
//   - In a start, SubjectField is followed by a separator ('/', or '_' in
//     an env: start) that a subject placed there may not contain, so where
//     the subject ends in a reference is never in doubt.
//   - No start's text before SubjectField begins another's, unless both are
//     one workspace's and the same text, so two places, or two workspaces,
//     never share a person's name space.
//
// A subject that cannot be written in a reference under a start (an env:
// start takes lowercase letters and digits, so that writing them in
// capitals gives no two people one name; the others take letters, digits
// and . _ @ -) can give no reference under it. That person can still give a
// value, where the workspace allows one.

// SubjectField is where a start names the person who gives a reference.
const SubjectField = "{subject}"

var (
	// envSubject is a subject an env: start can hold, in capitals: no '_',
	// which ends it, and no capital, so no two subjects are written alike.
	envSubject = regexp.MustCompile(`^[a-z0-9]{1,128}$`)
	// partSubject is a subject a keychain: or op:// start can hold: what a
	// part of one may contain (pkg/credref), without the space a reference
	// is trimmed of, and no '/', which ends it.
	partSubject = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,128}$`)
)

// CheckStart reports whether start may begin the references a workspace's
// people give. It names its person once, with SubjectField, after a place
// of the operator's own and followed by '/' (by '_' in an env: start, and in
// a keychain: start inside the service, before the account). With a person
// in it, it is one of the three schemes and, for env:, the start of the name
// of a HUMAGEN_ variable that is not a sign-in secret, which is what a client's
// reference may name too.
func CheckStart(start string) error {
	before, after, found := strings.Cut(start, SubjectField)
	switch {
	case strings.ContainsFunc(start, func(r rune) bool { return unicode.IsControl(r) || r == ',' || r == ';' }):
		return fmt.Errorf("%w: %q is not how a reference starts", ErrInvalid, start)
	case !found || strings.Contains(after, SubjectField):
		return fmt.Errorf("%w: %q: a start names the person whose references it holds once, with %s, so nobody can give another's", ErrInvalid, start, SubjectField)
	}
	// The scheme is judged with a person in place, and an env: start under
	// the old WARP_ prefix as the same start under HUMAGEN_, which is the
	// variable it reads (legacy.Lookup).
	before = canonical(before)
	sample := before + "X" + after
	var root string
	switch {
	case strings.HasPrefix(sample, "env:"):
		root = "env:" + legacy.Prefix
		name := strings.TrimPrefix(sample, "env:")
		if !strings.HasPrefix(name, legacy.Prefix) || legacy.SignInSecret(name) || strings.ContainsFunc(name, notEnvRune) {
			return fmt.Errorf("%w: %q: an env: start names a HUMAGEN_ variable, in capitals, that is not a sign-in secret", ErrInvalid, start)
		}
		if !strings.HasPrefix(after, "_") {
			return fmt.Errorf("%w: %q: in an env: start, %s is followed by _", ErrInvalid, start, SubjectField)
		}
	case strings.HasPrefix(sample, "keychain:"):
		root = "keychain:"
		if strings.Contains(before, "/") || !strings.HasPrefix(after, "/") {
			return fmt.Errorf("%w: %q: a keychain: start names its person in the service, as keychain:SERVICE%s/", ErrInvalid, start, SubjectField)
		}
	case strings.HasPrefix(sample, "op://"):
		root = "op://"
		if !strings.HasPrefix(after, "/") {
			return fmt.Errorf("%w: %q: in an op:// start, %s is followed by /", ErrInvalid, start, SubjectField)
		}
	default:
		return fmt.Errorf("%w: %q: a start is env:HUMAGEN_NAME, keychain:SERVICE or op://VAULT, or the start of one, with %s in it", ErrInvalid, start, SubjectField)
	}
	// A subject right after the scheme could be any variable, service or
	// vault the server has, its own included.
	if before == root {
		return fmt.Errorf("%w: %q: a start names a place of the operator's own before %s", ErrInvalid, start, SubjectField)
	}
	return nil
}

// CheckStarts checks every workspace's starts, by tenant, with CheckStart,
// and that no reference could be under two people's: no start's text before
// SubjectField begins another's, unless both are the same workspace's and
// the same text.
func CheckStarts(byTenant map[string][]string) error {
	type start struct{ tenant, start, before string }
	var all []start
	for _, tenant := range slices.Sorted(maps.Keys(byTenant)) {
		for _, s := range byTenant[tenant] {
			if err := CheckStart(s); err != nil {
				return fmt.Errorf("%s: %w", tenant, err)
			}
			before, _, _ := strings.Cut(s, SubjectField)
			all = append(all, start{tenant, s, canonical(before)})
		}
	}
	for i, a := range all {
		for _, b := range all[i+1:] {
			if a.tenant == b.tenant && a.before == b.before {
				continue
			}
			if strings.HasPrefix(a.before, b.before) || strings.HasPrefix(b.before, a.before) {
				return fmt.Errorf("%w: %q for %s and %q for %s could name one reference for two people; give each its own place",
					ErrInvalid, a.start, a.tenant, b.start, b.tenant)
			}
		}
	}
	return nil
}

// Expand is start with subject in place of SubjectField (in capitals in an
// env: start): how that person's own references under it start. It reports false for a start CheckStart
// refuses and for a subject that cannot be written there.
func Expand(start, subject string) (string, bool) {
	if CheckStart(start) != nil {
		return "", false
	}
	fits, written := partSubject, subject
	if strings.HasPrefix(start, "env:") {
		fits, written = envSubject, strings.ToUpper(subject)
	}
	own := strings.Replace(start, SubjectField, written, 1)
	if !fits.MatchString(subject) || (strings.HasPrefix(own, "env:") && legacy.SignInSecret(strings.TrimPrefix(own, "env:"))) {
		return "", false
	}
	return own, true
}

// Owns reports whether ref is subject's own under start: it begins with
// start with subject in place of SubjectField. It checks where ref points,
// not that it parses.
func Owns(start, subject, ref string) bool {
	own, ok := Expand(start, subject)
	return ok && strings.HasPrefix(canonical(ref), canonical(own))
}

// canonical is a reference or the start of one with an env: name under the
// old WARP_ prefix written under HUMAGEN_, so the two spellings of one
// variable compare equal: starts, references and grants written before the
// rename keep naming what they named.
func canonical(ref string) string {
	if name, ok := strings.CutPrefix(ref, "env:"); ok {
		return "env:" + legacy.Canonical(name)
	}
	return ref
}

func notEnvRune(r rune) bool {
	return (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_'
}
