package clients

import (
	"encoding/json"
	"slices"
	"strings"
)

// writeAccess are the author associations GitHub gives people who may write
// to a repository: its owner, a member of its organization, and an outside
// collaborator.
var writeAccess = []string{"OWNER", "MEMBER", "COLLABORATOR"}

// GitHubAsker reads who asks through a signature-verified GitHub delivery,
// and whether they may: a comment's or a review's author, a pull request's
// or an issue's author, or who requested a review. They may when GitHub says
// they can write to the repository (owner, member or collaborator) or the
// client maps them to a person, and, when the client names askers, they are
// one of them. A delivery nobody asks through (a push, a check run) is
// allowed. why says why someone may not.
func (c Client) GitHubAsker(kind string, body []byte) (login string, allowed bool, why string) {
	if c.Surface != GitHub || c.GitHub == nil {
		return "", true, ""
	}
	type author struct {
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		Association string `json:"author_association"`
	}
	var p struct {
		Action string `json:"action"`
		Sender struct {
			Login string `json:"login"`
		} `json:"sender"`
		Comment     author `json:"comment"`
		Review      author `json:"review"`
		Issue       author `json:"issue"`
		PullRequest author `json:"pull_request"`
	}
	if json.Unmarshal(body, &p) != nil {
		return "", false, "the payload could not be read"
	}
	var who author
	switch kind {
	case "issue_comment", "pull_request_review_comment":
		who = p.Comment
	case "pull_request_review":
		who = p.Review
	case "issues":
		who = p.Issue
	case "pull_request":
		if p.Action == "review_requested" {
			// Only people with write access may request a review.
			who.User.Login, who.Association = p.Sender.Login, "COLLABORATOR"
			break
		}
		who = p.PullRequest
	default:
		return "", true, ""
	}
	login = who.User.Login
	if login == "" {
		return "", false, "it names nobody who asks"
	}
	mapped := c.GitHub.People[login] != ""
	if !mapped && !slices.Contains(writeAccess, who.Association) {
		return login, false, login + " has no write access to the repository (" + strings.ToLower(cmpOr(who.Association, "none")) + ")"
	}
	if len(c.GitHub.Askers) > 0 && !slices.ContainsFunc(c.GitHub.Askers, func(a string) bool { return strings.EqualFold(a, login) }) {
		return login, false, login + " is not one of the client's askers"
	}
	return login, true, ""
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
