package library

// outcomeOrigin retains a trusted source through ordinary edits. Promotion
// starts a new scope and intentionally carries no private run capability.
func outcomeOrigin(previous []Revision, from *Provenance, trusted []string) string {
	if from != nil {
		return ""
	}
	if len(trusted) == 1 {
		return trusted[0]
	}
	if len(previous) > 0 {
		return previous[len(previous)-1].OriginRun
	}
	return ""
}
