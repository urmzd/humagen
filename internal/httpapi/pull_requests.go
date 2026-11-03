package httpapi

import (
	"context"
	"encoding/json"
	"github.com/urmzd/humagen/internal/platform"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	presentation "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/integrations/github"
)

type reviewedPull struct {
	Run       string    `json:"run"`
	Thread    string    `json:"thread"`
	Repo      string    `json:"repo"`
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Agent     string    `json:"agent"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	Errors    int       `json:"errors"`
	Warnings  int       `json:"warnings"`
	Infos     int       `json:"infos"`
	CI        string    `json:"ci"`
}

var pullLink = regexp.MustCompile(`https://github\.com/([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)/pull/([1-9][0-9]*)(?:\b)`)

// listReviewedPulls reads a bounded recent window and checks visibility before
// inspecting any answer. Findings counts come from the validated report.
func (s *Server) listReviewedPulls(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	q := r.URL.Query()
	if len(q.Get("q")) > 200 || len(q.Get("repo")) > 200 || len(q.Get("agent")) > 160 {
		fail(w, 400, "filter is too long")
		return
	}
	status := q.Get("status")
	if status != "" && status != "running" && status != "waiting" && status != "done" {
		fail(w, 400, "invalid status")
		return
	}
	out := []reviewedPull{}
	seer := s.P.Seer(who)
	cutoff := time.Now().AddDate(0, 0, -30)
	seen := map[string]bool{}
	recent := s.P.Runs.List(who.Tenant, 1000)
	latest := map[string]string{}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	metadata := map[string]platform.GitHubPull{}
	for _, run := range recent {
		if !run.CreatedAt.Before(cutoff) && seer.Run(run) {
			if _, ok := latest[run.Thread]; !ok {
				latest[run.Thread] = string(run.Status)
			}
		}
	}
	for _, raw := range recent {
		if raw.CreatedAt.Before(cutoff) || !seer.Run(raw) {
			continue
		}
		run := s.P.ShownRunAs(who, raw)
		var report presentation.ReviewReport
		if json.Unmarshal([]byte(presentation.ReviewJSON(run.Answer)), &report) != nil {
			continue
		}
		if _, err := presentation.FormatReview(report); err != nil {
			continue
		}
		link := pullLink.FindStringSubmatch(report.Summary + " " + run.Text)
		if link == nil {
			for _, rel := range report.Related {
				link = pullLink.FindStringSubmatch(rel.URL)
				if link != nil {
					break
				}
			}
		}
		if link == nil {
			continue
		}
		number, err := strconv.Atoi(link[2])
		if err != nil {
			continue
		}
		state := "running"
		switch latest[run.Thread] {
		case "completed":
			state = "waiting"
		case "failed", "stopped":
			state = "done"
		}
		item := reviewedPull{Run: run.ID, Thread: run.Thread, Repo: link[1], Number: number, Title: "Pull request #" + link[2], CI: "unknown", Agent: run.Agent, Status: state, CreatedAt: run.CreatedAt}
		for _, f := range report.Findings {
			switch f.Severity {
			case "error":
				item.Errors++
			case "warning":
				item.Warnings++
			case "info":
				item.Infos++
			}
		}
		key := item.Repo + "#" + link[2] + "/" + item.Agent
		if seen[key] {
			continue
		}
		seen[key] = true

		// Metadata comes only from the tenant's repository-authorized client.
		// A missing integration keeps the local review available with CI unknown.
		if q.Get("repo") != "" && item.Repo != q.Get("repo") || q.Get("agent") != "" && item.Agent != q.Get("agent") {
			continue
		}
		pullKey := item.Repo + "#" + link[2]
		detail, found := metadata[pullKey]
		if !found {
			detail, _ = s.P.GitHubPull(ctx, who.Tenant, item.Repo, item.Number)
			metadata[pullKey] = detail
		}
		if detail.Subject.Title != "" {
			item.Title = detail.Subject.Title
		}
		if detail.Subject.State == "closed" {
			item.Status = "done"
			state = "done"
		}
		item.CI = reviewedPullCI(detail.Workflows, detail.Subject.Head.SHA)
		if status != "" && state != status || q.Get("repo") != "" && item.Repo != q.Get("repo") || q.Get("agent") != "" && item.Agent != q.Get("agent") {
			continue
		}
		if !strings.Contains(strings.ToLower(item.Title+" "+report.Summary+" "+item.Repo+" #"+link[2]), strings.ToLower(q.Get("q"))) {
			continue
		}
		out = append(out, item)
		if len(out) == 100 {
			break
		}
	}
	ok(w, http.StatusOK, map[string]any{"pulls": out})
}

// Re-runs of one workflow supersede older runs on the same reviewed commit.
func reviewedPullCI(workflows []github.WorkflowRun, sha string) string {
	latest := map[string]github.WorkflowRun{}
	for _, job := range workflows {
		if sha == "" || job.HeadSHA != sha {
			continue
		}
		previous, ok := latest[job.Name]
		if !ok || job.ID > previous.ID || job.ID == previous.ID && job.Attempt > previous.Attempt {
			latest[job.Name] = job
		}
	}
	result := "unknown"
	for _, job := range latest {
		if job.Conclusion == "failure" || job.Conclusion == "timed_out" || job.Conclusion == "action_required" {
			return "failure"
		}
		if job.Status != "completed" {
			result = "pending"
			continue
		}
		if result == "pending" {
			continue
		}
		if job.Conclusion == "cancelled" {
			result = "cancelled"
			continue
		}
		if result != "cancelled" {
			result = "success"
		}
	}
	return result
}
