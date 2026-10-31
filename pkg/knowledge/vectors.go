package knowledge

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
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

func (s *Store) embedder() *denseIndex {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dense
}

// nudge asks this process's indexer, if any, to look for new passages.
func (s *Store) nudge() { s.embedder().nudge() }

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
		if err := s.sweep(ctx, d); err != nil && ctx.Err() == nil {
			// Left for the next sweep; search stays lexical for these passages.
			d.log.Warn("knowledge: passages not embedded; will retry", "model", d.model, "err", err)
		}
	}
}

// errStop ends a sweep early without an error to report.
var errStop = errors.New("knowledge: sweep stopped")

// sweep embeds every document with a passage that lacks a vector from d's
// model, tenant by tenant through the directory and source by source. The
// texts are embedded with nothing held, and kept only if the document still
// has the same text when they come back. Two processes sweeping at once may
// both embed a document; the vectors either keeps are the same.
func (s *Store) sweep(ctx context.Context, d *denseIndex) error {
	err := s.dir.Each(func(tenant string) error {
		srcs, _, err := s.sources(tenant)
		if err != nil {
			return err
		}
		for _, meta := range srcs {
			src, err := s.current(tenant, meta.Name)
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			for _, doc := range src.docs {
				var texts []string
				pending := false
				for _, p := range src.passages {
					if p.doc == doc {
						texts = append(texts, p.text)
						pending = pending || p.vec == nil || p.model != d.model
					}
				}
				if !pending || len(texts) == 0 {
					continue
				}
				if ctx.Err() != nil {
					return errStop
				}
				bctx, cancel := context.WithTimeout(ctx, batchTimeout)
				vectors, err := d.embedder.Embed(bctx, texts)
				cancel()
				if err == nil && len(vectors) != len(texts) {
					err = dense.ErrEmbedding
				}
				if err != nil {
					return fmt.Errorf("source %s: %w", meta.Name, err)
				}
				if err := s.keepVectors(tenant, meta.Name, src.seqs[doc.ID], texts, d.model, vectors); err != nil {
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

// keepVectors writes a document's vectors at the version of its source,
// unless its text changed or it left while they were computed.
func (s *Store) keepVectors(tenant, name string, seq int64, texts []string, model string, vectors [][]float32) error {
	rec := &vectorRecord{Model: model, Data: make([]string, len(vectors))}
	for i, v := range vectors {
		rec.Data[i] = encodeVector(v)
	}
	return state.Retry(func() error {
		src, srcAt, err := s.meta(tenant, name)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		have, err := s.kept.Lookup(tenant, textKey(name, seq))
		if errors.Is(err, state.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !slices.Equal(have.Value.Passages, texts) {
			return nil
		}
		b := s.kept.Batch()
		b.Put(tenant, vecKey(name, seq), stored{Vectors: rec})
		b.PutAt(tenant, sourceKey(name), stored{Source: &src}, srcAt)
		return b.Commit()
	})
}

func vecPrefix(name string) string { return "vec/" + name + "/" }

func vecKey(name string, seq int64) string { return vecPrefix(name) + state.Seq(seq) }

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
