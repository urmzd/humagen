// Package sessionstate persists orchagent session identities in Humagen's shared state backend.
package sessionstate

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/orchagent/flow/session"
)

const namespace = "agent-sessions"

type Store struct{ state state.Store }

var _ session.Store = (*Store)(nil)

func New(st state.Store) *Store {
	if st == nil {
		st = state.NewMemory()
	}
	return &Store{state: st}
}
func hash(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:16]) }
func put(tenant, key string, v any, version int64) state.Op {
	raw, _ := json.Marshal(v)
	o := state.Put(tenant, key, raw)
	o.If = version
	return o
}
func (s *Store) Get(ctx context.Context, tenant, id string) (session.Record, error) {
	row, err := s.state.Lookup(ctx, namespace, tenant, "s:"+id)
	if errors.Is(err, state.ErrNotFound) {
		return session.Record{}, session.ErrNotFound
	}
	if err != nil {
		return session.Record{}, err
	}
	var r session.Record
	err = json.Unmarshal(row.Value, &r)
	return r, err
}
func (s *Store) Open(ctx context.Context, tenant string, o session.Open) (session.Record, error) {
	k := session.Key{Tenant: tenant, Parent: o.Parent, Agent: o.Agent, Label: o.Label}.Normalize()
	if o.Fresh {
		var b [12]byte
		if _, err := rand.Read(b[:]); err != nil {
			return session.Record{}, err
		}
		k.Label = hex.EncodeToString(b[:])
	}
	if err := k.Validate(); err != nil {
		return session.Record{}, err
	}
	r := session.Record{ID: k.ID(), Key: k, Channel: o.Channel, CreatedAt: time.Now().UTC()}
	if strings.HasPrefix(k.Parent, "s_") {
		p, err := s.Get(ctx, tenant, k.Parent)
		if err != nil {
			return r, err
		}
		r.Depth = p.Depth + 1
		if r.Depth > 64 {
			return session.Record{}, fmt.Errorf("%w: maximum session depth is 64", session.ErrInvalid)
		}
	}
	if r.Channel == "" {
		r.Channel = "session://" + tenant + "/" + r.ID
	}
	err := s.state.Write(ctx, namespace, put(tenant, "s:"+r.ID, r, state.Missing), put(tenant, "p:"+hash(k.Parent)+":"+r.ID, r, state.Missing), put(tenant, "a:"+hash(k.Agent)+":"+r.ID, r, state.Missing))
	if errors.Is(err, state.ErrConflict) {
		return s.Get(ctx, tenant, r.ID)
	}
	return r, err
}
func (s *Store) List(ctx context.Context, tenant string, f session.Filter) ([]session.Record, error) {
	prefix := "s:"
	if f.Parent != "" {
		prefix = "p:" + hash(f.Parent) + ":"
	} else if f.Agent != "" {
		prefix = "a:" + hash(f.Agent) + ":"
	}
	n := f.Limit
	if n <= 0 || n > session.MaxPage {
		n = session.MaxPage
	}
	out := []session.Record{}
	after := ""
	// At most 50 pages are scanned, matching the workspace's 10,000 session cap.
	for page := 0; page < 50 && len(out) < n; page++ {
		rows, next, err := s.state.List(ctx, namespace, tenant, state.Query{Prefix: prefix, After: after, Limit: session.MaxPage})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			var r session.Record
			if err := json.Unmarshal(row.Value, &r); err != nil {
				return nil, err
			}
			if (f.Agent == "" || r.Key.Agent == f.Agent) && (f.Parent == "" || r.Key.Parent == f.Parent) {
				out = append(out, r)
				if len(out) == n {
					break
				}
			}
		}
		if next == "" {
			break
		}
		after = next
	}
	return out, nil
}
func (s *Store) Link(ctx context.Context, tenant, id string, l session.Link) error {
	if l.Kind != "context" && l.Kind != "evidence" {
		return fmt.Errorf("%w: link kind is context or evidence", session.ErrInvalid)
	}
	if _, err := s.Get(ctx, tenant, id); err != nil {
		return err
	}
	if _, err := s.Get(ctx, tenant, l.Target); err != nil {
		return err
	}
	if id == l.Target {
		return fmt.Errorf("%w: a session cannot link itself", session.ErrInvalid)
	}
	l.CreatedAt = time.Now().UTC()
	err := s.state.Write(ctx, namespace, put(tenant, "l:"+id+":"+l.Target, l, state.Missing))
	if errors.Is(err, state.ErrConflict) {
		return nil
	}
	return err
}
func (s *Store) Links(ctx context.Context, tenant, id string, limit int) ([]session.Link, error) {
	if _, err := s.Get(ctx, tenant, id); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > session.MaxPage {
		limit = session.MaxPage
	}
	rows, _, err := s.state.List(ctx, namespace, tenant, state.Query{Prefix: "l:" + id + ":", Limit: limit})
	if err != nil {
		return nil, err
	}
	out := []session.Link{}
	for _, row := range rows {
		var l session.Link
		if err := json.Unmarshal(row.Value, &l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}
func (s *Store) BindRun(ctx context.Context, tenant, id, run string) error {
	if run == "" {
		return session.ErrInvalid
	}
	if _, err := s.Get(ctx, tenant, id); err != nil {
		return err
	}
	err := s.state.Write(ctx, namespace, put(tenant, "r:"+id+":"+run, run, state.Missing))
	if errors.Is(err, state.ErrConflict) {
		return nil
	}
	return err
}
func (s *Store) Runs(ctx context.Context, tenant, id string, limit int) ([]string, error) {
	if _, err := s.Get(ctx, tenant, id); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > session.MaxPage {
		limit = session.MaxPage
	}
	rows, _, err := s.state.List(ctx, namespace, tenant, state.Query{Prefix: "r:" + id + ":", Limit: limit})
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, row := range rows {
		var id string
		if err := json.Unmarshal(row.Value, &id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}
