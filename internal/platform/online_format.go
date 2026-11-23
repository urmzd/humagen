package platform

import (
	github "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/runs"
)

const onlineFormatBytes = 64 << 10

// checkReviewFormat assesses a run the host saved as asked for a review
// (wire.ShapeReview). It verifies shape only: diff applicability and
// evidence authorization belong to delivery. head is the commit the host
// pinned, which the review is scoped to; the model never writes it. Any
// other run, a reply among them, is not checked, whatever its answer or
// its agent's instructions say.
func checkReviewFormat(shape, head, answer string) (evals.Verdict, bool) {
	if shape != wire.ShapeReview {
		return evals.Verdict{}, false
	}
	v := evals.Verdict{Check: "format", Passed: false, Reason: "A review was asked for, and the answer is not a review report"}
	if len(answer) > onlineFormatBytes {
		v.Reason = "The review exceeds the format-check size limit"
		return v, true
	}
	report, ok := github.ParseReview(answer, head)
	if !ok {
		return v, true
	}
	if _, err := github.FormatReview(report); err != nil {
		v.Reason = "The review does not match its format: " + err.Error()
		return v, true
	}
	v.Passed, v.Reason = true, ""
	return v, true
}

// onlineFormat checks a run's answer against its final shape: the one
// saved on its tag when it started, or a review once it called
// start_review, which its record holds. A retry is assessed as the run was
// asked, whatever its agent's instructions say now.
func (p *Platform) onlineFormat(run runs.Run) (evals.Verdict, bool) {
	shape := runShape(run)
	if shape != wire.ShapeReview {
		return evals.Verdict{}, false
	}
	return checkReviewFormat(shape, p.reviewedHead(run), run.Answer)
}
