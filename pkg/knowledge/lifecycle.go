package knowledge

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/state"
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
	Stale       Cause = "stale"
)

const MaxAttempts = 20
const MaxImportDocuments = 200
const MaxImportBytes = 8 << 20

// SourceConfig contains only public source metadata, never a credential.
// Provider identifies a registered implementation; origin is its resource ID.
type SourceConfig struct {
	Resources        []string `json:"resources,omitempty"`
	Snapshot         bool     `json:"snapshot,omitempty"`
	SyncEverySeconds int      `json:"sync_every_seconds,omitempty"`
	Mode             Mode     `json:"mode"`
	Provider         string   `json:"provider"`
	Origin           string   `json:"origin,omitempty"`
	Owner            string   `json:"owner,omitempty"`
	Project          string   `json:"project,omitempty"`
	StopAfterSeconds int      `json:"stop_after_seconds"`
}

// Equal compares public metadata, including the exact selected resource set.
func (c SourceConfig) Equal(other SourceConfig) bool {
	return c.Mode == other.Mode && c.Provider == other.Provider && c.Origin == other.Origin && c.Owner == other.Owner && c.Project == other.Project && c.StopAfterSeconds == other.StopAfterSeconds && c.Snapshot == other.Snapshot && c.SyncEverySeconds == other.SyncEverySeconds && slices.Equal(c.Resources, other.Resources)
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
		lc.Resources = slices.Clone(lc.Resources)
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
	src, _, err := s.meta(tenant, name)
	if err != nil {
		return Source{}, err
	}
	return cloneSource(src), nil
}

// Configure replaces public metadata. An active import cannot change identity.
func (s *Store) Configure(tenant, name string, cfg SourceConfig, at time.Time) (Source, error) {
	return s.configure(tenant, name, cfg, at, nil, false, false)
}

// ConfigureIfCurrent checks the exact authorized connection snapshot in the
// same write as the change. Nil expects a legacy source without lifecycle
// configuration.
func (s *Store) ConfigureIfCurrent(tenant, name string, cfg SourceConfig, at time.Time, expected *SourceConfig) (Source, error) {
	return s.configure(tenant, name, cfg, at, expected, true, false)
}

// KeepSnapshot explicitly acknowledges retaining the last good copy, including
// after a failed replacement, without claiming another provider read.
func (s *Store) KeepSnapshot(tenant, name string, cfg SourceConfig, at time.Time, expected *SourceConfig) (Source, error) {
	return s.configure(tenant, name, cfg, at, expected, true, true)
}
func (s *Store) configure(tenant, name string, cfg SourceConfig, at time.Time, expected *SourceConfig, guard bool, snapshotDecision bool) (Source, error) {
	var configErr error
	cfg, configErr = normalizeSourceConfig(cfg)
	if configErr != nil {
		return Source{}, configErr
	}
	return s.update(tenant, name, func(src *Source) error {
		if guard && !sameConfiguration(*src, expected) {
			return fmt.Errorf("%w: source configuration changed", ErrExists)
		}
		lc := lifecycle(*src)
		interrupt(lc, at)
		if active(lc) != "" {
			return fmt.Errorf("%w: refresh in progress", ErrExists)
		}
		// Changing origin/provider cannot retain a copy from a different authority.
		if (src.Documents > 0 || lc.LastGoodAt != nil) && (cfg.Provider != lc.Provider || cfg.Origin != lc.Origin || cfg.Project != lc.Project || cfg.Owner != lc.Owner) {
			return fmt.Errorf("%w: create a new source for a different origin, owner or project", ErrInvalid)
		}
		if cfg.Snapshot && (cfg.Mode != Indexed || lc.LastGoodAt == nil || cfg.Provider == "manual") {
			return fmt.Errorf("%w: snapshot requires a successful external indexed copy", ErrInvalid)
		}
		if cfg.Snapshot && (!lc.Snapshot || snapshotDecision) {
			lc.Status = Available
			lc.Cause = ""
		}
		lc.SourceConfig = cfg
		src.Lifecycle = lc
		src.UpdatedAt = at
		return nil
	})
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

// stale returns the active attempt when it has run longer than AttemptLease
// at at: the process running it ended before it finished.
func stale(lc *Lifecycle, at time.Time) string {
	id := active(lc)
	if id == "" || at.Sub(lc.Attempts[len(lc.Attempts)-1].StartedAt) < AttemptLease {
		return ""
	}
	return id
}

// interrupt records a stale attempt as failed, keeping the last good copy.
func interrupt(lc *Lifecycle, at time.Time) {
	if stale(lc, at) == "" {
		return
	}
	lc.Status = NeedsAttention
	lc.Cause = Interrupted
	last := &lc.Attempts[len(lc.Attempts)-1]
	last.FinishedAt = &at
	last.Status = "failed"
	last.Cause = Interrupted
}

// BeginSync durably records an attempt before making an external request.
func (s *Store) BeginSync(tenant, name string, at time.Time) (string, error) {
	return s.beginSync(tenant, name, at, nil, false)
}

// BeginSyncIfCurrent binds an import to the exact authorized source identity.
func (s *Store) BeginSyncIfCurrent(tenant, name string, at time.Time, expected *SourceConfig) (string, error) {
	return s.beginSync(tenant, name, at, expected, true)
}
func (s *Store) beginSync(tenant, name string, at time.Time, expected *SourceConfig, guard bool) (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	attemptID := hex.EncodeToString(id[:])
	_, err := s.update(tenant, name, func(src *Source) error {
		if guard && !sameConfiguration(*src, expected) {
			return fmt.Errorf("%w: source configuration changed", ErrExists)
		}
		lc := lifecycle(*src)
		interrupt(lc, at)
		if active(lc) != "" {
			return fmt.Errorf("%w: refresh in progress", ErrExists)
		}
		status := Importing
		if lc.LastGoodAt != nil {
			status = Syncing
		}
		lc.Status = status
		lc.Cause = ""
		attempt := Attempt{ID: attemptID, StartedAt: at, Status: string(status)}
		lc.Attempts = append(lc.Attempts, attempt)
		if len(lc.Attempts) > MaxAttempts {
			lc.Attempts = slices.Clone(lc.Attempts[len(lc.Attempts)-MaxAttempts:])
		}
		src.Lifecycle = lc
		src.UpdatedAt = at
		return nil
	})
	if err != nil {
		return "", err
	}
	return attemptID, nil
}

// FailSync preserves every document and the last successful refresh timestamp.
func (s *Store) FailSync(tenant, name, id string, cause Cause, at time.Time) (Source, error) {
	if cause != AuthExpired && cause != Quota && cause != Unsupported && cause != Unavailable && cause != Interrupted {
		return Source{}, fmt.Errorf("%w: failure cause", ErrInvalid)
	}
	return s.update(tenant, name, func(src *Source) error {
		lc := lifecycle(*src)
		if active(lc) != id || id == "" {
			return fmt.Errorf("%w: inactive attempt", ErrInvalid)
		}
		lc.Status = NeedsAttention
		lc.Cause = cause
		last := &lc.Attempts[len(lc.Attempts)-1]
		last.FinishedAt = &at
		last.Status = "failed"
		last.Cause = cause
		src.Lifecycle = lc
		src.UpdatedAt = at
		return nil
	})
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
	var out Source
	err := state.Retry(func() error {
		src, srcAt, err := s.load(tenant, name)
		if err != nil {
			return err
		}
		lc := lifecycle(src.Source)
		if active(lc) != id || id == "" {
			return fmt.Errorf("%w: inactive attempt", ErrInvalid)
		}
		clk, clkAt, err := s.clock(tenant, name)
		if err != nil {
			return err
		}
		next := &source{Source: cloneSource(src.Source), seqs: map[string]int64{}}
		b := s.kept.Batch()
		last := clk.Seq
		for _, seq := range src.seqs {
			b.Delete(tenant, docKey(name, seq))
			b.Delete(tenant, textKey(name, seq))
			b.Delete(tenant, vecKey(name, seq))
			last = max(last, seq)
		}
		seq := last
		for _, d := range docs {
			seq++
			texts := split(d.Text)
			doc := &Document{ID: d.ID, Title: strings.TrimSpace(d.Title), URI: d.URI, Bytes: len(d.Text), Passages: len(texts), AddedBy: by, AddedAt: at}
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
		attempt := &lc.Attempts[len(lc.Attempts)-1]
		attempt.Status = "succeeded"
		attempt.FinishedAt = &at
		next.Lifecycle = lc
		next.Documents = len(next.docs)
		next.Passages = len(next.passages)
		next.Excluded = 0
		next.Variants = []Variant{}
		next.UpdatedAt = at
		if retainedDigest(next) != retainedDigest(src) {
			version, err := nextVersion(src.Source, clk)
			if err != nil {
				return err
			}
			next.ContentVersion = version
		}
		b.PutAt(tenant, sourceKey(name), stored{Source: &next.Source}, srcAt)
		putClock(b, tenant, contentClock{Name: name, Value: max(clk.Value, next.ContentVersion), Seq: seq}, clkAt)
		if err := b.Commit(); err != nil {
			return err
		}
		out = next.Source
		return nil
	})
	if err != nil {
		return Source{}, err
	}
	s.nudge()
	return cloneSource(out), nil
}

// HardStale enforces a stop rule only for managed sources, including never-synced ones.
func (src Source) HardStale(at time.Time) bool {
	lc := src.Lifecycle
	if lc == nil || lc.Provider == "manual" || lc.Mode == Live || lc.Snapshot {
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
	_, err := s.update(tenant, name, func(src *Source) error {
		lc := lifecycle(*src)
		if active(lc) != id || id == "" {
			return fmt.Errorf("%w: inactive attempt", ErrInvalid)
		}
		lc.Status = Indexing
		lc.Attempts[len(lc.Attempts)-1].Status = string(Indexing)
		src.Lifecycle = lc
		src.UpdatedAt = at
		return nil
	})
	return err
}

func sameConfiguration(src Source, expected *SourceConfig) bool {
	if expected == nil {
		return src.Lifecycle == nil
	}
	return src.Lifecycle != nil && src.Lifecycle.Equal(*expected)
}

func normalizeSourceConfig(cfg SourceConfig) (SourceConfig, error) {
	cfg.Resources = slices.Clone(cfg.Resources)
	slices.Sort(cfg.Resources)
	if len(cfg.Resources) > MaxImportDocuments || len(slices.Compact(slices.Clone(cfg.Resources))) != len(cfg.Resources) {
		return SourceConfig{}, fmt.Errorf("%w: resource selection", ErrInvalid)
	}
	for _, id := range cfg.Resources {
		if !resourceID.MatchString(id) {
			return SourceConfig{}, fmt.Errorf("%w: selected resource ID", ErrInvalid)
		}
	}
	if cfg.Mode == Live && len(cfg.Resources) > 0 {
		return SourceConfig{}, fmt.Errorf("%w: live mode does not import resource selections", ErrInvalid)
	}
	if cfg.SyncEverySeconds != 0 && (cfg.SyncEverySeconds < 60 || cfg.SyncEverySeconds > 604800) {
		return SourceConfig{}, fmt.Errorf("%w: sync interval must be zero or between a minute and seven days", ErrInvalid)
	}
	if (cfg.Provider == "manual" || cfg.Mode == Live || cfg.Snapshot) && cfg.SyncEverySeconds != 0 {
		return SourceConfig{}, fmt.Errorf("%w: scheduled refresh requires an unfrozen external copy", ErrInvalid)
	}
	if cfg.Mode != "indexed" && cfg.Mode != "live" && cfg.Mode != "hybrid" {
		return SourceConfig{}, fmt.Errorf("%w: source mode", ErrInvalid)
	}
	if !ValidName(cfg.Provider) || len(cfg.Origin) > 1024 || len(cfg.Owner) > 256 || len(cfg.Project) > 256 || strings.ContainsAny(cfg.Origin, "\r\n") {
		return SourceConfig{}, fmt.Errorf("%w: source configuration", ErrInvalid)
	}
	if cfg.Provider == "manual" && cfg.Mode != Indexed {
		return SourceConfig{}, fmt.Errorf("%w: manual sources are indexed", ErrInvalid)
	}
	if cfg.Provider != "manual" && !resourceID.MatchString(cfg.Origin) {
		return SourceConfig{}, fmt.Errorf("%w: provider resource ID", ErrInvalid)
	}
	if cfg.StopAfterSeconds == 0 {
		cfg.StopAfterSeconds = 86400
	}
	if cfg.StopAfterSeconds < 60 || cfg.StopAfterSeconds > 2592000 {
		return SourceConfig{}, fmt.Errorf("%w: stop rule must be between a minute and 30 days", ErrInvalid)
	}
	return cfg, nil
}

// HealthAt projects an elapsed stop rule without inventing a refresh attempt.
// Last-good data and the recorded latest refresh outcome remain unchanged.
func (src Source) HealthAt(at time.Time) Source {
	if !src.HardStale(at) || src.Lifecycle == nil || src.Lifecycle.Status == NeedsAttention {
		return src
	}
	src = cloneSource(src)
	src.Lifecycle.Status = NeedsAttention
	src.Lifecycle.Cause = Stale
	return src
}
