package inbox

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

// Remind updates one still-pending item once. The same cause keeps its ID,
// audience and parts. Its change resets read marks; settling and retention
// remove it with the ordinary message.
func (s *Store) Remind(tenant, id string, at time.Time) (Message, bool, error) {
	return s.do(Change{Tenant: tenant, Message: id, What: Reminded, By: "system", At: at})
}

// OpenPage bounds reconciliation and rotates past long-lived pending items:
// up to limit open or snoozed messages after a cursor, in the order they
// were raised, and the cursor of the next page, "" at the end.
func (s *Store) OpenPage(tenant, after string, limit int) ([]Message, string, error) {
	if limit < 1 || limit > 200 {
		limit = 200
	}
	if !strings.HasPrefix(after, "l:") {
		after = ""
	}
	out := make([]Message, 0, limit)
	last := ""
	_, err := scan(s.st, tenant, "l:", after, false, limit, func(r state.Record) (bool, error) {
		var km keptMessage
		if err := json.Unmarshal(r.Value, &km); err != nil {
			return false, err
		}
		out = append(out, km.message(tenant).clone())
		last = r.Key
		return true, nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("inbox: read: %w", err)
	}
	if len(out) < limit {
		last = ""
	}
	return out, last, nil
}
