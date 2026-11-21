package threads

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Approval modes for a conversation's gated tool actions: Ask and Auto.
const (
	// ApprovalAsk asks a person, as every conversation does by default.
	ApprovalAsk = "ask"
	// ApprovalAuto lets the approval agent act for the person who chose it,
	// within what they may do themselves and within policy: it decides
	// gated tool actions, and it answers a question an agent asks a person
	// (ask_person: a choice or a reply) when the session already settles
	// the answer. Anything it is unsure of, a budget request, and an action
	// an agent or the policy always asks about still go to a person. It is
	// opt-in: a conversation asks until a person chooses it.
	ApprovalAuto = "auto"
	// approvalDecide is what Auto was stored as when answering questions
	// was a mode of its own (Run and Answer, beside Run). A conversation
	// stored with it reads as ApprovalAuto, and SetApproval takes it as
	// ApprovalAuto. Kept for stored data: do not reuse the name.
	approvalDecide = "decide"
)

// ApprovalModes are the modes a person may choose, the default first.
var ApprovalModes = []string{ApprovalAsk, ApprovalAuto}

// ApprovalModeOf is mode in its current spelling: the stored decide reads
// as ApprovalAuto, and any other value is unchanged.
func ApprovalModeOf(mode string) string {
	if mode == approvalDecide {
		return ApprovalAuto
	}
	return mode
}

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

// Auto reports whether the approval agent acts for the person who chose it
// (ApprovalAuto): it decides gated actions and answers what the session
// already settles.
func (a *Approval) Auto() bool {
	return a != nil && ApprovalModeOf(a.Mode) == ApprovalAuto
}

// ModeOf is the mode a conversation is in: ApprovalAsk for a nil Approval.
func (a *Approval) ModeOf() string {
	if a == nil || a.Mode == "" {
		return ApprovalAsk
	}
	return ApprovalModeOf(a.Mode)
}

// SetApproval sets a conversation's approval mode: ApprovalAuto for by,
// who holds roles, or ApprovalAsk, which clears it. The stored decide is
// taken as ApprovalAuto. Its version and when it was last active stay, as
// for an archive. A read-only or archived conversation keeps asking.
func (s *Store) SetApproval(tenantName, id, mode, by string, roles []string, at time.Time) (Thread, error) {
	mode = ApprovalModeOf(mode)
	var next *Approval
	switch {
	case mode == ApprovalAuto && by != "":
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
