package knowledge

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// retainedDigest ignores refresh clocks and provider ordering. It identifies
// exactly the retained text and provenance, including explicit curation.
func retainedDigest(src *source) [32]byte {
	type content struct {
		ID, Title, URI string
		Excluded       bool
		Passages       []string
	}
	docs := slices.Clone(src.docs)
	slices.SortFunc(docs, func(a, b *Document) int { return strings.Compare(a.ID, b.ID) })
	texts := map[string][]string{}
	for _, passage := range src.passages {
		texts[passage.doc.ID] = append(texts[passage.doc.ID], passage.text)
	}
	rows := []content{}
	for _, doc := range docs {
		rows = append(rows, content{doc.ID, doc.Title, doc.URI, doc.Excluded, texts[doc.ID]})
	}
	data, _ := json.Marshal(rows)
	return sha256.Sum256(data)
}

// MaxSourceNames bounds durable cursors, including deleted names. Retaining a
// cursor prevents a delete/recreate from looking like an old source revision.
const MaxSourceNames = 10000

// contentClock is a name's last content revision, kept after the source is
// deleted, and the last number its documents were given.
type contentClock struct {
	Name  string `json:"name"`
	Value uint64 `json:"value"`
	Seq   int64  `json:"seq,omitempty"`
}

func clockKey(name string) string { return "version/" + name }

// nextVersion is the revision after a source's: past its record and its
// name's clock, which a source kept before clocks may lack.
func nextVersion(src Source, clk contentClock) (uint64, error) {
	value := max(src.ContentVersion, clk.Value)
	if value == ^uint64(0) {
		return 0, fmt.Errorf("%w: source content revision exhausted", ErrInvalid)
	}
	return value + 1, nil
}

// ContentCursor returns a deleted name's final revision too. It exposes no
// content or freshness and is for internal dependency reconciliation only.
func (s *Store) ContentCursor(tenant, name string) (uint64, error) {
	clk, _, err := s.clock(tenant, name)
	if err != nil {
		return 0, err
	}
	src, _, err := s.meta(tenant, name)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return 0, err
	}
	return max(src.ContentVersion, clk.Value), nil
}
