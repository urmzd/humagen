// Package accounts keeps the accounts people linked to themselves on the
// surfaces Humagen answers on: a GitHub user, a Slack user. A link says that
// one person in a workspace is that surface's user. The store records it and
// decides nothing: what reads a link decides what it means.
//
// One record per surface user is the claim: a surface user is linked to one
// person in a workspace, and a second person's claim is refused until the
// first is unlinked. A Slack user is keyed by user and team, since an
// Enterprise Grid user (W...) keeps one id across the grid's teams. One
// record per person, surface and team points back at it, so a person has one
// GitHub account and one Slack account in each team. Both are written together, at the
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
// holding only the ID and Team of the user they linked, so both kinds of
// record are written in one batch.
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

// MaxPerPerson is the most accounts a person keeps on one surface: one on
// GitHub, one per Slack team.
const MaxPerPerson = 20

func known(surface string) bool { return surface == GitHub || surface == Slack }

func part(v string, empty bool) bool {
	return (v != "" || empty) && len(v) <= 64 && !strings.ContainsAny(v, "/\x00")
}

// valid reports a surface user the store can key: a Slack user names its
// team, a GitHub user names none.
func valid(tenant, surface, team, id string) bool {
	return tenant != "" && !strings.ContainsRune(tenant, 0) && known(surface) && part(id, false) &&
		(surface == Slack && part(team, false) || surface == GitHub && team == "")
}

func accountKey(surface, team, id string) string {
	if surface == Slack {
		return "a/" + surface + "/" + team + "/" + id
	}
	return "a/" + surface + "/" + id
}

func personPrefix(surface, subject string) string {
	sum := sha256.Sum256([]byte(subject))
	return "p/" + surface + "/" + hex.EncodeToString(sum[:]) + "/"
}

func personKey(surface, team, subject string) string {
	return personPrefix(surface, subject) + "t-" + team
}

// Get is the account a surface user is linked as, or ErrNotFound. team is
// the Slack team, empty on GitHub. A nil Store has none.
func (s *Store) Get(tenant, surface, team, id string) (Account, error) {
	if s == nil || !valid(tenant, surface, team, id) {
		return Account{}, ErrNotFound
	}
	it, err := s.records.Lookup(tenant, accountKey(surface, team, id))
	if errors.Is(err, state.ErrNotFound) {
		return Account{}, ErrNotFound
	}
	return it.Value, err
}

// Mine is every account a person linked on a surface: at most one on GitHub,
// one per Slack team.
func (s *Store) Mine(tenant, surface, subject string) ([]Account, error) {
	if s == nil || tenant == "" || subject == "" || !known(surface) {
		return nil, nil
	}
	ptrs, _, err := s.records.List(tenant, state.Query{Prefix: personPrefix(surface, subject), Limit: MaxPerPerson})
	if err != nil {
		return nil, err
	}
	var out []Account
	for _, p := range ptrs {
		a, err := s.Get(tenant, surface, p.Value.Team, p.Value.ID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if a.Subject == subject {
			out = append(out, a)
		}
	}
	return out, nil
}

// Of is the first account a person linked on a surface, or ErrNotFound.
func (s *Store) Of(tenant, surface, subject string) (Account, error) {
	all, err := s.Mine(tenant, surface, subject)
	if err != nil {
		return Account{}, err
	}
	if len(all) == 0 {
		return Account{}, ErrNotFound
	}
	return all[0], nil
}

// Link records a as its person's account on its surface (and Slack team). A
// user another person linked is refused with ErrClaimed. The person's earlier
// account there, if another, is unlinked in the same write; linking the same
// user again refreshes its login and avatar. Linking in another Slack team
// leaves the first team's link alone.
func (s *Store) Link(tenant string, a Account) (Account, error) {
	if s == nil || !valid(tenant, a.Surface, a.Team, a.ID) || a.Subject == "" || len(a.Subject) > 200 || len(a.Login) > 200 || len(a.Avatar) > 2048 {
		return Account{}, ErrInvalid
	}
	err := state.Retry(func() error {
		b := s.records.Batch()
		key := accountKey(a.Surface, a.Team, a.ID)
		held, err := s.records.Lookup(tenant, key)
		switch {
		case errors.Is(err, state.ErrNotFound):
			b.Create(tenant, key, a)
		case err != nil:
			return err
		case held.Value.Subject != a.Subject:
			return ErrClaimed
		default:
			b.PutAt(tenant, key, a, held.Version)
		}
		pkey := personKey(a.Surface, a.Team, a.Subject)
		mine, err := s.records.Lookup(tenant, pkey)
		switch {
		case errors.Is(err, state.ErrNotFound):
			b.Create(tenant, pkey, Account{ID: a.ID, Team: a.Team})
		case err != nil:
			return err
		default:
			if mine.Value.ID != a.ID {
				if err := s.dropOld(b, tenant, a.Surface, a.Team, a.Subject, mine.Value.ID); err != nil {
					return err
				}
			}
			b.PutAt(tenant, pkey, Account{ID: a.ID, Team: a.Team}, mine.Version)
		}
		return b.Commit()
	})
	if err != nil {
		return Account{}, err
	}
	return a, nil
}

// dropOld deletes a person's earlier account record when it is still theirs.
func (s *Store) dropOld(b *state.Batch[Account], tenant, surface, team, subject, id string) error {
	old, err := s.records.Lookup(tenant, accountKey(surface, team, id))
	if errors.Is(err, state.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if old.Value.Subject == subject {
		b.DeleteAt(tenant, accountKey(surface, team, id), old.Version)
	}
	return nil
}

// Unlink removes every account a person linked on a surface. Unlinking
// nothing is not an error.
func (s *Store) Unlink(tenant, surface, subject string) error {
	if s == nil || tenant == "" || subject == "" || !known(surface) {
		return ErrInvalid
	}
	// A page at a time, until a page is not full: nothing of theirs is left
	// behind however many they linked.
	for {
		full := false
		err := state.Retry(func() error {
			ptrs, _, err := s.records.List(tenant, state.Query{Prefix: personPrefix(surface, subject), Limit: MaxPerPerson})
			if err != nil {
				return err
			}
			full = len(ptrs) == MaxPerPerson
			b := s.records.Batch()
			for _, p := range ptrs {
				if err := s.dropOld(b, tenant, surface, p.Value.Team, subject, p.Value.ID); err != nil {
					return err
				}
				b.DeleteAt(tenant, p.Key, p.Version)
			}
			if b.Len() == 0 {
				return nil
			}
			return b.Commit()
		})
		if err != nil || !full {
			return err
		}
	}
}

// Remove unlinks one surface user from whoever linked it, as an
// administrator does for someone who left, and returns the account it
// removed. A user nobody linked is ErrNotFound.
func (s *Store) Remove(tenant, surface, team, id string) (Account, error) {
	if s == nil || !valid(tenant, surface, team, id) {
		return Account{}, ErrNotFound
	}
	var removed Account
	err := state.Retry(func() error {
		held, err := s.records.Lookup(tenant, accountKey(surface, team, id))
		if errors.Is(err, state.ErrNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		b := s.records.Batch()
		b.DeleteAt(tenant, held.Key, held.Version)
		pkey := personKey(surface, team, held.Value.Subject)
		if mine, err := s.records.Lookup(tenant, pkey); err == nil && mine.Value.ID == id {
			b.DeleteAt(tenant, pkey, mine.Version)
		} else if err != nil && !errors.Is(err, state.ErrNotFound) {
			return err
		}
		removed = held.Value
		return b.Commit()
	})
	return removed, err
}

// List is up to limit of a workspace's accounts on a surface, by user id. A
// Slack team narrows it to that team's; on GitHub team is empty.
func (s *Store) List(tenant, surface, team string, limit int) ([]Account, error) {
	if s == nil {
		return []Account{}, nil
	}
	if tenant == "" || !known(surface) || !part(team, true) {
		return nil, ErrInvalid
	}
	prefix := "a/" + surface + "/"
	if surface == Slack && team != "" {
		prefix += team + "/"
	}
	limit = min(max(limit, 1), MaxList)
	items, _, err := s.records.List(tenant, state.Query{Prefix: prefix, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]Account, 0, len(items))
	for _, it := range items {
		out = append(out, it.Value)
	}
	return out, nil
}
