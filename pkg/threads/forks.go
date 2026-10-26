package threads

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Fork history is passive text, never an executable message or run.
const (
	MaxForkTurns = 200
	MaxForkBytes = 128 * 1024
	MaxForkDepth = 8
)

// Fork records immutable lineage and a bounded snapshot of the source.
type Fork struct {
	Sources      *[]string  `json:"sources,omitempty"`
	SourceThread string     `json:"source_thread"`
	Through      string     `json:"through"`
	By           string     `json:"by"`
	At           time.Time  `json:"at"`
	Context      []ForkTurn `json:"context"`
}

// ForkTurn preserves attribution without actions, tags, tools or approvals.
type ForkTurn struct {
	Kind string    `json:"kind"`
	By   string    `json:"by"`
	Name string    `json:"name,omitempty"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

func (f *Fork) clone() *Fork {
	if f == nil {
		return nil
	}
	cp := *f
	cp.Context = slices.Clone(f.Context)
	if f.Sources != nil {
		names := slices.Clone(*f.Sources)
		if names == nil {
			names = []string{}
		}
		cp.Sources = &names
	}
	return &cp
}

func sameFork(a, b *Fork) bool {
	if a == nil || b == nil {
		return a == b
	}
	sameSources := a.Sources == nil && b.Sources == nil || a.Sources != nil && b.Sources != nil && slices.Equal(*a.Sources, *b.Sources)
	return sameSources && a.SourceThread == b.SourceThread && a.Through == b.Through && a.By == b.By && a.At.Equal(b.At) && slices.Equal(a.Context, b.Context)
}

func (f *Fork) validate(id string) error {
	if f == nil {
		return nil
	}
	if !ValidID(f.SourceThread) || f.SourceThread == id || (!validRun.MatchString(f.Through) && !validInboxID.MatchString(f.Through)) || strings.TrimSpace(f.By) == "" || f.At.IsZero() || len(f.Context) == 0 || len(f.Context) > MaxForkTurns {
		return fmt.Errorf("%w: a fork names another conversation, a saved turn, its author and bounded history", ErrInvalid)
	}
	if f.Sources != nil {
		if len(*f.Sources) > 16 {
			return fmt.Errorf("%w: at most 16 selected sources", ErrInvalid)
		}
		for _, name := range *f.Sources {
			if strings.TrimSpace(name) == "" || strings.ContainsAny(name, ",\n\r") {
				return fmt.Errorf("%w: invalid source name", ErrInvalid)
			}
		}
	}
	size := 0
	for _, t := range f.Context {
		size += len(t.Text) + len(t.By) + len(t.Name)
		if (t.Kind != Person && t.Kind != Agent && t.Kind != System) || strings.TrimSpace(t.By) == "" || t.At.IsZero() || !utf8.ValidString(t.Text) || strings.ContainsRune(t.Text, 0) || size > MaxForkBytes {
			return fmt.Errorf("%w: invalid or oversized passive fork history", ErrInvalid)
		}
	}
	return nil
}
