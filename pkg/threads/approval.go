package threads

import (
	"fmt"
	"slices"
	"strings"
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
	// ApprovalDecide is ApprovalAuto, and the approval agent also answers,
	// for the person who chose it, a question an agent asks a person
	// (ask_person: a choice or a reply) when the session already settles
	// the answer. Anything else it hands to a person. It is opt-in: a
	// conversation asks until a person chooses it.
	ApprovalDecide = "decide"
)

// ApprovalModes are the modes a person may choose, the default first.
var ApprovalModes = []string{ApprovalAsk, ApprovalAuto, ApprovalDecide}

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

// Auto reports whether gated actions are approved for the person who chose
// it: ApprovalAuto, or ApprovalDecide, which does that too.
func (a *Approval) Auto() bool {
	return a != nil && (a.Mode == ApprovalAuto || a.Mode == ApprovalDecide)
}

// Decides reports whether a question an agent asks a person may also be
// answered for the person who chose it (ApprovalDecide).
func (a *Approval) Decides() bool { return a != nil && a.Mode == ApprovalDecide }

// ModeOf is the mode a conversation is in: ApprovalAsk for a nil Approval.
func (a *Approval) ModeOf() string {
	if a == nil || a.Mode == "" {
		return ApprovalAsk
	}
	return a.Mode
}

// SetApproval sets a conversation's approval mode: ApprovalAuto or
// ApprovalDecide for by, who holds roles, or ApprovalAsk, which clears it. Its version and when it was last active stay,
// as for an archive. A read-only or archived conversation keeps asking.
func (s *Store) SetApproval(tenantName, id, mode, by string, roles []string, at time.Time) (Thread, error) {
	var next *Approval
	switch {
	case (mode == ApprovalAuto || mode == ApprovalDecide) && by != "":
		next = &Approval{Mode: mode, By: by, Roles: slices.Clone(roles), At: at.UTC()}
	case mode == ApprovalAsk:
	default:
		return Thread{}, fmt.Errorf("%w: an approval mode is %s, set by a person", ErrInvalid, strings.Join(ApprovalModes, ", "))
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
