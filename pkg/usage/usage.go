// Package usage counts what runs have used against a workspace's limits.
//
// It is a ledger and a projection of it. Every admission, ending, warning,
// refusal and model call is appended as an entry, and the counts a policy is
// judged on are moved in the same write as the entry that moves them. Nothing
// is counted that is not in the ledger, so replaying the entries into an empty
// ledger gives the same answers: Rebuild does exactly that, and a test holds
// the two equal.
//
// # Per request
//
// The ledger reads and writes its store per request and keeps nothing in
// memory: the store is the truth. An entry and the counts it moves are one
// write, at the versions they were read at (compare-and-set), or, with shared
// counters, one Postgres transaction. Every read is one record, or a bounded
// page of one tenant's records (see keys.go): a scope's counts by day, a
// day's entries, a tenant's newest notices, a run's grants. So two processes
// that open a ledger on one database agree, and the ledger does not need
// pkg/state's lock.
//
// A write that fails is an entry that was not kept, and is not counted: an
// admission is refused, and any other entry is reported to the caller, who
// knows what to do with it (a run's settled total, read from the runtime at
// its end, still counts a call whose own entry was lost).
//
// # Scale
//
// Admissions, spend and notices are kept for a little over two months, which
// is what a monthly limit needs, and a tenant's entries older than that are
// deleted once a day, by the first process to write for the tenant that day.
// Counts are kept per scope and UTC day, so a count over a period sums at most
// 62 days of totals, and only a period that starts inside a day reads that
// day's entries. Where an entry falls is decided by its own time, never by the
// order it was appended in, so an entry stamped earlier than the one before it
// is still counted in its own day.
//
// # Shared counts
//
// Opened WithCounters, the counts are kept in Postgres tables (Counters), and
// an entry is written in the same transaction that moves them. The counts an
// admission moves, runs started and runs in flight, are striped, and Admit
// takes a Quota of them exactly: two processes cannot both let in the last
// run a limit allows, and admissions that do not near a limit do not wait on
// each other. Without counters, the counts are records in the ledger's store,
// changed by compare-and-set, and a quota is exact the same way.
//
// # Spend, counted once
//
// What a run spends is counted from two sources, and never twice:
//
//   - Spent: one model call, as the runtime reported it on the run's stream
//     (usage.call). Its key is the runtime's AccountingID, which no other call
//     of any attempt shares, so a call delivered twice is counted once.
//   - Settled: the run's own total once it has ended, read from the runtime's
//     record (its terminal event, its output, or its ledger records for a run
//     that failed). Its key is "run:" and the run id.
//
// A run is counted as the larger, field by field, of its calls seen so far and
// its settled total. So a settlement adds only the calls whose events had not
// been seen, and a call seen after its run settled adds only what goes beyond
// the settled total: the settled total already counted it. A terminal event
// delivered twice, a reconciler pass, or a late call event counts nothing
// twice, in either order.
//
// A run's own total excludes the runs it called, which settle their own: so
// each model call in a tree of runs is counted once, in the run that made it.
package usage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"

	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/state"
)

// Kind is what a ledger entry records.
type Kind string

const (
	Admitted Kind = "admitted"
	Ended    Kind = "ended"
	Warned   Kind = "warned"
	Refused  Kind = "refused"
	// Spent is one model call a run made.
	Spent Kind = "spent"
	// Settled is a run's own total at its end.
	Settled Kind = "settled"
	// Halted is a run in flight stopped because its scope reached a limit.
	Halted Kind = "halted"
	// Held is a run in flight held for a person because its scope reached a
	// limit whose policy asks for approval.
	Held Kind = "held"
	// Granted is what a person let one held run use beyond a scope's limit:
	// Grant, by By, under the policy's Version.
	Granted Kind = "granted"
)

// Spend is what some model calls used. Micros is US dollars only: money in
// any other currency is kept apart in Other and never converted, and a call
// with no price is counted in Unpriced with no money at all.
type Spend struct {
	Micros     int64         `json:"micros,omitempty"`
	ModelCalls int           `json:"model_calls,omitempty"`
	Tokens     int64         `json:"tokens,omitempty"`
	Unpriced   int           `json:"unpriced_calls,omitempty"`
	Other      []policy.Cost `json:"other_costs,omitempty"`
}

// USD is the one currency a spend limit is held in.
const USD = "USD"

// OfCall is what one model call the runtime reported used, in the ledger's
// terms: dollars in Micros, any other currency in Other, and no price at all
// in Unpriced. A call saige answered from its response cache cost nothing and
// is not unpriced.
func OfCall(c event.UsageCall) Spend {
	s := Spend{ModelCalls: 1, Tokens: int64(c.PromptTokens + c.CompletionTokens)}
	switch {
	case c.Cost != nil && c.Cost.Currency == USD:
		s.Micros = c.Cost.Micros
	case c.Cost != nil:
		s.Other = []policy.Cost{{Micros: c.Cost.Micros, Currency: c.Cost.Currency}}
	case !c.ResponseCacheHit:
		s.Unpriced = 1
	}
	return s
}

// Of is what a sum of calls used, in the ledger's terms. Dollars are read
// wherever the runtime put them: in Cost when the first priced call was in
// dollars, in OtherCosts when it was not.
func Of(u event.Usage) Spend {
	s := Spend{ModelCalls: u.ModelCalls, Tokens: int64(u.PromptTokens + u.CompletionTokens), Unpriced: u.UnpricedCalls}
	costs := u.OtherCosts
	if u.Cost != nil {
		costs = append([]event.Cost{*u.Cost}, costs...)
	}
	for _, c := range costs {
		if c.Currency == USD {
			s.Micros += c.Micros
			continue
		}
		s.Other = append(s.Other, policy.Cost{Micros: c.Micros, Currency: c.Currency})
	}
	return s
}

// ceil is the larger of s and o, field by field and currency by currency.
func (s Spend) ceil(o Spend) Spend {
	return s.add(o.beyond(s))
}

// add returns s and o summed, money per currency.
func (s Spend) add(o Spend) Spend {
	s.Micros += o.Micros
	s.ModelCalls += o.ModelCalls
	s.Tokens += o.Tokens
	s.Unpriced += o.Unpriced
	s.Other = addCosts(s.Other, o.Other, 1)
	return s
}

// beyond is what s has that o does not: field by field, never below zero. It
// is what a run's settled total adds to the calls already counted for it.
func (s Spend) beyond(o Spend) Spend {
	pos := func(a, b int64) int64 { return max(a-b, 0) }
	out := Spend{
		Micros: pos(s.Micros, o.Micros), Tokens: pos(s.Tokens, o.Tokens),
		ModelCalls: int(pos(int64(s.ModelCalls), int64(o.ModelCalls))),
		Unpriced:   int(pos(int64(s.Unpriced), int64(o.Unpriced))),
	}
	for _, c := range s.Other {
		had := int64(0)
		for _, d := range o.Other {
			if d.Currency == c.Currency {
				had = d.Micros
			}
		}
		if left := pos(c.Micros, had); left > 0 {
			out.Other = append(out.Other, policy.Cost{Micros: left, Currency: c.Currency})
		}
	}
	return out
}

// IsZero reports whether nothing was used.
func (s Spend) IsZero() bool {
	return s.Micros == 0 && s.ModelCalls == 0 && s.Tokens == 0 && s.Unpriced == 0 && len(s.Other) == 0
}

func addCosts(to, from []policy.Cost, sign int64) []policy.Cost {
	out := append([]policy.Cost(nil), to...)
	for _, c := range from {
		i := 0
		for i < len(out) && out[i].Currency != c.Currency {
			i++
		}
		if i == len(out) {
			out = append(out, policy.Cost{Currency: c.Currency})
		}
		out[i].Micros += sign * c.Micros
	}
	return out
}

// Entry is one line of the ledger.
type Entry struct {
	Kind    Kind   `json:"kind"`
	Tenant  string `json:"tenant"`
	Project string `json:"project,omitempty"`
	// Within is the project's ancestors, nearest first and the root left
	// out, as they were when the run was admitted. The entry counts toward
	// each of them as it does toward its project, so a parent's limit holds
	// everything below it. An entry written before projects nested has
	// none, and counts toward its own project only.
	Within []string `json:"within,omitempty"`
	Agent  string   `json:"agent"`
	// Client is the external client that raised the run, if one did.
	Client string `json:"client,omitempty"`
	RunID  string `json:"run_id,omitempty"`
	// At is when the entry was appended, by the platform's clock, and decides
	// which period it counts in.
	At time.Time `json:"at"`
	// EventAt is when the runtime said the call happened, for a Spent entry.
	// It is kept for the record and counts nothing.
	EventAt time.Time `json:"event_at,omitzero"`
	// Key makes a Spent or Settled entry count once: the runtime's
	// AccountingID for a call, "run:<id>" for a run's settled total.
	Key string `json:"key,omitempty"`
	// Spend is what a Spent entry's call used, or a Settled entry's run used
	// of its own in all.
	Spend *Spend `json:"spend,omitempty"`
	// For a warning or a refusal: the policy that caused it, at the version
	// it had then, the limit it turned on, and the reason given.
	Scope   *policy.Scope `json:"scope,omitempty"`
	Version int           `json:"version,omitempty"`
	Limit   policy.Limit  `json:"limit,omitempty"`
	Reason  string        `json:"reason,omitempty"`
	// Grant is what a Granted entry let its run use beyond Scope's limit,
	// and By who granted it.
	Grant *policy.Grant `json:"grant,omitempty"`
	By    string        `json:"by,omitempty"`
}

type admission struct {
	project, agent, client, runID string
	within                        []string
	at                            time.Time
}

// scopes is the keys of every scope an admission falls in: the tenant, its
// project and each of the project's ancestors, its agent and its client.
func (a admission) scopes() []string {
	out := []string{"t"}
	if a.project != "" {
		out = append(out, "p\x00"+a.project)
	}
	for _, p := range a.within {
		if p != "" && p != a.project {
			out = append(out, "p\x00"+p)
		}
	}
	if a.agent != "" {
		out = append(out, "a\x00"+a.agent)
	}
	if a.client != "" {
		out = append(out, "c\x00"+a.client)
	}
	return out
}

func scopeKey(s policy.Scope) string {
	switch s.Kind {
	case policy.Project:
		return "p\x00" + s.Name
	case policy.Agent:
		return "a\x00" + s.Name
	case policy.Client:
		return "c\x00" + s.Name
	}
	return "t"
}

func matches(s policy.Scope, a admission) bool {
	switch s.Kind {
	case policy.Project:
		return a.project == s.Name || slices.Contains(a.within, s.Name)
	case policy.Agent:
		return a.agent == s.Name
	case policy.Client:
		return a.client == s.Name
	}
	return true
}

// sorted orders costs by currency, so a sum read from a map is the same every
// time.
func sorted(costs []policy.Cost) []policy.Cost {
	costs = slices.DeleteFunc(costs, func(c policy.Cost) bool { return c.Micros == 0 })
	if len(costs) == 0 {
		return nil
	}
	slices.SortFunc(costs, func(a, b policy.Cost) int { return strings.Compare(a.Currency, b.Currency) })
	return costs
}

// Retention and bounds.
const (
	// keepAdmissions is how long entries and counts are kept.
	keepAdmissions = 62 * 24 * time.Hour
	// keepNotices is the most notices Notices returns.
	keepNotices = 5000
	// maxDays bounds a scope's counts by day, which retention keeps to 64.
	maxDays = state.MaxList
	// maxGrants bounds the grants one run is read with. Each is a person's
	// approval, so a run never nears it.
	maxGrants = state.MaxList
	// maxDayEntries bounds the entries of the one day a period that starts
	// inside it reads.
	maxDayEntries = 200_000
	// maxEntries bounds Entries.
	maxEntries = 1_000_000
	// prunePages is how many pages of old entries one retention pass deletes.
	// A pass that stops there runs again on the tenant's next write.
	prunePages = 100
)

// Ledger is every tenant's usage, read and written per request.
type Ledger struct {
	st      state.Store
	records *state.Table[record]
	entries *state.Table[Entry]

	// shared, when set, holds the counts every process judges by.
	shared *Counters

	// retained is the day each tenant's old entries were last deleted by
	// this process: a cache that saves a read, dropped without changing an
	// answer.
	mu       sync.Mutex
	retained map[string]string
}

// Option configures a ledger opened with Open.
type Option func(*Ledger)

// WithCounters keeps the counts in shared counters, so every process that
// opens a ledger on them admits, and judges spend, against the same counts.
func WithCounters(c *Counters) Option { return func(l *Ledger) { l.shared = c } }

// NewLedger returns an empty ledger in memory.
func NewLedger() *Ledger {
	l, _ := Open(nil)
	return l
}

// Open returns a ledger that keeps its entries and counts in st. A nil st
// is a new state.NewMemory.
//
// With WithCounters, st must be the state store on the counters' database.
// Entries an earlier release kept are moved to where this one reads them,
// once, and the counters are filled from them the first time.
func Open(st state.Store, opts ...Option) (*Ledger, error) {
	l := &Ledger{retained: map[string]string{}}
	for _, o := range opts {
		o(l)
	}
	if st == nil {
		if l.shared != nil {
			return nil, errors.New("usage: shared counters need a store")
		}
		st = state.NewMemory()
	}
	l.st = st
	l.records = state.NewTable[record](st, namespace)
	l.entries = state.NewTable[Entry](st, namespace)
	if err := l.convert(); err != nil {
		return nil, fmt.Errorf("usage: convert: %w", err)
	}
	return l, nil
}

// Append records an entry and folds it into the counts. It is the only way the
// counts change. A Spent or Settled entry whose key was already counted, or
// an Ended entry for a run not in flight, is not recorded, and Append reports
// false: an event folded twice, by one process or two, is kept once. An error is a write that failed,
// and nothing was recorded.
func (l *Ledger) Append(e Entry) (bool, error) {
	_, ok, err := l.Record(e)
	return ok, err
}

// Record appends an entry, as Append does, and returns what it added to the
// counts: for a Spent entry its call, unless its run had already settled a
// total that covers it, and for a Settled entry only what the run's calls
// already counted did not cover. Anything else adds no spend. With shared
// counters, what was added counts calls other processes saw.
func (l *Ledger) Record(e Entry) (Spend, bool, error) {
	if (e.Kind == Spent || e.Kind == Settled) && (e.Key == "" || e.Spend == nil) {
		return Spend{}, false, nil
	}
	var added Spend
	var ok bool
	var err error
	if l.shared != nil {
		ctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
		defer cancel()
		_, added, ok, err = l.shared.record(ctx, e)
	} else {
		added, ok, err = recordLocal(l.st, e, nil)
	}
	if err != nil {
		return Spend{}, false, fmt.Errorf("usage: write: %w", err)
	}
	if ok {
		l.retain(e.Tenant, e.At)
	}
	return added, ok, nil
}

// QuotaKind is what a Quota bounds.
type QuotaKind string

const (
	// QuotaRuns bounds the runs a scope starts from Since.
	QuotaRuns QuotaKind = "runs"
	// QuotaConcurrent bounds the runs a scope has in flight.
	QuotaConcurrent QuotaKind = "concurrent"
)

// Quota is a bound on what an admission counts, held exactly: an admission
// that would take a scope past Max is refused, whichever process admits it.
type Quota struct {
	Scope policy.Scope
	Kind  QuotaKind
	// Since is the start of the period QuotaRuns counts from, the start of
	// a UTC day.
	Since time.Time
	Max   int
}

// Judge decides, from the counts read, whether an admission goes ahead. It
// reads them as they are, without holding them: a limit that must hold
// exactly is a Quota.
type Judge func(read Reader) (bool, error)

// Reader reads what a scope has used since a time.
type Reader func(scope policy.Scope, since time.Time) (policy.Usage, error)

// Admit judges an admission and, if the judge lets it in and every quota
// has room, records it. Quotas are taken as one step with the record: no
// other process admits between the check and the count. The judge reads the
// counts as they are, before that step, so a limit that must hold exactly is
// a quota; a judge decides what admission does not count, such as spend. It
// reports whether the run was admitted. An error is a judge's, or a write
// that failed, and the run is not admitted.
func (l *Ledger) Admit(e Entry, judge Judge, quotas ...Quota) (bool, error) {
	ok, _, err := l.AdmitQuotas(e, judge, quotas)
	return ok, err
}

// AdmitQuotas is Admit, and on a refusal by a quota, the quota that had no
// room. A run the judge refused has none.
func (l *Ledger) AdmitQuotas(e Entry, judge Judge, quotas []Quota) (ok bool, full *Quota, err error) {
	e.Kind = Admitted
	if judge != nil {
		in, err := judge(func(scope policy.Scope, since time.Time) (policy.Usage, error) {
			return l.Usage(e.Tenant, scope, since)
		})
		if err != nil || !in {
			return false, nil, err
		}
	}
	if l.shared != nil {
		ctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
		defer cancel()
		_, ok, full, err = l.shared.admit(ctx, e, quotas)
	} else {
		ok, full, err = admitLocal(l.st, e, quotas)
	}
	if err != nil {
		return false, nil, fmt.Errorf("usage: write: %w", err)
	}
	if ok {
		l.retain(e.Tenant, e.At)
	}
	return ok, full, nil
}

func admissionOf(e Entry) admission {
	return admission{project: e.Project, agent: e.Agent, client: e.Client, runID: e.RunID, within: e.Within, at: e.At}
}

// Usage counts what a scope has used: runs started since a time, runs in
// flight now, and what model calls since that time spent. Whole days are read
// from their totals; only a day since falls inside is read entry by entry.
func (l *Ledger) Usage(tenantName string, scope policy.Scope, since time.Time) (policy.Usage, error) {
	start := policy.PeriodStart(policy.Day, since)
	if since.Equal(start) {
		return l.dayUsage(tenantName, scope, start)
	}
	u, err := l.dayUsage(tenantName, scope, start.AddDate(0, 0, 1))
	if err != nil {
		return u, err
	}
	sum := Spend{Micros: u.Spend, ModelCalls: u.ModelCalls, Tokens: u.Tokens, Unpriced: u.UnpricedCalls, Other: u.OtherCosts}
	prefix := "e:" + dayKey(start) + ":"
	after := ""
	for read := 0; ; {
		page, next, err := l.records.List(tenantName, state.Query{Prefix: prefix, After: after, Limit: state.MaxList})
		if err != nil {
			return u, err
		}
		for _, it := range page {
			r := it.Value
			if r.At.Before(since) || !matches(scope, admissionOf(r.Entry)) {
				continue
			}
			switch r.Kind {
			case Admitted:
				u.Runs++
			case Spent, Settled:
				switch {
				case r.Added != nil:
					sum = sum.add(*r.Added)
				case r.Kind == Spent && r.Spend != nil:
					// Kept by an earlier release, which did not keep what
					// it added: a call counts as it was reported.
					sum = sum.add(*r.Spend)
				}
			}
		}
		if read += len(page); read > maxDayEntries {
			return u, fmt.Errorf("%w: usage: %q holds more than %d entries on %s", state.ErrTooMany, tenantName, maxDayEntries, dayKey(start))
		}
		if next == "" {
			break
		}
		after = next
	}
	u.Spend, u.Tokens, u.ModelCalls, u.UnpricedCalls, u.OtherCosts = sum.Micros, sum.Tokens, sum.ModelCalls, sum.Unpriced, sorted(sum.Other)
	return u, nil
}

// dayUsage is Usage since the start of a UTC day.
func (l *Ledger) dayUsage(tenantName string, scope policy.Scope, since time.Time) (policy.Usage, error) {
	if l.shared != nil {
		ctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
		defer cancel()
		return l.shared.Usage(ctx, tenantName, scope, since)
	}
	return usageLocal(l.st, tenantName, scope, since)
}

// RunSpend is what has been counted for one run so far: the larger of its
// calls and its settled total.
func (l *Ledger) RunSpend(tenantName, runID string) (Spend, error) {
	var out Spend
	var err error
	if l.shared != nil {
		ctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
		defer cancel()
		out, err = l.shared.runSpend(ctx, tenantName, runID)
	} else {
		out, err = runSpendLocal(l.st, tenantName, runID)
	}
	out.Other = sorted(out.Other)
	return out, err
}

// grantsOf reads one run's Granted entries.
func (l *Ledger) grantsOf(tenantName, runID string) ([]Entry, error) {
	items, err := l.entries.All(tenantName, grantPrefix(runID), maxGrants)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(items))
	for _, it := range items {
		// A run whose id is another's and more shares its prefix.
		if e := it.Value; e.RunID == runID && e.Scope != nil && e.Grant != nil {
			out = append(out, e)
		}
	}
	return out, nil
}

// Granted is what people granted one run beyond a scope's limits since a
// time: the sum of its Granted entries in that scope, whichever process
// recorded them.
func (l *Ledger) Granted(tenantName, runID string, scope policy.Scope, since time.Time) (policy.Grant, error) {
	gs, err := l.grantsOf(tenantName, runID)
	if err != nil {
		return policy.Grant{}, err
	}
	var out policy.Grant
	key := scopeKey(scope)
	for _, g := range gs {
		if scopeKey(*g.Scope) == key && !g.At.Before(since) {
			out.Spend += g.Grant.Spend
			out.Tokens += g.Grant.Tokens
		}
	}
	return out, nil
}

// Grants counts the Granted entries of one run in a scope, whenever they
// were made: how many times people let it go on there.
func (l *Ledger) Grants(tenantName, runID string, scope policy.Scope) (int, error) {
	gs, err := l.grantsOf(tenantName, runID)
	if err != nil {
		return 0, err
	}
	n, key := 0, scopeKey(scope)
	for _, g := range gs {
		if scopeKey(*g.Scope) == key {
			n++
		}
	}
	return n, nil
}

// Notices returns a tenant's warnings, refusals, halts, holds and grants,
// newest first: at most limit, and at most keepNotices, which a limit of 0
// asks for.
func (l *Ledger) Notices(tenantName string, limit int) ([]Entry, error) {
	if limit <= 0 || limit > keepNotices {
		limit = keepNotices
	}
	out := []Entry{}
	after := ""
	for len(out) < limit {
		page, next, err := l.entries.List(tenantName, state.Query{Prefix: "n:", After: after, Limit: min(limit-len(out), state.MaxList), Desc: true})
		if err != nil {
			return nil, err
		}
		for _, it := range page {
			out = append(out, it.Value)
		}
		if next == "" {
			break
		}
		after = next
	}
	return out, nil
}

// Entries returns a tenant's whole ledger, by day and in the order each day
// was appended. It is for tests and exports, and fails past maxEntries.
func (l *Ledger) Entries(tenantName string) ([]Entry, error) {
	items, err := l.records.All(tenantName, "e:", maxEntries)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, len(items))
	for i, it := range items {
		out[i] = it.Value.Entry
	}
	return out, nil
}

// Rebuild replays entries into a new ledger in memory. A ledger rebuilt from
// another's entries answers every Usage question the same way.
func Rebuild(entries []Entry) *Ledger {
	l := NewLedger()
	for _, e := range entries {
		_, _, _ = l.Record(e)
	}
	return l
}

// retain deletes a tenant's entries, and counts, no period can still need,
// once a day per tenant in this process, the day being that of the entry
// just written. It is housekeeping: an error leaves them for the next try.
func (l *Ledger) retain(tenant string, at time.Time) {
	day := dayKey(at)
	l.mu.Lock()
	if l.retained[tenant] >= day {
		l.mu.Unlock()
		return
	}
	l.retained[tenant] = day
	l.mu.Unlock()
	cutoff := policy.PeriodStart(policy.Day, at.Add(-keepAdmissions))
	done := true
	if l.shared != nil {
		ctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
		if err := l.shared.prune(ctx, tenant, cutoff); err != nil {
			done = false
		}
		cancel()
	}
	if ok, err := l.prune(tenant, cutoff); err != nil || !ok {
		done = false
	}
	if !done {
		l.mu.Lock()
		delete(l.retained, tenant)
		l.mu.Unlock()
	}
}

// prune deletes a tenant's entries of days before cutoff, oldest first, with
// the records each is read by, and without shared counters the counts of
// those days. It reports false when it stopped at prunePages with more left.
func (l *Ledger) prune(tenant string, cutoff time.Time) (bool, error) {
	before := dayKey(cutoff)
	runs := map[string]bool{}
	for range prunePages {
		page, next, err := l.records.List(tenant, state.Query{Prefix: "e:", Limit: state.MaxList})
		if err != nil {
			return false, err
		}
		var ops []state.Op
		gone := map[string]bool{}
		del := func(key string) {
			if !gone[key] {
				gone[key] = true
				ops = append(ops, state.Delete(tenant, key))
			}
		}
		last := false
		for _, it := range page {
			if len(it.Key) < 10 || it.Key[2:10] >= before {
				last = true
				break
			}
			r, seq := it.Value, seqOf(it.Key)
			del(it.Key)
			if isNotice(r.Kind) {
				del("n:" + seq)
			}
			if r.Kind == Granted && r.RunID != "" {
				del(grantPrefix(r.RunID) + seq)
			}
			if l.shared == nil {
				switch r.Kind {
				case Spent, Settled:
					del(spentKey(r.Key))
					runs[r.RunID] = true
					fallthrough
				case Admitted:
					for _, k := range admissionOf(r.Entry).scopes() {
						del(dayCountKey(k, it.Key[2:10]))
					}
				}
			}
		}
		if len(ops) > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
			err := l.st.Write(ctx, namespace, ops...)
			cancel()
			if err != nil {
				return false, err
			}
		}
		if last || next == "" {
			return true, l.pruneRuns(tenant, cutoff, runs)
		}
	}
	return false, l.pruneRuns(tenant, cutoff, runs)
}

// pruneRuns deletes what runs whose last spend is before cutoff spent.
func (l *Ledger) pruneRuns(tenant string, cutoff time.Time, runs map[string]bool) error {
	t := state.NewTable[runRec](l.st, namespace)
	for run := range runs {
		r, err := t.Lookup(tenant, runKey(run))
		if errors.Is(err, state.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if !r.Value.Last.Before(cutoff) {
			continue
		}
		b := t.Batch()
		b.DeleteAt(tenant, runKey(run), r.Version)
		// A spend recorded since is kept with its run.
		if err := b.Commit(); err != nil && !errors.Is(err, state.ErrConflict) {
			return err
		}
	}
	return nil
}
