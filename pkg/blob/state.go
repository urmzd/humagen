package blob

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

// stateNamespace is where State keeps bytes.
const stateNamespace = "blobs"

// State keeps bytes as records of a state.Store, so they are kept wherever
// the platform's records are. It is for small content, memory files, where a
// deployment has a database and no bucket: List reads the whole namespace.
type State struct {
	st  state.Store
	now func() time.Time
}

// NewState returns a store on st.
func NewState(st state.Store) *State { return &State{st: st, now: time.Now} }

// A record is the time it was written, as Unix nanoseconds, then the bytes.
func encode(at time.Time, b []byte) []byte {
	out := make([]byte, 8, 8+len(b))
	binary.BigEndian.PutUint64(out, uint64(at.UnixNano()))
	return append(out, b...)
}

func decode(key string, v []byte) ([]byte, Info, error) {
	if len(v) < 8 {
		return nil, Info{}, fmt.Errorf("blob: record %q is damaged", key)
	}
	b := v[8:]
	at := time.Unix(0, int64(binary.BigEndian.Uint64(v[:8]))).UTC()
	return b, Info{Key: key, Size: int64(len(b)), SHA256: sum(b), At: at}, nil
}

// Put implements Store.
func (s *State) Put(ctx context.Context, tenant, key string, r io.Reader, max int64) (Info, error) {
	if err := check(tenant, key); err != nil {
		return Info{}, err
	}
	b, err := read(r, max)
	if err != nil {
		return Info{}, err
	}
	at := s.now()
	if err := s.st.Write(ctx, stateNamespace, state.Put(tenant, key, encode(at, b))); err != nil {
		return Info{}, fmt.Errorf("blob: %w", err)
	}
	return Info{Key: key, Size: int64(len(b)), SHA256: sum(b), At: at.UTC()}, nil
}

func (s *State) load(ctx context.Context, tenant, key string) ([]byte, Info, error) {
	if err := check(tenant, key); err != nil {
		return nil, Info{}, err
	}
	v, err := s.st.Get(ctx, stateNamespace, tenant, key)
	if errors.Is(err, state.ErrNotFound) {
		return nil, Info{}, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	if err != nil {
		return nil, Info{}, fmt.Errorf("blob: %w", err)
	}
	return decode(key, v)
}

// Get implements Store.
func (s *State) Get(ctx context.Context, tenant, key string) (io.ReadCloser, Info, error) {
	b, info, err := s.load(ctx, tenant, key)
	if err != nil {
		return nil, Info{}, err
	}
	return io.NopCloser(bytes.NewReader(b)), info, nil
}

// Stat implements Store.
func (s *State) Stat(ctx context.Context, tenant, key string) (Info, error) {
	_, info, err := s.load(ctx, tenant, key)
	return info, err
}

// Delete implements Store.
func (s *State) Delete(ctx context.Context, tenant, key string) error {
	if _, err := s.Stat(ctx, tenant, key); err != nil {
		return err
	}
	if err := s.st.Write(ctx, stateNamespace, state.Delete(tenant, key)); err != nil {
		return fmt.Errorf("blob: %w", err)
	}
	return nil
}

// List implements Store.
func (s *State) List(ctx context.Context, tenant, prefix string) ([]Info, error) {
	if err := check(tenant, "x"); err != nil {
		return nil, err
	}
	recs, err := s.st.Load(ctx, stateNamespace)
	if err != nil {
		return nil, fmt.Errorf("blob: %w", err)
	}
	var out []Info
	for _, r := range recs {
		if r.Tenant != tenant || !strings.HasPrefix(r.Key, prefix) {
			continue
		}
		_, info, err := decode(r.Key, r.Value)
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	slices.SortFunc(out, func(a, b Info) int { return strings.Compare(a.Key, b.Key) })
	return out, nil
}
