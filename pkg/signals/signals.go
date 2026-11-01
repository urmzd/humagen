// Package signals sums up how one run went: what it cost, how long it took,
// how its tools and attempts fared, and how people rated it.
//
// A Record is derived. It is a pure fold of the run's events and its feedback
// records, holds no text, and can be rebuilt at any time from the journal and
// pkg/feedback. It never decides whether a run is over: the ledger does. It
// reports only what the events say (docs/rfcs/0008).
package signals

import (
	"cmp"
	"slices"

	"github.com/urmzd/humagen/fiber/pkg/event"

	"github.com/urmzd/humagen/pkg/feedback"
	"github.com/urmzd/humagen/pkg/usage"
)

// Ratings a run can have, as a whole.
const (
	Up    = "up"
	Down  = "down"
	Mixed = "mixed"
)

// Record is one run's signals.
type Record struct {
	Run string `json:"run"`

	// Spend is what the run's own model calls cost, in micro-USD priced in
	// US dollars. Calls it delegated are counted on their own runs, so a
	// tree of runs is not counted twice. It is a lower bound when
	// UnpricedCalls is not zero or some calls were priced in another
	// currency.
	Spend         int64 `json:"spend"`
	ModelCalls    int   `json:"model_calls"`
	Tokens        int64 `json:"tokens"`
	UnpricedCalls int   `json:"unpriced_calls"`

	// LatencyMS is from the run's first event to its last ending event.
	// Zero until an ending event is seen.
	LatencyMS int64 `json:"latency_ms"`
	// WaitingMS is the part of it the run spent parked, waiting on a person
	// or on another run, so a slow approver is not a slow agent.
	WaitingMS int64 `json:"waiting_ms"`

	// ToolCalls is how many tool calls finished, and ToolErrors how many of
	// them finished with an error.
	ToolCalls  int `json:"tool_calls"`
	ToolErrors int `json:"tool_errors"`

	// Attempts is the highest attempt a lifecycle event reported, and
	// Retries is that less the first.
	Attempts int `json:"attempts"`
	Retries  int `json:"retries"`

	// Up and Down count people's latest ratings. Rating is Up, Down, Mixed
	// or empty when nobody rated the run.
	Up     int    `json:"up"`
	Down   int    `json:"down"`
	Rating string `json:"rating,omitempty"`
}

// Fold derives run's signals from its events, in any order, and its feedback
// records. Events of other runs are ignored, and an event delivered twice
// (the same id) counts once.
func Fold(run string, events []event.Envelope, ratings []feedback.Record) Record {
	r := Record{Run: run}
	evs := make([]event.Envelope, 0, len(events))
	seen := map[string]bool{}
	for _, e := range events {
		if e.Run != run || (e.ID != "" && seen[e.ID]) {
			continue
		}
		seen[e.ID] = true
		evs = append(evs, e)
	}
	slices.SortStableFunc(evs, func(a, b event.Envelope) int {
		return cmp.Or(a.Time.Compare(b.Time), cmp.Compare(a.Seq, b.Seq))
	})

	var (
		tally   event.Tally
		final   *event.RunUsage
		parked  = -1 // index of the event the run parked at, or -1
		ended   = -1 // index of the last ending event, or -1
		waiting int64
	)
	unpark := func(i int) {
		if parked >= 0 {
			waiting += max(evs[i].Time.Sub(evs[parked].Time).Milliseconds(), 0)
			parked = -1
		}
	}
	for i, e := range evs {
		switch b := e.Data.(type) {
		case event.Accepted:
			r.Attempts = max(r.Attempts, b.Attempt)
		case event.Started:
			r.Attempts = max(r.Attempts, b.Attempt)
		case event.Parked:
			r.Attempts = max(r.Attempts, b.Attempt)
			if parked < 0 {
				parked = i
			}
		case event.Resumed:
			r.Attempts = max(r.Attempts, b.Attempt)
			unpark(i)
		case event.Completed:
			r.Attempts = max(r.Attempts, b.Attempt)
			unpark(i)
			ended = i
			if b.Usage != nil {
				final = b.Usage
			}
		case event.Stopped:
			r.Attempts = max(r.Attempts, b.Attempt)
			unpark(i)
			ended = i
			if b.Usage != nil {
				final = b.Usage
			}
		case event.Failed:
			// A failed attempt may be retried. Only a terminal failure is
			// an ending.
			r.Attempts = max(r.Attempts, b.Attempt)
			if b.Terminal {
				unpark(i)
				ended = i
			}
		case event.ToolDone:
			r.ToolCalls++
			if b.Error != "" {
				r.ToolErrors++
			}
		case event.UsageCall:
			tally.Call(b)
		}
	}
	r.Retries = max(r.Attempts-1, 0)
	if ended >= 0 {
		r.LatencyMS = max(evs[ended].Time.Sub(evs[0].Time).Milliseconds(), 0)
		r.WaitingMS = min(waiting, r.LatencyMS)
	}

	// The runtime's own record replaces what was tallied from the calls.
	own := tally.Usage().Own
	if final != nil {
		own = final.Own
	}
	s := usage.Of(own)
	r.Spend, r.ModelCalls, r.Tokens, r.UnpricedCalls = s.Micros, s.ModelCalls, s.Tokens, s.Unpriced

	for _, f := range ratings {
		if f.Run != run {
			continue
		}
		switch f.Rating {
		case Up:
			r.Up++
		case Down:
			r.Down++
		}
	}
	switch {
	case r.Up > 0 && r.Down > 0:
		r.Rating = Mixed
	case r.Up > 0:
		r.Rating = Up
	case r.Down > 0:
		r.Rating = Down
	}
	return r
}
