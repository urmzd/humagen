package wire

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// retiredAgentFields are fields an agent spec once took and no longer reads,
// each with why. A spec in a config file that still has one loads without
// it, with a note, so an older config never stops a start. The API takes the
// spec as it is now and refuses them.
var retiredAgentFields = map[string]string{
	"choices": "people pick agents, not models: an agent's definition sets its model chain and effort",
}

// DropRetiredAgentFields is an agent spec in JSON without the fields it no
// longer reads, and a note for each it held. Anything other than a JSON
// object is returned as it is, for the strict decode after it to refuse.
func DropRetiredAgentFields(raw []byte) ([]byte, []string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(trimmed, "{") {
		return raw, nil, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return raw, nil, nil
	}
	var notes []string
	for name, why := range retiredAgentFields {
		if _, ok := fields[name]; ok {
			delete(fields, name)
			notes = append(notes, fmt.Sprintf("%s is no longer read: %s", name, why))
		}
	}
	if len(notes) == 0 {
		return raw, nil, nil
	}
	slices.Sort(notes)
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, nil, err
	}
	return out, notes, nil
}
