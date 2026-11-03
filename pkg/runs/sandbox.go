package runs

import (
	"slices"
	"unicode/utf8"
)

// Bounds on what a run's sandbox record keeps.
const (
	// MaxSandboxFiles is the most files one run's record names. Past it,
	// More counts the rest.
	MaxSandboxFiles = 64
	// MaxSandboxConflicts is the most copyback refusals one run's record keeps.
	MaxSandboxConflicts = 16
	maxSandboxText      = 512
)

// SandboxFile is a file a run wrote or removed in its conversation's files.
type SandboxFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	// Revision is the sha256 of the content the run left, empty when it
	// removed the file.
	Revision string `json:"revision,omitempty"`
	Removed  bool   `json:"removed,omitempty"`
}

// Sandbox is what a run did in its own sandbox (RFC 0004): the files it
// left in its conversation's files when its instance was released, or, in an
// unisolated deployment, the files its tools wrote there directly. Conflicts
// are the changes copyback refused because a person or another run changed
// the same file after this run loaded it.
type Sandbox struct {
	Isolated  bool          `json:"isolated"`
	Files     []SandboxFile `json:"files"`
	More      int           `json:"more,omitempty"`
	Conflicts []string      `json:"conflicts,omitempty"`
}

func clipSandbox(s string) string {
	if len(s) <= maxSandboxText {
		return s
	}
	s = s[:maxSandboxText]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// merged folds files and conflicts into a copy of s, the latest write of a
// path replacing an earlier one, within the record's bounds.
func (s *Sandbox) merged(isolated bool, files []SandboxFile, conflicts []string) Sandbox {
	out := Sandbox{Isolated: isolated, Files: []SandboxFile{}}
	if s != nil {
		out.Isolated = s.Isolated || isolated
		out.Files = slices.Clone(s.Files)
		out.More = s.More
		out.Conflicts = slices.Clone(s.Conflicts)
	}
	for _, f := range files {
		f.Path = clipSandbox(f.Path)
		if f.Removed {
			f.Revision, f.Size = "", 0
		}
		if i := slices.IndexFunc(out.Files, func(g SandboxFile) bool { return g.Path == f.Path }); i >= 0 {
			out.Files[i] = f
			continue
		}
		if len(out.Files) >= MaxSandboxFiles {
			out.More++
			continue
		}
		out.Files = append(out.Files, f)
	}
	for _, c := range conflicts {
		if len(out.Conflicts) >= MaxSandboxConflicts {
			break
		}
		out.Conflicts = append(out.Conflicts, clipSandbox(c))
	}
	return out
}

// RecordSandbox keeps what a run wrote and removed in its sandbox, and what
// copyback refused. It is recorded after the run ended too: an instance is
// released once the turn is over. A run with no record is skipped.
func (x *Index) RecordSandbox(tenant, runID string, isolated bool, files []SandboxFile, conflicts []string) error {
	if len(files) == 0 && len(conflicts) == 0 {
		return nil
	}
	return x.do(tenant, func(t *txn) error {
		r, err := t.run(runID)
		if err != nil {
			return err
		}
		if r == nil {
			return ErrNotFound
		}
		next := r.Sandbox.merged(isolated, files, conflicts)
		r.Sandbox = &next
		t.touch(r)
		return nil
	})
}
