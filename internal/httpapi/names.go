package httpapi

import (
	"encoding/json"

	"github.com/urmzd/fiber/pkg/envelope"

	"github.com/urmzd/humagen/internal/platform"
)

// How a run's events call the people in them.
//
// The runtime records who answered a question as the platform authenticated
// them, by subject, and an event keeps it so: it is the record. A console
// shows a name, and never a subject that holds an address, so an event a
// route hands out carries what the answerer is called beside the subject
// (answer.by_name on lifecycle.resumed). The journal is not changed.

// shownEvent is an event as it is handed out: a resumed run's answer with
// what whoever answered is called. Any other event, and one whose body does
// not read as the runtime writes it, is handed out as it is.
func shownEvent(names platform.Names, e envelope.Event) envelope.Event {
	if e.Kind != envelope.KindResumed || len(e.Payload) == 0 {
		return e
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(e.Payload, &body); err != nil || len(body["answer"]) == 0 {
		return e
	}
	var answer map[string]json.RawMessage
	if err := json.Unmarshal(body["answer"], &answer); err != nil || answer == nil {
		return e
	}
	var by string
	if err := json.Unmarshal(answer["by"], &by); err != nil || by == "" {
		return e
	}
	name, err := json.Marshal(names.Of(by))
	if err != nil {
		return e
	}
	answer["by_name"] = name
	raw, err := json.Marshal(answer)
	if err != nil {
		return e
	}
	body["answer"] = raw
	payload, err := json.Marshal(body)
	if err != nil {
		return e
	}
	e.Payload = payload
	return e
}

// shownEvents is a run's events as they are handed out (shownEvent).
func shownEvents(names platform.Names, events []envelope.Event) []envelope.Event {
	out := make([]envelope.Event, len(events))
	for i, e := range events {
		out[i] = shownEvent(names, e)
	}
	return out
}
