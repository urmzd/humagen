package drive

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/urmzd/humagen/internal/textindex"
)

// maxIndexed is the largest file search reads. Larger files are installed and
// read whole, but not found by search.
const maxIndexed = 64 << 10

// maxIndexes bounds how many trees' indexes are held at once.
const maxIndexes = 1024

// Hit is one file a search found.
type Hit struct {
	Tree    Tree   `json:"tree"`
	Path    string `json:"path"`
	Version int64  `json:"version"`
	// Score is how many of the query's words the file holds.
	Score   int    `json:"score"`
	Snippet string `json:"snippet"`
}

// index is one version's words, by path. A version of one generation of a
// tree never changes, so an index of it is right for as long as it is kept.
type index struct {
	gen     string
	version int64
	words   map[string][]string
	text    map[string]string
}

func words(text string) []string {
	out := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	slices.Sort(out)
	return slices.Compact(out)
}

// indexOf returns the index of a tree's head, read per call: the index
// cached for the head's generation and version, or one built from the head's
// files, at most MaxEntries of at most maxIndexed bytes each.
func (s *Store) indexOf(ctx context.Context, tenant string, tree Tree) (*index, error) {
	h, _, err := s.head(ctx, tenant, tree)
	if err != nil {
		return nil, err
	}
	k := tk(tenant, tree)
	s.indexMu.Lock()
	x := s.indexes[k]
	s.indexMu.Unlock()
	if x != nil && x.gen == h.Gen && x.version == h.Version {
		return x, nil
	}
	v, err := s.version(ctx, tenant, h, h.Version)
	if err != nil {
		return nil, err
	}
	x = &index{gen: h.Gen, version: v.N, words: map[string][]string{}, text: map[string]string{}}
	for _, e := range v.Entries {
		if e.Size > maxIndexed {
			continue
		}
		b, err := s.Content(ctx, tenant, e.Hash)
		if err != nil {
			return nil, err
		}
		if !utf8.Valid(b) {
			continue
		}
		x.words[e.Path] = words(e.Path + " " + string(b))
		x.text[e.Path] = string(b)
	}
	s.indexMu.Lock()
	if len(s.indexes) >= maxIndexes {
		// Indexes are derived: dropping them costs a rebuild, nothing else.
		clear(s.indexes)
	}
	s.indexes[k] = x
	s.indexMu.Unlock()
	return x, nil
}

// Search finds the files in trees that hold the query's words, ranked by how
// many they hold. Trees the actor may not read are left out, not refused.
// It answers at most limit hits, and at most MaxPage.
func (s *Store) Search(ctx context.Context, tenant string, who Actor, trees []Tree, query string, limit int) ([]Hit, error) {
	if len(query) > maxQuery || len(trees) > MaxSearchTrees {
		return nil, fmt.Errorf("%w: a search reads at most %d trees with a query of at most %d bytes", ErrInvalid, MaxSearchTrees, maxQuery)
	}
	terms := words(query)
	if len(terms) == 0 || len(terms) > 16 {
		return []Hit{}, nil
	}
	if limit <= 0 || limit > MaxPage {
		limit = MaxPage
	}
	hits := []Hit{}
	for _, tree := range trees {
		if tree.Valid() != nil || !who.grant(tree).Read {
			continue
		}
		x, err := s.indexOf(ctx, tenant, tree)
		if err != nil {
			return nil, err
		}
		for p, have := range x.words {
			score := 0
			for _, term := range terms {
				if _, ok := slices.BinarySearch(have, term); ok {
					score++
				}
			}
			if score > 0 {
				hits = append(hits, Hit{Tree: tree, Path: p, Version: x.version, Score: score, Snippet: textindex.Snippet(x.text[p], query)})
			}
		}
	}
	slices.SortFunc(hits, func(a, b Hit) int {
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		if c := strings.Compare(a.Tree.Key(), b.Tree.Key()); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	return hits[:min(len(hits), limit)], nil
}
