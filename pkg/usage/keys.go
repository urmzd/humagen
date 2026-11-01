package usage

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

// The ledger's records, in the namespace "usage", under the entry's tenant:
//
//	e:<day>:<seq>   an entry (record), by the UTC day of its At and its place
//	n:<seq>         a warning, refusal, halt, hold or grant (Entry), newest last
//	g:<run>:<seq>   a grant (Entry), by the run it was made for
//
// and, kept only by a ledger without shared counters, the counts:
//
//	h               the last place given to an entry (head)
//	d:<scope>:<day> runs admitted and spend in a scope on a day (dayCount)
//	c:<scope>       runs in flight in a scope (inFlight)
//	a:<run>         a run in flight (activeRun)
//	r:<run>         what a run has spent (runRec)
//	k:<key>         a Spent or Settled key already counted (keyRec)
//
// A day is written as YYYYMMDD and a place as state.Seq. In the tenant "",
// layoutMarker says the entries of an earlier release, kept there under
// their place alone, have moved.
const (
	namespace    = "usage"
	layoutMarker = "layout:v2"
	headKey      = "h"
)

// dayKey is a UTC day as keys write it.
func dayKey(t time.Time) string { return t.UTC().Format("20060102") }

func entryKey(at time.Time, seq int64) string { return "e:" + dayKey(at) + ":" + state.Seq(seq) }
func noticeKey(seq int64) string              { return "n:" + state.Seq(seq) }
func grantPrefix(run string) string           { return "g:" + run + ":" }
func dayCountPrefix(scope string) string      { return "d:" + dbScope(scope) + ":" }
func dayCountKey(scope string, day string) string {
	return dayCountPrefix(scope) + day
}
func inFlightKey(scope string) string { return "c:" + dbScope(scope) }
func activeKey(run string) string     { return "a:" + run }
func runKey(run string) string        { return "r:" + run }
func spentKey(key string) string      { return "k:" + key }

// dayOf is the day a key under prefix names, when the key is the prefix and
// a day and nothing else: a scope whose name holds a colon has keys that
// another scope's prefix also matches.
func dayOf(key, prefix string) (string, bool) {
	rest, ok := strings.CutPrefix(key, prefix)
	if !ok || len(rest) != 8 {
		return "", false
	}
	for _, c := range rest {
		if c < '0' || c > '9' {
			return "", false
		}
	}
	return rest, true
}

// record is an entry as it is kept: with what it added to the counts, for a
// Spent or Settled entry, so a period that starts inside a day can be read
// from that day's entries. An entry kept by an earlier release has none.
type record struct {
	Entry
	Added *Spend `json:"added,omitempty"`
}

func isNotice(k Kind) bool {
	switch k {
	case Warned, Refused, Halted, Held, Granted:
		return true
	}
	return false
}

// entryOps are the writes that keep an entry at seq: the entry itself, and
// for a notice or a grant the records it is read by.
func entryOps(e Entry, added *Spend, seq int64) ([]state.Op, error) {
	raw, err := json.Marshal(record{Entry: e, Added: added})
	if err != nil {
		return nil, err
	}
	ops := []state.Op{state.Put(e.Tenant, entryKey(e.At, seq), raw)}
	if isNotice(e.Kind) {
		plain, err := json.Marshal(e)
		if err != nil {
			return nil, err
		}
		ops = append(ops, state.Put(e.Tenant, noticeKey(seq), plain))
		if e.Kind == Granted && e.RunID != "" {
			ops = append(ops, state.Put(e.Tenant, grantPrefix(e.RunID)+state.Seq(seq), plain))
		}
	}
	return ops, nil
}

// seqOf is the place an entry's key names.
func seqOf(key string) string {
	if i := strings.LastIndexByte(key, ':'); i >= 0 {
		return key[i+1:]
	}
	return key
}

type dayCount struct {
	Runs  int   `json:"runs,omitempty"`
	Spend Spend `json:"spend"`
}

type inFlight struct {
	N int `json:"n"`
}

type activeRun struct {
	Project string   `json:"project,omitempty"`
	Within  []string `json:"within,omitempty"`
	Agent   string   `json:"agent,omitempty"`
	Client  string   `json:"client,omitempty"`
}

type runRec struct {
	Live    Spend     `json:"live"`
	Settled Spend     `json:"settled"`
	Last    time.Time `json:"last"`
}

func (r runRec) counted() Spend { return r.Live.ceil(r.Settled) }

type keyRec struct {
	At time.Time `json:"at"`
}

type headRec struct {
	N int64 `json:"n"`
}
