// Package rooms keeps a conversation readable, and admissible, when a great
// many people are in it (RFC 0004, "A million people in one room").
//
// A room is not a broadcast. Sending every event to every viewer costs the
// events times the viewers, which no server can pay for a large room. So a
// room is read in two ways at once, and which one a viewer is given depends
// on how many viewers the room has:
//
//   - In a small room, at most Config.CoalesceAbove viewers, every viewer is
//     sent every event as it happens, exactly as a stream that follows the
//     whole workspace would send them.
//   - In a large room, a viewer is sent at once only what involves them: the
//     messages they posted or are named in, the runs they asked for, and the
//     questions put to them. Everything else is folded into the room's tail,
//     a Page sent to every viewer at most once every Config.TailInterval. A
//     page says how much happened and nothing about what: the viewer reads
//     the conversation's newest turns through the API, as anyone else would.
//
// The Hub holds one room per conversation with viewers on this server and
// reads the conversation's events once for all of them (Source), so what a
// server delivers each second is bounded by the events that involve someone
// plus the viewers divided by the tail interval, whatever the message rate.
//
// The Gate is the admission half: a per-room rate of runs and a per-person
// limit on runs in flight, kept in Counters, which a shared store can hold.
package rooms

import (
	"errors"
	"fmt"
	"time"
)

// Defaults. Each is far above what an ordinary conversation reaches, so a
// room that is not large behaves as it did before rooms were coalesced or
// rate limited.
const (
	// DefaultCoalesceAbove is how many viewers a room has on one server
	// before its tail is coalesced.
	DefaultCoalesceAbove = 200
	// DefaultTailInterval is how often a large room's tail is sent.
	DefaultTailInterval = 3 * time.Second
	// DefaultRoomRunsPerMinute is how many runs one conversation starts in
	// a minute.
	DefaultRoomRunsPerMinute = 600
	// DefaultPersonRuns is how many runs one person has in flight at once.
	DefaultPersonRuns = 25
)

// Bounds on what a deployment may set.
const (
	MaxCoalesceAbove     = 10_000_000
	MinTailInterval      = 500 * time.Millisecond
	MaxTailInterval      = time.Minute
	MaxRoomRunsPerMinute = 100_000
	MaxPersonRuns        = 1000
	// ViewerBuffer is how many immediate events a viewer may fall behind by
	// before it is ended, as the bus ends a slow consumer. Pages never fill
	// it: a viewer that missed a page is sent the newest one.
	ViewerBuffer = 256
)

// ErrSlow ends a viewer that fell ViewerBuffer events behind. The client
// reconnects and reads the conversation again.
var ErrSlow = errors.New("rooms: the viewer fell behind")

// ErrClosed ends every viewer of a hub that was closed.
var ErrClosed = errors.New("rooms: closed")

// Config is how a deployment reads and admits large rooms. A zero field is
// its default (Defaults).
type Config struct {
	// CoalesceAbove is how many viewers a room has on one server before a
	// viewer is sent only what involves them at once and the rest as pages.
	CoalesceAbove int
	// TailInterval is how often a large room's tail is sent.
	TailInterval time.Duration
	// RoomRunsPerMinute is how many runs one conversation starts in any
	// minute.
	RoomRunsPerMinute int
	// PersonRuns is how many runs one person has in flight at once.
	PersonRuns int
}

// Defaults fills every zero field with its default.
func (c Config) Defaults() Config {
	if c.CoalesceAbove == 0 {
		c.CoalesceAbove = DefaultCoalesceAbove
	}
	if c.TailInterval == 0 {
		c.TailInterval = DefaultTailInterval
	}
	if c.RoomRunsPerMinute == 0 {
		c.RoomRunsPerMinute = DefaultRoomRunsPerMinute
	}
	if c.PersonRuns == 0 {
		c.PersonRuns = DefaultPersonRuns
	}
	return c
}

// Check refuses a setting outside its bounds. Zero is the default, so it is
// accepted.
func (c Config) Check() error {
	switch {
	case c.CoalesceAbove < 0 || c.CoalesceAbove > MaxCoalesceAbove:
		return fmt.Errorf("rooms: coalesce_above is 1 to %d viewers", MaxCoalesceAbove)
	case c.TailInterval != 0 && (c.TailInterval < MinTailInterval || c.TailInterval > MaxTailInterval):
		return fmt.Errorf("rooms: tail_interval is %s to %s", MinTailInterval, MaxTailInterval)
	case c.RoomRunsPerMinute < 0 || c.RoomRunsPerMinute > MaxRoomRunsPerMinute:
		return fmt.Errorf("rooms: max_runs_per_minute is 1 to %d", MaxRoomRunsPerMinute)
	case c.PersonRuns < 0 || c.PersonRuns > MaxPersonRuns:
		return fmt.Errorf("rooms: max_runs_per_person is 1 to %d", MaxPersonRuns)
	}
	return nil
}
