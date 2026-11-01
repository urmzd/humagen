package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/state"
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

// EvidenceStore keeps each tenant's citation evidence in a state.Store, one
// record per run, and reads it per request.
type EvidenceStore struct {
	st state.Store
}

// evidenceNamespace holds one record per run, keyed by the run's ID, and in
// each tenant its usage: how many runs it keeps and how many bytes they
// take. Every write that changes a run moves the usage, so the bounds hold
// across processes.
const (
	evidenceNamespace = "source_citation_evidence"
	evidenceUsageKey  = "#usage"
)

type evidenceUsage struct {
	Runs  int `json:"runs"`
	Bytes int `json:"bytes"`
}

// NewEvidenceStore returns a store in this process's memory.
func NewEvidenceStore() *EvidenceStore { s, _ := OpenEvidenceStore(nil); return s }

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

// OpenEvidenceStore returns a store on st, which may be shared with other
// processes. A nil st keeps evidence in this process's memory. A tenant's
// usage is counted from its records the first time a process writes to it.
func OpenEvidenceStore(st state.Store) (*EvidenceStore, error) {
	if st == nil {
		st = state.NewMemory()
	}
	return &EvidenceStore{st: st}, nil
}

// usage reads a tenant's usage and its version, counting it from the
// tenant's records when it has none. Every write moves the usage, so a count
// made while another process wrote conflicts when it is created.
func (s *EvidenceStore) usage(tenant string) (evidenceUsage, int64, error) {
	var u evidenceUsage
	at, err := lookupInto(s.st, evidenceNamespace, tenant, evidenceUsageKey, &u)
	if err == nil {
		return u, at, nil
	}
	if !errors.Is(err, state.ErrNotFound) {
		return u, 0, err
	}
	recs, err := state.NewTable[json.RawMessage](s.st, evidenceNamespace).All(tenant, "", MaxEvidenceRuns+1)
	if err != nil {
		return u, 0, err
	}
	for _, r := range recs {
		if r.Key == evidenceUsageKey {
			continue
		}
		var row evidenceRecord
		if err := json.Unmarshal(r.Value, &row); err != nil {
			return u, 0, fmt.Errorf("knowledge: evidence %q: %w", r.Key, err)
		}
		raw, _ := json.Marshal(row)
		u.Runs++
		u.Bytes += len(raw)
	}
	return u, 0, nil
}

// run reads one run's evidence and its version, 0 when it has none.
func (s *EvidenceStore) run(tenant, run string) (evidenceRecord, int64, error) {
	var row evidenceRecord
	if !evidenceRunID.MatchString(run) {
		return row, 0, nil
	}
	at, err := lookupInto(s.st, evidenceNamespace, tenant, run, &row)
	if errors.Is(err, state.ErrNotFound) {
		return evidenceRecord{}, 0, nil
	}
	return row, at, err
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
	var out []CitationEvidence
	err := state.Retry(func() error {
		var err error
		out, err = s.tryRecord(tenant, run, query, slices.Clone(entries))
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// tryRecord reads the run and the usage and writes at the versions read. It
// returns state.ErrConflict when another writer moved either.
func (s *EvidenceStore) tryRecord(tenant, run, query string, entries []CitationEvidence) ([]CitationEvidence, error) {
	u, usageAt, err := s.usage(tenant)
	if err != nil {
		return nil, err
	}
	old, oldAt, err := s.run(tenant, run)
	if err != nil {
		return nil, err
	}
	exists := oldAt > 0
	if !exists && u.Runs >= MaxEvidenceRuns {
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
	added := false
	for _, entry := range entries {
		found := slices.IndexFunc(next.Citations, func(e CitationEvidence) bool { return e.Label == entry.Label })
		if found >= 0 {
			if !equalEvidence(next.Citations[found], entry) {
				return nil, fmt.Errorf("%w: citation evidence changed within an attempt", ErrExists)
			}
			continue
		}
		next.Citations = append(next.Citations, entry)
		added = true
	}
	if len(next.Citations) > MaxEvidencePerRun {
		return nil, fmt.Errorf("%w: citation evidence capacity", ErrExists)
	}
	if !added {
		return entries, nil
	}
	before, _ := json.Marshal(old)
	if !exists {
		before = nil
	}
	after, _ := json.Marshal(next)
	size := u.Bytes - len(before) + len(after)
	if size > MaxEvidenceBytes {
		return nil, fmt.Errorf("%w: citation evidence capacity", ErrExists)
	}
	if !exists {
		u.Runs++
	}
	u.Bytes = size
	nextRaw, _ := json.Marshal(next)
	usageRaw, _ := json.Marshal(u)
	runOp, usageOp := state.Put(tenant, run, nextRaw), state.Put(tenant, evidenceUsageKey, usageRaw)
	runOp.If, usageOp.If = oldAt, usageAt
	if !exists {
		runOp.If = state.Missing
	}
	if usageAt == 0 {
		usageOp.If = state.Missing
	}
	c, cancel := context.WithTimeout(context.Background(), state.Timeout)
	defer cancel()
	if err := s.st.Write(c, evidenceNamespace, runOp, usageOp); err != nil {
		return nil, err
	}
	return entries, nil
}

// Get reads one citation's evidence. Evidence that cannot be read is not
// found: a citation without proof is never trusted.
func (s *EvidenceStore) Get(tenant, run, label, documentID string) (CitationEvidence, bool) {
	if s == nil {
		return CitationEvidence{}, false
	}
	row, _, err := s.run(tenant, run)
	if err != nil {
		return CitationEvidence{}, false
	}
	for _, e := range row.Citations {
		if e.Label == label && (documentID == "" || e.DocumentID == documentID) {
			return cloneEvidence(e), true
		}
	}
	return CitationEvidence{}, false
}

// ForRun returns at most MaxEvidencePerRun retained proofs for one exact run.
// This is an internal composition input, not reader-authorized display data.
func (s *EvidenceStore) ForRun(tenant, run string) ([]CitationEvidence, error) {
	if s == nil {
		return []CitationEvidence{}, nil
	}
	row, _, err := s.run(tenant, run)
	if err != nil {
		return nil, err
	}
	return cloneEvidenceRecord(row).Citations, nil
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
func (s *EvidenceStore) RunReferences(tenant, source string, limit int, keep func(string, CitationEvidence) bool) ([]string, error) {
	out := []string{}
	if s == nil || limit <= 0 {
		return out, nil
	}
	limit = min(limit, 200)
	// The tenant's runs, at most MaxEvidenceRuns, are read a page at a
	// time in ID order until limit of them are kept.
	kept := state.NewTable[evidenceRecord](s.st, evidenceNamespace)
	after := ""
	for range MaxEvidenceRuns/state.MaxList + 2 {
		page, next, err := kept.List(tenant, state.Query{Prefix: "", After: after, Limit: state.MaxList})
		if err != nil {
			return nil, err
		}
		for _, it := range page {
			if it.Key == evidenceUsageKey {
				continue
			}
			for _, proof := range it.Value.Citations {
				if proof.Source == source && keep != nil && keep(it.Key, cloneEvidence(proof)) {
					out = append(out, it.Key)
					break
				}
			}
			if len(out) == limit {
				return out, nil
			}
		}
		if next == "" {
			break
		}
		after = next
	}
	return out, nil
}

// lookupInto reads one record into v, and its version.
func lookupInto(st state.Store, namespace, tenant, key string, v any) (int64, error) {
	c, cancel := context.WithTimeout(context.Background(), state.Timeout)
	defer cancel()
	r, err := st.Lookup(c, namespace, tenant, key)
	if err != nil {
		return 0, err
	}
	if err := json.Unmarshal(r.Value, v); err != nil {
		return 0, fmt.Errorf("knowledge: %s %q: %w", namespace, key, err)
	}
	return r.Version, nil
}
