package platform

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/rooms"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// openRooms starts the rooms of conversations followed on this server and
// the gate that admits runs into them (pkg/rooms).
func (p *Platform) openRooms() error {
	if err := p.cfg.Rooms.Check(); err != nil {
		return err
	}
	p.Rooms = rooms.NewHub(p.cfg.Rooms, p.roomSource)
	counters := p.cfg.RoomCounters
	if counters == nil && p.cfg.Backends.RoomCounters != nil {
		counters = p.cfg.Backends.RoomCounters
	}
	p.roomGate = rooms.NewGate(p.cfg.Rooms, counters)
	p.closers = append(p.closers, p.Rooms.Close)
	return nil
}

// replaySlack is how much older than its room an event may be and still be
// read as live. The bus replays what it retains to a new subscriber; a room
// opened for its first viewer is not owed that, since the viewer reads the
// conversation through the API when it connects.
const replaySlack = time.Second

// roomCache bounds the runs a room remembers the place of.
const roomCache = 4096

// roomSource reads one conversation's run events and thread notices once,
// for every viewer of its room, and says whom each involves.
func (p *Platform) roomSource(ctx context.Context, tenant, thread string, publish func(rooms.Item)) error {
	opened := time.Now()
	stream, err := p.Subscribe(ctx, tenant, EventFilter{})
	if err != nil {
		return err
	}
	defer func() { _ = stream.Close() }()
	notices, unwatch := p.WatchThreads(tenant)
	defer unwatch()

	events := make(chan event.Envelope)
	failed := make(chan error, 1)
	go func() {
		for {
			e, err := stream.Recv(ctx)
			if err != nil {
				failed <- err
				return
			}
			select {
			case events <- e:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Whether each run is this conversation's, and whom it involves.
	type known struct {
		in       bool
		involves []string
	}
	cache := map[string]known{}
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-failed:
			if ctx.Err() != nil {
				return nil
			}
			return err
		case n := <-notices:
			if n.ID != thread {
				continue
			}
			publish(rooms.Item{Kind: rooms.KindThread, Involves: p.messageInvolves(tenant, thread, n.Message), Value: n})
		case e := <-events:
			if e.Time.Before(opened.Add(-replaySlack)) {
				continue
			}
			k, ok := cache[e.Run]
			// A change of state can change whom a run involves: a question
			// put to someone.
			if !ok || runStateChange(e.Type) {
				at, found := p.placeOfRunID(tenant, e.Run)
				k = known{in: found && at.Thread == thread}
				if k.in {
					k.involves = p.runInvolves(tenant, e)
				}
				if len(cache) >= roomCache {
					clear(cache)
				}
				// A run not placed yet is asked again at its next event.
				if found {
					cache[e.Run] = k
				}
			}
			if k.in {
				publish(rooms.Item{Kind: rooms.KindRun, Involves: k.involves, Value: e})
			}
		}
	}
}

// runStateChange reports a type that changes a run's state.
func runStateChange(t string) bool {
	switch t {
	case event.TypeAccepted, event.TypeStarted, event.TypeParked, event.TypeChildParked, event.TypeResumed,
		event.TypeCompleted, event.TypeFailed, event.TypeStopped:
		return true
	}
	return false
}

// runInvolves is whom a run's event is for: who asked for the run, by every
// name they go by, and whom a question it parked on is put to.
func (p *Platform) runInvolves(tenant string, e event.Envelope) []string {
	var out []string
	add := func(names ...string) {
		for _, n := range names {
			if n != "" && !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
	}
	if run, err := p.Runs.Get(tenant, e.Run); err == nil {
		add(runs.AskedBy(run))
		add(run.Requesters()...)
		if q := run.Question; q != nil {
			add(q.AskedOf)
			add(q.Approvers...)
		}
	} else if v, ok := p.admitting.Load(e.Run); ok {
		if a := v.(admitted); a.tenant == tenant {
			add(a.person, a.actor.User)
		}
	}
	switch b := e.Data.(type) {
	case event.Parked:
		if b.Question != nil {
			add(b.Question.AskedOf)
			add(b.Question.Approvers...)
		}
	case event.ChildParked:
		add(b.Question.AskedOf)
		add(b.Question.Approvers...)
	}
	return out
}

// messageInvolves is whom a message is for: who posted it and the people it
// names.
func (p *Platform) messageInvolves(tenant, thread, id string) []string {
	if id == "" {
		return nil
	}
	m, err := p.Threads.Message(tenant, thread, id)
	if err != nil {
		return nil
	}
	out := []string{m.By}
	for _, mn := range m.Mentions {
		if mn.Kind == threads.Person && !slices.Contains(out, mn.Name) {
			out = append(out, mn.Name)
		}
	}
	return out
}

// admitRoom holds a run to its conversation's rate and to the limit on runs
// in flight of the person who asked for it. Only a run a person or a surface
// started counts: a call between agents is its root's work, and a schedule's
// or an eval's run is asked by nobody.
func (p *Platform) admitRoom(tag event.Envelope, edge call.Edge, project string) error {
	if edge.Parent != "" {
		return nil
	}
	runID := runtime.RunID(tag.Tenant, tag.ID)
	ask := rooms.Ask{
		Tenant: tag.Tenant,
		Room:   p.placeAdmitted(tag, edge, runID, project).Thread,
		Person: personOfTag(tag),
		Run:    runID,
	}
	err := p.roomGate.Admit(ask, p.now(), func(run string) bool {
		r, err := p.Runs.Get(tag.Tenant, run)
		return err != nil || r.Status.Terminal()
	})
	var refused *rooms.Refused
	if errors.As(err, &refused) {
		p.log.Warn("platform: run refused by a room limit", "tenant", tag.Tenant, "agent", tag.Agent, "thread", ask.Room, "limit", refused.Limit)
	}
	return err
}

// unadmitRoom gives back the place admitRoom took for a run that was not let
// in after all.
func (p *Platform) unadmitRoom(tag event.Envelope, edge call.Edge) {
	if edge.Parent != "" {
		return
	}
	_ = p.roomGate.Release(tag.Tenant, personOfTag(tag), runtime.RunID(tag.Tenant, tag.ID))
}

// releaseRoom gives an ended run's place back to whoever asked for it.
func (p *Platform) releaseRoom(run runs.Run) {
	if run.Parent != "" {
		return
	}
	if err := p.roomGate.Release(run.Tenant, personOfTag(run.Tag()), run.ID); err != nil {
		p.log.Error("platform: a room place could not be given back", "run", run.ID, "error", err)
	}
}

// personOfTag is who asked for a run, as runs.AskedBy names them, read from
// its tag before the run is recorded.
func personOfTag(tag event.Envelope) string {
	actor := wire.ActorOf(tag)
	switch {
	case actor.Bot:
		return ""
	case actor.User != "":
		return actor.User
	case wire.Origin(tag) == runs.OriginAPI && !strings.HasPrefix(tag.ID, "api-retry-"):
		return actor.ID
	}
	return ""
}
