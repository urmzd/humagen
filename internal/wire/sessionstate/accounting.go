package sessionstate

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/session"
)

var _ session.UsageStore = (*Store)(nil)
var _ session.BudgetStore = (*Store)(nil)

func (s *Store) readValue(ctx context.Context, tenant, key string, value any) (int64, error) {
	r, err := s.state.Lookup(ctx, namespace, tenant, key)
	if errors.Is(err, state.ErrNotFound) {
		return state.Missing, nil
	}
	if err != nil {
		return 0, err
	}
	return r.Version, json.Unmarshal(r.Value, value)
}
func (s *Store) RecordUsage(ctx context.Context, tenant, id string, c event.UsageCall) error {
	if c.AccountingID == "" {
		return session.ErrInvalid
	}
	chain := []session.Record{}
	for depth := 0; depth <= 64; depth++ {
		r, err := s.Get(ctx, tenant, id)
		if err != nil {
			return err
		}
		chain = append(chain, r)
		if r.Depth == 0 {
			break
		}
		id = r.Key.Parent
		if depth == 64 {
			return session.ErrInvalid
		}
	}
	return state.Retry(func() error {
		ops := []state.Op{}
		for i, r := range chain {
			dedupe := "usage-id:" + r.ID + ":" + hash(c.AccountingID)
			if _, err := s.state.Lookup(ctx, namespace, tenant, dedupe); err == nil {
				continue
			} else if !errors.Is(err, state.ErrNotFound) {
				return err
			}
			key := "usage:" + r.ID
			var u event.RunUsage
			v, err := s.readValue(ctx, tenant, key, &u)
			if err != nil {
				return err
			}
			if i == 0 {
				u.Own = u.Own.AddCall(c)
			} else {
				u.Children = u.Children.AddCall(c)
			}
			u.Total = u.Own.Add(u.Children)
			ops = append(ops, put(tenant, key, u, v), put(tenant, dedupe, true, state.Missing))
		}
		if len(ops) == 0 {
			return nil
		}
		return s.state.Write(ctx, namespace, ops...)
	})
}
func (s *Store) Usage(ctx context.Context, tenant, id string) (event.RunUsage, error) {
	var u event.RunUsage
	if _, err := s.Get(ctx, tenant, id); err != nil {
		return u, err
	}
	_, err := s.readValue(ctx, tenant, "usage:"+id, &u)
	return u, err
}
func (s *Store) OpenBudget(ctx context.Context, tenant, id string, b session.Budget) error {
	if tenant == "" || id == "" || !b.Valid() {
		return session.ErrBudget
	}
	err := s.state.Write(ctx, namespace, put(tenant, "budget:"+hash(id), session.Account{Limit: b, Claims: map[string]session.BudgetClaim{}}, state.Missing))
	if !errors.Is(err, state.ErrConflict) {
		return err
	}
	a, err := s.BudgetAccount(ctx, tenant, id)
	if err == nil && a.Limit != b {
		return session.ErrInvalid
	}
	return err
}
func (s *Store) BudgetAccount(ctx context.Context, tenant, id string) (session.Account, error) {
	var a session.Account
	v, err := s.readValue(ctx, tenant, "budget:"+hash(id), &a)
	if err == nil && v == state.Missing {
		err = session.ErrNotFound
	}
	return a, err
}
func (s *Store) updateBudget(ctx context.Context, tenant, id string, fn func(*session.Account) error) error {
	return state.Retry(func() error {
		var a session.Account
		v, err := s.readValue(ctx, tenant, "budget:"+hash(id), &a)
		if err != nil {
			return err
		}
		if v == state.Missing {
			return session.ErrNotFound
		}
		if err = fn(&a); err != nil {
			return err
		}
		return s.state.Write(ctx, namespace, put(tenant, "budget:"+hash(id), a, v))
	})
}
func (s *Store) ReserveBudget(ctx context.Context, tenant, id, claim string, want session.Budget) (session.Budget, error) {
	var got session.Budget
	err := s.updateBudget(ctx, tenant, id, func(a *session.Account) error {
		if _, exists := a.Claims[claim]; !exists && len(a.Claims) >= 10000 {
			return session.ErrLimit
		}
		var err error
		got, err = a.Reserve(claim, want)
		return err
	})
	return got, err
}
func (s *Store) SettleBudget(ctx context.Context, tenant, id, claim string, actual session.Budget) error {
	return s.updateBudget(ctx, tenant, id, func(a *session.Account) error { return a.Settle(claim, actual) })
}

func (s *Store) CloseBudget(ctx context.Context, tenant, id string) error {
	return s.updateBudget(ctx, tenant, id, func(a *session.Account) error { a.Closed = true; return nil })
}
