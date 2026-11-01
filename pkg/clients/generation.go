package clients

import (
	"errors"
	"fmt"

	"github.com/urmzd/humagen/pkg/state"
)

// Generations is a store that counts changes to the clients of every tenant,
// so a process can tell, with one read, that another process on its database
// added, changed or removed a client and its own connectors are out of date.
type Generations interface {
	// Touch counts one change and returns the count it made. A caller
	// touches after a change is whole: the record and its secrets.
	Touch() (int64, error)
	// Generation is how many changes were counted.
	Generation() (int64, error)
}

// generationNamespace and generationKey hold the count, under no tenant: it
// is the deployment's, one record every process reads.
const (
	generationNamespace = "clients.generation"
	generationKey       = "count"
)

var _ Generations = (*Memory)(nil)

func (m *Memory) generations() *state.Table[int64] {
	return state.NewTable[int64](m.st, generationNamespace)
}

// Touch implements Generations.
func (m *Memory) Touch() (int64, error) {
	n, err := m.generations().Update("", generationKey, func(n int64, _ bool) (int64, error) { return n + 1, nil })
	if err != nil {
		return 0, fmt.Errorf("clients: touch: %w", err)
	}
	return n, nil
}

// Generation implements Generations.
func (m *Memory) Generation() (int64, error) {
	it, err := m.generations().Lookup("", generationKey)
	if errors.Is(err, state.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("clients: generation: %w", err)
	}
	return it.Value, nil
}
