package github

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	transport "github.com/urmzd/saige/flow/connectors/github"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/golden")

const goldenHead = "3f9c2a7e1b4d5c6a8e9f0a1b2c3d4e5f6a7b8c9d"

var goldenTarget = Target{Repo: "https://github.com/acme/webhooks", Head: goldenHead}

const goldenSession = "https://humagen.example/sessions/s_4k2"

// goldenFiles is the pull request's diff: retry.go lines 36-48 and
// client.go lines 10-15 on the new side.
func goldenFiles() []transport.ContextFile {
	return []transport.ContextFile{
		{Filename: "internal/retry/retry.go", Patch: "@@ -36,8 +36,13 @@ func Do(ctx context.Context, op func() error) error {\n \tvar lastErr error\n \tfor attempt := 0; attempt < max; attempt++ {\n-\t\tif err := op(); err == nil {\n+\t\terr := op()\n+\t\tif err == nil {\n \t\t\treturn nil\n \t\t}\n+\t\tlastErr = err\n+\t\ttime.Sleep(backoff(attempt))\n \t}\n-\treturn lastErr\n+\treturn nil\n }\n+\n+func backoff(n int) time.Duration { return base << n }\n"},
		{Filename: "internal/webhooks/client.go", Patch: "@@ -10,3 +10,6 @@ type Client struct {\n \thttp   *http.Client\n \tsecret string\n+\tlog    *slog.Logger\n+\tlimit  int\n+\ttoken  string\n }\n"},
	}
}

func permalink(path string, line int) string {
	return "https://github.com/acme/webhooks/blob/" + goldenHead + "/" + path + "#L" + itoa(line)
}

func itoa(n int) string { return strconv.Itoa(n) }

// goldenFindings are what a model writes for the retry change, most urgent
// first. The model writes no commit; the host pins it.
func goldenFindings() []ReviewFinding {
	returnErr := "\treturn lastErr"
	return []ReviewFinding{
		{ID: "f1", Severity: "error", Title: "Retry loop drops the final error", Location: &ReviewLocation{Path: "internal/retry/retry.go", Line: 45}, Change: &returnErr,
			Why:        "After the last failed attempt `Do` returns `nil`, so callers treat a failed delivery as a success.",
			Suggestion: "Return `lastErr` when the loop ends.",
			Evidence:   []ReviewEvidence{{Observation: "`Do` returns `nil` after the loop.", Source: permalink("internal/retry/retry.go", 45), Step: "tc_9f2"}},
			Prompt:     "Return lastErr from Do and add a test for three failures.", FoundBy: "code-reviewer"},
		{ID: "f2", Severity: "warning", Title: "Backoff ignores the context deadline", Location: &ReviewLocation{Path: "internal/retry/retry.go", Line: 43},
			Why:        "`time.Sleep` keeps waiting after the caller cancels, so a shutdown can hang for the whole backoff.",
			Suggestion: "Wait on a timer and `ctx.Done()` in a `select`.",
			Evidence:   []ReviewEvidence{{Observation: "The wait is a bare `time.Sleep`.", Source: permalink("internal/retry/retry.go", 43), Step: "tc_a11"}},
			Prompt:     "Make the backoff wait honor ctx.", FoundBy: "qa-reviewer"},
		{ID: "f3", Severity: "warning", Title: "The webhook token reaches the debug log", Location: &ReviewLocation{Path: "internal/webhooks/client.go", Line: 14},
			Why:        "The client logs itself with `slog.Any`, which prints every field, the new `token` among them.",
			Suggestion: "Give `Client` a `LogValue` method that leaves the token out.",
			Evidence:   []ReviewEvidence{{Observation: "`Deliver` logs the whole client at debug level.", Source: permalink("internal/webhooks/client.go", 14), Session: "s_7c1"}},
			Prompt:     "Redact the token in client logs.", FoundBy: "security-reviewer#a"},
		{ID: "f4", Severity: "info", Title: "Backoff has no upper bound", Location: &ReviewLocation{Path: "internal/retry/retry.go", Line: 48},
			Suggestion: "Cap the delay, for example at five seconds.",
			Evidence:   []ReviewEvidence{{Observation: "`base << n` doubles without a cap.", Source: permalink("internal/retry/retry.go", 48), Step: "tc_b20"}},
			Prompt:     "Cap the backoff.", FoundBy: "code-reviewer"},
		{ID: "f5", Severity: "info", Title: "The limit field is never read", Location: &ReviewLocation{Path: "internal/webhooks/client.go", Line: 13},
			Suggestion: "Use `limit` in `Deliver` or remove it.",
			Evidence:   []ReviewEvidence{{Observation: "Nothing reads `Client.limit`.", Source: "internal/webhooks/client.go:13", Step: "tc_b31"}},
			Prompt:     "Use or remove Client.limit.", FoundBy: "architect-reviewer"},
	}
}

func goldenReport(verdict, summary string, findings ...ReviewFinding) ReviewReport {
	r := ReviewReport{Format: ReviewFormatV2, Title: "Retry changes", Verdict: verdict, Summary: summary, Findings: findings}
	r.Pin(goldenHead)
	return r
}

// goldenReview is a native review as GitHub shows it: its event, its body
// and each line comment, with the session link the host adds.
func goldenReview(t *testing.T, report ReviewReport) string {
	t.Helper()
	review, err := NativeReview(report, goldenFiles(), goldenTarget)
	if err != nil {
		t.Fatal(err)
	}
	review.Body += SessionFooter(goldenSession)
	var b strings.Builder
	b.WriteString("<!-- event: " + review.Event + " -->\n\n" + review.Body + "\n")
	for _, c := range review.Comments {
		b.WriteString("\n<!-- line comment on " + c.Path + ":" + itoa(c.Line) + " -->\n\n" + c.Body + "\n")
	}
	return b.String()
}

func TestGoldenGitHubOutput(t *testing.T) {
	findings := goldenFindings()
	docs := ReviewFinding{ID: "f1", Severity: "warning", Title: "The docs still say two retries",
		Why:        "`docs/reference/webhooks.md` says a delivery is retried twice, and this change makes it three times.",
		Suggestion: "Update the retry count and the backoff in that page.",
		Evidence:   []ReviewEvidence{{Observation: "The page says deliveries retry twice.", Source: "https://github.com/acme/webhooks/blob/" + goldenHead + "/docs/reference/webhooks.md#L31", Step: "tc_c02"}},
		Prompt:     "Update the retry docs.", FoundBy: "code-reviewer"}
	approved := findings[3]
	for name, render := range map[string]func() string{
		"reply": func() string {
			v := transport.View{Agent: "code-reviewer", Status: transport.StatusDone, Channel: "github://acme/webhooks/pull/212?head_sha=" + goldenHead,
				Text: "Yes. `Deliver` retries three times, with a backoff that doubles from 200 ms. The limit is set in [`internal/retry/retry.go:18`](" + permalink("internal/retry/retry.go", 18) + ").\n\nTo allow more attempts, pass `WithMaxAttempts` when you build the client:\n\n```go\nclient := webhooks.New(webhooks.WithMaxAttempts(5))\n```"}
			return RenderComment(v, goldenSession) + "\n"
		},
		"review-one-finding": func() string {
			return goldenReview(t, goldenReport("request_changes", "This change adds retries with backoff to webhook delivery. The loop now loses the final error, so a failed delivery looks like a success.", findings[0]))
		},
		"review-five-findings": func() string {
			return goldenReview(t, goldenReport("request_changes", "This change adds retries with backoff to webhook delivery and a token to the client. The loop loses the final error, which blocks the merge. The backoff and the debug log need a look too.", findings...))
		},
		"review-approve": func() string {
			return goldenReview(t, goldenReport("approve", "This change adds retries with backoff to webhook delivery. Errors and cancellation are handled, and the new tests cover three failures in a row.", approved))
		},
		"review-no-location": func() string {
			return goldenReview(t, goldenReport("comment", "This change makes webhook delivery retry three times. The code is fine; one page of the docs is now out of date.", docs))
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := render()
			assertHumanVoice(t, got)
			path := filepath.Join("testdata", "golden", name+".md")
			if *update {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v: run go test -run TestGoldenGitHubOutput -update", err)
			}
			if got != string(want) {
				t.Fatalf("%s changed; run with -update and read the diff\ngot:\n%s", path, got)
			}
		})
	}
}

// hidden is every HTML comment: machine markers a person never sees.
var hidden = regexp.MustCompile(`(?s)<!--.*?-->`)

// internalName is an id a person should not see: a run, a session, a tool
// step, a tool name, or the report's own JSON.
var internalName = regexp.MustCompile(`\b(?:s|run|tc|step)(?:_|&#95;)[A-Za-z0-9]+\b|github_read_context|github_read_file|file_read|file_list|session_read|"format"\s*:|\{"|humagen\.review\.v2|humagen\.github\.review|found_by|#[a-z]\b`)

// fillers open an answer without saying anything.
var fillers = []string{"great question", "i'd be happy to", "i would be happy to", "happy to help", "certainly!", "absolutely!", "let me know if", "hope this helps", "as an ai"}

// assertHumanVoice checks what a person sees reads as a colleague wrote
// it: no em dash, no filler, no internal ids, tool names or JSON, and no
// wall of emoji. Links (the session's address) and hidden markers are left
// out of the check.
func assertHumanVoice(t *testing.T, body string) {
	t.Helper()
	visible := hidden.ReplaceAllString(body, "")
	visible = regexp.MustCompile(`\]\([^)]*\)`).ReplaceAllString(visible, "]")
	if strings.Contains(visible, "—") {
		t.Errorf("an em dash is visible:\n%s", visible)
	}
	lower := strings.ToLower(visible)
	for _, f := range fillers {
		if strings.Contains(lower, f) {
			t.Errorf("filler %q is visible", f)
		}
	}
	if m := internalName.FindString(visible); m != "" {
		t.Errorf("an internal name %q is visible:\n%s", m, visible)
	}
	for _, emoji := range []string{"🔴", "🟡", "🔵", "⚠️", "👍", "👎"} {
		if strings.Contains(visible, emoji) {
			t.Errorf("emoji %s is visible", emoji)
		}
	}
	if strings.Contains(visible, "## ") {
		t.Errorf("a heading is visible:\n%s", visible)
	}
}
