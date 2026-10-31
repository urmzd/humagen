package catalog

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"time"

	dense "github.com/urmzd/humagen/fiber/pkg/knowledge"
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
	// Embedded before the lock: it may call a model.
	s.mu.RLock()
	d := s.dense
	s.mu.RUnlock()
	qv, model := d.query(query)

	names := slices.Clone(connectors)
	slices.Sort(names)
	names = slices.Compact(names)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var in []*entry
	for _, n := range names {
		if c, ok := s.tenants[tenant][n]; ok {
			in = append(in, c.entries...)
		}
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
		s.sweep(ctx, d)
	}
}

// pending is a batch of one connector's entries that need vectors.
type pending struct {
	tenant, name string
	generation   int64
	entries      []*entry
	texts        []string
}

// sweep embeds every entry without a vector from d's model. Texts are read
// under the read lock, embedded with no lock held, and kept only if the
// connector has not been crawled again meanwhile.
func (s *Store) sweep(ctx context.Context, d *denseIndex) {
	var jobs []pending
	s.mu.RLock()
	for tenant, cs := range s.tenants {
		for name, c := range cs {
			var job *pending
			for _, e := range c.entries {
				if e.vec != nil && e.model == d.model {
					continue
				}
				if job == nil || len(job.entries) == batchSize {
					jobs = append(jobs, pending{tenant: tenant, name: name, generation: c.Generation})
					job = &jobs[len(jobs)-1]
				}
				job.entries = append(job.entries, e)
				job.texts = append(job.texts, Text(e.Entry))
			}
		}
	}
	s.mu.RUnlock()

	for _, j := range jobs {
		if ctx.Err() != nil {
			return
		}
		bctx, cancel := context.WithTimeout(ctx, batchTimeout)
		vectors, err := d.embedder.Embed(bctx, j.texts)
		cancel()
		if err == nil && len(vectors) != len(j.texts) {
			err = dense.ErrEmbedding
		}
		if err != nil {
			d.log.Warn("catalog: entries not embedded; will retry", "model", d.model, "connector", j.name, "err", err)
			return
		}
		s.keepVectors(j, d.model, vectors)
	}
}

func (s *Store) keepVectors(j pending, model string, vectors [][]float32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.tenants[j.tenant][j.name]
	if !ok || c.Generation != j.generation {
		return
	}
	b := s.kept.Batch()
	for i, e := range j.entries {
		b.Put(j.tenant, vectorKey(j.name, e.Path), record{Vector: &vectorRecord{Model: model, Generation: j.generation, Data: encode(vectors[i])}})
	}
	if err := b.Commit(); err != nil {
		return
	}
	for i, e := range j.entries {
		e.vec, e.model = vectors[i], model
	}
}
