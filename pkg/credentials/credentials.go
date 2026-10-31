// Package credentials keeps the credentials people give humagen for themselves:
// at most one per person for each person-scoped tool provider (an MCP server
// whose credential scope is person, docs/reference/spec.md), so a run that acts for
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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/urmzd/humagen/fiber/pkg/toolprovider"

	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/state"
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
	Ref        string    `json:"ref,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
	LastUsedAt time.Time `json:"last_used_at,omitzero"`
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
// It reads and writes the database per request (pkg/state, per request): it
// holds no record in memory, so any number of processes on one database
// agree, and it does not need pkg/state's single-writer lock. A write that
// fails changes nothing it reports.
type Store struct {
	st      state.Store
	kept    *state.Table[Credential]
	secrets Secrets
}

// namespace is where records are kept. Within a tenant:
//
//	<subject>/<provider>  a person's credential for a provider
//	<subject>/#           how many credentials the person holds
//
// A provider's name holds no slash and no "#", so neither key is another
// person's. The count is written, at the version read, with every
// credential a person adds, so two processes adding at once cannot take
// them past MaxPerPerson. A person kept before the count is counted on
// their next new credential.
const namespace = "credentials"

// New returns an empty store in this process's memory that keeps values in
// secrets. With a nil secrets it keeps references only.
func New(secrets Secrets) *Store {
	s, _ := Open(nil, secrets)
	return s
}

// key names a record within its tenant: the subject, then the provider,
// whose name holds no slash.
func key(subject, provider string) string { return subject + "/" + provider }

func guardKey(subject string) string { return subject + "/#" }

// Open returns a store that keeps its records in st, which may be shared
// with other processes, and its values in secrets. A nil st keeps records in
// this process's memory.
func Open(st state.Store, secrets Secrets) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	return &Store{st: st, kept: state.NewTable[Credential](st, namespace), secrets: secrets}, nil
}

// count reads a person's count and its version: 0 when they have none yet,
// and then n is what they hold, counted.
func (s *Store) count(tenant, subject string) (n int, version int64, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
	defer cancel()
	r, err := s.st.Lookup(ctx, namespace, tenant, guardKey(subject))
	if err == nil {
		if err := json.Unmarshal(r.Value, &n); err != nil {
			return 0, 0, fmt.Errorf("credentials: count: %w", err)
		}
		return n, r.Version, nil
	}
	if !errors.Is(err, state.ErrNotFound) {
		return 0, 0, err
	}
	held, err := s.mine(tenant, subject)
	return len(held), 0, err
}

// opAt is an op that writes v at version: created when version is 0.
func opAt(tenant, key string, v any, version int64) (state.Op, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return state.Op{}, err
	}
	if version == 0 {
		return state.Create(tenant, key, raw), nil
	}
	return state.PutAt(tenant, key, raw, version), nil
}

func (s *Store) write(ops ...state.Op) error {
	ctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
	defer cancel()
	return s.st.Write(ctx, namespace, ops...)
}

// mine reads a person's credentials, by provider name. Keys under the
// person's prefix that belong to someone else (a subject that continues it
// past a slash) or to the count are passed over.
func (s *Store) mine(tenant, subject string) ([]Credential, error) {
	ctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
	defer cancel()
	prefix := subject + "/"
	var out []Credential
	after := ""
	for {
		page, next, err := s.st.List(ctx, namespace, tenant, state.Query{Prefix: prefix, After: after, Limit: state.MaxList})
		if err != nil {
			return nil, fmt.Errorf("credentials: read: %w", err)
		}
		for _, r := range page {
			rest := strings.TrimPrefix(r.Key, prefix)
			if !toolprovider.ValidName(rest) {
				continue
			}
			var c Credential
			if err := json.Unmarshal(r.Value, &c); err != nil {
				return nil, fmt.Errorf("credentials: %q: %w", r.Key, err)
			}
			if c.Provider == rest {
				out = append(out, c)
			}
		}
		if len(out) > MaxPerPerson {
			return nil, fmt.Errorf("credentials: %w: a person holds more than %d credentials", state.ErrTooMany, MaxPerPerson)
		}
		if next == "" {
			return out, nil
		}
		after = next
	}
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

// put writes a person's credential at the version read, and, when it is
// new to them, their count with it.
func (s *Store) put(tenant, subject string, c Credential) error {
	return state.Retry(func() error {
		cur, err := s.kept.Lookup(tenant, key(subject, c.Provider))
		if err == nil {
			op, err := opAt(tenant, key(subject, c.Provider), c, cur.Version)
			if err != nil {
				return err
			}
			return s.write(op)
		}
		if !errors.Is(err, state.ErrNotFound) {
			return err
		}
		n, version, err := s.count(tenant, subject)
		if err != nil {
			return err
		}
		if n >= MaxPerPerson {
			return fmt.Errorf("%w: a person holds at most %d credentials", ErrLimit, MaxPerPerson)
		}
		rec, err := opAt(tenant, key(subject, c.Provider), c, 0)
		if err != nil {
			return err
		}
		head, err := opAt(tenant, guardKey(subject), n+1, version)
		if err != nil {
			return err
		}
		return s.write(rec, head)
	})
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
	who := owner(subject)
	if c.Kind == Value {
		if s.secrets == nil {
			return Credential{}, fmt.Errorf("%w: this store keeps no values", ErrInvalid)
		}
		before, _ := s.secrets.Get(tenant, who, provider)
		if err := s.secrets.Put(tenant, who, provider, in.Value); err != nil {
			return Credential{}, fmt.Errorf("credentials: write: %w", err)
		}
		if err := s.put(tenant, subject, c); err != nil {
			// The record was not written, so the value it would have
			// named is put back as it was.
			_ = s.secrets.Put(tenant, who, provider, before)
			if errors.Is(err, ErrLimit) {
				return Credential{}, err
			}
			return Credential{}, fmt.Errorf("credentials: write: %w", err)
		}
		return c, nil
	}
	if err := s.put(tenant, subject, c); err != nil {
		if errors.Is(err, ErrLimit) {
			return Credential{}, err
		}
		return Credential{}, fmt.Errorf("credentials: write: %w", err)
	}
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

// Get returns a person's credential for a provider. ok is false when they
// have none.
func (s *Store) Get(tenant, subject, provider string) (Credential, bool, error) {
	it, err := s.kept.Lookup(tenant, key(subject, provider))
	if errors.Is(err, state.ErrNotFound) {
		return Credential{}, false, nil
	}
	if err != nil {
		return Credential{}, false, fmt.Errorf("credentials: read: %w", err)
	}
	return it.Value, true, nil
}

// List returns a person's credentials by provider name, never more than
// MaxPerPerson.
func (s *Store) List(tenant, subject string) ([]Credential, error) {
	out, err := s.mine(tenant, subject)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []Credential{}
	}
	return out, nil
}

// Value returns the value a person stored for a provider. ok is false when
// their credential is a reference, when they have none, and when the value
// is no longer held.
func (s *Store) Value(tenant, subject, provider string) (string, bool, error) {
	c, has, err := s.Get(tenant, subject, provider)
	if err != nil || !has || c.Kind != Value || s.secrets == nil {
		return "", false, err
	}
	v, ok := s.secrets.Get(tenant, owner(subject), provider)
	return v, ok && v != "", nil
}

// Delete removes a person's credential for a provider, and the value if it
// was one.
func (s *Store) Delete(tenant, subject, provider string) error {
	err := state.Retry(func() error {
		cur, err := s.kept.Lookup(tenant, key(subject, provider))
		if errors.Is(err, state.ErrNotFound) {
			return fmt.Errorf("%w: no credential for %q", ErrNotFound, provider)
		}
		if err != nil {
			return err
		}
		ops := []state.Op{state.DeleteAt(tenant, key(subject, provider), cur.Version)}
		n, version, err := s.count(tenant, subject)
		if err != nil {
			return err
		}
		if version != 0 {
			head, err := opAt(tenant, guardKey(subject), max(n-1, 0), version)
			if err != nil {
				return err
			}
			ops = append(ops, head)
		}
		return s.write(ops...)
	})
	if errors.Is(err, ErrNotFound) {
		return err
	}
	if err != nil {
		return fmt.Errorf("credentials: write: %w", err)
	}
	return s.forget(tenant, owner(subject), provider)
}

// MarkUsed records credential resolution by an agent before exposing it to a
// provider. It never reads or copies the secret, and fails closed on durability.
func (s *Store) MarkUsed(tenant, subject, provider string, at time.Time) error {
	if err := checkWho(tenant, subject, provider); err != nil {
		return err
	}
	if at.IsZero() {
		return ErrInvalid
	}
	err := state.Retry(func() error {
		cur, err := s.kept.Lookup(tenant, key(subject, provider))
		if errors.Is(err, state.ErrNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		held := cur.Value
		if !held.LastUsedAt.IsZero() && !at.After(held.LastUsedAt) {
			return nil
		}
		held.LastUsedAt = at.UTC()
		b := s.kept.Batch()
		b.PutAt(tenant, key(subject, provider), held, cur.Version)
		return b.Commit()
	})
	return err
}
