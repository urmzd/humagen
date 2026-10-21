// Package people is a workspace's directory of people: who can be found,
// tagged, and named on a project.
//
// A person is recorded when they sign in, when a client maps a Slack person
// to them, and when the policy grants a role to their subject. Nothing here
// decides who anyone is or what they may do: the identity provider and the
// policy do. This is what the rest of warp shows and addresses them by, a
// name and a handle.
//
// # No email
//
// No email is kept or shown, and no field could hold one. A name that is an
// email address is not kept as a name, and a handle is never made from an
// address's domain. A person's picture is their own photo in pkg/prefs,
// served by warp: a picture URL from the identity provider is never kept, so
// showing the directory sends nobody else a request.
//
// # Handles
//
// A handle is how a person is tagged: @dana-scully. It is unique in a
// workspace across people and agents, made from the person's name or, with
// none known, from their subject. A handle someone already holds, an agent's
// name (through WithTaken) or a reserved word gets a suffix: dana-2. Once a
// person has a name their handle does not change, so a tag keeps meaning the
// same person.
package people

import (
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Errors a caller can match.
var (
	// ErrNotFound reports a person the workspace has not recorded.
	ErrNotFound = errors.New("people: not found")
	// ErrInvalid reports a sighting, a query or a record the directory
	// refuses.
	ErrInvalid = errors.New("people: invalid")
	// ErrLimit reports a workspace that has recorded as many people as it
	// may. Signing in still works; the person is just not listed.
	ErrLimit = errors.New("people: limit reached")
)

// Where a person was seen.
const (
	// SignIn is a person signing in to the console or the API.
	SignIn = "signin"
	// Slack is a client mapping a Slack person to the person's subject.
	Slack = "slack"
	// Policy is the policy granting a role to the person's subject.
	Policy = "policy"
)

// Bounds.
const (
	// MaxPeople is the most people a workspace records.
	MaxPeople = 5000
	// MaxPage is the most people one page lists, and a page when none is
	// asked for.
	MaxPage = 50
	// MaxHandle is the longest handle.
	MaxHandle = 32
	MaxName   = 120
	// MaxSubject is the longest subject, as pkg/clients bounds one.
	MaxSubject = 200
	MaxRoles   = 32
	MaxRole    = 64
	MaxQuery   = 100
	// Quiet is how long a sighting that changes nothing goes unwritten, so
	// signing in does not write on every request.
	Quiet = time.Hour
)

// Reserved is the handles no person can hold: the built-in router, and the
// project's experts.
var Reserved = []string{"auto", "sme"}

// Person is one person in the directory.
type Person struct {
	// Subject is who they are to the identity provider: the identity every
	// member entry, grant and run names them by.
	Subject string `json:"subject"`
	// Name is the name they go by. Empty until one is known.
	Name string `json:"name,omitempty"`
	// Handle is how they are tagged, unique in the workspace.
	Handle string `json:"handle"`
	// Roles are the workspace roles they held when last seen, by name.
	Roles []string `json:"roles"`
	// Sources are where they were seen: SignIn, Slack or Policy.
	Sources []string `json:"sources"`
	// SeenAt is when they last signed in. Zero for someone who never has.
	SeenAt    time.Time `json:"seen_at,omitzero"`
	CreatedAt time.Time `json:"created_at"`
}

func (p Person) clone() Person {
	p.Roles = slices.Clone(p.Roles)
	p.Sources = slices.Clone(p.Sources)
	return p
}

// Sighting is a person seen somewhere.
type Sighting struct {
	Subject string
	// Name is the name they gave. Empty keeps the one on record.
	Name string
	// Roles are the roles they hold. Nil keeps those on record. A sign-in's
	// roles are the whole set, so they replace the record's; the policy's,
	// granted to the subject alone, are kept only for someone who has never
	// signed in.
	Roles  []string
	Source string
}

var validHandle = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)

// ValidHandle reports whether s can be a handle.
func ValidHandle(s string) bool { return validHandle.MatchString(s) }

// IsReserved reports whether a handle is one no person can hold.
func IsReserved(handle string) bool { return slices.Contains(Reserved, handle) }

func validSource(s string) bool { return s == SignIn || s == Slack || s == Policy }

func validSubject(s string) bool {
	return s != "" && len(s) <= MaxSubject && utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl)
}

// nameOf is a name as the directory keeps it: one line of at most MaxName
// characters, spaces collapsed, and nothing when it is an email address.
func nameOf(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if strings.Contains(s, "@") {
		return ""
	}
	if r := []rune(s); len(r) > MaxName {
		s = strings.TrimSpace(string(r[:MaxName]))
	}
	return s
}

// rolesOf is roles as they are kept: sorted, once each. Nil stays nil.
func rolesOf(roles []string) ([]string, error) {
	if roles == nil {
		return nil, nil
	}
	if len(roles) > MaxRoles {
		return nil, errors.Join(ErrInvalid, errors.New("people: at most 32 roles"))
	}
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		if r == "" || len(r) > MaxRole || strings.ContainsFunc(r, func(c rune) bool { return unicode.IsSpace(c) || unicode.IsControl(c) }) {
			return nil, errors.Join(ErrInvalid, errors.New("people: a role is a name of at most 64 characters"))
		}
		out = append(out, r)
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// fold takes the accents off letters, so "Zoë Núñez" makes zoe-nunez.
var fold = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// slug is text as a handle: lower case, accents folded, every run of anything
// else one dash, at most MaxHandle characters. It is empty when nothing is
// left.
func slug(s string) string {
	if folded, _, err := transform.String(fold, s); err == nil {
		s = folded
	}
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		default:
			dash = true
		}
		if b.Len() >= MaxHandle {
			break
		}
	}
	return clip(b.String(), MaxHandle)
}

// clip cuts a handle to n characters, and any dash it then ends with.
func clip(h string, n int) string {
	if len(h) > n {
		h = h[:n]
	}
	return strings.TrimRight(h, "-._")
}

// fromSubject is what a handle is made from when no name is known: the part
// after a provider's prefix ("okta|00u1ana", "tailscale:ada"), and never the
// domain of an address.
func fromSubject(subject string) string {
	if at := strings.IndexByte(subject, '@'); at >= 0 {
		subject = subject[:at]
	}
	if i := strings.LastIndexAny(subject, ":|"); i >= 0 && i < len(subject)-1 {
		subject = subject[i+1:]
	}
	return subject
}

// candidate is the nth handle to try for a person: the base, then the base
// with -2, -3 and on.
func candidate(base string, n int) string {
	if n <= 1 {
		return base
	}
	suffix := "-" + strconv.Itoa(n)
	return clip(base, MaxHandle-len(suffix)) + suffix
}

// baseHandle is the handle a person's name, or else their subject, makes.
func baseHandle(name, subject string) string {
	if h := slug(name); h != "" {
		return h
	}
	if h := slug(fromSubject(subject)); h != "" {
		return h
	}
	return "person"
}
