package platform

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/urmzd/humagen/internal/legacy"
	github "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/runs"
)

const onlineFormatBytes = 64 << 10

var declaredReviewFormat = regexp.MustCompile(`"format"\s*:\s*"([^"\r\n]{1,80})"`)

// checkReviewFormat assesses the known structured review contract. It verifies
// shape only: diff applicability and evidence authorization belong to delivery.
// A format explicitly named by the immutable agent instructions is required;
// otherwise only an answer declaring a recognized format opts into this check.
func checkReviewFormat(instructions, answer string) (evals.Verdict, bool) {
	expected := ""
	for _, format := range []string{github.ReviewFormatV2, github.ReviewFormat, legacy.ReviewFormat} {
		if strings.Contains(instructions, format) {
			expected = format
			break
		}
	}
	text := github.ReviewJSON(answer[:min(len(answer), onlineFormatBytes)])
	declared := ""
	var head struct {
		Format string `json:"format"`
	}
	if err := json.Unmarshal([]byte(text), &head); err == nil {
		if github.IsReviewFormat(head.Format) {
			declared = head.Format
		}
	} else if match := declaredReviewFormat.FindStringSubmatch(text); len(match) == 2 && github.IsReviewFormat(match[1]) {
		declared = match[1]
	}
	if expected == "" && declared == "" {
		return evals.Verdict{}, false
	}
	v := evals.Verdict{Check: "format", Passed: false, Reason: "The answer does not match its declared review format"}
	if len(answer) > onlineFormatBytes {
		v.Reason = "The structured answer exceeds the format-check size limit"
		return v, true
	}
	var report github.ReviewReport
	if json.Unmarshal([]byte(text), &report) != nil || !github.IsReviewFormat(report.Format) || expected != "" && report.Format != expected {
		return v, true
	}
	if _, err := github.FormatReview(report); err != nil {
		return v, true
	}
	v.Passed, v.Reason = true, ""
	return v, true
}

func (p *Platform) onlineFormat(run runs.Run) (evals.Verdict, bool) {
	// The saved run's revision is immutable. Later instruction edits must not
	// change a retry's assessment or impose a format on an older answer.
	instructions := ""
	if run.Rev > 0 {
		if revision, err := p.Catalog().Get(context.Background(), run.Tenant, run.Agent, run.Rev); err == nil {
			instructions = revision.Spec.SystemPrompt
		}
	}
	return checkReviewFormat(instructions, run.Answer)
}
