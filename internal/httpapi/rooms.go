package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/rooms"
	"github.com/urmzd/humagen/pkg/threads"
)

// roomEvents streams one conversation as its room is read (pkg/rooms): in a
// small room every run event and thread notice as it happens, as the
// workspace's stream sends them; in a large one what involves the reader at
// once, and the rest as a page every few seconds (event: page), after which
// the reader fetches the conversation's newest turns.
//
// What reaches the reader is checked as on the workspace's stream, so a page
// is the only thing sent without a per-event check, and it says only how
// much happened.
func (s *Server) roomEvents(w http.ResponseWriter, r *http.Request, who auth.Principal, thread string, types []string) {
	if !s.P.SeesThread(who, thread) {
		notFound(w)
		return
	}
	v := s.P.Rooms.Join(who.Tenant, thread, who.Subject)
	defer v.Leave()

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_ = rc.Flush()

	gate := newRunGate(s.P, who)
	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-v.Done():
			// Fell behind, or the room's source ended: the client
			// reconnects and reads the conversation again.
			return
		case <-tick.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
		case it := <-v.Items():
			if err := s.sendRoomItem(w, who, gate, types, it); err != nil {
				return
			}
		case <-v.Pages():
			page, fresh := v.Page()
			if !fresh {
				continue
			}
			// A reader who lost the conversation since joining is told
			// nothing more of it.
			if !s.P.SeesThread(who, thread) {
				return
			}
			b, err := json.Marshal(page)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: page\ndata: %s\n\n", b); err != nil {
				return
			}
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}

// sendRoomItem writes one item of a room to a reader, when they may read it
// and it is of a type they asked for.
func (s *Server) sendRoomItem(w http.ResponseWriter, who auth.Principal, gate *runGate, types []string, it rooms.Item) error {
	switch v := it.Value.(type) {
	case threads.Notice:
		if !s.P.SeesThread(who, v.ID) {
			return nil
		}
		b, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		_, err = fmt.Fprintf(w, "event: thread\ndata: %s\n\n", b)
		return err
	case event.Envelope:
		if len(types) > 0 && !slices.ContainsFunc(types, func(p string) bool { return event.Selects(p, v.Type) }) {
			return nil
		}
		return s.sendRunEvent(w, who, gate, v)
	}
	return nil
}
