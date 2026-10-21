package inbox

// PartDraft is a post an agent wrote for a project that waits for one of the
// project's owners: publishing it posts it as an announcement from the agent,
// recording who approved it, and discarding it drops it. A draft is shown only
// to the people it is addressed to, and never reaches a channel.
const PartDraft PartType = "draft"

// DraftPart is what an agent would post, and where.
type DraftPart struct {
	// Project is where it would be posted. "" is the root.
	Project string `json:"project"`
	// Agent wrote it, in Run.
	Agent string `json:"agent"`
	Run   string `json:"run"`
	// Subject and Text are what would be posted, as the agent wrote them:
	// plain text, never trusted.
	Subject string `json:"subject"`
	Text    string `json:"text,omitempty"`
}

// clone returns a copy that shares nothing with d.
func (d *DraftPart) clone() *DraftPart {
	if d == nil {
		return nil
	}
	c := *d
	return &c
}
