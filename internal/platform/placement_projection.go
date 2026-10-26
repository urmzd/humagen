package platform

import (
	"encoding/json"
	"strings"

	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/journal"
	"github.com/urmzd/humagen/pkg/runs"
)

// ShownEvent removes historical private placement and input provenance from a
// moved conversation's operational event view. Durable journal bytes are never
// rewritten. The publication preview names this exclusion explicitly.
func (p *Platform) ShownEvent(who auth.Principal, event envelope.Event) envelope.Event {
	run, err := p.Runs.Get(who.Tenant, event.RunID)
	if err != nil {
		return event
	}
	th, err := p.Threads.Get(who.Tenant, run.Thread)
	if err != nil || th.Moves == 0 {
		return event
	}
	var body any
	if json.Unmarshal(event.Payload, &body) != nil {
		event.Payload = nil
		return event
	}
	hidden := append([]string{run.Project}, run.Within...)
	hidden = append(hidden, runs.SourcesOf(run.Tag().Meta)...)
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
	raw, err := json.Marshal(scrub(body))
	if err != nil {
		event.Payload = nil
	} else {
		event.Payload = raw
	}
	return event
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
		hidden = append(hidden, runs.SourcesOf(run.Tag().Meta)...)
		for _, old := range hidden {
			if old != "" {
				out[i].Name = strings.ReplaceAll(out[i].Name, old, "[Private context]")
				out[i].Error = strings.ReplaceAll(out[i].Error, old, "[Private context]")
			}
		}
	}
	return out
}
