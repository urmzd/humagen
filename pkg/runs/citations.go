package runs

import (
	"fmt"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/urmzd/humagen/fiber/pkg/event"
)

const MaxCitations = 64

// Citation retains the exact turn's evidence. Missing Mode is unknown; a
// source's current configuration must never rewrite historical facts.
type Citation struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	URL           string     `json:"url,omitempty"`
	Snippet       string     `json:"snippet,omitempty"`
	Mode          string     `json:"mode,omitempty"`
	CopyAt        *time.Time `json:"copy_at,omitempty"`
	RefreshFailed *bool      `json:"refresh_failed,omitempty"`
}

// CitationMetadata is supplied only by a trusted retrieval recorder. Raw
// event payload metadata is deliberately ignored by RecordCitation.
type CitationMetadata struct {
	Mode          string
	CopyAt        *time.Time
	RefreshFailed bool
}

func clipCitation(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
func CloneCitations(in []Citation) []Citation {
	out := slices.Clone(in)
	for i, c := range out {
		if c.CopyAt != nil {
			at := *c.CopyAt
			out[i].CopyAt = &at
		}
		if c.RefreshFailed != nil {
			failed := *c.RefreshFailed
			out[i].RefreshFailed = &failed
		}
	}
	return out
}

// RecordCitation writes through, so a successful citation never depends on
// bounded journal retention or a later dirty-index flush.
func (x *Index) RecordCitation(e event.Envelope, metadata *CitationMetadata) error {
	if e.Type != event.TypeCitation {
		return nil
	}
	body, ok := e.Data.(event.Citation)
	if !ok {
		return fmt.Errorf("runs: %s carries %T", e.Type, e.Data)
	}
	label := body.Label
	if label == "" {
		label = strconv.Itoa(body.Index)
	}
	title := body.Source.DocumentTitle
	if title == "" {
		title = body.Source.SectionHeading
	}
	if title == "" {
		title = "Source"
	}
	next := Citation{ID: clipCitation(label, 128), Title: clipCitation(title, 500), URL: clipCitation(body.Source.SourceURI, 2048), Snippet: clipCitation(body.Text, 8000)}
	if metadata != nil && (metadata.Mode == "indexed" || metadata.Mode == "live") {
		next.Mode = metadata.Mode
		if metadata.CopyAt != nil {
			at := *metadata.CopyAt
			next.CopyAt = &at
		}
		failed := metadata.RefreshFailed
		next.RefreshFailed = &failed
	}
	return x.do(e.Tenant, func(t *txn) error {
		r, err := t.run(e.Run)
		if err != nil {
			return err
		}
		if r == nil {
			return ErrNotFound
		}
		citations := CloneCitations(r.Citations)
		index := slices.IndexFunc(citations, func(c Citation) bool { return c.ID == next.ID })
		if index >= 0 {
			if citations[index].Mode != "" && next.Mode == "" {
				return errSkip
			}
			citations[index] = next
		} else {
			if len(citations) >= MaxCitations {
				return errSkip
			}
			citations = append(citations, next)
		}
		r.Citations = citations
		t.touch(r)
		return nil
	})
}

// CitationPayload preserves the established wire body while adding only
// recorder-proven metadata. The application constructs it after verification.
type CitationPayload struct {
	event.Citation
	Mode          string     `json:"mode,omitempty"`
	CopyAt        *time.Time `json:"copy_at,omitempty"`
	RefreshFailed *bool      `json:"refresh_failed,omitempty"`
}

func citationSnapshot(r *Run) Run {
	out := *r
	out.Citations = CloneCitations(r.Citations)
	// Where the store keeps the run is not part of it: two processes, or
	// two indexes, return the same run the same.
	out.seq, out.observedBy, out.lastSeq, out.openAttached, out.tally = 0, "", 0, 0, nil
	return out
}
