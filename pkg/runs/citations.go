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
	x.mu.Lock()
	defer x.mu.Unlock()
	r, ok := x.runs[e.Run]
	if !ok || r.Tenant != e.Tenant {
		return ErrNotFound
	}
	copy := *r
	copy.Citations = CloneCitations(r.Citations)
	index := slices.IndexFunc(copy.Citations, func(c Citation) bool { return c.ID == next.ID })
	if index >= 0 {
		if copy.Citations[index].Mode != "" && next.Mode == "" {
			return nil
		}
		copy.Citations[index] = next
	} else {
		if len(copy.Citations) >= MaxCitations {
			return nil
		}
		copy.Citations = append(copy.Citations, next)
	}
	if x.kept != nil {
		if err := x.kept.runs.Put(copy.Tenant, copy.ID, x.kept.record(&copy)); err != nil {
			return err
		}
	}
	r.Citations = copy.Citations
	return nil
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
	return out
}
