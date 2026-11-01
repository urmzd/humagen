package threads

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/urmzd/humagen/internal/textindex"
	"github.com/urmzd/humagen/pkg/state"
)

// Search bounds.
const (
	// maxWord is the most characters of a word its index entry keeps. A
	// longer word is found by its start, and matched in full.
	maxWord = 64
	// maxTerms is the most words a search intersects.
	maxTerms = 16
	// searchThreads is the most threads one search reads messages of, and
	// searchMessages the most messages it reads in all.
	searchThreads  = 200
	searchMessages = 4000
)

// words are a text's whole words, lowercased, each once, as textindex reads
// them.
func words(text string) []string {
	out := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	for i, w := range out {
		if r := []rune(w); len(r) > maxWord {
			out[i] = string(r[:maxWord])
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func wordKey(word, id string) string { return keyWord + word + ":" + id }

// Search returns, newest first, at most limit messages posted since that say
// every word of query, in the threads keep holds.
//
// It reads the stored word index ("w:<word>:<thread>"), which says which
// threads say each word: at most MaxThreads entries a word, and at most
// maxTerms words. Of the threads that say every word, active since, it
// reads the messages of the searchThreads most recently active, newest
// first, at most searchMessages in all, and matches each in full.
func (s *Store) Search(tenantName, query string, since time.Time, limit int, keep func(Thread) bool) ([]Message, error) {
	limit = min(max(limit, 1), 1000)
	terms := words(query)
	out := []Message{}
	if len(terms) == 0 || len(terms) > maxTerms {
		return out, nil
	}
	var in map[string]bool
	for _, term := range terms {
		items, err := s.kept.All(tenantName, keyWord+term+":", MaxThreads)
		if err != nil {
			return nil, readErr(err)
		}
		has := make(map[string]bool, len(items))
		for _, it := range items {
			id := strings.TrimPrefix(it.Key, keyWord+term+":")
			if in == nil || in[id] {
				has[id] = true
			}
		}
		if in = has; len(in) == 0 {
			return out, nil
		}
	}
	// The activity index holds the threads most recently active first, so
	// the threads to read are found in that order, and none active before
	// since is read.
	from := stamp(since)
	var order []string
	after := ""
	for read := 0; read < MaxThreads && len(order) < min(searchThreads, len(in)); {
		items, next, err := s.kept.List(tenantName, state.Query{Prefix: keyActivity, After: after, Limit: state.MaxList})
		if err != nil {
			return nil, readErr(err)
		}
		read += len(items)
		done := false
		for _, it := range items {
			at, id, ok := fromActivity(it.Key)
			if !ok {
				continue
			}
			if at < from {
				done = true
				break
			}
			if in[id] && len(order) < searchThreads {
				order = append(order, id)
			}
		}
		if done || next == "" {
			break
		}
		after = next
	}
	budget := searchMessages
	for _, id := range order {
		if budget <= 0 {
			break
		}
		h, _, err := s.lookup(tenantName, id)
		if err != nil {
			continue
		}
		if h.Archive != nil && h.Archive.Deleted != nil || keep != nil && !keep(h.Thread) {
			continue
		}
		before := ""
		for budget > 0 {
			q := state.Query{Prefix: messagePrefix(id), After: before, Limit: min(MaxPage, budget), Desc: true}
			items, next, err := s.kept.List(tenantName, q)
			if err != nil {
				return nil, readErr(err)
			}
			budget -= len(items)
			for _, it := range items {
				var m Message
				if err := json.Unmarshal(it.Value, &m); err != nil {
					return nil, readErr(err)
				}
				if !m.At.Before(since) && textindex.Matches(m.Text, query) {
					out = append(out, m)
				}
			}
			if next == "" {
				break
			}
			before = next
		}
	}
	slices.SortFunc(out, func(a, b Message) int {
		return cmp.Or(b.At.Compare(a.At), strings.Compare(a.Thread+":"+a.ID, b.Thread+":"+b.ID))
	})
	return out[:min(len(out), limit)], nil
}
