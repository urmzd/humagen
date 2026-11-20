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
		if !a.Paused && !labelAction(a.Action) && a.Event == kind && a.Action == p.Action && strings.EqualFold(a.Repository, p.Repository.FullName) && c.Routes(a.Agent) && (a.Conclusion == "" || a.Conclusion == p.CheckRun.Conclusion) {
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

// AutoReviewRequest is a pull request to review automatically. It holds only
// bounded identifiers, never text from the webhook.
type AutoReviewRequest struct {
	// Repository is "owner/repo" as GitHub spells it.
	Repository string
	Number     int
	SHA        string
	// Push is a push to the pull request; otherwise it opened, reopened or
	// left draft, and is reviewed whole.
	Push bool
}

// MatchAutoReview reads a signature-verified pull_request payload and reports
// whether it asks for an automatic review: a pull request that
// opens or reopens ready for review, leaves draft, or gets a push, in a
// repository this connection reviews. Drafts and closed pull requests wait.
// The connection's default agent reviews, so a connection without one
// reviews nothing.
func (c Client) MatchAutoReview(kind string, body []byte) (AutoReviewRequest, bool) {
	if kind != "pull_request" || c.Surface != GitHub || !c.Enabled || c.GitHub == nil || !c.AllowsEvent(kind) || c.DefaultAgent == "" || !c.Routes(c.DefaultAgent) {
		return AutoReviewRequest{}, false
	}
	var p struct {
		Action      string `json:"action"`
		Number      int    `json:"number"`
		PullRequest struct {
			Draft bool   `json:"draft"`
			State string `json:"state"`
			Head  struct {
				SHA string `json:"sha"`
			} `json:"head"`
		} `json:"pull_request"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if json.Unmarshal(body, &p) != nil {
		return AutoReviewRequest{}, false
	}
	switch p.Action {
	case "opened", "reopened", "ready_for_review", "synchronize":
	default:
		return AutoReviewRequest{}, false
	}
	repo := p.Repository.FullName
	owner, _, _ := strings.Cut(repo, "/")
	if p.Number <= 0 || p.PullRequest.Draft || p.PullRequest.State != "open" || !fullSHA(p.PullRequest.Head.SHA) || !repoPattern.MatchString(repo) || !c.Serves(owner) {
		return AutoReviewRequest{}, false
	}
	if !c.GitHub.AutoReview.Reviews(repo) {
		return AutoReviewRequest{}, false
	}
	return AutoReviewRequest{Repository: repo, Number: p.Number, SHA: strings.ToLower(p.PullRequest.Head.SHA), Push: p.Action == "synchronize"}, true
}

func fullSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// MentionOnPullRequest reads a signature-verified comment or review payload
// and reports the pull request a person mentioned the app or one of this
// client's agents on. Automatic review resumes there.
func (c Client) MentionOnPullRequest(kind string, body []byte) (repo string, number int, ok bool) {
	if c.Surface != GitHub || !c.Enabled || c.GitHub == nil {
		return "", 0, false
	}
	var p struct {
		Issue *struct {
			Number      int       `json:"number"`
			PullRequest *struct{} `json:"pull_request"`
		} `json:"issue"`
		PullRequest *struct {
			Number int `json:"number"`
		} `json:"pull_request"`
		Comment struct {
			Body string `json:"body"`
		} `json:"comment"`
		Review struct {
			Body string `json:"body"`
		} `json:"review"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		Sender struct {
			Type string `json:"type"`
		} `json:"sender"`
	}
	if json.Unmarshal(body, &p) != nil || p.Sender.Type == "Bot" {
		return "", 0, false
	}
	text := p.Comment.Body
	switch {
	case kind == "issue_comment" && p.Issue != nil && p.Issue.PullRequest != nil:
		number = p.Issue.Number
	case (kind == "pull_request_review_comment" || kind == "pull_request_review") && p.PullRequest != nil:
		number = p.PullRequest.Number
		if kind == "pull_request_review" {
			text = p.Review.Body
		}
	default:
		return "", 0, false
	}
	repo = p.Repository.FullName
	owner, _, _ := strings.Cut(repo, "/")
	if number <= 0 || !repoPattern.MatchString(repo) || !c.Serves(owner) {
		return "", 0, false
	}
	for _, name := range c.mentionable() {
		if mentions(text, name) {
			return repo, number, true
		}
	}
	return "", 0, false
}

// mentionable is what a comment may @mention to address this client: the
// app's login, and the agents and bots it routes to.
func (c Client) mentionable() []string {
	names := []string{strings.TrimSuffix(c.GitHub.Login, "[bot]"), c.DefaultAgent}
	for _, a := range c.Agents {
		if a != "*" {
			names = append(names, a)
		}
	}
	for login, agent := range c.Bots {
		names = append(names, strings.TrimSuffix(login, "[bot]"), agent)
	}
	return names
}

// mentions reports whether text @mentions name as a whole word: not part of
// a longer name or an email address. A sentence may end right after it.
func mentions(text, name string) bool {
	if name == "" {
		return false
	}
	lower, at := strings.ToLower(text), "@"+strings.ToLower(name)
	for from := 0; ; {
		i := strings.Index(lower[from:], at)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(at)
		before := i == 0 || !nameByte(lower[i-1]) && lower[i-1] != '.' && lower[i-1] != '@'
		rest := lower[end:]
		after := rest == "" || !nameByte(rest[0]) && (rest[0] != '.' || len(rest) == 1 || !nameByte(rest[1]))
		if before && after {
			return true
		}
		from = end
	}
}

func nameByte(b byte) bool {
	return b == '-' || b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z'
}
