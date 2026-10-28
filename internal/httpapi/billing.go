package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/usage"
)

// Billing policies are a workspace's limits. Anyone in the workspace can read
// them and what has been used against them. Only an admin can change them.
func (s *Server) billingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/policies", s.authed(auth.BillingRead, s.listPolicies))
	mux.HandleFunc("PUT /api/policies", s.authed(auth.BillingWrite, s.putPolicy))
	mux.HandleFunc("DELETE /api/policies", s.authed(auth.BillingWrite, s.deletePolicy))
	mux.HandleFunc("GET /api/policies/history", s.authed(auth.BillingRead, s.policyHistory))
	mux.HandleFunc("GET /api/usage", s.authed(auth.BillingRead, s.usage))
	mux.HandleFunc("GET /api/usage/notices", s.authed(auth.BillingRead, s.usageNotices))
}

// policyView is a policy and what whoever last changed it is called. The
// policy names them by subject; the console shows the name.
type policyView struct {
	policy.Policy
	UpdatedByName string `json:"updated_by_name,omitempty"`
}

func viewPolicy(names platform.Names, pol policy.Policy) policyView {
	return policyView{Policy: pol, UpdatedByName: names.Of(pol.UpdatedBy)}
}

// policyChangeView is one change to a policy and what whoever made it is
// called.
type policyChangeView struct {
	policy.Change
	ByName string `json:"by_name,omitempty"`
}

func (s *Server) listPolicies(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	seer := s.P.Seer(who)
	names := s.P.Names(who.Tenant)
	out := []policyView{}
	for _, pol := range s.P.Policies.List(who.Tenant) {
		if seer.Scope(pol.Scope) {
			out = append(out, viewPolicy(names, pol))
		}
	}
	ok(w, http.StatusOK, map[string]any{"policies": out})
}

// exists checks that a scope names something in the caller's tenant. The
// workspace's own limits are the tenant scope, never a project scope. A scope
// the caller may not read does not exist for them.
func (s *Server) exists(r *http.Request, who auth.Principal, scope policy.Scope) error {
	if !s.P.Seer(who).Scope(scope) {
		return errNotSeen
	}
	switch scope.Kind {
	case policy.Project:
		if projects.NameOf(scope.Name) == projects.Root {
			return fmt.Errorf("%w: the workspace's own limits are the tenant scope", policy.ErrInvalid)
		}
		if !s.P.HasProject(who.Tenant, scope.Name) {
			return platform.ErrProjectNotFound
		}
	case policy.Agent:
		_, err := s.P.Catalog().Get(r.Context(), who.Tenant, scope.Name, 0)
		return err
	case policy.Client:
		_, err := s.P.Clients.Get(who.Tenant, scope.Name)
		return err
	}
	return nil
}

func (s *Server) putPolicy(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Scope         policy.Scope  `json:"scope"`
		Period        policy.Period `json:"period"`
		MaxRuns       int           `json:"max_runs"`
		MaxConcurrent int           `json:"max_concurrent"`
		SpendLimit    int64         `json:"spend_limit"`
		MaxLease      int64         `json:"max_lease"`
		MaxStorage    int64         `json:"max_storage"`
		MaxTokens     int64         `json:"max_tokens"`
		MaxClients    int           `json:"max_clients"`
		MaxPerSurface int           `json:"max_clients_per_surface"`
		WarnAt        int           `json:"warn_at"`
		Action        policy.Action `json:"action"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := s.exists(r, who, body.Scope); err != nil {
		s.problem(w, err)
		return
	}
	if body.Action == "" {
		body.Action = policy.Block
	}
	// A project's limit is written while the project is held, so a delete
	// that checks for its limits cannot miss this one. It holds every
	// project below it too, which admission reads along the tree.
	project := ""
	if body.Scope.Kind == policy.Project {
		project = body.Scope.Name
	}
	var saved policy.Policy
	err := s.filed(who.Tenant, project, func() error {
		var err error
		saved, err = s.P.Policies.Put(who.Tenant, policy.Policy{
			Scope: body.Scope, Period: body.Period, MaxRuns: body.MaxRuns, MaxConcurrent: body.MaxConcurrent,
			SpendLimit: body.SpendLimit, MaxLease: body.MaxLease, MaxStorage: body.MaxStorage, MaxTokens: body.MaxTokens,
			MaxClients: body.MaxClients, MaxClientsPerSurface: body.MaxPerSurface, WarnAt: body.WarnAt, Action: body.Action,
		}, who.Subject, time.Now())
		return err
	})
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, viewPolicy(s.P.Names(who.Tenant), saved))
}

func (s *Server) deletePolicy(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	q := r.URL.Query()
	scope := policy.Scope{Kind: policy.Kind(q.Get("kind")), Name: q.Get("name")}
	if !s.P.Seer(who).Scope(scope) {
		notFound(w)
		return
	}
	if err := s.P.Policies.Delete(who.Tenant, scope, who.Subject, time.Now()); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) policyHistory(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	seer := s.P.Seer(who)
	names := s.P.Names(who.Tenant)
	out := []policyChangeView{}
	for _, c := range s.P.Policies.History(who.Tenant) {
		if seer.Scope(c.Scope) {
			out = append(out, policyChangeView{Change: c, ByName: names.Of(c.By)})
		}
	}
	ok(w, http.StatusOK, map[string]any{"changes": out})
}

// measure is one limit and what has been used against it. A limit of zero
// means the scope sets none.
type measure struct {
	Used  int64 `json:"used"`
	Limit int64 `json:"limit"`
	// Projected is where Used ends the period at the pace so far. It is left
	// out for a count that does not accumulate, such as runs in flight.
	Projected *int64 `json:"projected,omitempty"`
}

// scopeUsage is one policy and where its scope stands against it.
type scopeUsage struct {
	Scope       policy.Scope  `json:"scope"`
	Period      policy.Period `json:"period"`
	PeriodStart time.Time     `json:"period_start"`
	PeriodEnd   time.Time     `json:"period_end"`
	Version     int           `json:"version"`
	WarnAt      int           `json:"warn_at"`
	Action      policy.Action `json:"action"`
	Runs        measure       `json:"runs"`
	Concurrent  measure       `json:"concurrent"`
	// Spend is what was priced in US dollars, in micro-USD, and is enforced.
	// Calls with no price and money in other currencies cannot be held
	// against it: they are counted here, so the reader knows Used is a
	// lower bound.
	Spend spendMeasure `json:"spend"`
	// Tokens is every model token used, priced or not.
	Tokens   measure `json:"tokens"`
	MaxLease int64   `json:"max_lease"`
	// State is the worst of the enforced limits: ok, warn or over.
	State string `json:"state"`
}

// spendMeasure is spend against its limit, and what could not be counted in
// it.
type spendMeasure struct {
	measure
	// Metered is true: the runtime prices calls, and spend is enforced.
	Metered bool `json:"metered"`
	// UnpricedCalls had no price, so their spend is unknown, not zero.
	UnpricedCalls int `json:"unpriced_calls"`
	// OtherCosts is money in currencies other than US dollars, never
	// converted and never held against the limit.
	OtherCosts []policy.Cost `json:"other_costs,omitempty"`
}

// project estimates where a count ends the period at the pace so far.
func project(used int64, start, end, now time.Time) *int64 {
	elapsed := now.Sub(start)
	if elapsed <= 0 {
		return nil
	}
	p := int64(float64(used) * float64(end.Sub(start)) / float64(elapsed))
	return &p
}

func (s *Server) usage(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	now := time.Now()
	out := []scopeUsage{}
	seer := s.P.Seer(who)
	for _, pol := range s.P.Policies.List(who.Tenant) {
		if !seer.Scope(pol.Scope) {
			continue
		}
		start, end := policy.PeriodStart(pol.Period, now), policy.PeriodEnd(pol.Period, now)
		u := s.P.Usage.Usage(who.Tenant, pol.Scope, start)
		warn := pol.WarnAt
		if warn == 0 {
			warn = policy.DefaultWarnAt
		}
		v := scopeUsage{
			Scope: pol.Scope, Period: pol.Period, PeriodStart: start, PeriodEnd: end,
			Version: pol.Version, WarnAt: warn, Action: pol.Action, MaxLease: pol.MaxLease,
			Runs:       measure{Used: int64(u.Runs), Limit: int64(pol.MaxRuns), Projected: project(int64(u.Runs), start, end, now)},
			Concurrent: measure{Used: int64(u.Concurrent), Limit: int64(pol.MaxConcurrent)},
			State:      "ok",
		}
		v.Spend = spendMeasure{
			measure: measure{Used: u.Spend, Limit: pol.SpendLimit, Projected: project(u.Spend, start, end, now)},
			Metered: true, UnpricedCalls: u.UnpricedCalls, OtherCosts: u.OtherCosts,
		}
		v.Tokens = measure{Used: u.Tokens, Limit: pol.MaxTokens, Projected: project(u.Tokens, start, end, now)}
		for _, m := range []measure{v.Runs, v.Concurrent, v.Spend.measure, v.Tokens} {
			switch {
			case m.Limit > 0 && m.Used >= m.Limit:
				v.State = "over"
			case m.Limit > 0 && policy.Reaches(m.Used, m.Limit, warn) && v.State == "ok":
				v.State = "warn"
			}
		}
		out = append(out, v)
	}
	ok(w, http.StatusOK, map[string]any{"scopes": out})
}

func (s *Server) usageNotices(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	seer := s.P.Seer(who)
	out := []usage.Entry{}
	for _, e := range s.P.Usage.Notices(who.Tenant, 200) {
		if seer.Notice(e) {
			out = append(out, e)
		}
	}
	ok(w, http.StatusOK, map[string]any{"notices": out})
}

// denied answers a run a policy refused. A count limit is "too many, try
// later" and a money limit is "payment required". Either way the body names
// the policy, its version, the limit and what was left, so a caller can show
// exactly why.
func denied(w http.ResponseWriter, err error) bool {
	var d *platform.Denied
	if !errors.As(err, &d) {
		return false
	}
	status := http.StatusTooManyRequests
	if d.Decision.Limit == policy.LimitSpend || d.Decision.Limit == policy.LimitLease {
		status = http.StatusPaymentRequired
	}
	body := map[string]any{
		"error": d.Decision.Reason, "limit": d.Decision.Limit, "remaining": d.Decision.Remaining,
	}
	if p := d.Decision.Policy; p != nil {
		body["policy"] = map[string]any{"scope": p.Scope, "version": p.Version, "period": p.Period}
		switch d.Decision.Limit {
		case policy.LimitRuns, policy.LimitSpend, policy.LimitTokens:
			// When the count starts again.
			body["resets_at"] = policy.PeriodEnd(p.Period, time.Now())
		}
	}
	ok(w, status, body)
	return true
}
