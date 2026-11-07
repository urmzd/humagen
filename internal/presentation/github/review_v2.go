package github

import (
	"fmt"
	"regexp"
	"strings"
)

// ReviewScope pins the review to the exact source revision it inspected.
type ReviewScope struct {
	Commit string `json:"commit"`
}

// ReviewLocation identifies the changed line or range a finding concerns.
type ReviewLocation struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Side      string `json:"side,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
}

var fullCommit = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

func validateV2(r ReviewReport) error {
	if strings.TrimSpace(r.Title) == "" || len(r.Title) > 200 {
		return fmt.Errorf("github: review v2 needs a title of at most 200 characters")
	}
	switch r.Verdict {
	case "approve", "comment", "request_changes":
	default:
		return fmt.Errorf("github: review v2 has an invalid verdict")
	}
	if r.Scope == nil || !fullCommit.MatchString(r.Scope.Commit) {
		return fmt.Errorf("github: review v2 needs scope.commit pinned to a full SHA")
	}
	if len(r.Findings) > 100 {
		return fmt.Errorf("github: review has more than 100 findings")
	}
	seen := map[string]bool{}
	for _, f := range r.Findings {
		if strings.TrimSpace(f.ID) == "" || len(f.ID) > 100 || seen[f.ID] {
			return fmt.Errorf("github: review v2 findings need unique ids")
		}
		seen[f.ID] = true
	}
	return nil
}
