// Package textindex provides a derived lexical lookup. Owners serialize access
// and rebuild it from their authoritative records after restart.
package textindex

import (
	"slices"
	"strings"
	"unicode"
)

const MaxText = 65536

// Index maps normalized whole words to record identifiers. Its zero value works.
type Index struct {
	postings map[string]map[string]bool
	terms    map[string][]string
}

func words(text string) []string {
	out := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	slices.Sort(out)
	return slices.Compact(out)
}

func (x *Index) Delete(id string) {
	for _, term := range x.terms[id] {
		delete(x.postings[term], id)
		if len(x.postings[term]) == 0 {
			delete(x.postings, term)
		}
	}
	delete(x.terms, id)
}

func (x *Index) Put(id string, text string) {
	if x.postings == nil {
		x.postings = map[string]map[string]bool{}
		x.terms = map[string][]string{}
	}
	for _, term := range x.terms[id] {
		delete(x.postings[term], id)
		if len(x.postings[term]) == 0 {
			delete(x.postings, term)
		}
	}
	runes := []rune(text)
	if len(runes) > MaxText {
		text = string(runes[:MaxText])
	}
	x.terms[id] = words(text)
	for _, term := range x.terms[id] {
		if x.postings[term] == nil {
			x.postings[term] = map[string]bool{}
		}
		x.postings[term][id] = true
	}
}

// Search intersects at most 16 query terms and returns at most limit IDs.
// keep runs before the cap, under the owner's lock; it must not reenter it.
func (x *Index) Search(query string, limit int, keep func(string) bool) []string {
	terms := words(query)
	if len(terms) == 0 || len(terms) > 16 || limit <= 0 {
		return nil
	}
	slices.SortFunc(terms, func(a, b string) int { return len(x.postings[a]) - len(x.postings[b]) })
	out := []string{}
	for id := range x.postings[terms[0]] {
		matches := true
		for _, term := range terms[1:] {
			if !x.postings[term][id] {
				matches = false
				break
			}
		}
		if matches && (keep == nil || keep(id)) {
			at, _ := slices.BinarySearch(out, id)
			if at >= limit {
				continue
			}
			out = slices.Insert(out, at, id)
			if len(out) > limit {
				out = out[:limit]
			}
		}
	}
	slices.Sort(out)
	return out[:min(len(out), limit)]
}

// Matches checks projected text with the same whole-word grammar as Search.
func Matches(text, query string) bool {
	terms := words(query)
	if len(terms) == 0 || len(terms) > 16 {
		return false
	}
	available := words(text)
	for _, term := range terms {
		if _, ok := slices.BinarySearch(available, term); !ok {
			return false
		}
	}
	return true
}

// Snippet shows a compact excerpt around the first query word.
func Snippet(text, query string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	start := 0
	lower := strings.ToLower(text)
	for _, term := range words(query) {
		if at := strings.Index(lower, term); at >= 0 {
			start = max(0, len([]rune(lower[:at]))-60)
			break
		}
	}
	end := min(len(runes), start+240)
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}
