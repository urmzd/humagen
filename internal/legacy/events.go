package legacy

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/urmzd/humagen/fiber/pkg/event"
	fl "github.com/urmzd/humagen/fiber/pkg/event/legacy"
)

// Run triggers and run events before the event union.
//
// A run was raised by a tag and published events with a kind. Stored run
// rows hold the tag, the journal holds the events, and an old reader still
// names what it streams with a topic and filters the journal by kind.
// Everything that reads those shapes is here.

// HumagenChannelScheme is the scheme of the channel a run raised on no
// surface publishes on. It is wire.ChannelScheme, spelled here because wire
// cannot be imported by this package's callers below it.
const HumagenChannelScheme = "humagen"

// surfaces are the origins whose old tags carried the surface they answer.
var surfaces = map[string]bool{"slack": true, "github": true, "jira": true}

// DecodeTag reads a run's old tag as the event it is now: the tag's agent is
// the one the event addresses, its meta the request context. Keys starting
// with "_" stay in the context, which is kept with the run and never shown
// to a model, rather than moving to Private, which is never kept: the run
// row is their only home. A tag Humagen raised itself answers on no surface.
func DecodeTag(raw []byte) (event.Envelope, error) {
	t, err := fl.DecodeTag(raw)
	if err != nil {
		return event.Envelope{}, err
	}
	e := t.Event
	e.Agent = t.Agent
	if len(e.Private) > 0 {
		r, ok := event.RequestOf(e.Data)
		if ok {
			ctx := make(map[string]string, len(r.Context)+len(e.Private))
			for k, v := range r.Context {
				ctx[k] = v
			}
			for k, v := range e.Private {
				ctx[k] = v
			}
			r.Context = ctx
			if b, ok := event.WithRequest(e.Data, r); ok {
				e.Data = b
				e.Private = nil
			}
		}
	}
	origin, _, _ := strings.Cut(e.Source, ":")
	if !surfaces[origin] {
		e.Source = origin + ":" + e.Tenant
		e.Channel = HumagenChannelScheme + "://" + e.Tenant
	}
	return e, nil
}

// KindOf is the kind an event's type had before the union, and false for a
// type that had none.
func KindOf(typ string) (string, bool) { return fl.KindOf(typ) }

// KindSelector reads an old kind selector ("lifecycle", "delta.text") as the
// type prefixes it means now. An unknown or dropped selector is an error.
func KindSelector(sel string) ([]string, error) { return fl.KindSelector(sel) }

// TypeMatchesKind reports whether a type is what an old kind selector chose:
// an exact kind or a family.
func TypeMatchesKind(typ, sel string) bool {
	prefixes, err := fl.KindSelector(sel)
	if err != nil {
		return false
	}
	for _, p := range prefixes {
		if event.Selects(p, typ) {
			return true
		}
	}
	return false
}

// RevOf is the revision a definition version names: N for "N.0.0", or for
// the bare "N" an event read before the union carries. The API's run and
// journal records still count revisions.
func RevOf(version string) int {
	major, _, _ := strings.Cut(version, ".")
	n, err := strconv.Atoi(major)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// DecodeV1 reads an event stored in the old shape, with the origin it
// carried.
func DecodeV1(raw []byte) (event.Envelope, string, error) {
	var probe struct {
		Origin string `json:"origin"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return event.Envelope{}, "", err
	}
	e, err := fl.DecodeEntry(raw)
	return e, probe.Origin, err
}

// Topic reads an old SSE topic ("agent", "agent:<name>", "agent:<name>:<run>")
// as the agent and run it named. An empty topic is everything.
func Topic(raw string) (agent, run string, err error) {
	if raw == "" || raw == "agent" {
		return "", "", nil
	}
	parts := strings.Split(raw, ":")
	if parts[0] != "agent" || len(parts) > 3 {
		return "", "", fmt.Errorf("legacy: topic %q is not agent[:<name>[:<run>]]", raw)
	}
	for _, p := range parts[1:] {
		if p == "" {
			return "", "", fmt.Errorf("legacy: topic %q has an empty segment", raw)
		}
	}
	agent = parts[1]
	if len(parts) == 3 {
		run = parts[2]
	}
	return agent, run, nil
}

// Stored is an event as a record keeps it: written as the envelope, read in
// either shape, an envelope or a tag written before the union.
type Stored struct{ Event event.Envelope }

// MarshalJSON writes the envelope.
func (s Stored) MarshalJSON() ([]byte, error) { return json.Marshal(s.Event) }

// UnmarshalJSON reads an envelope, or an old tag through DecodeTag.
func (s *Stored) UnmarshalJSON(b []byte) error {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return err
	}
	if probe.Type == "" {
		e, err := DecodeTag(b)
		if err != nil {
			return err
		}
		s.Event = e
		return nil
	}
	return json.Unmarshal(b, &s.Event)
}

// ScheduleContext reads the context of a schedule stored before the event
// union, when a schedule's context was kept under "Meta" and its delivery
// routes under "Routes", which nothing reads now. A schedule with neither
// has no context.
func ScheduleContext(raw []byte) (map[string]string, error) {
	var old struct {
		Meta map[string]string `json:"Meta"`
	}
	if err := json.Unmarshal(raw, &old); err != nil {
		return nil, fmt.Errorf("legacy: schedule: %w", err)
	}
	return old.Meta, nil
}
