package legacy

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
)

// Notes prints what a person should know at start about an old name in use,
// each distinct note once, as "humagen: note". A nil *Notes drops them.
type Notes struct {
	mu   sync.Mutex
	w    io.Writer
	seen map[string]bool
	all  []string
}

// NewNotes writes notes to w.
func NewNotes(w io.Writer) *Notes { return &Notes{w: w, seen: map[string]bool{}} }

// Add prints a note unless it was printed already.
func (n *Notes) Add(format string, a ...any) {
	if n == nil {
		return
	}
	s := fmt.Sprintf(format, a...)
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.seen[s] {
		return
	}
	n.seen[s] = true
	n.all = append(n.all, s)
	if n.w != nil {
		fmt.Fprintln(n.w, "humagen: "+s)
	}
}

// All is every note added, in order.
func (n *Notes) All() []string {
	if n == nil {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.all...)
}

// FromCommand notes a program started by its old name, arg0 being argv[0].
func FromCommand(arg0 string, n *Notes) {
	base := strings.TrimSuffix(filepath.Base(arg0), ".exe")
	if base == Command {
		n.Add("the command is humagen now; %s still runs it", Command)
	}
}
