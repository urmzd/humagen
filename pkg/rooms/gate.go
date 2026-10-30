package rooms

import (
	"fmt"
	"sync"
	"time"
)

// Limit names what refused a run.
type Limit string

const (
	// LimitRoomRate is a conversation's runs in any minute.
	LimitRoomRate Limit = "room_rate"
	// LimitPersonRuns is one person's runs in flight.
	LimitPersonRuns Limit = "person_runs"
)

// Refused is a run the gate did not let in.
type Refused struct {
	Limit Limit
	// Max is the limit.
	Max int
	// RetryAfter is when a place is next free, for LimitRoomRate. A
	// person's place frees when one of their runs ends, which nothing here
	// can say.
	RetryAfter time.Duration
}

func (e *Refused) Error() string {
	if e.Limit == LimitRoomRate {
		return fmt.Sprintf("this conversation has started %d runs in the last minute, the most one conversation may; try again in %s", e.Max, e.RetryAfter.Round(time.Second))
	}
	return fmt.Sprintf("you have %d runs in flight, the most one person may; wait for one to finish", e.Max)
}

// Counters is where the gate keeps its counts. Memory serves one process;
// a shared store implementing it serves every replica.
type Counters interface {
	// Take counts one admission against key when fewer than max were
	// counted in the window that ends at now. When it does not, retry is
	// how long until one leaves the window.
	Take(key string, now time.Time, window time.Duration, max int) (ok bool, retry time.Duration, err error)
	// Hold adds holder to key's holders when it has fewer than max, after
	// letting go of those gone reports have ended. A holder already held is
	// held once.
	Hold(key, holder string, max int, gone func(holder string) bool) (bool, error)
	// Free lets holder go from key. Letting go of one not held does
	// nothing.
	Free(key, holder string) error
}

// Ask is a run asking to be let in.
type Ask struct {
	Tenant string
	// Room is the conversation it starts in. "" counts toward no room.
	Room string
	// Person is the subject of whoever asked for it. "" counts toward
	// nobody: an agent's call, a schedule, an eval.
	Person string
	// Run is the run's id, which holds the person's place until Release.
	Run string
}

// Gate admits runs under a room's rate and a person's limit on runs in
// flight. It holds no lock of its own across the two: the caller serializes
// admission (the platform's admission lock), and Counters keeps each count
// consistent on its own.
type Gate struct {
	cfg      Config
	counters Counters
}

// NewGate returns a gate over counters. Nil counters are in memory.
func NewGate(cfg Config, counters Counters) *Gate {
	if counters == nil {
		counters = NewMemoryCounters()
	}
	return &Gate{cfg: cfg.Defaults(), counters: counters}
}

// Admit lets a run in or refuses it with *Refused. gone reports whether a
// run that holds a person's place has ended, so a place a run never gave
// back (a crash between admission and its end) is reclaimed the next time
// the person is at their limit.
func (g *Gate) Admit(a Ask, now time.Time, gone func(run string) bool) error {
	held := false
	if a.Person != "" {
		ok, err := g.counters.Hold(personKey(a.Tenant, a.Person), a.Run, g.cfg.PersonRuns, gone)
		if err != nil {
			return err
		}
		if !ok {
			return &Refused{Limit: LimitPersonRuns, Max: g.cfg.PersonRuns}
		}
		held = true
	}
	if a.Room != "" {
		ok, retry, err := g.counters.Take(roomKey(a.Tenant, a.Room), now, time.Minute, g.cfg.RoomRunsPerMinute)
		if err == nil && !ok {
			err = &Refused{Limit: LimitRoomRate, Max: g.cfg.RoomRunsPerMinute, RetryAfter: retry}
		}
		if err != nil {
			if held {
				_ = g.counters.Free(personKey(a.Tenant, a.Person), a.Run)
			}
			return err
		}
	}
	return nil
}

// Release gives a run's place back to the person who asked for it.
func (g *Gate) Release(tenant, person, run string) error {
	if person == "" {
		return nil
	}
	return g.counters.Free(personKey(tenant, person), run)
}

func personKey(tenant, person string) string { return "person\x00" + tenant + "\x00" + person }
func roomKey(tenant, room string) string     { return "room\x00" + tenant + "\x00" + room }

// MemoryCounters keeps counts in this process.
type MemoryCounters struct {
	mu    sync.Mutex
	rates map[string][]time.Time
	holds map[string]map[string]struct{}
}

// NewMemoryCounters returns empty counters.
func NewMemoryCounters() *MemoryCounters {
	return &MemoryCounters{rates: map[string][]time.Time{}, holds: map[string]map[string]struct{}{}}
}

// maxKeys bounds the rate keys kept before the ones with nothing left in
// their window are swept.
const maxKeys = 100_000

// Take implements Counters with a sliding window of at most max times.
func (m *MemoryCounters) Take(key string, now time.Time, window time.Duration, max int) (bool, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.rates) >= maxKeys {
		for k, times := range m.rates {
			if len(times) == 0 || !times[len(times)-1].After(now.Add(-window)) {
				delete(m.rates, k)
			}
		}
	}
	times := m.rates[key]
	cut := 0
	for cut < len(times) && !times[cut].After(now.Add(-window)) {
		cut++
	}
	times = times[cut:]
	if len(times) >= max {
		m.rates[key] = times
		return false, times[0].Add(window).Sub(now), nil
	}
	m.rates[key] = append(times, now)
	return true, 0, nil
}

// Hold implements Counters.
func (m *MemoryCounters) Hold(key, holder string, max int, gone func(string) bool) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	held := m.holds[key]
	if _, ok := held[holder]; ok {
		return true, nil
	}
	if len(held) >= max && gone != nil {
		for h := range held {
			if gone(h) {
				delete(held, h)
			}
		}
	}
	if len(held) >= max {
		return false, nil
	}
	if held == nil {
		held = map[string]struct{}{}
		m.holds[key] = held
	}
	held[holder] = struct{}{}
	return true, nil
}

// Free implements Counters.
func (m *MemoryCounters) Free(key, holder string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if held := m.holds[key]; held != nil {
		delete(held, holder)
		if len(held) == 0 {
			delete(m.holds, key)
		}
	}
	return nil
}
