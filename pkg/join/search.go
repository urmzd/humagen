package join

import (
	"math"
	"slices"
	"sort"
)

// Search ranking. A message is matched against each agent's name, prompt,
// description and the asks it answered well, with BM25, the way tool search
// finds a tool by its description. Only the best few are then asked, so a
// workspace with many agents costs a handful of model calls per message, not
// one per agent.
const (
	// Shortlist is how many of the best matches are asked about a message.
	Shortlist = 3
	// MaxAsks bounds how many asks an agent answered well are searched.
	MaxAsks = 25
	// maxAsk bounds how much of one ask is searched.
	maxAsk = 500
	// nameWeight counts a word of an agent's name this many times: a
	// message that names what the agent is called is the strongest signal.
	nameWeight = 3
	// BM25's term saturation and length normalisation, as pkg/catalog uses.
	k1 = 1.2
	b  = 0.75
)

// Match is a candidate the search found for a message: its score, and the
// words of the message that matched it, in the order the message says them.
type Match struct {
	Candidate
	Score float64
	Terms []string
}

// Search ranks candidates against message and returns at most k that match
// it at all, best first. Candidates that score the same keep their order, so
// the answer is the same every time. A message that shares no word with any
// candidate finds none.
func Search(cs []Candidate, message string, k int) []Match {
	if k <= 0 || len(cs) == 0 {
		return nil
	}
	query := unique(words(message))
	if len(query) == 0 {
		return nil
	}
	docs := make([]map[string]int, len(cs))
	lengths := make([]int, len(cs))
	df := map[string]int{}
	total := 0
	for i, c := range cs {
		tf := map[string]int{}
		n := 0
		add := func(text string, weight int) {
			for _, w := range words(text) {
				tf[w] += weight
				n += weight
			}
		}
		add(c.Name, nameWeight)
		add(c.Description, 1)
		add(clip(c.Prompt, maxPrompt), 1)
		for j, ask := range c.Asks {
			if j == MaxAsks {
				break
			}
			add(clip(ask, maxAsk), 1)
		}
		for w := range tf {
			df[w]++
		}
		docs[i], lengths[i] = tf, n
		total += n
	}
	avg := float64(total) / float64(len(cs))
	if avg == 0 {
		return nil
	}
	var out []Match
	for i, c := range cs {
		var score float64
		var terms []string
		for _, w := range query {
			f := float64(docs[i][w])
			if f == 0 {
				continue
			}
			idf := math.Log(1 + (float64(len(cs))-float64(df[w])+0.5)/(float64(df[w])+0.5))
			score += idf * f * (k1 + 1) / (f + k1*(1-b+b*float64(lengths[i])/avg))
			terms = append(terms, w)
		}
		if score > 0 {
			out = append(out, Match{Candidate: c, Score: score, Terms: terms})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > k {
		out = out[:k]
	}
	return out
}

// Candidates are the candidates of matches, in the matches' order.
func Candidates(ms []Match) []Candidate {
	out := make([]Candidate, len(ms))
	for i, m := range ms {
		out[i] = m.Candidate
	}
	return out
}

func unique(ws []string) []string {
	var out []string
	for _, w := range ws {
		if !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}
