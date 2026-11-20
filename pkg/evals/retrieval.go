package evals

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/urmzd/humagen/pkg/state"
)

// Retrieval is the exact context given to a model, including a recorded empty
// result. Sources and blocks are retained data; replay never dereferences them.
type Retrieval struct {
	Input   string         `json:"input"`
	Query   string         `json:"query"`
	Sources []string       `json:"sources"`
	Prompt  string         `json:"prompt"`
	Blocks  []ContextBlock `json:"blocks"`
	Digest  string         `json:"digest"`
}
type ContextBlock struct {
	Text       string     `json:"text"`
	Citation   string     `json:"citation"`
	Provenance Provenance `json:"provenance"`
}
type Provenance struct {
	DocumentUUID   string `json:"document_uuid,omitempty"`
	DocumentTitle  string `json:"document_title,omitempty"`
	SourceURI      string `json:"source_uri,omitempty"`
	SectionUUID    string `json:"section_uuid,omitempty"`
	SectionHeading string `json:"section_heading,omitempty"`
	SectionIndex   int    `json:"section_index,omitempty"`
}

// Seal binds the complete snapshot, including its case input and source set.
func (r Retrieval) Seal() Retrieval {
	r.Sources, r.Blocks = slices.Clone(r.Sources), slices.Clone(r.Blocks)
	r.Digest = ""
	raw, _ := json.Marshal(r)
	r.Digest = identity(string(raw))
	return r
}
func (r Retrieval) Validate() error {
	raw, _ := json.Marshal(r)
	if len(r.Sources) == 0 || len(r.Sources) > 64 || len(r.Blocks) > 64 || len(raw) > 512000 || r.Digest == "" || r.Seal().Digest != r.Digest {
		return fmt.Errorf("%w: malformed or changed retrieval snapshot", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, name := range r.Sources {
		if name == "" || len(name) > 256 || seen[name] {
			return fmt.Errorf("%w: invalid recorded source", ErrInvalid)
		}
		seen[name] = true
	}
	return nil
}

// RecordRetrieval is immutable per execution attempt and survives process
// replacement. Recording different context under the same attempt fails closed.
func (s *Store) RecordRetrieval(tenant, run string, attempt int, r Retrieval) error {
	if tenant == "" || run == "" || attempt < 1 || attempt > 100 {
		return ErrInvalid
	}
	if err := r.Validate(); err != nil {
		return err
	}
	_, err := s.retrievals.Update(tenant, run+"/"+state.Seq(int64(attempt)), func(old Retrieval, exists bool) (Retrieval, error) {
		if exists && old.Digest != r.Digest {
			return old, ErrExists
		}
		return r, nil
	})
	return err
}
func (s *Store) LastRetrieval(tenant, run string) (Retrieval, error) {
	rows, _, err := s.retrievals.List(tenant, state.Query{Prefix: run + "/", Desc: true, Limit: 1})
	if err != nil {
		return Retrieval{}, err
	}
	if len(rows) == 0 {
		return Retrieval{}, ErrNotFound
	}
	return rows[0].Value, rows[0].Value.Validate()
}
