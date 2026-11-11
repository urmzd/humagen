package sessionstate

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/saige/flow/session"
)

var _ session.AdmissionStore = (*Store)(nil)
var _ session.AccessStore = (*Store)(nil)

func (s *Store) reservations(ctx context.Context, tenant, parent string) ([]session.Reservation, int64, error) {
	row, err := s.state.Lookup(ctx, namespace, tenant, "reservations:"+parent)
	if errors.Is(err, state.ErrNotFound) {
		return []session.Reservation{}, state.Missing, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var rows []session.Reservation
	err = json.Unmarshal(row.Value, &rows)
	return rows, row.Version, err
}
func (s *Store) Reservations(ctx context.Context, tenant, parent string) ([]session.Reservation, error) {
	if _, err := s.Get(ctx, tenant, parent); err != nil {
		return nil, err
	}
	rows, _, err := s.reservations(ctx, tenant, parent)
	return rows, err
}
func (s *Store) updateReservations(ctx context.Context, tenant, parent string, f func([]session.Reservation) ([]session.Reservation, error)) error {
	if _, err := s.Get(ctx, tenant, parent); err != nil {
		return err
	}
	return state.Retry(func() error {
		rows, version, err := s.reservations(ctx, tenant, parent)
		if err != nil {
			return err
		}
		rows, err = f(rows)
		if err != nil {
			return err
		}
		return s.state.Write(ctx, namespace, put(tenant, "reservations:"+parent, rows, version))
	})
}
func (s *Store) Reserve(ctx context.Context, tenant, parent, call, child string, max int) error {
	if call == "" || max < 1 || max > 64 {
		return session.ErrInvalid
	}
	c, err := s.Get(ctx, tenant, child)
	if err != nil {
		return err
	}
	if c.Key.Parent != parent {
		return session.ErrDenied
	}
	return s.updateReservations(ctx, tenant, parent, func(rows []session.Reservation) ([]session.Reservation, error) {
		for _, r := range rows {
			if r.Call == call {
				if r.Child != child {
					return nil, session.ErrInvalid
				}
				return rows, nil
			}
		}
		if len(rows) >= max {
			return nil, session.ErrLimit
		}
		return append(rows, session.Reservation{Call: call, Child: child}), nil
	})
}
func (s *Store) BindReservation(ctx context.Context, tenant, parent, call, run string) error {
	if run == "" {
		return session.ErrInvalid
	}
	return s.updateReservations(ctx, tenant, parent, func(rows []session.Reservation) ([]session.Reservation, error) {
		for i, r := range rows {
			if r.Call == call {
				if r.Run != "" && r.Run != run {
					return nil, session.ErrInvalid
				}
				rows[i].Run = run
				return rows, nil
			}
		}
		return nil, session.ErrNotFound
	})
}
func (s *Store) Release(ctx context.Context, tenant, parent, call string) error {
	return s.updateReservations(ctx, tenant, parent, func(rows []session.Reservation) ([]session.Reservation, error) {
		return slices.DeleteFunc(rows, func(r session.Reservation) bool { return r.Call == call }), nil
	})
}
func (s *Store) GrantRead(ctx context.Context, tenant, target, reader string) error {
	if _, err := s.Get(ctx, tenant, target); err != nil {
		return err
	}
	if _, err := s.Get(ctx, tenant, reader); err != nil {
		return err
	}
	return s.state.Write(ctx, namespace, put(tenant, "grant:"+target+":"+reader, true, 0))
}
func (s *Store) RevokeRead(ctx context.Context, tenant, target, reader string) error {
	return s.state.Write(ctx, namespace, state.Delete(tenant, "grant:"+target+":"+reader))
}
func (s *Store) MayRead(ctx context.Context, tenant, target, reader string) (bool, error) {
	if _, err := s.Get(ctx, tenant, target); err != nil {
		return false, err
	}
	if _, err := s.Get(ctx, tenant, reader); err != nil {
		return false, err
	}
	_, err := s.state.Lookup(ctx, namespace, tenant, "grant:"+target+":"+reader)
	if errors.Is(err, state.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}
