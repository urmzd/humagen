// Package dense holds the embedders behind Humagen's dense retrieval: the
// Embedder interface (Embed, Dimensions, Model), a deterministic Hashing
// embedder for tests and local runs, an HTTP embedder for OpenAI-compatible
// embeddings endpoints, and Vertex AI.
package dense

import (
	"errors"
	"strings"
	"unicode"
)

// ErrInvalid reports an embedder configuration that cannot work.
var ErrInvalid = errors.New("knowledge: invalid")

// ErrEmbedding reports that the embedder failed or returned vectors a store
// cannot use: the wrong count, the wrong width, or a value that is not a
// finite number. Callers wrap the embedder's own error in it.
var ErrEmbedding = errors.New("knowledge: embedding failed")

// tokenize splits text into lowercase words of letters and digits.
func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
