package clients

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// MatchAutomations reads only a signature-verified payload. Rules cannot grant
// repository access, credentials, or routing beyond their installation.
func (c Client) MatchAutomations(kind string, body []byte) []GitHubAutomation {
	if c.Surface != GitHub || !c.Enabled || c.GitHub == nil || !c.AllowsEvent(kind) {
		return nil
	}
	type author struct {
		Name     string `json:"name"`
		Username string `json:"username"`
		Email    string `json:"email"`
	}
	type commit struct {
		Author author `json:"author"`
	}
	var p struct {
		Action     string `json:"action"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		Sender struct {
			Login string `json:"login"`
		} `json:"sender"`
		Label struct {
			Name string `json:"name"`
		} `json:"label"`
		HeadCommit *commit `json:"head_commit"`
		CheckRun   struct {
			Conclusion string `json:"conclusion"`
			CheckSuite struct {
				HeadCommit *commit `json:"head_commit"`
			} `json:"check_suite"`
		} `json:"check_run"`
	}
	if json.Unmarshal(body, &p) != nil {
		return nil
	}
	own := func(login string) bool {
		if login == "" {
			return false
		}
		if strings.EqualFold(login, c.GitHub.Login) || strings.Contains(strings.ToLower(login), "[bot]") {
			return true
		}
		for bot := range c.Bots {
			if strings.EqualFold(login, bot) {
				return true
			}
		}
		return false
	}
	if strings.EqualFold(p.Sender.Login, c.GitHub.Login) && c.GitHub.Login != "" {
		return nil
	}
	for bot := range c.Bots {
		if strings.EqualFold(bot, p.Sender.Login) {
			return nil
		}
	}
	head := p.HeadCommit
	if kind == "check_run" {
		head = p.CheckRun.CheckSuite.HeadCommit
	}
	if kind == "push" || kind == "check_run" {
		// Unknown authors fail closed: an incomplete payload cannot bypass the
		// self-commit guard. GitHub supplies head_commit for push and check suites.
		if head == nil || (head.Author.Username == "" && head.Author.Name == "" && head.Author.Email == "") {
			return nil
		}
		if own(head.Author.Username) || own(head.Author.Name) || strings.Contains(strings.ToLower(head.Author.Email), "[bot]") {
			return nil
		}
	}
	var out []GitHubAutomation
	for _, a := range c.GitHub.Automations {
		if !a.Paused && a.Event == kind && a.Action == p.Action && strings.EqualFold(a.Repository, p.Repository.FullName) && c.Routes(a.Agent) && (a.Conclusion == "" || a.Conclusion == p.CheckRun.Conclusion) && (a.Label == "" || a.Label == p.Label.Name) {
			out = append(out, a)
		}
	}
	return out
}

// AutomationContext keeps only bounded GitHub identifiers, never arbitrary
// webhook text, in the prompt that accompanies a rule's instructions.
func AutomationContext(body []byte) string {
	var p struct {
		Number int    `json:"number"`
		After  string `json:"after"`
		Issue  struct {
			Number int `json:"number"`
		} `json:"issue"`
		CheckRun struct {
			HeadSHA string `json:"head_sha"`
		} `json:"check_run"`
	}
	if json.Unmarshal(body, &p) != nil {
		return ""
	}
	if p.Number == 0 {
		p.Number = p.Issue.Number
	}
	text := ""
	if p.Number > 0 {
		text += fmt.Sprintf("\nIssue or pull request: #%d", p.Number)
	}
	sha := p.After
	if p.CheckRun.HeadSHA != "" {
		sha = p.CheckRun.HeadSHA
	}
	if len(sha) == 40 || len(sha) == 64 {
		if _, err := hex.DecodeString(sha); err == nil {
			text += "\nHead SHA: " + sha
		}
	}
	return text
}

// AutomationThread is the pull request or issue a signed payload is about, as
// an address path under its repository ("pull/7", "issues/42"), so an
// automation can answer where it was triggered. A push, or a check run with no
// pull request, has none.
func AutomationThread(kind string, body []byte) (string, bool) {
	var p struct {
		PullRequest *struct {
			Number int `json:"number"`
		} `json:"pull_request"`
		Issue *struct {
			Number int `json:"number"`
		} `json:"issue"`
		CheckRun struct {
			PullRequests []struct {
				Number int `json:"number"`
			} `json:"pull_requests"`
		} `json:"check_run"`
	}
	if json.Unmarshal(body, &p) != nil {
		return "", false
	}
	switch {
	case kind == "pull_request" && p.PullRequest != nil && p.PullRequest.Number > 0:
		return "pull/" + strconv.Itoa(p.PullRequest.Number), true
	case kind == "issues" && p.Issue != nil && p.Issue.Number > 0:
		return "issues/" + strconv.Itoa(p.Issue.Number), true
	case kind == "check_run" && len(p.CheckRun.PullRequests) > 0 && p.CheckRun.PullRequests[0].Number > 0:
		return "pull/" + strconv.Itoa(p.CheckRun.PullRequests[0].Number), true
	}
	return "", false
}
