package evals

import "time"

// NextRun is when a scheduled suite is next due: its latest start, by a
// person or by the schedule, plus its period. It is the zero time when the
// suite has a schedule and has never run, so the next scan starts it. ok is
// false when the suite has no schedule.
func NextRun(s Suite, runs []Run) (next time.Time, ok bool) {
	if s.EverySeconds <= 0 {
		return time.Time{}, false
	}
	var last time.Time
	for _, r := range runs {
		if r.StartedAt.After(last) {
			last = r.StartedAt
		}
	}
	if last.IsZero() {
		return time.Time{}, true
	}
	return last.Add(time.Duration(s.EverySeconds) * time.Second), true
}
