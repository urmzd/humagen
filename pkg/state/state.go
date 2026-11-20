// Package state is where humagen's own stores keep what must outlive the
// process: small documents, each named by a namespace, a tenant and a key.
//
// It holds no domain logic. Every store built on it reads and writes per
// request: it treats the database as the truth, reads one record (Lookup),
// a bounded set of a tenant's keys at once (LookupMany) or one bounded page
// of them (List), and changes a record only at
// the version it read (Op.If), retrying on ErrConflict. A cache in front of it
// may be dropped at any time without changing an answer. So any number of
// processes on one database agree, and OpenPostgres takes no lock. The
// backend is a choice of configuration: Memory, which is lost with the
// process, or Postgres.
//
// Load reads a whole namespace across tenants. It is for a store moving
// records an older release kept into its layout, once, never for a request.
//
// Table carries the pieces such a store is made of: Update changes one record
// by compare-and-set, Retry runs a change over several records again when it
// conflicts, All reads a capped collection, and Tenants is the directory a
// background loop pages instead of reading a namespace across tenants.
//
// Table carries the pieces such a store is made of: Update changes one record
// by compare-and-set, Retry runs a change over several records again when it
// conflicts, All reads a capped collection, and Tenants is the directory a
// background loop pages instead of reading a namespace across tenants.
//
// # Shard keys
//
// The tenant is the shard key of every record, and every key is laid out so
// that the reads a request makes name one tenant:
//
//   - A request reads one tenant. Lookup and List take one; only Load, which
//     a conversion uses once, reads across tenants.
//   - A key starts with what a request pages by, so the page is a prefix: a
//     conversation's messages under "m:<conversation>:", ordered by Seq.
//   - A Write changes one tenant. A link to another tenant's record is its
//     id, and work that spans tenants is a run that retries, never one
//     transaction.
//
// # Write-through
//
// A mutation that cannot be written is a mutation that did not happen: a
// store writes before it reports success. Nothing is accepted that a restart
// would lose.
package state

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Errors a caller can match.
var (
	// ErrNotFound reports a key with no record.
	ErrNotFound = errors.New("state: not found")
	// ErrInvalid reports a namespace, tenant or key the store cannot keep,
	// or a query it will not run.
	ErrInvalid = errors.New("state: invalid")
	// ErrConflict reports a conditional write whose record was not at the
	// version it named. Nothing in the write was applied.
	ErrConflict = errors.New("state: conflict")
)

// Missing is the version a conditional op names to require that its key has
// no record.
const Missing int64 = -1

// MaxList is the most records one List returns.
const MaxList = 1000

// Record is one stored document.
type Record struct {
	Tenant string
	Key    string
	Value  []byte
	// Version is 1 when the record is first written, and one more on every
	// write after. A conditional op names it.
	Version int64
}

// Op is one change in a Write: a put, or a delete when Value is nil.
type Op struct {
	Tenant string
	Key    string
	Value  []byte
	// If makes the op conditional: it applies only when the key's record is
	// at this version, or, for Missing, when the key has no record. Zero
	// applies it whatever is there. A Write whose condition fails applies
	// nothing and returns ErrConflict.
	If int64
}

// Query selects one page of a tenant's records in a namespace, ordered by key
// as bytes.
type Query struct {
	// Prefix keeps only keys that start with it.
	Prefix string
	// After keeps only keys past it in the order of the page: greater, or
	// with Desc less. A page's next cursor is its last key.
	After string
	// Limit is how many records the page holds at most, from 1 to MaxList.
	// A larger limit is cut to MaxList; zero or less is ErrInvalid.
	Limit int
	// Desc pages from the greatest key down.
	Desc bool
}

// Put returns an op that stores value under a key.
func Put(tenant, key string, value []byte) Op {
	if value == nil {
		value = []byte{}
	}
	return Op{Tenant: tenant, Key: key, Value: value}
}

// Delete returns an op that removes a key. Removing a key with no record is
// not an error.
func Delete(tenant, key string) Op { return Op{Tenant: tenant, Key: key} }

// Create returns an op that stores value under a key that has no record, and
// fails the write with ErrConflict when it has one.
func Create(tenant, key string, value []byte) Op {
	op := Put(tenant, key, value)
	op.If = Missing
	return op
}

// PutAt returns an op that replaces the record at version, and fails the
// write with ErrConflict when the record has moved on or is gone.
func PutAt(tenant, key string, value []byte, version int64) Op {
	op := Put(tenant, key, value)
	op.If = version
	return op
}

// DeleteAt returns an op that removes the record at version, and fails the
// write with ErrConflict when the record has moved on or is gone.
func DeleteAt(tenant, key string, version int64) Op {
	return Op{Tenant: tenant, Key: key, If: version}
}

// Store keeps records by namespace, tenant and key.
//
// Every implementation is held to the suite in statetest.
type Store interface {
	// Get returns one record's value, or ErrNotFound.
	Get(ctx context.Context, namespace, tenant, key string) ([]byte, error)
	// Lookup returns one record with its version, or ErrNotFound.
	Lookup(ctx context.Context, namespace, tenant, key string) (Record, error)
	// List returns one page of a tenant's records in a namespace, and the
	// cursor of the next page: the last key, when more may follow, or ""
	// when none do.
	List(ctx context.Context, namespace, tenant string, q Query) ([]Record, string, error)
	// Load returns every record in a namespace, across tenants, ordered by
	// tenant and then key, compared as bytes. Only a conversion of records an
	// older release kept calls it, once.
	Load(ctx context.Context, namespace string) ([]Record, error)
	// Write applies ops in order, all or none. A conditional op whose record
	// is not at its version fails the write with ErrConflict.
	Write(ctx context.Context, namespace string, ops ...Op) error
}

// Many is a Store that reads several of one tenant's keys in one statement.
// Both backends are one; LookupMany asks any Store.
type Many interface {
	// LookupMany returns the records of the keys that have one, in no
	// order. A key with no record is left out.
	LookupMany(ctx context.Context, namespace, tenant string, keys []string) ([]Record, error)
}

// LookupMany reads several of one tenant's keys at once: the records of the
// keys that have one, in no order, a key with none left out. It is the read
// for a page of rows that each name a record, in place of a Lookup a row. At
// most MaxList keys are read at once; more is ErrInvalid. A Store that is not
// a Many is asked for each key in turn.
func LookupMany(ctx context.Context, st Store, namespace, tenant string, keys []string) ([]Record, error) {
	if err := checkNames(namespace, nil); err != nil {
		return nil, err
	}
	if len(keys) > MaxList {
		return nil, errors.Join(ErrInvalid, errors.New("state: too many keys in one read"))
	}
	seen := make(map[string]bool, len(keys))
	uniq := make([]string, 0, len(keys))
	for _, k := range keys {
		if k == "" || !utf8.ValidString(k) || strings.ContainsRune(k, 0) {
			return nil, errors.Join(ErrInvalid, errors.New("state: a key is required, as UTF-8 without a NUL"))
		}
		if !seen[k] {
			seen[k] = true
			uniq = append(uniq, k)
		}
	}
	if len(uniq) == 0 {
		return nil, nil
	}
	if m, ok := st.(Many); ok {
		return m.LookupMany(ctx, namespace, tenant, uniq)
	}
	out := make([]Record, 0, len(uniq))
	for _, k := range uniq {
		r, err := st.Lookup(ctx, namespace, tenant, k)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func checkNames(namespace string, ops []Op) error {
	if namespace == "" {
		return errors.Join(ErrInvalid, errors.New("state: a namespace is required"))
	}
	if strings.ContainsRune(namespace, 0) {
		return errors.Join(ErrInvalid, errors.New("state: a name cannot hold a NUL"))
	}
	for _, op := range ops {
		if op.Key == "" {
			return errors.Join(ErrInvalid, errors.New("state: a key is required"))
		}
		if strings.ContainsRune(op.Key, 0) || strings.ContainsRune(op.Tenant, 0) {
			return errors.Join(ErrInvalid, errors.New("state: a name cannot hold a NUL"))
		}
		if op.If < Missing {
			return errors.Join(ErrInvalid, errors.New("state: a version is Missing or at least 1"))
		}
		if op.If == Missing && op.Value == nil {
			return errors.Join(ErrInvalid, errors.New("state: a delete names the version it removes"))
		}
	}
	return nil
}

// checkQuery checks a List's names and query, and returns its limit.
func checkQuery(namespace string, q Query) (int, error) {
	if err := checkNames(namespace, nil); err != nil {
		return 0, err
	}
	if q.Limit <= 0 {
		return 0, errors.Join(ErrInvalid, errors.New("state: a list names its limit"))
	}
	if !utf8.ValidString(q.Prefix) || !utf8.ValidString(q.After) {
		return 0, errors.Join(ErrInvalid, errors.New("state: a prefix or cursor is not UTF-8"))
	}
	return min(q.Limit, MaxList), nil
}

// prefixEnd returns the least string greater than every string that starts
// with prefix, compared as bytes, and false when there is none. UTF-8 orders
// as its code points do, so the last code point that can grow, grown by one,
// is that bound.
func prefixEnd(prefix string) (string, bool) {
	rs := []rune(prefix)
	for i := len(rs) - 1; i >= 0; i-- {
		r := rs[i] + 1
		if r == 0xD800 {
			r = 0xE000 // past the surrogates, which UTF-8 cannot hold
		}
		if r <= unicode.MaxRune {
			rs[i] = r
			return string(rs[:i+1]), true
		}
	}
	return "", false
}
