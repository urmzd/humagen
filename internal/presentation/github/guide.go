package github

import (
	"fmt"
	"strings"

	transport "github.com/urmzd/fiber/pkg/connectors/github"
)

// GuideBody renders the pull request guide for one page of evidence: the
// intent, the changed files, and how to work with the agent. The agent name,
// title, body and file names are escaped; login is the bot's GitHub login and
// is mentioned as given. Set mutations when the bot accepts /fix and /check.
func GuideBody(evidence transport.Context, agent, login string, mutations bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## PR guide\n\n**%s**\n\nCurrent commit: `%s`\n\n", reviewText(evidence.Subject.Title), evidence.Subject.Head.SHA)
	if evidence.Subject.Body != "" {
		fmt.Fprintf(&b, "### Intent\n\n%s\n\n", reviewText(clamp(evidence.Subject.Body, 1600)))
	}
	b.WriteString("### Changes to inspect\n\n")
	if len(evidence.Files) == 0 {
		b.WriteString("No file patches were returned on this page. Read the PR diff before making a review claim.\n")
	} else {
		b.WriteString("| File | Change | Evidence available |\n| :--- | :---: | :--- |\n")
		for _, file := range evidence.Files {
			available := "Patch available"
			if file.Patch == "" {
				available = "Patch unavailable; inspect the file"
			}
			fmt.Fprintf(&b, "| %s | %s | %s |\n", reviewText(clamp(file.Filename, 400)), reviewText(file.Status), available)
		}
	}
	if evidence.NextPage != 0 {
		b.WriteString("\nMore context is available. This guide lists the first page; the review must read the remaining pages.\n")
	}
	fmt.Fprintf(&b, "\n### Working with %s\n\nTag @%s in a PR comment or review thread to ask a question, request a review, or discuss a suggested fix. Each review includes severity counts, evidence and follow-up prompts.\n\nAutomatic reviews continue for new commits while the configured auto label remains. Removing it stops automatic commit reviews.\n\nReviews and checks identify the commit they cover. A completed agent run does not mean tests passed. React with 👍 or 👎 on the review, or reply and tag the agent.\n", reviewText(agent), login)
	if mutations {
		b.WriteString("\nRepository writers can follow the bot mention with `/fix <requested change>` or `/check <workflow-run-id>`. Fixes create a commit; check requests rerun an existing workflow on its exact commit. The agent reports actual results or a pending state.\n")
	}
	return b.String()
}
