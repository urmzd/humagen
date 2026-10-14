// Package github renders agent output as GitHub-flavored Markdown.
//
// It owns presentation only: [FormatReview] and [RenderReview] turn a
// structured, evidence-backed review into a comment body, and [GuideBody]
// writes the pull request guide. Transport (webhooks, API calls, delivery)
// stays in github.com/urmzd/mandatum, whose connector types are the inputs.
//
// Model-supplied text is escaped before it is placed in a table cell or a
// paragraph, so a finding cannot inject markup, links or mentions.
package github
