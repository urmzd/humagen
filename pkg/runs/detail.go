package runs

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/envelope"
)

// MaxCitations bounds evidence retained independently of the bounded journal.
const MaxCitations = 64

// Outcome is the agent's authored result, never an action approval or proof
// of external execution. Tool and call records remain the execution truth.
type Outcome struct {
	Kind       string   `json:"kind"`
	Summary    string   `json:"summary"`
	Decisions  []string `json:"decisions,omitempty"`
	NextAction string   `json:"next_action,omitempty"`
}

// Validate refuses ambiguous kinds and oversized authored result fields.
func (o Outcome) Validate() error {
	if o.Kind != "recommendation" && o.Kind != "decision" && o.Kind != "update" {
		return errors.New("outcome: kind must be recommendation, decision or update")
	}
	if strings.TrimSpace(o.Summary) == "" || len(o.Summary) > 4000 || len(o.NextAction) > 2000 || len(o.Decisions) > 20 || !utf8.ValidString(o.Summary+o.NextAction) {
		return errors.New("outcome: a summary up to 4000 bytes, next action up to 2000 bytes and at most 20 decisions are required")
	}
	for _, d := range o.Decisions {
		if len(d) > 1000 || !utf8.ValidString(d) {
			return errors.New("outcome: each decision must be valid text of at most 1000 bytes")
		}
	}
	return nil
}

// Citation is the reader-facing evidence used for this run, with no storage
// keys or hidden source identifiers. Its title and passage are pinned at use.
type Citation struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	URL     string `json:"url,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

func clipDetail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
func (x *Index) observeDetail(e envelope.Event) Run {
	x.mu.Lock()
	defer x.mu.Unlock()
	r, ok := x.runs[e.RunID]
	if !ok || r.Tenant != e.Tenant || r.Status.Terminal() {
		return Run{}
	}
	switch e.Kind {
	case envelope.KindCitation:
		c, err := payload.Decode[payload.Citation](e)
		if err != nil {
			return Run{}
		}
		title := c.Source.DocumentTitle
		if title == "" {
			title = c.Source.SectionHeading
		}
		if title == "" {
			title = "Source"
		}
		label := c.Label
		if label == "" {
			label = strconv.Itoa(c.Index)
		}
		next := Citation{ID: clipDetail(label, 128), Title: clipDetail(title, 500), URL: clipDetail(c.Source.SourceURI, 2048), Snippet: clipDetail(c.Text, 8000)}
		found := -1
		for i, old := range r.Citations {
			if old.ID == next.ID {
				found = i
				break
			}
		}
		if found >= 0 {
			r.Citations[found] = next
		} else if len(r.Citations) < MaxCitations {
			r.Citations = append(r.Citations, next)
		}
	case envelope.KindActionTaken:
		action, err := payload.Decode[payload.Action](e)
		if err != nil || action.Name != "record_outcome" || action.Error != "" || len(action.Data) > 30000 {
			return Run{}
		}
		var outcome Outcome
		if json.Unmarshal(action.Data, &outcome) != nil || outcome.Validate() != nil {
			return Run{}
		}
		r.Outcome = &outcome
	}
	x.touch(r.ID)
	return *r
}
