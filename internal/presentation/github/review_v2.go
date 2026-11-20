package github

import (
	"fmt"
	"regexp"
	"strings"

	transport "github.com/urmzd/saige/flow/connectors/github"
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

// foundBy is empty or the agent that found a finding, with its session label:
// security-reviewer or security-reviewer#a.
var foundBy = regexp.MustCompile(`^([a-z0-9][a-z0-9._-]{0,63}(#[A-Za-z0-9._-]{1,32})?)?$`)

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
		if !foundBy.MatchString(f.FoundBy) {
			return fmt.Errorf("github: review v2 found_by names an agent, with an optional #label, in at most 100 characters")
		}
	}
	return nil
}

// NativeReview prepares a real pull request review, preserving invalid anchors
// in the body. The transport checks every anchor against the actual diff.
// Agents never approve: an approve verdict is posted as a comment,
// and a person approves and merges.
func NativeReview(report ReviewReport, files []transport.ContextFile) (transport.Review, error) {
	r, err := nativeReview(report)
	if err != nil {
		return transport.Review{}, err
	}
	return transport.PrepareReview(r, files)
}

// RecoveredReview reproduces a review already posted, so a retried delivery
// matches it instead of posting another. disposition is the posted event. An
// approval posted before agents stopped approving is reproduced as one; the
// transport prepares it and never submits a new one.
func RecoveredReview(report ReviewReport, files []transport.ContextFile, disposition string) (transport.Review, error) {
	r, err := nativeReview(report)
	if err != nil {
		return transport.Review{}, err
	}
	if disposition == "APPROVE" && report.Verdict == "approve" {
		r.Event = disposition
		return r, nil
	}
	return transport.PrepareReview(r, files)
}

func nativeReview(report ReviewReport) (transport.Review, error) {
	body, err := FormatReview(report)
	if err != nil {
		return transport.Review{}, err
	}
	if report.Format != ReviewFormatV2 {
		return transport.Review{}, fmt.Errorf("github: native reviews require %s", ReviewFormatV2)
	}
	r := transport.Review{CommitID: report.Scope.Commit, Body: body, Event: "COMMENT"}
	if report.Verdict == "request_changes" {
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
		title := "**" + reviewText(f.Title) + "**"
		if f.FoundBy != "" {
			title += "\n\nFound by " + reviewText(f.FoundBy)
		}
		text := fmt.Sprintf("%s\n\n%s\n\n<details><summary>Evidence</summary>\n\n%s\n</details>", title, reviewText(f.Suggestion), evidence.String())
		r.Comments = append(r.Comments, transport.ReviewComment{FindingID: f.ID, Path: l.Path, Body: text, Line: l.Line, Side: side, StartLine: l.StartLine, StartSide: side, Change: f.Change})
	}
	return r, nil
}
