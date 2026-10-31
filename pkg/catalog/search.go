package catalog

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	dense "github.com/urmzd/humagen/fiber/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/state"
)

// Embedder turns text into vectors. It is fiber's, the same one
// pkg/knowledge uses, so the System spec's knowledge.embedder serves both.
type Embedder = dense.Embedder

// The same constants pkg/knowledge searches with.
const (
	queryTimeout    = 3 * time.Second
	batchTimeout    = 2 * time.Minute
	retryEvery      = 5 * time.Minute
	rrfK            = 60
	denseCandidates = 50
	// batchSize bounds how many entries one call to the embedder carries.
	batchSize = 64
)

// Search ranks the entries of the named connectors against a query: BM25
// over each entry's text, fused with vector similarity by reciprocal rank
// fusion when an embedder is set. An empty selection finds nothing, never
// the whole tenant. Connectors that do not exist are skipped.
func (s *Store) Search(tenant string, connectors []string, query string, limit int) ([]Hit, error) {
	if len(query) > MaxQuery {
		return nil, fmt.Errorf("%w: a query is at most %d bytes", ErrInvalid, MaxQuery)
	}
	q := tokens(query)
	slices.Sort(q)
	q = slices.Compact(q)
	if len(q) == 0 {
		return nil, fmt.Errorf("%w: a search needs words to look for", ErrInvalid)
	}
	if limit <= 0 || limit > MaxHits {
		limit = 10
	}
	if len(connectors) == 0 {
		return []Hit{}, nil
	}
	// Embedded first: it may call a model.
	qv, model := s.embedder().query(query)

	names := slices.Clone(connectors)
	slices.Sort(names)
	names = slices.Compact(names)
	var in []*entry
	for _, n := range names {
		es, err := s.current(tenant, n)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		in = append(in, es...)
	}
	if len(in) == 0 {
		return []Hit{}, nil
	}

	const k1, b = 1.2, 0.75
	total := 0
	df := map[string]int{}
	for _, e := range in {
		total += e.n
		for _, t := range q {
			if e.terms[t] > 0 {
				df[t]++
			}
		}
	}
	count := float64(len(in))
	avg := float64(total) / count
	var lexical []scored
	for _, e := range in {
		var score float64
		for _, t := range q {
			tf := float64(e.terms[t])
			if tf == 0 {
				continue
			}
			idf := math.Log(1 + (count-float64(df[t])+0.5)/(float64(df[t])+0.5))
			score += idf * tf * (k1 + 1) / (tf + k1*(1-b+b*float64(e.n)/avg))
		}
		if score > 0 {
			lexical = append(lexical, scored{e, score})
		}
	}
	byScore(lexical)
	ranked := lexical
	if qv != nil {
		var near []scored
		for _, e := range in {
			if e.vec != nil && e.model == model {
				near = append(near, scored{e, dot(qv, e.vec)})
			}
		}
		byScore(near)
		ranked = fuse(lexical, near)
	}
	ranked = ranked[:min(len(ranked), limit)]
	hits := make([]Hit, 0, len(ranked))
	for _, r := range ranked {
		hits = append(hits, Hit{Entry: r.e.Entry, Score: r.score})
	}
	return hits, nil
}

type scored struct {
	e     *entry
	score float64
}

// byScore orders highest first, keeping path order among equals.
func byScore(xs []scored) {
	slices.SortStableFunc(xs, func(a, b scored) int {
		switch {
		case a.score > b.score:
			return -1
		case a.score < b.score:
			return 1
		}
		return 0
	})
}

// fuse merges two rankings by reciprocal rank fusion. Only positions count,
// so a BM25 score and a cosine need no calibration.
func fuse(lexical, near []scored) []scored {
	near = near[:min(len(near), denseCandidates)]
	at := map[*entry]int{}
	var out []scored
	for _, list := range [][]scored{lexical, near} {
		for rank, c := range list {
			score := 1 / float64(rrfK+rank+1)
			if i, ok := at[c.e]; ok {
				out[i].score += score
				continue
			}
			at[c.e] = len(out)
			out = append(out, scored{c.e, score})
		}
	}
	byScore(out)
	return out
}

// dot is the cosine of two unit vectors. Vectors of different widths score 0.
func dot(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var sum float64
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}

// vectorRecord is one entry's vector: a little-endian float32 array in
// base64, the model that made it, and the crawl it was made for.
type vectorRecord struct {
	Model      string `json:"model"`
	Generation int64  `json:"generation"`
	Data       string `json:"data"`
}

func (r *vectorRecord) decode() ([]float32, string) {
	b, err := base64.StdEncoding.DecodeString(r.Data)
	if err != nil || len(b) == 0 || len(b)%4 != 0 {
		return nil, ""
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v, r.Model
}

func encode(v []float32) string {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return base64.StdEncoding.EncodeToString(b)
}

// denseIndex is the store's embedder. A nil *denseIndex is lexical only.
type denseIndex struct {
	embedder Embedder
	model    string
	wake     chan struct{}
	log      *slog.Logger
}

// UseEmbedder turns on hybrid search. Entries without a vector from e are
// embedded in the background until ctx is done. It is called once, at
// start. A nil e leaves the store lexical.
func (s *Store) UseEmbedder(ctx context.Context, e Embedder, log *slog.Logger) {
	if e == nil {
		return
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	d := &denseIndex{embedder: e, model: e.Model(), wake: make(chan struct{}, 1), log: log}
	s.mu.Lock()
	s.dense = d
	s.mu.Unlock()
	go s.index(ctx, d)
	d.nudge()
}

func (s *Store) embedder() *denseIndex {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dense
}

func (s *Store) nudge() { s.embedder().nudge() }

func (d *denseIndex) nudge() {
	if d == nil {
		return
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *denseIndex) query(text string) ([]float32, string) {
	if d == nil {
		return nil, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	v, err := d.embedder.Embed(ctx, []string{text})
	if err != nil || len(v) != 1 || len(v[0]) != d.embedder.Dimensions() {
		d.log.Warn("catalog: query not embedded; searching lexically", "model", d.model, "err", err)
		return nil, ""
	}
	return v[0], d.model
}

func (s *Store) index(ctx context.Context, d *denseIndex) {
	tick := time.NewTicker(retryEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.wake:
		case <-tick.C:
		}
		if err := s.sweep(ctx, d); err != nil && ctx.Err() == nil {
			d.log.Warn("catalog: entries not embedded; will retry", "model", d.model, "err", err)
		}
	}
}

// errStop ends a sweep early without an error to report.
var errStop = errors.New("catalog: sweep stopped")

// sweep embeds every entry without a vector from d's model, tenant by
// tenant through the directory and connector by connector. A batch is kept
// only if its connector has not been crawled again meanwhile. Two processes
// sweeping at once may both embed an entry; the vector either keeps is the
// same.
func (s *Store) sweep(ctx context.Context, d *denseIndex) error {
	err := s.dir.Each(func(tenant string) error {
		cs, err := s.Connectors(tenant)
		if err != nil {
			return err
		}
		for _, c := range cs {
			es, err := s.current(tenant, c.Name)
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			var todo []*entry
			for _, e := range es {
				if e.vec == nil || e.model != d.model {
					todo = append(todo, e)
				}
			}
			for batch := range slices.Chunk(todo, batchSize) {
				if ctx.Err() != nil {
					return errStop
				}
				texts := make([]string, len(batch))
				for i, e := range batch {
					texts[i] = Text(e.Entry)
				}
				bctx, cancel := context.WithTimeout(ctx, batchTimeout)
				vectors, err := d.embedder.Embed(bctx, texts)
				cancel()
				if err == nil && len(vectors) != len(texts) {
					err = dense.ErrEmbedding
				}
				if err != nil {
					return err
				}
				if err := s.keepVectors(tenant, c.Name, c.Generation, batch, d.model, vectors); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if errors.Is(err, errStop) {
		return nil
	}
	return err
}

// keepVectors writes a batch's vectors at the version of its connector,
// unless the connector was crawled again since generation.
func (s *Store) keepVectors(tenant, name string, generation int64, batch []*entry, model string, vectors [][]float32) error {
	return state.Retry(func() error {
		c, version, err := s.connector(tenant, name)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if c.Generation != generation {
			return nil
		}
		b := s.kept.Batch()
		for i, e := range batch {
			b.Put(tenant, vectorKey(name, e.Path), record{Vector: &vectorRecord{Model: model, Generation: generation, Data: encode(vectors[i])}})
		}
		b.PutAt(tenant, connectorKey(name), record{Connector: &c}, version)
		return b.Commit()
	})
}

// current returns a connector's entries, indexed for search, with the
// vectors of its generation, as of one version of the connector record.
func (s *Store) current(tenant, name string) ([]*entry, error) {
	for range state.MaxTries {
		c, version, err := s.connector(tenant, name)
		if err != nil {
			return nil, err
		}
		es, err := s.entries(tenant, name, c, version)
		if errors.Is(err, state.ErrConflict) {
			continue
		}
		return es, err
	}
	return nil, state.ErrBusy
}

// entries reads a connector's entries and vectors as of the version of its
// record that c was read at, from this process's cache when it has them at
// that version. A read that the connector moved under returns
// state.ErrConflict. The entries returned are shared: they are never
// changed.
func (s *Store) entries(tenant, name string, c Connector, version int64) ([]*entry, error) {
	if es, ok := s.read.get(tenant, name, c, version); ok {
		return es, nil
	}
	items, err := s.kept.All(tenant, entryPrefix(name), MaxEntries)
	if err != nil {
		return nil, err
	}
	vecs, err := s.kept.All(tenant, vectorPrefix(name), MaxEntries)
	if err != nil {
		return nil, err
	}
	if _, again, err := s.connector(tenant, name); err != nil {
		return nil, err
	} else if again != version {
		return nil, state.ErrConflict
	}
	byPath := make(map[string]*vectorRecord, len(vecs))
	for _, it := range vecs {
		if it.Value.Vector != nil {
			byPath[strings.TrimPrefix(it.Key, vectorPrefix(name))] = it.Value.Vector
		}
	}
	out := make([]*entry, 0, len(items))
	for _, it := range items {
		if it.Value.Entry == nil {
			continue
		}
		in := indexed(*it.Value.Entry)
		if v := byPath[in.Path]; v != nil && v.Generation == c.Generation {
			in.vec, in.model = v.decode()
		}
		out = append(out, in)
	}
	slices.SortFunc(out, func(a, b *entry) int { return strings.Compare(a.Path, b.Path) })
	s.read.put(tenant, name, c, version, out)
	return out, nil
}

// readCache keeps the entries this process read, by connector, its record
// and the version of its record. Every change to a connector's entries or
// vectors moves that version, and a connector removed and declared again
// starts its versions over with another record (its crawl time differs), so
// an entry here is what the database holds at both, and dropping one changes
// no answer.
type readCache struct {
	mu sync.Mutex
	by map[[2]string]cached
}

type cached struct {
	connector Connector
	version   int64
	entries   []*entry
}

// cacheSize bounds how many connectors' entries a process keeps.
const cacheSize = 64

func newReadCache() *readCache { return &readCache{by: map[[2]string]cached{}} }

func (r *readCache) get(tenant, name string, connector Connector, version int64) ([]*entry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.by[[2]string{tenant, name}]
	if !ok || c.version != version || !sameConnector(c.connector, connector) {
		return nil, false
	}
	return c.entries, true
}

func (r *readCache) put(tenant, name string, connector Connector, version int64, entries []*entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := [2]string{tenant, name}
	if _, ok := r.by[k]; !ok && len(r.by) >= cacheSize {
		for other := range r.by {
			delete(r.by, other)
			break
		}
	}
	r.by[k] = cached{connector, version, entries}
}

// sameConnector compares two connector records by value.
func sameConnector(a, b Connector) bool {
	at := func(t *time.Time) time.Time {
		if t == nil {
			return time.Time{}
		}
		return *t
	}
	mark := func(m *Mark) Mark {
		if m == nil {
			return Mark{}
		}
		return *m
	}
	return a.Name == b.Name && a.System == b.System && a.Owner == b.Owner && a.Project == b.Project && a.Entries == b.Entries && a.Generation == b.Generation &&
		at(a.CrawledAt).Equal(at(b.CrawledAt)) && (a.CrawledAt == nil) == (b.CrawledAt == nil) &&
		mark(a.Published) == mark(b.Published) && (a.Published == nil) == (b.Published == nil) &&
		mark(a.Withdrawn) == mark(b.Withdrawn) && (a.Withdrawn == nil) == (b.Withdrawn == nil)
}
