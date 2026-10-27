package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/urmzd/humagen/pkg/state"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const MaxEvidencePerRun = 64
const MaxEvidenceRuns = 10000
const MaxEvidenceBytes = 64 << 20

var evidenceRunID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

// CitationEvidence is an actual retrieval snapshot, written before model input.
// Source, Provider, Project, Person and DocumentID are private binding/visibility facts;
// only the approved display fields are projected into citation responses.
type CitationEvidence struct {
	QueryKey string `json:"query_key,omitempty"`
	Label    string `json:"label"`
	Source   string `json:"source"`
	// Provider records query-time authority. Empty is legacy unknown authority,
	// never an implicit local/manual grant.
	Provider      string     `json:"provider,omitempty"`
	Project       string     `json:"project,omitempty"`
	Person        string     `json:"person,omitempty"`
	DocumentID    string     `json:"document_id"`
	Index         int        `json:"index"`
	Title         string     `json:"title"`
	URI           string     `json:"uri,omitempty"`
	Text          string     `json:"text"`
	Mode          Mode       `json:"mode"`
	CopyAt        *time.Time `json:"copy_at,omitempty"`
	RefreshFailed bool       `json:"refresh_failed,omitempty"`
	Digest        string     `json:"digest"`
}
type evidenceRecord struct {
	Run       string             `json:"run"`
	Citations []CitationEvidence `json:"citations"`
}
type EvidenceStore struct {
	mu    sync.RWMutex
	by    map[string]map[string]evidenceRecord
	bytes map[string]int
	kept  *state.Table[evidenceRecord]
}

func NewEvidenceStore() *EvidenceStore {
	return &EvidenceStore{by: map[string]map[string]evidenceRecord{}, bytes: map[string]int{}}
}
func citationDigest(doc, title, uri, text string, index int) string {
	raw, _ := json.Marshal([]any{doc, title, uri, text, index})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func EvidenceDigest(doc, title, uri, text string, index int) string {
	return citationDigest(doc, title, uri, text, index)
}
func cloneEvidence(e CitationEvidence) CitationEvidence {
	if e.CopyAt != nil {
		at := *e.CopyAt
		e.CopyAt = &at
	}
	return e
}
func cloneEvidenceRecord(r evidenceRecord) evidenceRecord {
	r.Citations = slices.Clone(r.Citations)
	for i, e := range r.Citations {
		r.Citations[i] = cloneEvidence(e)
	}
	return r
}
func checkEvidence(e CitationEvidence) error {
	if len(e.QueryKey) > 128 || e.Label == "" || len(e.Label) > 128 || !ValidName(e.Source) || e.DocumentID == "" || len(e.DocumentID) > 256 || e.Index < 0 || len(e.Project) > 256 || len(e.Person) > 256 || len(e.Title) > 4096 || len(e.URI) > 4096 || len(e.Text) > 8000 {
		return ErrInvalid
	}
	if e.Provider != "" && !ValidName(e.Provider) {
		return ErrInvalid
	}
	if e.Mode != Indexed && e.Mode != Live || e.Mode == Live && (e.CopyAt != nil || e.RefreshFailed) {
		return ErrInvalid
	}
	if e.Digest != citationDigest(e.DocumentID, e.Title, e.URI, e.Text, e.Index) {
		return ErrInvalid
	}
	return nil
}
func OpenEvidenceStore(st state.Store) (*EvidenceStore, error) {
	s := NewEvidenceStore()
	s.kept = state.NewTable[evidenceRecord](st, "source_citation_evidence")
	err := s.kept.Load(func(tenant, key string, row evidenceRecord) error {
		if row.Run != key || !evidenceRunID.MatchString(key) || len(row.Citations) > MaxEvidencePerRun || len(s.by[tenant]) >= MaxEvidenceRuns {
			return ErrInvalid
		}
		labels := map[string]bool{}
		for _, e := range row.Citations {
			if err := checkEvidence(e); err != nil {
				return err
			}
			if labels[e.Label] {
				return ErrInvalid
			}
			labels[e.Label] = true
		}
		raw, _ := json.Marshal(row)
		s.bytes[tenant] += len(raw)
		if s.bytes[tenant] > MaxEvidenceBytes {
			return ErrInvalid
		}
		if s.by[tenant] == nil {
			s.by[tenant] = map[string]evidenceRecord{}
		}
		s.by[tenant][key] = cloneEvidenceRecord(row)
		return nil
	})
	return s, err
}

// Record never replaces a citation binding within one execution attempt.
// Repeated identical retrieval is idempotent; conflicting evidence fails closed.
func (s *EvidenceStore) Record(tenant, run string, entries []CitationEvidence) error {
	_, err := s.recordQuery(tenant, run, "", entries)
	return err
}

// RecordQuery allocates numeric labels unique within this run. The query key
// identifies one runtime attempt or one actual tool call, never a tenant clock.
func (s *EvidenceStore) RecordQuery(tenant, run, query string, entries []CitationEvidence) ([]CitationEvidence, error) {
	if query == "" || len(query) > 128 {
		return nil, ErrInvalid
	}
	return s.recordQuery(tenant, run, query, entries)
}
func (s *EvidenceStore) recordQuery(tenant, run, query string, entries []CitationEvidence) ([]CitationEvidence, error) {
	if s == nil || !evidenceRunID.MatchString(run) || len(entries) > MaxEvidencePerRun {
		return nil, ErrInvalid
	}
	if len(entries) == 0 {
		return []CitationEvidence{}, nil
	}
	entries = slices.Clone(entries)
	s.mu.Lock()
	defer s.mu.Unlock()
	old, exists := s.by[tenant][run]
	if !exists && len(s.by[tenant]) >= MaxEvidenceRuns {
		return nil, fmt.Errorf("%w: citation evidence capacity", ErrExists)
	}
	if query != "" {
		previous := []CitationEvidence{}
		largest := 0
		for _, e := range old.Citations {
			if e.QueryKey == query {
				previous = append(previous, e)
			}
			n, _ := strconv.Atoi(strings.Trim(e.Label, "[]"))
			largest = max(largest, n)
		}
		if len(previous) > 0 && len(previous) != len(entries) {
			return nil, fmt.Errorf("%w: citation evidence changed within an attempt", ErrExists)
		}
		for i := range entries {
			entries[i].QueryKey = query
			if len(previous) > 0 {
				entries[i].Label = previous[i].Label
			} else {
				entries[i].Label = fmt.Sprintf("[%d]", largest+i+1)
			}
		}
	}
	for i, e := range entries {
		e.Digest = citationDigest(e.DocumentID, e.Title, e.URI, e.Text, e.Index)
		if err := checkEvidence(e); err != nil {
			return nil, err
		}
		entries[i] = cloneEvidence(e)
	}
	next := cloneEvidenceRecord(old)
	next.Run = run
	for _, entry := range entries {
		found := slices.IndexFunc(next.Citations, func(e CitationEvidence) bool { return e.Label == entry.Label })
		if found >= 0 {
			if !equalEvidence(next.Citations[found], entry) {
				return nil, fmt.Errorf("%w: citation evidence changed within an attempt", ErrExists)
			}
			continue
		}
		next.Citations = append(next.Citations, entry)
	}
	if len(next.Citations) > MaxEvidencePerRun {
		return nil, fmt.Errorf("%w: citation evidence capacity", ErrExists)
	}
	before, _ := json.Marshal(old)
	if !exists {
		before = nil
	}
	after, _ := json.Marshal(next)
	size := s.bytes[tenant] - len(before) + len(after)
	if size > MaxEvidenceBytes {
		return nil, fmt.Errorf("%w: citation evidence capacity", ErrExists)
	}
	if err := s.kept.Put(tenant, run, next); err != nil {
		return nil, err
	}
	if s.by[tenant] == nil {
		s.by[tenant] = map[string]evidenceRecord{}
	}
	s.by[tenant][run] = cloneEvidenceRecord(next)
	s.bytes[tenant] = size
	return entries, nil
}

func (s *EvidenceStore) Get(tenant, run, label, documentID string) (CitationEvidence, bool) {
	if s == nil {
		return CitationEvidence{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, e := range s.by[tenant][run].Citations {
		if e.Label == label && (documentID == "" || e.DocumentID == documentID) {
			return cloneEvidence(e), true
		}
	}
	return CitationEvidence{}, false
}

// ForRun returns at most MaxEvidencePerRun retained proofs for one exact run.
// This is an internal composition input, not reader-authorized display data.
func (s *EvidenceStore) ForRun(tenant, run string) []CitationEvidence {
	if s == nil {
		return []CitationEvidence{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneEvidenceRecord(s.by[tenant][run]).Citations
}

func equalEvidence(a, b CitationEvidence) bool {
	if (a.CopyAt == nil) != (b.CopyAt == nil) {
		return false
	}
	if a.CopyAt != nil && !a.CopyAt.Equal(*b.CopyAt) {
		return false
	}
	a.CopyAt = nil
	b.CopyAt = nil
	return reflect.DeepEqual(a, b)
}

// RunReferences returns only accepted query-time relationships. Visibility is
// checked before the limit, so hidden retrievals consume no public slots.
// Both the retained run set and per-run evidence are bounded by this store.
func (s *EvidenceStore) RunReferences(tenant, source string, limit int, keep func(string, CitationEvidence) bool) []string {
	out := []string{}
	if s == nil || limit <= 0 {
		return out
	}
	limit = min(limit, 200)
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.by[tenant]))
	for id := range s.by[tenant] {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		for _, proof := range s.by[tenant][id].Citations {
			if proof.Source == source && keep != nil && keep(id, cloneEvidence(proof)) {
				out = append(out, id)
				break
			}
		}
		if len(out) == limit {
			break
		}
	}
	return out
}
