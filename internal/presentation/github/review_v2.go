package github

import (
	"fmt"
	"regexp"
	"strings"

	transport "github.com/urmzd/orchagent/flow/connectors/github"
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

// NativeReview prepares a real pull request review, preserving invalid anchors
// in the body. The transport checks every anchor against the actual diff and
// degrades approval unless project policy explicitly allows it.
func NativeReview(report ReviewReport, files []transport.ContextFile, allowApprove bool) (transport.Review, error) {
	body, err := FormatReview(report)
	if err != nil {
		return transport.Review{}, err
	}
	if report.Format != ReviewFormatV2 {
		return transport.Review{}, fmt.Errorf("github: native reviews require %s", ReviewFormatV2)
	}
	r := transport.Review{CommitID: report.Scope.Commit, Body: body, Event: "COMMENT"}
	switch report.Verdict {
	case "approve":
		if allowApprove {
			r.Event = "APPROVE"
		} else {
			r.Body += "\n\nApproval was not submitted because project policy permits comments only."
		}
	case "request_changes":
		r.Event = "REQUEST_CHANGES"
	}
	for _, f := range report.Findings {
		if f.Location == nil {
			continue
		}
		l := f.Location
		side := l.Side
		if side == "" {
			side = "RIGHT"
		}
		var evidence strings.Builder
		for _, e := range f.Evidence {
			fmt.Fprintf(&evidence, "- %s (%s)", reviewText(e.Observation), reviewPointer(e.Source))
			if e.Step != "" {
				fmt.Fprintf(&evidence, " step %s", reviewText(e.Step))
			}
			if e.Session != "" {
				fmt.Fprintf(&evidence, " session %s", reviewText(e.Session))
			}
			if e.Step == "" && e.Session == "" {
				evidence.WriteString(" (unverified)")
			}
			evidence.WriteString("\n")
		}
		text := fmt.Sprintf("**%s**\n\n%s\n\n<details><summary>Evidence</summary>\n\n%s\n</details>", reviewText(f.Title), reviewText(f.Suggestion), evidence.String())
		r.Comments = append(r.Comments, transport.ReviewComment{FindingID: f.ID, Path: l.Path, Body: text, Line: l.Line, Side: side, StartLine: l.StartLine, StartSide: side, Change: f.Change})
	}
	return transport.PrepareReview(r, files, allowApprove)
}
