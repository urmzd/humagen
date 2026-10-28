package platform

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/journal"
	"github.com/urmzd/humagen/pkg/runs"
)

// ShownEvent removes historical private placement and input provenance from a
// moved conversation's operational event view. Durable journal bytes are never
// rewritten. The publication preview names this exclusion explicitly.
func (p *Platform) ShownEvent(who auth.Principal, e event.Envelope) event.Envelope {
	hidden, moved := p.movedContext(who.Tenant, e.Run)
	if !moved || e.Data == nil {
		return e
	}
	raw, err := json.Marshal(e.Data)
	if err == nil {
		raw = scrubPayload(raw, hidden)
	}
	if err != nil || raw == nil {
		// What cannot be scrubbed is not shown: an empty body of its type.
		raw = []byte("{}")
	}
	e.Data = wire.BodyOf(e.Type, raw)
	return e
}

// APIEvent is an event as the API sends it: the envelope, with data as the
// JSON the reader is shown.
type APIEvent struct {
	ID      string          `json:"id"`
	Source  string          `json:"source"`
	Type    string          `json:"type"`
	Subject string          `json:"subject,omitempty"`
	Channel string          `json:"channel"`
	Time    time.Time       `json:"time"`
	Hops    int             `json:"hops"`
	Tenant  string          `json:"tenant,omitempty"`
	Run     string          `json:"run,omitempty"`
	Seq     uint64          `json:"seq,omitempty"`
	Agent   string          `json:"agent,omitempty"`
	Version string          `json:"version,omitempty"`
	Digest  string          `json:"digest,omitempty"`
	Data    json.RawMessage `json:"data"`
}

// ShownAPI is ShownEvent as the API sends it: a citation carries the
// evidence the platform recorded for it, and a moved conversation's private
// context is scrubbed from the data. ok is false for an event the API never
// sends: one outside the run family, or one whose data cannot be shown.
func (p *Platform) ShownAPI(who auth.Principal, e event.Envelope) (APIEvent, bool) {
	if event.Family(e.Type) != "run" || e.Tenant != who.Tenant {
		return APIEvent{}, false
	}
	var raw []byte
	switch {
	case e.Type == event.TypeCitation:
		if raw = p.citationPayload(e); raw == nil {
			return APIEvent{}, false
		}
	case e.Data != nil:
		b, err := json.Marshal(e.Data)
		if err != nil {
			return APIEvent{}, false
		}
		raw = b
	}
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	if hidden, moved := p.movedContext(who.Tenant, e.Run); moved {
		if raw = scrubPayload(raw, hidden); raw == nil {
			return APIEvent{}, false
		}
	}
	return APIEvent{
		ID: e.ID, Source: e.Source, Type: e.Type, Subject: e.Subject, Channel: e.Channel, Time: e.Time, Hops: e.Hops,
		Tenant: e.Tenant, Run: e.Run, Seq: e.Seq, Agent: e.Agent, Version: e.Version, Digest: e.Digest, Data: raw,
	}, true
}

// movedContext is what a moved conversation's run hides: its old project,
// what it counted toward and the sources it was pinned with. moved is false
// for a run whose conversation never moved.
func (p *Platform) movedContext(tenant, runID string) (hidden []string, moved bool) {
	run, err := p.Runs.Get(tenant, runID)
	if err != nil {
		return nil, false
	}
	th, err := p.Threads.Get(tenant, run.Thread)
	if err != nil || th.Moves == 0 {
		return nil, false
	}
	hidden = append([]string{run.Project}, run.Within...)
	return append(hidden, runs.SourcesOf(wire.Meta(run.Tag()))...), true
}

// scrubPayload removes provenance keys from a JSON body and replaces every
// hidden name in its strings. A body that is not JSON is dropped: nil.
func scrubPayload(raw []byte, hidden []string) []byte {
	var body any
	if json.Unmarshal(raw, &body) != nil {
		return nil
	}
	var scrub func(any) any
	scrub = func(value any) any {
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				switch key {
				case "meta", "input", "context", "citations", "sources", "source", "within":
					delete(v, key)
					continue
				}
				v[key] = scrub(child)
			}
		case []any:
			for i, child := range v {
				v[i] = scrub(child)
			}
		case string:
			for _, old := range hidden {
				if old != "" {
					v = strings.ReplaceAll(v, old, "[Private context]")
				}
			}
			return v
		}
		return value
	}
	out, err := json.Marshal(scrub(body))
	if err != nil {
		return nil
	}
	return out
}

// ShownSpans keeps activity timing and fixed operation labels while omitting
// private execution provenance from historical error details after a move.
func (p *Platform) ShownSpans(who auth.Principal, spans []journal.Span) []journal.Span {
	out := make([]journal.Span, len(spans))
	copy(out, spans)
	for i, span := range out {
		run, err := p.Runs.Get(who.Tenant, span.RunID)
		if err != nil {
			continue
		}
		th, err := p.Threads.Get(who.Tenant, run.Thread)
		if err != nil || th.Moves == 0 {
			continue
		}
		hidden := append([]string{run.Project}, run.Within...)
		hidden = append(hidden, runs.SourcesOf(wire.Meta(run.Tag()))...)
		for _, old := range hidden {
			if old != "" {
				out[i].Name = strings.ReplaceAll(out[i].Name, old, "[Private context]")
				out[i].Error = strings.ReplaceAll(out[i].Error, old, "[Private context]")
			}
		}
	}
	return out
}
