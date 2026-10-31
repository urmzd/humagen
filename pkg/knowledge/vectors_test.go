package knowledge

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dense "github.com/urmzd/humagen/fiber/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/state"
)

// counting wraps an embedder and counts the texts it was asked to embed.
type counting struct {
	Embedder
	texts atomic.Int64
	fail  atomic.Bool
}

func (c *counting) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if c.fail.Load() {
		return nil, errors.New("embedder down")
	}
	c.texts.Add(int64(len(texts)))
	return c.Embedder.Embed(ctx, texts)
}

func hashing(dims int) *counting { return &counting{Embedder: dense.NewHashing(dims)} }

// indexed waits until every passage in the store has a vector from model.
func indexed(t *testing.T, s *Store, model string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		done := true
		err := s.dir.Each(func(tenant string) error {
			srcs, _, err := s.sources(tenant)
			for _, meta := range srcs {
				src, err := s.current(tenant, meta.Name)
				if err != nil {
					return err
				}
				for _, p := range src.passages {
					if p.vec == nil || p.model != model {
						done = false
					}
				}
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if done {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("passages were not embedded in time")
}

func seedRotation(t *testing.T, s *Store) {
	t.Helper()
	at := time.Unix(0, 0).UTC()
	if _, err := s.Create("acme", "runbooks", "", "ana", at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("acme", "runbooks", "Credentials", "doc://creds", "How to perform key rotation for service credentials.", "ana", at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("acme", "runbooks", "Lunch", "doc://lunch", "The cafeteria serves soup on Fridays.", "ana", at); err != nil {
		t.Fatal(err)
	}
}

// The case hybrid search exists for: no word in common, a shared stem.
func TestHybridFindsWhatLexicalMisses(t *testing.T) {
	s := NewStore()
	seedRotation(t, s)
	if hits, err := s.Search("acme", nil, "rotating keys", 5, ""); err != nil || len(hits) != 0 {
		t.Fatalf("lexical: %v, %v", hits, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e := hashing(256)
	s.UseEmbedder(ctx, e, nil)
	indexed(t, s, e.Model())
	hits, err := s.Search("acme", nil, "rotating keys", 1, "")
	if err != nil || len(hits) != 1 || hits[0].URI != "doc://creds" {
		t.Fatalf("hybrid: %+v, %v", hits, err)
	}
}

func TestHybridKeepsExactMatchesFirst(t *testing.T) {
	s := NewStore()
	seedRotation(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e := hashing(256)
	s.UseEmbedder(ctx, e, nil)
	indexed(t, s, e.Model())
	hits, err := s.Search("acme", nil, "cafeteria", 2, "")
	if err != nil || len(hits) == 0 || hits[0].URI != "doc://lunch" {
		t.Fatalf("%+v, %v", hits, err)
	}
}

// Vectors are written beside the passages and read back, so a restart does
// not pay for them again; a different model computes them again.
func TestVectorsSurviveARestartAndFollowTheModel(t *testing.T) {
	st := state.NewMemory()
	s, err := Open(st)
	if err != nil {
		t.Fatal(err)
	}
	seedRotation(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	first := hashing(256)
	s.UseEmbedder(ctx, first, nil)
	indexed(t, s, first.Model())
	cancel()

	again, err := Open(st)
	if err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	same := hashing(256)
	again.UseEmbedder(ctx2, same, nil)
	indexed(t, again, same.Model())
	if hits, err := again.Search("acme", nil, "rotating keys", 1, ""); err != nil || len(hits) != 1 || hits[0].URI != "doc://creds" {
		t.Fatalf("after restart: %+v, %v", hits, err)
	}
	if n := same.texts.Load(); n != 1 {
		t.Fatalf("restart embedded %d texts; want only the query", n)
	}

	other, err := Open(st)
	if err != nil {
		t.Fatal(err)
	}
	wider := hashing(512)
	other.UseEmbedder(ctx2, wider, nil)
	indexed(t, other, wider.Model())
	if wider.texts.Load() < 2 {
		t.Fatalf("a new model embedded %d texts; want every passage again", wider.texts.Load())
	}
}

// A model outage never fails a write or a search: search is lexical.
func TestHybridFallsBackWhenTheEmbedderFails(t *testing.T) {
	s := NewStore()
	e := hashing(256)
	e.fail.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.UseEmbedder(ctx, e, nil)
	seedRotation(t, s)
	hits, err := s.Search("acme", nil, "cafeteria", 5, "")
	if err != nil || len(hits) != 1 || hits[0].URI != "doc://lunch" {
		t.Fatalf("%+v, %v", hits, err)
	}
}

// Fusion runs over what the lexical arm may see: an excluded document's
// vectors never surface it.
func TestHybridRespectsExclusion(t *testing.T) {
	s := NewStore()
	seedRotation(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e := hashing(256)
	s.UseEmbedder(ctx, e, nil)
	indexed(t, s, e.Model())
	docs, _ := s.Documents("acme", "runbooks")
	for _, d := range docs {
		if d.URI == "doc://creds" {
			if _, err := s.SetExcluded("acme", "runbooks", d.ID, true, time.Unix(1, 0)); err != nil {
				t.Fatal(err)
			}
		}
	}
	hits, err := s.Search("acme", nil, "rotating keys", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.URI == "doc://creds" {
			t.Fatalf("excluded document returned: %+v", hits)
		}
	}
}

// Removing a document removes its vectors from the state store too.
func TestRemoveDeletesVectors(t *testing.T) {
	st := state.NewMemory()
	s, _ := Open(st)
	seedRotation(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e := hashing(256)
	s.UseEmbedder(ctx, e, nil)
	indexed(t, s, e.Model())
	docs, _ := s.Documents("acme", "runbooks")
	for _, d := range docs {
		if err := s.Remove("acme", "runbooks", d.ID, time.Unix(2, 0)); err != nil {
			t.Fatal(err)
		}
	}
	vectors := 0
	recs, err := st.Load(context.Background(), "knowledge")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if strings.HasPrefix(r.Key, "vec/") {
			vectors++
		}
	}
	if vectors != 0 {
		t.Fatalf("%d vector records left", vectors)
	}
}

func TestVectorEncodingRoundTrips(t *testing.T) {
	v := []float32{0.5, -1, 3.25}
	got, ok := decodeVector(encodeVector(v))
	if !ok || len(got) != 3 || got[0] != 0.5 || got[1] != -1 || got[2] != 3.25 {
		t.Fatalf("%v %v", got, ok)
	}
	if _, ok := decodeVector("!!"); ok {
		t.Fatal("accepted bad base64")
	}
}
