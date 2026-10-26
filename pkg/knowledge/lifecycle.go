package knowledge

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

var resourceID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

// Mode determines whether a source supplies a retained copy or reader-scoped live results.
type Mode string

const (
	Indexed Mode = "indexed"
	Live    Mode = "live"
	Hybrid  Mode = "hybrid"
)

// Status reports completed work rather than an optimistic connection claim.
type Status string

const (
	Connected      Status = "connected"
	Importing      Status = "importing"
	Indexing       Status = "indexing"
	Available      Status = "available"
	Syncing        Status = "syncing"
	InSync         Status = "in_sync"
	NeedsAttention Status = "needs_attention"
)

// Cause is deliberately bounded and never holds a remote response or credential.
type Cause string

const (
	AuthExpired Cause = "auth_expired"
	Quota       Cause = "quota"
	Unsupported Cause = "unsupported"
	Unavailable Cause = "unavailable"
	Interrupted Cause = "interrupted"
)

const MaxAttempts = 20
const MaxImportDocuments = 200
const MaxImportBytes = 8 << 20

// SourceConfig contains only public source metadata, never a credential.
// Provider identifies a registered implementation; origin is its resource ID.
type SourceConfig struct {
	Mode             Mode   `json:"mode"`
	Provider         string `json:"provider"`
	Origin           string `json:"origin,omitempty"`
	Owner            string `json:"owner,omitempty"`
	Project          string `json:"project,omitempty"`
	StopAfterSeconds int    `json:"stop_after_seconds"`
}

// Attempt is one bounded refresh. A pending attempt after restart is not success.
type Attempt struct {
	ID         string     `json:"id"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Status     string     `json:"status"`
	Cause      Cause      `json:"cause,omitempty"`
}

// Lifecycle keeps the last good copy independently from the latest attempt.
type Lifecycle struct {
	ConnectedAt time.Time `json:"connected_at"`
	SourceConfig
	Status     Status     `json:"status"`
	Cause      Cause      `json:"cause,omitempty"`
	LastGoodAt *time.Time `json:"last_good_at,omitempty"`
	Attempts   []Attempt  `json:"attempts"`
}

func cloneSource(src Source) Source {
	src.Variants = slices.Clone(src.Variants)
	for i := range src.Variants {
		src.Variants[i].Excluded = slices.Clone(src.Variants[i].Excluded)
	}
	if src.Lifecycle != nil {
		lc := *src.Lifecycle
		lc.Attempts = slices.Clone(lc.Attempts)
		if lc.LastGoodAt != nil {
			at := *lc.LastGoodAt
			lc.LastGoodAt = &at
		}
		for i := range lc.Attempts {
			if lc.Attempts[i].FinishedAt != nil {
				at := *lc.Attempts[i].FinishedAt
				lc.Attempts[i].FinishedAt = &at
			}
		}
		src.Lifecycle = &lc
	}
	return src
}

func lifecycle(src Source) *Lifecycle {
	if src.Lifecycle != nil {
		return cloneSource(src).Lifecycle
	}
	lc := &Lifecycle{SourceConfig: SourceConfig{Mode: Indexed, Provider: "manual", Owner: src.CreatedBy, StopAfterSeconds: 86400}, ConnectedAt: src.UpdatedAt, Status: Connected, Attempts: []Attempt{}}
	if src.Documents > 0 {
		lc.Status = Available
	}
	return lc
}

// Get reads one source without revealing another tenant's objects.
func (s *Store) Get(tenant, name string) (Source, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	src, err := s.get(tenant, name)
	if err != nil {
		return Source{}, err
	}
	return cloneSource(src.Source), nil
}

// Configure replaces public metadata. An active import cannot change identity.
func (s *Store) Configure(tenant, name string, cfg SourceConfig, at time.Time) (Source, error) {
	if cfg.Mode != "indexed" && cfg.Mode != "live" && cfg.Mode != "hybrid" {
		return Source{}, fmt.Errorf("%w: source mode", ErrInvalid)
	}
	if !ValidName(cfg.Provider) || len(cfg.Origin) > 1024 || len(cfg.Owner) > 256 || len(cfg.Project) > 256 || strings.ContainsAny(cfg.Origin, "\r\n") {
		return Source{}, fmt.Errorf("%w: source configuration", ErrInvalid)
	}
	if cfg.Provider == "manual" && cfg.Mode != Indexed {
		return Source{}, fmt.Errorf("%w: manual sources are indexed", ErrInvalid)
	}
	if cfg.Provider != "manual" && !resourceID.MatchString(cfg.Origin) {
		return Source{}, fmt.Errorf("%w: provider resource ID", ErrInvalid)
	}
	if cfg.StopAfterSeconds == 0 {
		cfg.StopAfterSeconds = 86400
	}
	if cfg.StopAfterSeconds < 60 || cfg.StopAfterSeconds > 2592000 {
		return Source{}, fmt.Errorf("%w: stop rule must be between a minute and 30 days", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	src, err := s.get(tenant, name)
	if err != nil {
		return Source{}, err
	}
	lc := lifecycle(src.Source)
	if active(lc) != "" {
		return Source{}, fmt.Errorf("%w: refresh in progress", ErrExists)
	}
	// Changing origin/provider cannot retain a copy from a different authority.
	if src.Documents > 0 && (cfg.Provider != lc.Provider || cfg.Origin != lc.Origin || cfg.Project != lc.Project || cfg.Owner != lc.Owner) {
		return Source{}, fmt.Errorf("%w: create a new source for a different origin, owner or project", ErrInvalid)
	}
	lc.SourceConfig = cfg
	next := cloneSource(src.Source)
	next.Lifecycle = lc
	next.UpdatedAt = at
	if err := s.kept.Put(tenant, sourceKey(name), stored{Source: &next}); err != nil {
		return Source{}, err
	}
	src.Source = next
	return cloneSource(next), nil
}

func active(lc *Lifecycle) string {
	if len(lc.Attempts) == 0 {
		return ""
	}
	last := lc.Attempts[len(lc.Attempts)-1]
	if last.FinishedAt == nil {
		return last.ID
	}
	return ""
}

// BeginSync durably records an attempt before making an external request.
func (s *Store) BeginSync(tenant, name string, at time.Time) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src, err := s.get(tenant, name)
	if err != nil {
		return "", err
	}
	lc := lifecycle(src.Source)
	if active(lc) != "" {
		return "", fmt.Errorf("%w: refresh in progress", ErrExists)
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	state := Importing
	if lc.LastGoodAt != nil {
		state = Syncing
	}
	lc.Status = state
	lc.Cause = ""
	attempt := Attempt{ID: hex.EncodeToString(id[:]), StartedAt: at, Status: string(state)}
	lc.Attempts = append(lc.Attempts, attempt)
	if len(lc.Attempts) > MaxAttempts {
		lc.Attempts = slices.Clone(lc.Attempts[len(lc.Attempts)-MaxAttempts:])
	}
	next := cloneSource(src.Source)
	next.Lifecycle = lc
	next.UpdatedAt = at
	if err := s.kept.Put(tenant, sourceKey(name), stored{Source: &next}); err != nil {
		return "", err
	}
	src.Source = next
	return attempt.ID, nil
}

// FailSync preserves every document and the last successful refresh timestamp.
func (s *Store) FailSync(tenant, name, id string, cause Cause, at time.Time) (Source, error) {
	if cause != AuthExpired && cause != Quota && cause != Unsupported && cause != Unavailable && cause != Interrupted {
		return Source{}, fmt.Errorf("%w: failure cause", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	src, err := s.get(tenant, name)
	if err != nil {
		return Source{}, err
	}
	lc := lifecycle(src.Source)
	if active(lc) != id || id == "" {
		return Source{}, fmt.Errorf("%w: inactive attempt", ErrInvalid)
	}
	lc.Status = NeedsAttention
	lc.Cause = cause
	last := &lc.Attempts[len(lc.Attempts)-1]
	last.FinishedAt = &at
	last.Status = "failed"
	last.Cause = cause
	next := cloneSource(src.Source)
	next.Lifecycle = lc
	next.UpdatedAt = at
	if err := s.kept.Put(tenant, sourceKey(name), stored{Source: &next}); err != nil {
		return Source{}, err
	}
	src.Source = next
	return cloneSource(next), nil
}

// ImportDocument is a validated provider result, not an independently committed write.
type ImportDocument struct{ ID, Title, URI, Text string }

// CompleteSync publishes a full copy atomically, including removal of old text.
// Incomplete/over-limit provider responses must fail the attempt instead.
func (s *Store) CompleteSync(tenant, name, id string, docs []ImportDocument, by string, at time.Time) (Source, error) {
	if docs == nil || len(docs) > MaxImportDocuments {
		return Source{}, fmt.Errorf("%w: too many imported documents", ErrInvalid)
	}
	bytes := 0
	seen := map[string]bool{}
	for _, d := range docs {
		bytes += len(d.Text)
		if strings.TrimSpace(d.Title) == "" || strings.TrimSpace(d.Text) == "" || len(d.Title) > 4096 || len(d.URI) > 4096 || bytes > MaxImportBytes || d.ID == "" || len(d.ID) > 256 || seen[d.ID] {
			return Source{}, fmt.Errorf("%w: imported document", ErrInvalid)
		}
		seen[d.ID] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	src, err := s.get(tenant, name)
	if err != nil {
		return Source{}, err
	}
	lc := lifecycle(src.Source)
	if active(lc) != id || id == "" {
		return Source{}, fmt.Errorf("%w: inactive attempt", ErrInvalid)
	}
	next := &source{Source: cloneSource(src.Source), seqs: map[string]int64{}}
	b := s.kept.Batch()
	for _, seq := range src.seqs {
		b.Delete(tenant, docKey(name, seq))
		b.Delete(tenant, textKey(name, seq))
	}
	seq := s.seq
	for _, d := range docs {
		seq++
		documentID := d.ID
		texts := split(d.Text)
		doc := &Document{ID: documentID, Title: strings.TrimSpace(d.Title), URI: d.URI, Bytes: len(d.Text), Passages: len(texts), AddedBy: by, AddedAt: at}
		next.docs = append(next.docs, doc)
		next.passages = append(next.passages, passagesOf(doc, texts)...)
		next.seqs[doc.ID] = seq
		b.Put(tenant, docKey(name, seq), stored{Document: doc})
		b.Put(tenant, textKey(name, seq), stored{Passages: texts})
	}
	lc.Status = Available
	if lc.Provider != "manual" {
		lc.Status = InSync
	}
	lc.Cause = ""
	lc.LastGoodAt = &at
	last := &lc.Attempts[len(lc.Attempts)-1]
	last.Status = "succeeded"
	last.FinishedAt = &at
	next.Lifecycle = lc
	next.Documents = len(next.docs)
	next.Passages = len(next.passages)
	next.Excluded = 0
	next.Variants = []Variant{}
	next.UpdatedAt = at
	b.Put(tenant, sourceKey(name), stored{Source: &next.Source})
	if err := b.Commit(); err != nil {
		return Source{}, err
	}
	s.seq = seq
	s.tenants[tenant][name] = next
	return cloneSource(next.Source), nil
}

// HardStale enforces a stop rule only for managed sources, including never-synced ones.
func (src Source) HardStale(at time.Time) bool {
	lc := src.Lifecycle
	if lc == nil || lc.Provider == "manual" || lc.Mode == Live {
		return false
	}
	since := lc.ConnectedAt
	if since.IsZero() {
		since = src.UpdatedAt
	}
	if lc.LastGoodAt != nil {
		since = *lc.LastGoodAt
	}
	return at.Sub(since) >= time.Duration(lc.StopAfterSeconds)*time.Second
}

// IndexSync records that provider fetching completed and local indexing begins.
// It never replaces or marks the retained documents successful.
func (s *Store) IndexSync(tenant, name, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	src, err := s.get(tenant, name)
	if err != nil {
		return err
	}
	lc := lifecycle(src.Source)
	if active(lc) != id || id == "" {
		return fmt.Errorf("%w: inactive attempt", ErrInvalid)
	}
	lc.Status = Indexing
	lc.Attempts[len(lc.Attempts)-1].Status = string(Indexing)
	next := cloneSource(src.Source)
	next.Lifecycle = lc
	next.UpdatedAt = at
	if err := s.kept.Put(tenant, sourceKey(name), stored{Source: &next}); err != nil {
		return err
	}
	src.Source = next
	return nil
}
