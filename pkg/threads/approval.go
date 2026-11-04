package threads

import (
	"fmt"
	"slices"
	"time"
)

// Approval modes for a conversation's gated tool actions.
const (
	// ApprovalAsk asks a person, as every conversation does by default.
	ApprovalAsk = "ask"
	// ApprovalAuto approves for the person who chose it what they may
	// approve themselves, within policy. A budget request, and an action an
	// agent or the policy always asks about, still goes to a person.
	ApprovalAuto = "auto"
)

// Approval is a conversation's approval mode, set by a person. A nil
// Approval is ApprovalAsk.
type Approval struct {
	Mode string `json:"mode"`
	By   string `json:"by"`
	// Roles are the roles By held when they chose it: an approval given for
	// them never holds more, and holds less once they hold less.
	Roles []string  `json:"roles,omitempty"`
	At    time.Time `json:"at"`
}

func (a *Approval) clone() *Approval {
	if a == nil {
		return nil
	}
	c := *a
	c.Roles = slices.Clone(a.Roles)
	return &c
}

func sameApproval(a, b *Approval) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Mode == b.Mode && a.By == b.By && a.At.Equal(b.At) && slices.Equal(a.Roles, b.Roles)
}

// Auto reports whether gated actions are approved for the person who chose it.
func (a *Approval) Auto() bool { return a != nil && a.Mode == ApprovalAuto }

// SetApproval sets a conversation's approval mode: ApprovalAuto for by, who
// holds roles, or
// ApprovalAsk, which clears it. Its version and when it was last active stay,
// as for an archive. A read-only or archived conversation keeps asking.
func (s *Store) SetApproval(tenantName, id, mode, by string, roles []string, at time.Time) (Thread, error) {
	var next *Approval
	switch {
	case mode == ApprovalAuto && by != "":
		next = &Approval{Mode: ApprovalAuto, By: by, Roles: slices.Clone(roles), At: at.UTC()}
	case mode == ApprovalAsk:
	default:
		return Thread{}, fmt.Errorf("%w: an approval mode is %s or %s, set by a person", ErrInvalid, ApprovalAsk, ApprovalAuto)
	}
	h, err := s.edit(tenantName, id, func(h *head, _ *write) error {
		if next != nil && (h.ReadOnly || h.Archive != nil) {
			return fmt.Errorf("%w: a read-only or archived conversation keeps asking", ErrInvalid)
		}
		h.Approval = next.clone()
		return nil
	})
	if err != nil {
		return Thread{}, err
	}
	s.tell(tenantName, Notice{ID: id, At: at})
	return h.Thread, nil
}
