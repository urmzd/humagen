package knowledge

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"log/slog"
	"math"
	"slices"
	"time"

	dense "github.com/urmzd/humagen/fiber/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/state"
)

// Embedder turns text into vectors. It is fiber's, so every embedder fiber
// has (hashing, OpenAI-compatible HTTP, Vertex AI) works here.
type Embedder = dense.Embedder

// Timeouts for calls to the embedder. A query is embedded on the request path,
// so it gives up quickly and the search is lexical; a batch of passages is
// embedded in the background and may take longer.
const (
	queryTimeout = 3 * time.Second
	batchTimeout = 2 * time.Minute
	// retryEvery is how often the indexer looks again after a failure, or for
	// work a missed nudge left behind.
	retryEvery = 5 * time.Minute
	// rrfK is reciprocal rank fusion's constant, as fiber's hybrid store uses:
	// it keeps the top of one list from drowning the other.
	rrfK = 60
	// denseCandidates bounds how many vector matches join the fusion.
	denseCandidates = 50
)

// denseIndex is the store's embedder and the indexer that fills passages'
// vectors. A nil *denseIndex is lexical only: every method is safe on nil.
type denseIndex struct {
	embedder Embedder
	model    string
	wake     chan struct{}
	log      *slog.Logger
}

// UseEmbedder turns on hybrid search. Passages without a vector from e, or
// with one from another model, are embedded in the background until ctx is
// done; search fuses vector and lexical rankings over passages that have one.
// It is called once, at start. A nil e leaves the store lexical.
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

// nudge asks the indexer to look for passages without vectors. It never
// blocks: one pending nudge covers any number of writes.
func (d *denseIndex) nudge() {
	if d == nil {
		return
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// query embeds a search query. It returns nil when there is no embedder or
// the call fails, and search is then lexical.
func (d *denseIndex) query(text string) ([]float32, string) {
	if d == nil {
		return nil, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	v, err := d.embedder.Embed(ctx, []string{text})
	if err != nil || len(v) != 1 || len(v[0]) != d.embedder.Dimensions() {
		d.log.Warn("knowledge: query not embedded; searching lexically", "model", d.model, "err", err)
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

// job is one document whose passages need vectors.
type job struct {
	tenant, name string
	seq          int64
	doc          *Document
	texts        []string
}

// sweep embeds every document with a passage that lacks a vector from d's
// model. The texts are read under the read lock, embedded with no lock held,
// and kept only if the document is still the same one when they come back.
func (s *Store) sweep(ctx context.Context, d *denseIndex) {
	var jobs []job
	s.mu.RLock()
	for tenant, sources := range s.tenants {
		for name, src := range sources {
			pending := map[*Document][]string{}
			for _, p := range src.passages {
				if p.vec == nil || p.model != d.model {
					pending[p.doc] = nil
				}
			}
			for doc := range pending {
				var texts []string
				for _, p := range src.passages {
					if p.doc == doc {
						texts = append(texts, p.text)
					}
				}
				jobs = append(jobs, job{tenant: tenant, name: name, seq: src.seqs[doc.ID], doc: doc, texts: texts})
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
			// Left for the next sweep; search stays lexical for these passages.
			d.log.Warn("knowledge: passages not embedded; will retry", "model", d.model, "source", j.name, "err", err)
			return
		}
		s.keepVectors(j, d.model, vectors)
	}
}

// keepVectors writes a document's vectors and attaches them, unless the
// document changed or left while they were computed.
func (s *Store) keepVectors(j job, model string, vectors [][]float32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src, ok := s.tenants[j.tenant][j.name]
	if !ok || src.seqs[j.doc.ID] != j.seq {
		return
	}
	var ps []*passage
	for _, p := range src.passages {
		if p.doc == j.doc {
			ps = append(ps, p)
		}
	}
	if len(ps) != len(vectors) {
		return
	}
	rec := &vectorRecord{Model: model, Data: make([]string, len(vectors))}
	for i, v := range vectors {
		rec.Data[i] = encodeVector(v)
	}
	if err := s.kept.Put(j.tenant, vecKey(j.name, j.seq), stored{Vectors: rec}); err != nil {
		return
	}
	rec.attach(ps)
}

func vecKey(name string, seq int64) string { return "vec/" + name + "/" + state.Seq(seq) }

// vectorRecord is one document's passage vectors, in passage order, each a
// little-endian float32 array in base64. Model says which space they are in.
type vectorRecord struct {
	Model string   `json:"model"`
	Data  []string `json:"data"`
}

// attach gives each passage its vector. A record that does not match the
// passages (a different count, a bad encoding) attaches nothing, and the
// indexer computes them again.
func (r *vectorRecord) attach(ps []*passage) {
	if r == nil || len(r.Data) != len(ps) {
		return
	}
	vs := make([][]float32, len(ps))
	for i, enc := range r.Data {
		v, ok := decodeVector(enc)
		if !ok {
			return
		}
		vs[i] = v
	}
	for i, p := range ps {
		p.vec, p.model = vs[i], r.Model
	}
}

func encodeVector(v []float32) string {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return base64.StdEncoding.EncodeToString(b)
}

func decodeVector(s string) ([]float32, bool) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b)%4 != 0 || len(b) == 0 {
		return nil, false
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v, true
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

// scored is a passage with its score in one ranking.
type scored struct {
	src   *source
	p     *passage
	score float64
}

// byScore orders highest first, keeping the store's order among equals.
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

// fuse merges the lexical and dense rankings by reciprocal rank fusion: each
// passage scores the sum of 1/(rrfK+rank) over the lists it appears in. Only
// positions count, so a BM25 score and a cosine need no calibration. The
// dense list is cut to its head first: every passage has some cosine, and
// only the closest should be able to outrank a lexical match.
func fuse(lexical, denseRanked []scored) []scored {
	if len(denseRanked) > denseCandidates {
		denseRanked = denseRanked[:denseCandidates]
	}
	at := map[*passage]int{}
	var out []scored
	add := func(list []scored) {
		for rank, c := range list {
			score := 1 / float64(rrfK+rank+1)
			if i, ok := at[c.p]; ok {
				out[i].score += score
				continue
			}
			at[c.p] = len(out)
			out = append(out, scored{src: c.src, p: c.p, score: score})
		}
	}
	add(lexical)
	add(denseRanked)
	byScore(out)
	return out
}
