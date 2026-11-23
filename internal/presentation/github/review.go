package github

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	transport "github.com/urmzd/saige/flow/connectors/github"

	"github.com/urmzd/humagen/internal/legacy"
)

// ReviewFormat identifies a structured final answer, distinct from ordinary text.
const ReviewFormat = "humagen.github.review.v1"

// ReviewFormatV2 adds native review locations, suggestions and provenance.
const ReviewFormatV2 = "humagen.review.v2"

// IsReviewFormat reports whether format names a review: ReviewFormat, or the
// id it had before the rename, which agents' instructions written then still
// ask for.
func IsReviewFormat(format string) bool {
	return format == ReviewFormatV2 || format == ReviewFormat || format == legacy.ReviewFormat
}

// NamesReviewFormat reports whether text mentions either id of the format,
// as a review still streaming does.
func NamesReviewFormat(text string) bool {
	return strings.Contains(text, ReviewFormatV2) || strings.Contains(text, ReviewFormat) || strings.Contains(text, legacy.ReviewFormat)
}

// ReviewJSON returns the agent's answer with surrounding space and one
// Markdown code fence removed, since models often wrap the report in
// ```json ... ```. A fence still streaming has no closing line yet.
func ReviewJSON(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "```") {
		return text
	}
	open, body, ok := strings.Cut(text, "\n")
	if !ok || strings.Contains(open[3:], "`") {
		return text
	}
	body = strings.TrimSpace(body)
	body = strings.TrimSuffix(body, "```")
	return strings.TrimSpace(body)
}

// ParseReview reads an agent's answer as a review report. ok is false for
// anything that is not one. head is the commit the host pinned for the
// review: a v2 report is scoped to it whatever the model wrote, since the
// model never supplies the reviewed commit. An empty or malformed head
// leaves the report's own scope.
func ParseReview(text, head string) (ReviewReport, bool) {
	var report ReviewReport
	if json.Unmarshal([]byte(ReviewJSON(text)), &report) != nil || !IsReviewFormat(report.Format) {
		return ReviewReport{}, false
	}
	report.Pin(head)
	return report, true
}

// Pin scopes a v2 report to head, the commit the host pinned. It does
// nothing for another format or a head that is not a full SHA.
func (r *ReviewReport) Pin(head string) {
	if r.Format == ReviewFormatV2 && transport.ValidSHA(head) {
		r.Scope = &ReviewScope{Commit: head}
	}
}

// ReviewReport is the agent's evidence-backed final answer. Counts are derived
// from findings, never supplied by the model. Evidence is cited, not verified
// by the formatter; the agent must obtain it from tools before making claims.
type ReviewReport struct {
	Title    string          `json:"title,omitempty"`
	Verdict  string          `json:"verdict,omitempty"`
	Scope    *ReviewScope    `json:"scope,omitempty"`
	Format   string          `json:"format"`
	Summary  string          `json:"summary"`
	Comments []string        `json:"comments,omitempty"`
	Findings []ReviewFinding `json:"findings"`
	Related  []ReviewLink    `json:"related,omitempty"`
}

// ReviewFinding couples a severity and suggestion to inspectable evidence.
type ReviewFinding struct {
	ID       string          `json:"id,omitempty"`
	Location *ReviewLocation `json:"location,omitempty"`
	Change   *string         `json:"change,omitempty"`
	Severity string          `json:"severity"`
	Title    string          `json:"title"`
	// Why says what goes wrong and why it matters, in a sentence or two.
	// Optional: a finding without it reads as its title and suggestion.
	Why        string           `json:"why,omitempty"`
	Suggestion string           `json:"suggestion"`
	Evidence   []ReviewEvidence `json:"evidence"`
	Prompt     string           `json:"prompt"`
	// FoundBy is the agent whose session found it, such as security-reviewer
	// or security-reviewer#a, when a reviewer combines specialists' findings.
	FoundBy string `json:"found_by,omitempty"`
}

// ReviewEvidence contains an observed fact and a source pointer. Source can be
// an HTTPS permalink or a concrete local location, such as a test command.
type ReviewEvidence struct {
	Step        string `json:"step,omitempty"`
	Session     string `json:"session,omitempty"`
	Observation string `json:"observation"`
	Source      string `json:"source"`
}

// ReviewLink is a related concept or reference shown after the findings.
type ReviewLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Target is where a review is shown: the repository's web address, such as
// https://github.com/acme/site, and the reviewed head. Locations link to
// the file at that head; a zero Target shows them as plain text.
type Target struct {
	Repo string
	Head string
}

// TargetOf reads a Target from a run's GitHub channel, such as
// github://acme/site/pull/7?head_sha=<sha>. Anything else is zero.
func TargetOf(channel string) Target {
	a, err := parseAddress(channel)
	if err != nil || a.Connector != "github" || a.Workspace == "" || len(a.Path) == 0 {
		return Target{}
	}
	t := Target{Repo: "https://github.com/" + url.PathEscape(a.Workspace) + "/" + url.PathEscape(a.Path[0])}
	if head, _ := a.Param("head_sha"); transport.ValidSHA(head) {
		t.Head = head
	}
	return t
}

// ValidateReview checks a report's structure and bounds. It rejects the
// wrong format, an invalid severity, or a finding that lacks evidence,
// instead of presenting it as a clean review. Reasons are written for the
// person reading the comment.
func ValidateReview(report ReviewReport) error {
	if !IsReviewFormat(report.Format) || strings.TrimSpace(report.Summary) == "" {
		return fmt.Errorf("the report needs format %q and a summary", ReviewFormatV2)
	}
	if report.Format == ReviewFormatV2 {
		if err := validateV2(report); err != nil {
			return err
		}
	}
	for i, f := range report.Findings {
		if _, ok := severities[f.Severity]; !ok {
			return fmt.Errorf("finding %d has severity %q, not error, warning or info", i+1, f.Severity)
		}
		if strings.TrimSpace(f.Title) == "" || strings.TrimSpace(f.Suggestion) == "" || strings.TrimSpace(f.Prompt) == "" || len(f.Evidence) == 0 {
			return fmt.Errorf("finding %d needs a title, a suggestion, evidence and a prompt", i+1)
		}
		for _, e := range f.Evidence {
			if strings.TrimSpace(e.Observation) == "" || (strings.TrimSpace(e.Source) == "" && (report.Format != ReviewFormatV2 || e.Step == "" && e.Session == "")) {
				return fmt.Errorf("finding %d has evidence without an observation or a source", i+1)
			}
		}
	}
	return nil
}

// FormatReview renders a complete report as Markdown, after ValidateReview.
func FormatReview(report ReviewReport) (string, error) {
	return FormatReviewAt(report, Target{})
}

// FormatReviewAt renders a complete report for a pull request at t. Every
// finding shows its change, since nothing else carries it.
func FormatReviewAt(report ReviewReport, t Target) (string, error) {
	if err := ValidateReview(report); err != nil {
		return "", err
	}
	result := renderReview(report, t, nil)
	if len(result) > MaxBodyChars-2048 {
		return "", fmt.Errorf("the review is too long for one GitHub comment; shorten the findings and keep their evidence")
	}
	return result, nil
}

// severities ranks each severity, most urgent first.
var severities = map[string]int{"error": 0, "warning": 1, "info": 2}

// severityLabel is the small label a finding carries, in words.
var severityLabel = map[string]string{"error": "Must fix", "warning": "Should fix", "info": "Note"}

// ordered is the report's findings, most urgent first, in the order the
// agent gave them within a severity.
func ordered(findings []ReviewFinding) []ReviewFinding {
	out := slices.Clone(findings)
	slices.SortStableFunc(out, func(a, b ReviewFinding) int { return severities[a.Severity] - severities[b.Severity] })
	return out
}

// renderReview lays out a validated report: the verdict in words, the
// summary, each finding as a short paragraph, a collapsed table when there
// are many, the evidence collapsed, and a hidden marker. anchored names the
// findings that have their own line comment, whose change is shown there;
// nil shows every finding's change here.
func renderReview(report ReviewReport, t Target, anchored map[string]bool) string {
	if t.Head == "" && report.Scope != nil {
		t.Head = report.Scope.Commit
	}
	findings := ordered(report.Findings)
	var b strings.Builder
	fmt.Fprintf(&b, "**%s**\n\n%s\n", verdictLine(report.Verdict, findings), proseIn(report.Summary, t.Repo))
	if len(report.Comments) > 0 {
		b.WriteString("\n")
		for _, comment := range report.Comments {
			fmt.Fprintf(&b, "- %s\n", proseIn(comment, t.Repo))
		}
	}
	for i, f := range findings {
		fmt.Fprintf(&b, "\n**%d. %s**\n<sub>%s</sub>\n\n%s\n", i+1, prose(f.Title), findingLabel(f, t), findingText(f, t.Repo))
		if f.Change != nil && !anchored[f.ID] {
			fmt.Fprintf(&b, "\n<details><summary>Suggested change</summary>\n\n%s\n\n</details>\n", fenced(*f.Change, f.Location))
		}
	}
	if len(findings) >= tableFrom {
		fmt.Fprintf(&b, "\n<details><summary>All %d findings</summary>\n\n| # | Finding | Severity | Where |\n| ---: | :--- | :--- | :--- |\n", len(findings))
		for i, f := range findings {
			where := "General"
			if f.Location != nil {
				where = "`" + locationLabel(f.Location) + "`"
			}
			fmt.Fprintf(&b, "| %d | %s | %s | %s |\n", i+1, cell(f.Title), severityLabel[f.Severity], where)
		}
		b.WriteString("\n</details>\n")
	}
	if len(findings) > 0 {
		b.WriteString("\n<details><summary>Evidence</summary>\n\n")
		for i, f := range findings {
			for _, e := range f.Evidence {
				fmt.Fprintf(&b, "- **%d.** %s%s\n", i+1, proseIn(e.Observation, t.Repo), evidenceNote(e, report.Format, t.Repo))
			}
		}
		b.WriteString("\n</details>\n")
	}
	if len(report.Related) > 0 {
		var links []string
		for _, link := range report.Related {
			links = append(links, repoLink(prose(link.Title), link.URL, t.Repo))
		}
		fmt.Fprintf(&b, "\n<sub>Related: %s</sub>\n", strings.Join(links, " · "))
	}
	fmt.Fprintf(&b, "\n%s", reviewMarker(report, findings))
	return b.String()
}

// reviewMarker is the hidden line a program reads a posted review by: its
// format, verdict and finding counts. It is an HTML comment, so a person
// never sees it.
func reviewMarker(report ReviewReport, findings []ReviewFinding) string {
	counts := map[string]int{}
	for _, f := range findings {
		counts[f.Severity]++
	}
	verdict := report.Verdict
	if verdict == "" {
		verdict = "comment"
	}
	return fmt.Sprintf("<!-- %s verdict=%s errors=%d warnings=%d info=%d -->", report.Format, verdict, counts["error"], counts["warning"], counts["info"])
}

// verdictLine says the verdict in words, with how many problems stand
// between the change and a merge.
func verdictLine(verdict string, findings []ReviewFinding) string {
	blocking, notes, errors := 0, 0, 0
	for _, f := range findings {
		switch f.Severity {
		case "info":
			notes++
		case "error":
			errors++
			blocking++
		default:
			blocking++
		}
	}
	switch verdict {
	case "approve":
		switch {
		case blocking == 1:
			return "Looks good to merge after one fix."
		case blocking > 1:
			return "Looks good to merge after " + count(blocking) + " fixes."
		case notes > 0:
			return "Looks good to merge. " + capital(plural(notes, "note")) + " below."
		}
		return "Looks good to merge."
	case "request_changes":
		switch more := blocking - errors; {
		case errors > 0 && more > 0:
			return "Needs changes: " + plural(errors, "problem") + " to fix before merging, and " + count(more) + " more worth fixing."
		case errors > 0:
			return "Needs changes: " + plural(errors, "problem") + " to fix before merging."
		case more > 0:
			return "Needs changes: " + plural(more, "problem") + " worth fixing before merging."
		}
		return "Needs changes before merging."
	}
	switch {
	case len(findings) == 0:
		return "No problems found."
	case blocking == 0:
		return capital(plural(notes, "note")) + ", nothing blocking."
	}
	return capital(plural(blocking, "problem")) + " worth fixing before merging."
}

var words = []string{"no", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}

// count is n in words up to nine, then in digits.
func count(n int) string {
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return fmt.Sprint(n)
}

func plural(n int, noun string) string {
	if n == 1 {
		return "one " + noun
	}
	return count(n) + " " + noun + "s"
}

func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// findingLabel is the small line under a finding's title: its severity,
// where it is, and the specialist who found it.
func findingLabel(f ReviewFinding, t Target) string {
	parts := []string{severityLabel[f.Severity]}
	if f.Location != nil {
		parts = append(parts, locationLink(f.Location, t))
	}
	if who := foundByName(f.FoundBy); who != "" {
		parts = append(parts, "found by "+who)
	}
	return strings.Join(parts, " · ")
}

// foundByName is the agent in found_by, without the session label after
// "#", which is an internal name.
func foundByName(s string) string {
	name, _, _ := strings.Cut(s, "#")
	return cell(name)
}

// findingText is what is wrong and what to do, as one paragraph.
func findingText(f ReviewFinding, repo string) string {
	if strings.TrimSpace(f.Why) == "" {
		return proseIn(f.Suggestion, repo)
	}
	return proseIn(f.Why, repo) + " " + proseIn(f.Suggestion, repo)
}

// tableFrom is how many findings a review needs before a table of them is
// worth having under the numbered findings, collapsed.
const tableFrom = 8

// repoLink links label to dest when dest is in repo. A model's address
// anywhere else is shown as code beside the label, so a review links to
// nothing outside the repository it reviews.
func repoLink(label, dest, repo string) string {
	if u := webURL(dest); u != "" && repo != "" && strings.HasPrefix(strings.ToLower(dest), strings.ToLower(repo)+"/") {
		return "[" + label + "](" + u + ")"
	}
	if code := codeSpan(dest); code != "" {
		return label + " " + code
	}
	return label
}

// evidenceNote is how a piece of evidence is sourced: a link, a location
// in code, or a note that the host could not verify it.
func evidenceNote(e ReviewEvidence, format, repo string) string {
	note := ""
	switch src := strings.TrimSpace(e.Source); {
	case src == "" || src == "Unverified evidence":
	case webURL(src) != "":
		note = " (" + repoLink("source", src, repo) + ")"
	default:
		note = " (" + codeSpan(src) + ")"
	}
	if format == ReviewFormatV2 && e.Step == "" && e.Session == "" {
		note += " _(not verified)_"
	}
	return note
}

// locationLabel is path:line, or path:start-end for a range.
func locationLabel(l *ReviewLocation) string {
	label := fmt.Sprintf("%s:%d", l.Path, l.Line)
	if l.StartLine > 0 && l.StartLine < l.Line {
		label = fmt.Sprintf("%s:%d-%d", l.Path, l.StartLine, l.Line)
	}
	return label
}

// locationLink links a location to its lines at the reviewed head, or
// shows it as code when there is no head, or the line is a removed one.
func locationLink(l *ReviewLocation, t Target) string {
	label := codeSpan(locationLabel(l))
	if t.Repo == "" || !transport.ValidSHA(t.Head) || l.Side == "LEFT" {
		return label
	}
	segments := strings.Split(l.Path, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	anchor := fmt.Sprintf("#L%d", l.Line)
	if l.StartLine > 0 && l.StartLine < l.Line {
		anchor = fmt.Sprintf("#L%d-L%d", l.StartLine, l.Line)
	}
	return linkTo(label, t.Repo+"/blob/"+t.Head+"/"+strings.Join(segments, "/")+anchor)
}

// fenced is a change as a code block whose fence no line of it can close.
func fenced(change string, l *ReviewLocation) string {
	fence := "```"
	for strings.Contains(change, fence) {
		fence += "`"
	}
	lang := ""
	if l != nil {
		lang = languageOf(l.Path)
	}
	return fence + lang + "\n" + strings.TrimRight(change, "\n") + "\n" + fence
}

var languages = map[string]string{".go": "go", ".ts": "ts", ".tsx": "tsx", ".js": "js", ".jsx": "jsx", ".py": "python", ".rs": "rust", ".rb": "ruby", ".java": "java", ".kt": "kotlin", ".sh": "sh", ".sql": "sql", ".yaml": "yaml", ".yml": "yaml", ".json": "json", ".md": "markdown", ".css": "css", ".html": "html", ".tf": "hcl"}

func languageOf(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return languages[strings.ToLower(path[i:])]
	}
	return ""
}

// prose keeps model text inside its own paragraph: whitespace becomes one
// space, and HTML, links, emphasis and mentions are escaped, so the text
// cannot change the layout or ping anyone. Inline `code` stays code.
func prose(s string) string { return proseIn(s, "") }

// proseIn is prose for a review of repo (https://github.com/owner/name): a
// bare address into that repository stays a link, and any other is code.
func proseIn(s, repo string) string {
	s = strings.Join(strings.Fields(s), " ")
	var b strings.Builder
	for s != "" {
		open := strings.IndexByte(s, '`')
		if open < 0 {
			b.WriteString(text(s, repo))
			break
		}
		closing := strings.IndexByte(s[open+1:], '`')
		if closing <= 0 {
			// A lone backtick, or an empty pair, is text.
			b.WriteString(text(s[:open+1], repo))
			s = s[open+1:]
			continue
		}
		b.WriteString(text(s[:open], repo))
		b.WriteString(codeSpan(s[open+1 : open+1+closing]))
		s = s[open+1+closing+1:]
	}
	return b.String()
}

// bareURL is an address GitHub would link by itself. Trailing punctuation
// is the sentence's, not the address's.
var bareURL = regexp.MustCompile(`(?i)\b(?:https?://|www\.)[^\s<>\[\]()` + "`" + `]*[^\s<>\[\]()` + "`" + `.,;:!?'"]`)

// text is model text outside code: a bare address into repo is a link,
// any other address is code so GitHub links nothing, and the rest is
// escaped.
func text(s, repo string) string {
	var b strings.Builder
	for {
		loc := bareURL.FindStringIndex(s)
		if loc == nil {
			b.WriteString(escape(s))
			return b.String()
		}
		b.WriteString(escape(s[:loc[0]]))
		u := s[loc[0]:loc[1]]
		if dest := webURL(u); repo != "" && dest != "" && strings.HasPrefix(strings.ToLower(u), strings.ToLower(repo)+"/") {
			b.WriteString("[" + escape(u) + "](" + dest + ")")
		} else {
			b.WriteString(codeSpan(u))
		}
		s = s[loc[1]:]
	}
}

// joiner is the invisible word joiner (U+2060). Put after "@" or "#" it
// stops GitHub reading a mention, a team mention or a reference to an
// issue, while the text reads the same. An HTML entity does not: GitHub
// decodes &#64;name and mentions the person.
const joiner = "\u2060"

var escaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "|", "&#124;", "\\", "&#92;", "`", "&#96;", "*", "&#42;", "_", "&#95;", "[", "&#91;", "]", "&#93;", "~", "&#126;", "@", "@"+joiner, "#", "#"+joiner, " \u2014 ", ", ", "\u2014", ", ")

// ghReference is GH-123, which GitHub links to an issue.
var ghReference = regexp.MustCompile(`(?i)\bgh-([0-9])`)

// escape keeps text as typed: HTML, links, emphasis, mentions and issue
// references do nothing, and an em dash reads as a comma.
func escape(s string) string {
	s = escaper.Replace(s)
	return ghReference.ReplaceAllStringFunc(s, func(m string) string { return m[:3] + joiner + m[3:] })
}

// cell is text for a table cell: prose without code spans, whose pipes
// would still split the cell.
func cell(s string) string { return text(strings.Join(strings.Fields(s), " "), "") }

// reviewText is kept for the PR guide: text with all markup escaped.
func reviewText(s string) string { return cell(s) }

// codeSpan wraps text in a code span that its own backticks cannot close.
// Text in a code span is shown as written, so mentions and HTML in it do
// nothing.
func codeSpan(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	fence := "`"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		s = " " + s + " "
	}
	return fence + s + fence
}

// webURL is source as a link destination when it is a plain http(s) URL
// without credentials, else "".
func webURL(source string) string {
	u, err := url.Parse(source)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return ""
	}
	return strings.NewReplacer("(", "%28", ")", "%29", "<", "%3C", ">", "%3E", "|", "%7C", " ", "%20", "\n", "%0A", "\r", "%0D").Replace(u.String())
}

// linkTo links label to dest when dest is a plain web address, else shows
// the label alone.
func linkTo(label, dest string) string {
	if u := webURL(dest); u != "" {
		return "[" + label + "](" + u + ")"
	}
	return label
}

// RenderReview renders a run snapshot as a comment body. Ordinary text uses the
// default layout. A structured review shows progress while it streams and the
// formatted report once the run is terminal; a truncated or invalid report is
// reported as incomplete, never as a review with zero findings.
func RenderReview(v transport.View) string {
	// Structured agent output stays out of the comment until it is a complete
	// report. Progress still comes from lifecycle and tool events.
	t := TargetOf(v.Channel)
	text := ReviewJSON(v.Text)
	report, structured := ParseReview(text, t.Head)
	pending := strings.HasPrefix(text, "{") && NamesReviewFormat(text)
	if !structured && !pending && (!strings.HasPrefix(text, "{") || v.Terminal()) {
		return clamp(transport.RenderDefault(v), MaxBodyChars)
	}
	v.Text = ""
	v.Thinking = ""
	if !v.Terminal() {
		v.Text = "Reviewing the change…"
		return clamp(transport.RenderDefault(v), MaxBodyChars)
	}
	if !structured {
		return "**The review is incomplete.** The agent stopped before it finished its report, so there are no findings to show. The session has what it read."
	}
	body, err := FormatReviewAt(report, t)
	if err != nil {
		return "**The review could not be shown.** " + capital(prose(strings.TrimPrefix(err.Error(), "github: "))) + ". The session has the full report."
	}
	if v.Error != "" {
		body = "**The run failed after the review was written:** " + prose(v.Error) + "\n\n" + body
	}
	return clamp(body, MaxBodyChars)
}

// MaxBodyChars is GitHub's limit on a comment body.
const MaxBodyChars = transport.MaxBodyChars

// clamp cuts s to at most n bytes without splitting a UTF-8 sequence.
func clamp(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
