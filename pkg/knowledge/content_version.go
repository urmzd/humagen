package knowledge

import (
	"crypto/sha256"
	"encoding/json"
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

type contentClock struct {
	Name  string `json:"name"`
	Value uint64 `json:"value"`
}

func clockKey(name string) string { return "version/" + name }
func (s *Store) rememberVersion(tenant, name string, value uint64) {
	if s.versions[tenant] == nil {
		s.versions[tenant] = map[string]uint64{}
	}
	s.versions[tenant][name] = max(s.versions[tenant][name], value)
}
func (s *Store) nextVersion(tenant, name string) (uint64, error) {
	value := s.versions[tenant][name]
	if value == ^uint64(0) {
		return 0, fmt.Errorf("%w: source content revision exhausted", ErrInvalid)
	}
	return value + 1, nil
}

// ContentCursor returns a deleted name's final revision too. It exposes no
// content or freshness and is for internal dependency reconciliation only.
func (s *Store) ContentCursor(tenant, name string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.versions[tenant][name]
}
