// Package state is where warp's own stores keep what must outlive the
// process: small documents, each named by a namespace, a tenant and a key.
//
// It holds no domain logic. Each store (the inbox, the alert engine, the run
// index and the rest) keeps its rules and its indexes in memory, as its read
// model, and writes the records that state is built from through a Store on
// every change. At construction it loads them back. So a store's behavior is
// the same whichever backend is under it, and the backend is a choice of
// configuration: Memory, which is lost with the process, or Postgres.
//
// # One writer
//
// A store reads the database once, when it is built, and from then on trusts
// its memory. Two processes writing one database would each be blind to the
// other's writes, so the model is one warp process per database. Postgres
// enforces it: it holds an advisory lock for as long as it is open, and a
// second process opening the same schema is refused with ErrLocked rather
// than allowed to diverge quietly.
//
// # Write-through
//
// A mutation that cannot be written is a mutation that did not happen: a
// store writes before it reports success, and undoes its change in memory when
// the write fails. Nothing is accepted in memory that a restart would lose.
package state

import (
	"context"
	"errors"
)

// Errors a caller can match.
var (
	// ErrNotFound reports a key with no record.
	ErrNotFound = errors.New("state: not found")
	// ErrLocked reports a database another process already writes to.
	ErrLocked = errors.New("state: another process holds this database")
	// ErrInvalid reports a namespace, tenant or key the store cannot keep.
	ErrInvalid = errors.New("state: invalid")
)

// Record is one stored document.
type Record struct {
	Tenant string
	Key    string
	Value  []byte
}

// Op is one change in a Write: a put, or a delete when Value is nil.
type Op struct {
	Tenant string
	Key    string
	Value  []byte
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

// Store keeps records by namespace, tenant and key.
//
// Every implementation is held to the suite in statetest.
type Store interface {
	// Get returns one record's value, or ErrNotFound.
	Get(ctx context.Context, namespace, tenant, key string) ([]byte, error)
	// Load returns every record in a namespace, across tenants, ordered by
	// tenant and then key, compared as bytes.
	Load(ctx context.Context, namespace string) ([]Record, error)
	// Write applies ops in order, all or none.
	Write(ctx context.Context, namespace string, ops ...Op) error
}

func checkNames(namespace string, ops []Op) error {
	if namespace == "" {
		return errors.Join(ErrInvalid, errors.New("state: a namespace is required"))
	}
	for _, op := range ops {
		if op.Key == "" {
			return errors.Join(ErrInvalid, errors.New("state: a key is required"))
		}
	}
	return nil
}
