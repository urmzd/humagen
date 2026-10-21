// Package credentials keeps the credentials people give warp for themselves:
// at most one per person for each person-scoped tool provider (an MCP server
// whose credential scope is person, docs/spec.md), so a run that acts for
// someone reaches that provider as them and as nobody else.
//
// # References and values
//
// A credential is a reference or a value:
//
//   - A reference (env:NAME, keychain:SERVICE[/ACCOUNT] or
//     op://VAULT/ITEM/FIELD, the forms pkg/credref reads) is a name for where
//     the secret is kept. The reference is stored; the secret it names is not.
//     It is read when a run uses it, so rotating it is changing it where it
//     lives.
//   - A value is the secret itself, and it is stored. It is kept the way a
//     client's secret is: in the Secrets store, apart from the record, never
//     returned by anything this package gives out and never formatted into a
//     log. With -state=postgres that store holds it in the database as
//     written, so the database is to be guarded as the secrets are.
//
// This package checks a credential's form and keeps it. Which references a
// workspace's people may give, whether they may store values at all, and who
// a run acts for are the platform's decisions.
package credentials

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/urmzd/fiber/pkg/toolprovider"

	"github.com/urmzd/warp/pkg/credref"
	"github.com/urmzd/warp/pkg/state"
)

// Errors. A message never carries a value.
var (
	ErrInvalid  = errors.New("credentials: invalid")
	ErrNotFound = errors.New("credentials: not found")
	// ErrLimit reports a person who already holds as many credentials as
	// they may.
	ErrLimit = errors.New("credentials: limit reached")
)

// Kind is how a credential is held.
type Kind string

const (
	// Reference is a name for where the secret is kept. Only the name is
	// stored.
	Reference Kind = "reference"
	// Value is the secret itself, kept in the Secrets store.
	Value Kind = "value"
)

// Bounds.
const (
	// MaxPerPerson is the most credentials one person holds in a workspace,
	// one per provider. It also bounds every list.
	MaxPerPerson = 64
	// MaxValue is the longest value, in bytes.
	MaxValue = 8 << 10
	// MaxRef is the longest reference.
	MaxRef = 512
	// maxSubject is the longest subject, as pkg/clients bounds it.
	maxSubject = 200
)

// Credential is a person's credential for one provider, as it is shown: the
// reference when it is one, and never a value.
type Credential struct {
	Provider string `json:"provider"`
	Kind     Kind   `json:"kind"`
	// Ref is the reference, for a Reference. It names where the secret is,
	// which the person who gave it may see.
	Ref       string    `json:"ref,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Input writes one credential: a reference or a value, exactly one. It is
// accepted and its value is never given back.
type Input struct {
	Ref   string `json:"ref,omitempty"`
	Value string `json:"value,omitempty"`
}

// String names the reference, or says a value was given without printing it.
func (in Input) String() string {
	switch {
	case in.Value != "":
		return "[secret]"
	default:
		return in.Ref
	}
}

// GoString is String, so %#v cannot print the value either.
func (in Input) GoString() string { return in.String() }

// Check reports whether in is a credential of a valid form. It does not read
// a reference: whether it names anything is found out when a run uses it,
// so nobody can ask the server whether a variable or an item exists.
func (in Input) Check() error {
	switch {
	case in.Ref != "" && in.Value != "":
		return fmt.Errorf("%w: give a reference or a value, not both", ErrInvalid)
	case in.Ref == "" && in.Value == "":
		return fmt.Errorf("%w: give a reference or a value", ErrInvalid)
	case in.Value != "":
		if len(in.Value) > MaxValue {
			return fmt.Errorf("%w: a value is at most %d bytes", ErrInvalid, MaxValue)
		}
		// A value is sent as a header or a variable, so it is one line.
		if strings.TrimSpace(in.Value) == "" || strings.ContainsFunc(in.Value, unicode.IsControl) {
			return fmt.Errorf("%w: a value is one line of text", ErrInvalid)
		}
		return nil
	}
	if len(in.Ref) > MaxRef {
		return fmt.Errorf("%w: a reference is at most %d characters", ErrInvalid, MaxRef)
	}
	// Not echoed: a secret pasted where a reference belongs is not said back.
	if _, err := credref.Parse(in.Ref); err != nil {
		return fmt.Errorf("%w: a reference is env:NAME, keychain:SERVICE[/ACCOUNT] or op://VAULT/ITEM/FIELD", ErrInvalid)
	}
	return nil
}

// CheckStart reports whether start may begin the references a workspace's
// people give: one of the three schemes and, for env:, the name or the
// start of the name of a WARP_ variable that is not a sign-in secret, which
// is what a client's reference may name too. The operator lists these
// (System person_refs), because a reference is read with the server's own
// access, so a person may name anything that starts with one.
func CheckStart(start string) error {
	switch {
	case strings.ContainsFunc(start, func(r rune) bool { return unicode.IsControl(r) || r == ',' || r == ';' }):
		return fmt.Errorf("%w: %q is not how a reference starts", ErrInvalid, start)
	case strings.HasPrefix(start, "env:"):
		name := strings.TrimPrefix(start, "env:")
		if !strings.HasPrefix(name, "WARP_") || strings.HasPrefix(name, "WARP_OIDC") || strings.ContainsFunc(name, notEnvRune) {
			return fmt.Errorf("%w: %q: an env: start names a WARP_ variable that is not a sign-in secret", ErrInvalid, start)
		}
	case strings.HasPrefix(start, "keychain:") && len(start) > len("keychain:"),
		strings.HasPrefix(start, "op://") && len(start) > len("op://"):
	default:
		return fmt.Errorf("%w: %q: a start is env:WARP_NAME, keychain:SERVICE or op://VAULT, or the start of one", ErrInvalid, start)
	}
	return nil
}

func notEnvRune(r rune) bool {
	return (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_'
}

// Secrets keeps values apart from the records. clients.Secrets is one: the
// platform hands this store the very store its clients' secrets are kept in,
// so both are kept, and guarded, the same way.
type Secrets interface {
	Put(tenant, owner, name, value string) error
	Get(tenant, owner, name string) (string, bool)
	Drop(tenant, owner string) error
}

// owner is who a person's values are kept under in Secrets. A client's id
// holds no slash, so a person's values never share an owner with a client's
// secrets, and a provider's name holds none either, so the last slash of the
// key Secrets writes parts the owner from the provider.
func owner(subject string) string { return "person/" + subject }

// Store keeps people's credentials. Every read and write names a tenant and
// a subject, and one person's credentials are never another's.
//
// Built with Open, it writes every record through a state.Store and reads
// them back when it is built again. A write that fails changes nothing it
// reports.
type Store struct {
	mu      sync.RWMutex
	by      map[[2]string]map[string]Credential
	kept    *state.Table[Credential]
	secrets Secrets
}

// New returns an empty store in memory that keeps values in secrets. With a
// nil secrets it keeps references only.
func New(secrets Secrets) *Store {
	return &Store{by: map[[2]string]map[string]Credential{}, secrets: secrets}
}

// key names a record within its tenant: the subject, then the provider,
// whose name holds no slash.
func key(subject, provider string) string { return subject + "/" + provider }

// Open returns a store that keeps its records in st and its values in
// secrets, holding what st already has. A nil st keeps records in memory.
func Open(st state.Store, secrets Secrets) (*Store, error) {
	s := New(secrets)
	s.kept = state.NewTable[Credential](st, "credentials")
	err := s.kept.Load(func(tenant, k string, c Credential) error {
		i := strings.LastIndexByte(k, '/')
		if i <= 0 || k[i+1:] != c.Provider {
			return fmt.Errorf("credentials: a record is kept under %q", k)
		}
		s.of(tenant, k[:i])[c.Provider] = c
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("credentials: load: %w", err)
	}
	return s, nil
}

func (s *Store) of(tenant, subject string) map[string]Credential {
	m := s.by[[2]string{tenant, subject}]
	if m == nil {
		m = map[string]Credential{}
		s.by[[2]string{tenant, subject}] = m
	}
	return m
}

func checkWho(tenant, subject, provider string) error {
	switch {
	case tenant == "":
		return fmt.Errorf("%w: a credential belongs to a workspace", ErrInvalid)
	case strings.TrimSpace(subject) == "" || len(subject) > maxSubject || strings.ContainsFunc(subject, unicode.IsControl):
		return fmt.Errorf("%w: a credential belongs to a person", ErrInvalid)
	case !toolprovider.ValidName(provider):
		return fmt.Errorf("%w: %q is not a tool provider's name", ErrInvalid, provider)
	}
	return nil
}

// Put sets a person's credential for a provider, in place of the one they
// had. A value is kept in Secrets; a reference replaces a value the person
// had, which is then forgotten.
func (s *Store) Put(tenant, subject, provider string, in Input, at time.Time) (Credential, error) {
	if err := checkWho(tenant, subject, provider); err != nil {
		return Credential{}, err
	}
	if err := in.Check(); err != nil {
		return Credential{}, err
	}
	c := Credential{Provider: provider, Kind: Reference, Ref: strings.TrimSpace(in.Ref), UpdatedAt: at.UTC()}
	if in.Value != "" {
		c.Kind, c.Ref = Value, ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	mine := s.of(tenant, subject)
	if _, had := mine[provider]; !had && len(mine) >= MaxPerPerson {
		return Credential{}, fmt.Errorf("%w: a person holds at most %d credentials", ErrLimit, MaxPerPerson)
	}
	who := owner(subject)
	if c.Kind == Value {
		if s.secrets == nil {
			return Credential{}, fmt.Errorf("%w: this store keeps no values", ErrInvalid)
		}
		before, _ := s.secrets.Get(tenant, who, provider)
		if err := s.secrets.Put(tenant, who, provider, in.Value); err != nil {
			return Credential{}, fmt.Errorf("credentials: write: %w", err)
		}
		if err := s.kept.Put(tenant, key(subject, provider), c); err != nil {
			// The record was not written, so the value it would have
			// named is put back as it was.
			_ = s.secrets.Put(tenant, who, provider, before)
			return Credential{}, fmt.Errorf("credentials: write: %w", err)
		}
		mine[provider] = c
		return c, nil
	}
	if err := s.kept.Put(tenant, key(subject, provider), c); err != nil {
		return Credential{}, fmt.Errorf("credentials: write: %w", err)
	}
	mine[provider] = c
	if err := s.forget(tenant, who, provider); err != nil {
		return c, err
	}
	return c, nil
}

// forget drops a value a person no longer holds, if there is one.
func (s *Store) forget(tenant, who, provider string) error {
	if s.secrets == nil {
		return nil
	}
	if v, ok := s.secrets.Get(tenant, who, provider); !ok || v == "" {
		return nil
	}
	if err := s.secrets.Put(tenant, who, provider, ""); err != nil {
		return fmt.Errorf("credentials: the value held before could not be forgotten: %w", err)
	}
	return nil
}

// Get returns a person's credential for a provider.
func (s *Store) Get(tenant, subject, provider string) (Credential, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.by[[2]string{tenant, subject}][provider]
	return c, ok
}

// List returns a person's credentials by provider name, never more than
// MaxPerPerson.
func (s *Store) List(tenant, subject string) []Credential {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Credential, 0, len(s.by[[2]string{tenant, subject}]))
	for _, c := range s.by[[2]string{tenant, subject}] {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b Credential) int { return strings.Compare(a.Provider, b.Provider) })
	if len(out) > MaxPerPerson {
		out = out[:MaxPerPerson]
	}
	return out
}

// Value returns the value a person stored for a provider. ok is false when
// their credential is a reference, when they have none, and when the value
// is no longer held.
func (s *Store) Value(tenant, subject, provider string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, has := s.by[[2]string{tenant, subject}][provider]
	if !has || c.Kind != Value || s.secrets == nil {
		return "", false
	}
	v, ok := s.secrets.Get(tenant, owner(subject), provider)
	return v, ok && v != ""
}

// Delete removes a person's credential for a provider, and the value if it
// was one.
func (s *Store) Delete(tenant, subject, provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	mine := s.by[[2]string{tenant, subject}]
	if _, ok := mine[provider]; !ok {
		return fmt.Errorf("%w: no credential for %q", ErrNotFound, provider)
	}
	if err := s.kept.Delete(tenant, key(subject, provider)); err != nil {
		return fmt.Errorf("credentials: write: %w", err)
	}
	delete(mine, provider)
	return s.forget(tenant, owner(subject), provider)
}
