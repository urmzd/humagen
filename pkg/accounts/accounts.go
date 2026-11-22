// Package accounts keeps the accounts people linked to themselves on the
// surfaces Humagen answers on: a GitHub user, a Slack user. A link says that
// one person in a workspace is that surface's user, so a message the user
// writes there acts for the person.
//
// One record per surface user is the claim: a surface user is linked to one
// person in a workspace, and a second person's claim is refused until the
// first is unlinked. One record per person and surface points back at it, so
// a person has one account on each surface. Both are written together, at the
// versions read, so two claims at once cannot both win. Every record is the
// tenant's, keyed for one tenant's reads.
//
// A link holds no credential. The sign-in that proved it is spent and its
// token is not kept.
package accounts

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

// The surfaces an account is linked on.
const (
	GitHub = "github"
	Slack  = "slack"
)

// MaxList is the most accounts one List returns.
const MaxList = 200

var (
	// ErrNotFound reports no account linked there.
	ErrNotFound = errors.New("accounts: not linked")
	// ErrClaimed reports a surface user another person in the workspace
	// already linked.
	ErrClaimed = errors.New("this account is linked to another person in this workspace; they must disconnect it first")
	// ErrInvalid reports a surface, user or person the store cannot keep.
	ErrInvalid = errors.New("accounts: invalid account")
)

// Account is one surface user linked to one person.
type Account struct {
	Surface string `json:"surface"`
	// ID is the surface's own stable id for the user: GitHub's numeric user
	// id in decimal, Slack's user id. A login can change; this does not.
	ID string `json:"id"`
	// Team is the Slack team the user signed in from. Empty on GitHub.
	Team string `json:"team,omitempty"`
	// Login is what the surface calls the user, for display: a GitHub
	// login, a Slack display name. It is the one at the last sign-in and
	// never decides who the user is.
	Login   string    `json:"login"`
	Avatar  string    `json:"avatar_url,omitempty"`
	Subject string    `json:"subject"`
	At      time.Time `json:"linked_at"`
}

// Store keeps the links of every workspace. A person's record is an Account
// holding only the ID of the user they linked, so both kinds of record are
// written in one batch.
type Store struct {
	records *state.Table[Account]
}

// Open returns the store over st, or over memory when st is nil.
func Open(st state.Store) *Store {
	if st == nil {
		st = state.NewMemory()
	}
	return &Store{records: state.NewTable[Account](st, "linked_accounts")}
}

func known(surface string) bool { return surface == GitHub || surface == Slack }

func valid(tenant, surface, id string) bool {
	return tenant != "" && known(surface) && id != "" && len(id) <= 64 && !strings.ContainsAny(id, "/\x00") && !strings.ContainsRune(tenant, 0)
}

func accountKey(surface, id string) string { return "a/" + surface + "/" + id }

func personKey(surface, subject string) string {
	sum := sha256.Sum256([]byte(subject))
	return "p/" + surface + "/" + hex.EncodeToString(sum[:])
}

// Get is the account a surface user is linked as, or ErrNotFound.
func (s *Store) Get(tenant, surface, id string) (Account, error) {
	if !valid(tenant, surface, id) {
		return Account{}, ErrNotFound
	}
	it, err := s.records.Lookup(tenant, accountKey(surface, id))
	if errors.Is(err, state.ErrNotFound) {
		return Account{}, ErrNotFound
	}
	return it.Value, err
}

// Of is the account a person linked on a surface, or ErrNotFound.
func (s *Store) Of(tenant, surface, subject string) (Account, error) {
	if tenant == "" || subject == "" || !known(surface) {
		return Account{}, ErrNotFound
	}
	p, err := s.records.Lookup(tenant, personKey(surface, subject))
	if errors.Is(err, state.ErrNotFound) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	a, err := s.Get(tenant, surface, p.Value.ID)
	if err != nil || a.Subject != subject {
		return Account{}, ErrNotFound
	}
	return a, nil
}

// Link records a as its person's account on its surface. A user another
// person linked is refused with ErrClaimed. The person's earlier account on
// the surface, if another, is unlinked in the same write; linking the same
// user again refreshes its login and avatar.
func (s *Store) Link(tenant string, a Account) (Account, error) {
	if !valid(tenant, a.Surface, a.ID) || a.Subject == "" || len(a.Subject) > 200 || len(a.Login) > 200 || len(a.Avatar) > 2048 || len(a.Team) > 64 {
		return Account{}, ErrInvalid
	}
	err := state.Retry(func() error {
		b := s.records.Batch()
		held, err := s.records.Lookup(tenant, accountKey(a.Surface, a.ID))
		switch {
		case errors.Is(err, state.ErrNotFound):
			b.Create(tenant, accountKey(a.Surface, a.ID), a)
		case err != nil:
			return err
		case held.Value.Subject != a.Subject:
			return ErrClaimed
		default:
			b.PutAt(tenant, accountKey(a.Surface, a.ID), a, held.Version)
		}
		mine, err := s.records.Lookup(tenant, personKey(a.Surface, a.Subject))
		switch {
		case errors.Is(err, state.ErrNotFound):
			b.Create(tenant, personKey(a.Surface, a.Subject), Account{ID: a.ID})
		case err != nil:
			return err
		default:
			if mine.Value.ID != a.ID {
				if err := s.dropOld(b, tenant, a.Surface, a.Subject, mine.Value.ID); err != nil {
					return err
				}
			}
			b.PutAt(tenant, personKey(a.Surface, a.Subject), Account{ID: a.ID}, mine.Version)
		}
		return b.Commit()
	})
	if err != nil {
		return Account{}, err
	}
	return a, nil
}

// dropOld deletes a person's earlier account record when it is still theirs.
func (s *Store) dropOld(b *state.Batch[Account], tenant, surface, subject, id string) error {
	old, err := s.records.Lookup(tenant, accountKey(surface, id))
	if errors.Is(err, state.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if old.Value.Subject == subject {
		b.DeleteAt(tenant, accountKey(surface, id), old.Version)
	}
	return nil
}

// Unlink removes a person's account on a surface. Unlinking nothing is not
// an error.
func (s *Store) Unlink(tenant, surface, subject string) error {
	if tenant == "" || subject == "" || !known(surface) {
		return ErrInvalid
	}
	return state.Retry(func() error {
		mine, err := s.records.Lookup(tenant, personKey(surface, subject))
		if errors.Is(err, state.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		b := s.records.Batch()
		if err := s.dropOld(b, tenant, surface, subject, mine.Value.ID); err != nil {
			return err
		}
		b.DeleteAt(tenant, personKey(surface, subject), mine.Version)
		return b.Commit()
	})
}

// List is up to limit of a workspace's accounts on a surface, by user id.
func (s *Store) List(tenant, surface string, limit int) ([]Account, error) {
	if tenant == "" || !known(surface) {
		return nil, ErrInvalid
	}
	limit = min(max(limit, 1), MaxList)
	items, _, err := s.records.List(tenant, state.Query{Prefix: "a/" + surface + "/", Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]Account, 0, len(items))
	for _, it := range items {
		out = append(out, it.Value)
	}
	return out, nil
}
