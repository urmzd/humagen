package github

import (
	"fmt"
	"net/url"
	"strings"

	transport "github.com/urmzd/saige/flow/connectors/github"
)

// The layout a native review was posted in before reviews read as prose,
// kept byte for byte so a review posted then and not yet receipted is
// recovered, not refused. Recovery compares a digest of the review it
// reproduces with the one posted; only this layout reproduces it. Nothing
// new is posted in it.

// LegacyRecoveredReview reproduces a review posted in the earlier layout:
// its table body and its line comments, without the session link. The
// report is not checked against the current bounds, since it was posted
// under the earlier ones.
func LegacyRecoveredReview(report ReviewReport, files []transport.ContextFile, disposition string) (transport.Review, error) {
	if report.Format != ReviewFormatV2 || report.Scope == nil {
		return transport.Review{}, fmt.Errorf("github: native reviews require %s", ReviewFormatV2)
	}
	r := transport.Review{CommitID: report.Scope.Commit, Body: legacyBody(report), Event: "COMMENT"}
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
			fmt.Fprintf(&evidence, "- %s (%s)", legacyText(e.Observation), legacyPointer(e.Source))
			if e.Step != "" {
				fmt.Fprintf(&evidence, " step %s", legacyText(e.Step))
			}
			if e.Session != "" {
				fmt.Fprintf(&evidence, " session %s", legacyText(e.Session))
			}
			if e.Step == "" && e.Session == "" {
				evidence.WriteString(" (unverified)")
			}
			evidence.WriteString("\n")
		}
		title := "**" + legacyText(f.Title) + "**"
		if f.FoundBy != "" {
			title += "\n\nFound by " + legacyText(f.FoundBy)
		}
		text := fmt.Sprintf("%s\n\n%s\n\n<details><summary>Evidence</summary>\n\n%s\n</details>", title, legacyText(f.Suggestion), evidence.String())
		r.Comments = append(r.Comments, transport.ReviewComment{FindingID: f.ID, Path: l.Path, Body: text, Line: l.Line, Side: side, StartLine: l.StartLine, StartSide: side, Change: f.Change})
	}
	if disposition == "APPROVE" && report.Verdict == "approve" {
		r.Event = disposition
		return r, nil
	}
	return transport.PrepareReview(r, files)
}

func legacyBody(report ReviewReport) string {
	counts := map[string]int{"error": 0, "warning": 0, "info": 0}
	for _, f := range report.Findings {
		counts[f.Severity]++
	}
	var b strings.Builder
	if report.Title != "" {
		fmt.Fprintf(&b, "## %s\n\n", legacyText(report.Title))
	}
	errorLabel, warningLabel := "errors", "warnings"
	if counts["error"] == 1 {
		errorLabel = "error"
	}
	if counts["warning"] == 1 {
		warningLabel = "warning"
	}
	fmt.Fprintf(&b, "🔴 **%d %s** · 🟡 **%d %s** · 🔵 **%d info**\n\n## Summary\n\n%s\n", counts["error"], errorLabel, counts["warning"], warningLabel, counts["info"], legacyText(report.Summary))
	if len(report.Comments) > 0 {
		b.WriteString("\n## Comments\n\n")
		for _, comment := range report.Comments {
			fmt.Fprintf(&b, "- %s\n", legacyText(comment))
		}
	}
	if len(report.Findings) > 0 {
		b.WriteString("\n## Findings\n\n| Level | Finding | Suggestion | Evidence | Follow-up prompt |\n| :---: | :--- | :--- | :--- | :--- |\n")
		icons := map[string]string{"error": "🔴 Error", "warning": "🟡 Warning", "info": "🔵 Info"}
		for _, f := range report.Findings {
			var evidence []string
			for _, e := range f.Evidence {
				pointer := legacyPointer(e.Source)
				if e.Step != "" {
					pointer += " step " + legacyText(e.Step)
				}
				if e.Session != "" {
					pointer += " session " + legacyText(e.Session)
				}
				if report.Format == ReviewFormatV2 && e.Step == "" && e.Session == "" {
					pointer += "; unverified"
				}
				evidence = append(evidence, legacyText(e.Observation)+" ("+pointer+")")
			}
			title := legacyText(f.Title)
			if f.FoundBy != "" {
				title += "<br>Found by " + legacyText(f.FoundBy)
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", icons[f.Severity], title, legacyText(f.Suggestion), strings.Join(evidence, "<br><br>"), legacyText(f.Prompt))
		}
	} else {
		b.WriteString("\nNo findings reported. See the summary and comments for review scope and limitations.\n")
	}
	if len(report.Related) > 0 {
		b.WriteString("\n## Related concepts\n\n")
		for _, link := range report.Related {
			fmt.Fprintf(&b, "- %s: %s\n", legacyText(link.Title), legacyPointer(link.URL))
		}
	}
	b.WriteString("\n---\n\nReact with 👍 or 👎 on this comment. Reply and tag the agent to continue the review.\n")
	return b.String()
}

func legacyText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "|", "&#124;", "\\", "&#92;", "`", "&#96;", "*", "&#42;", "_", "&#95;", "[", "&#91;", "]", "&#93;", "@", "&#64;").Replace(s)
}

func legacyPointer(source string) string {
	u, err := url.Parse(source)
	if err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil {
		dest := strings.NewReplacer("(", "%28", ")", "%29", "<", "%3C", ">", "%3E", "|", "%7C", " ", "%20", "\n", "%0A", "\r", "%0D").Replace(u.String())
		return "[source](" + dest + ")"
	}
	return legacyText(source)
}
